// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/datahubtesting"
	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
)

// Stage C: apply and destroy the runnable examples against a live DataHub
// instance. Design: docs/design/live-example-execution.md.
//
// What this proves that nothing else does: that the configuration a user copies
// off the repository applies, converges, and destroys as written. Stage B runs
// terraform validate, which never contacts a server, so it cannot see a Create
// writing an aspect the Read cannot parse back, a Delete that silently leaves
// the entity in place, or a server-normalised value producing a permanent diff.
// The acceptance suite asserts resource semantics thoroughly but works from its
// own configurations, so it says nothing about the published ones.
//
// Deliberately generic. No assertion below names an attribute of any resource:
// checking that a tag came back with the right description would duplicate
// TestAcc_Tag_* exactly and double the cost of every schema change. Existence is
// not content -- it is one boolean per URN that no schema change affects.

// liveExampleEnvVar gates this the way TF_ACC gates acceptance tests. A live run
// boots a DataHub instance, so it must never happen under a plain
// `go test ./...`. `make test-examples-live` sets it.
const liveExampleEnvVar = "TF_EXAMPLE_LIVE"

// liveExampleFilterEnvVar narrows a run to named directories, space or comma
// separated, for local iteration. An override, never the source of truth: the
// run list lives in example_live_classification_test.go so it has a review
// surface and a completeness check.
const liveExampleFilterEnvVar = "EXAMPLES"

// settleAfterDestroyPause is how long the harness waits before checking absence
// for a configuration that deleted structured properties.
//
// The provider already waits for a zero-count streak before issuing such a
// delete (settleStructuredPropertyAssignments), and the code says plainly that
// it cannot close the window by construction, because the side effect runs
// asynchronously against a replicated index. This pause is not a fix for that
// and must not grow into one: if a husk appears it is a real failure, and the
// end-of-run sweep is what catches a resurrection arriving later still.
const settleAfterDestroyPause = 15 * time.Second

// cleanupDestroyTimeout bounds the teardown that runs after a test has already
// finished or failed. Generous, because this is the last chance to leave the
// instance clean, but bounded so a hung destroy cannot hold the whole run open.
const cleanupDestroyTimeout = 15 * time.Minute

// waitForURNBudget and waitForURNInterval bound the preflight poll an example
// can ask for via waitForURN.
//
// The budget is deliberately generous against what it is waiting for. DataHub's
// asynchronous bootstrap templates are ingested through the same MCP pipeline as
// any other write, so the wait is one round trip through Kafka and the ingestion
// consumer rather than a fixed startup step -- and a contended runner stretches
// that. Ninety seconds is long enough that exhausting it says something real
// happened, and short enough that a genuinely absent entity does not cost the
// run its timeout.
const (
	waitForURNBudget   = 90 * time.Second
	waitForURNInterval = 2 * time.Second
)

// urnAttribute maps a resource type to the state attribute holding its URN,
// where that is not the conventional "urn".
//
// datahub_local_user_login is the odd one out: it manages a corpUser's native
// credentials, and its URN lives in user_urn because "urn" would read as the
// login's own identity rather than the user's.
var urnAttribute = map[string]string{
	"datahub_local_user_login": "user_urn",
}

// urnTemplate derives a URN from the resource's id for types that expose no URN
// attribute at all.
//
// datahub_ingestion_source is the only entity resource in the provider missing
// one, which is a gap worth closing on the resource rather than here; until then
// this keeps the harvest complete instead of silently skipping it.
var urnTemplate = map[string]string{
	"datahub_ingestion_source": "urn:li:dataHubIngestionSource:%s",
}

// urnlessLiveResources are resource types with no entity of their own. Their
// destroy removes an aspect edge, so asserting entity absence would be
// asserting the wrong thing -- these are correctly skipped, not overlooked.
var urnlessLiveResources = map[string]bool{
	"datahub_corp_group_member":              true,
	"datahub_role_assignment":                true,
	"datahub_structured_property_assignment": true,
}

// harvestedURN is one managed resource's identity, carried from before the
// destroy (where state still exists) to after it.
type harvestedURN struct {
	address      string // e.g. datahub_domain.finance
	resourceType string // e.g. datahub_domain
	urn          string
}

// teardownOutcome records what the registered cleanup destroy did, so the
// end-of-run sweep can say what a late reappearance means.
//
// Written by the cleanup closure and read after the subtest has finished, hence
// the pointer: a non-parallel t.Run returns only once its cleanups have run, so
// by sweep time these fields are settled.
type teardownOutcome struct {
	// fired is true when the cleanup destroy actually ran, which happens only when
	// a configuration was still standing at the end of the subtest.
	fired bool
	// errored is true when it ran and still failed after its one retry.
	errored bool
}

// liveExampleRun is one example's contribution to the sweep: the URNs it proved
// absent, and what its teardown did.
type liveExampleRun struct {
	dir      string
	absent   []harvestedURN
	teardown *teardownOutcome
}

// TestLiveExamples applies each classified example, asserts against it, and
// destroys it.
//
// Serial by construction, and not only because of the destroy blast radius:
// several examples read instance-wide plural data sources, whose results include
// everything on the instance. Two configurations running concurrently appear in
// each other's outputs regardless of what their own entities are called, so
// renaming cannot decouple them.
func TestLiveExamples(t *testing.T) {
	env := setupLiveExamples(t)

	var runs []liveExampleRun

	for _, ex := range selectLiveExamples(t) {
		// Not t.Parallel: see the comment above.
		t.Run(ex.dir, func(t *testing.T) {
			urns, teardown := runLiveExample(t, env, ex)
			runs = append(runs, liveExampleRun{dir: ex.dir, absent: urns, teardown: teardown})
		})
	}

	// The end-of-run sweep. This is the only assertion that catches a late
	// CAT-2583 resurrection: the side effect is asynchronous, so an entity that
	// was provably absent when its own example finished can reappear while a
	// later example runs. Reported separately from the per-example checks
	// because the two have different causes and different fixes -- a URN failing
	// only here means the delete worked and something put it back.
	t.Run("end-of-run-sweep", func(t *testing.T) {
		var total int
		for _, run := range runs {
			total += len(run.absent)
		}
		if total == 0 {
			t.Skip("no URNs harvested; nothing to sweep")
		}
		for _, run := range runs {
			for _, h := range run.absent {
				present, err := datahubtesting.URNPresent(context.Background(), env.client, h.urn)
				if err != nil {
					t.Errorf("sweep: probing %s (%s): %v", h.urn, h.address, err)
					continue
				}
				if present {
					reportLateReappearance(t, run, h)
				}
			}
		}
		t.Logf("swept %d URNs from %d examples", total, len(runs))
	})
}

// reportLateReappearance renders the sweep's verdict on one URN that is back.
//
// The wording has to depend on the example's teardown, because since the
// re-apply check became the default the sweep no longer has one explanation. Its
// original message blamed an asynchronous CAT-2583 resurrection, which was sound
// while a URN reaching the sweep had been destroyed once and left alone. A
// re-applied example destroys twice and hands the second teardown to the
// cleanup, and a cleanup destroy that failed is a far likelier explanation for a
// URN being present than a server side effect reaching across examples.
//
// Getting this wrong is expensive in a specific way: it sends a maintainer to an
// upstream bug for a failure whose cause is one scroll up in the same log.
func reportLateReappearance(t *testing.T, run liveExampleRun, h harvestedURN) {
	t.Helper()

	switch {
	case run.teardown == nil || !run.teardown.fired:
		// The example tore itself down through its own asserted destroys and the
		// safety net never ran. Nothing in the harness touched this URN after it
		// was proved absent, so a resurrection is the explanation.
		t.Errorf("sweep: %s (%s) is present again at the end of the run, having "+
			"passed its own post-destroy check. The destroy worked and something "+
			"put it back -- the CAT-2583 side effect is asynchronous and can land "+
			"after a later example has started.", h.urn, h.address)

	case run.teardown.errored:
		// Already reported, with the terraform output, by the subtest that owns it.
		// A second Errorf here would double-count one failure and invite a hunt for
		// an upstream bug that is not involved.
		t.Logf("sweep: %s (%s) is present, and [%s] reported its cleanup destroy failing. "+
			"This is debris from that failure rather than a separate finding -- triage "+
			"the destroy, not the sweep.", h.urn, h.address, run.dir)

	default:
		// The cleanup ran and reported success, yet the URN is back. Either the
		// destroy did not remove what it claimed, or a resurrection landed after it.
		// Nothing here can separate the two, and guessing would be worse than saying
		// so.
		t.Errorf("sweep: %s (%s) is present again at the end of the run. [%s] ran a cleanup "+
			"destroy that reported success, so this is UNDETERMINED: either that destroy "+
			"left the entity behind while exiting 0, or a CAT-2583 resurrection landed "+
			"after it. Check whether the entity carries content aspects -- a husk points "+
			"at the resurrection, a populated entity at the destroy.", h.urn, h.address, run.dir)
	}
}

// runLiveExample drives one example through apply, assertions and destroy,
// returning the URNs it proved absent so the end-of-run sweep can re-check them,
// and what its teardown did so the sweep can interpret a reappearance.
func runLiveExample(t *testing.T, env liveExampleEnv, ex liveExample) ([]harvestedURN, *teardownOutcome) {
	t.Helper()

	// Before anything is copied or applied: wait for the entity this example
	// reads but does not create. An example that looks one up would otherwise
	// fail its apply for a reason that has nothing to do with the provider.
	awaitPreflightURN(t, env, ex)

	dir := t.TempDir()
	copyExampleDir(t, filepath.Join(env.examplesDir, "runnable", ex.dir), dir)

	vars := liveVars(t, ex)
	phases := ex.phases
	if len(phases) == 0 {
		phases = []map[string]string{nil}
	}

	// applied tracks whether a configuration is currently standing. Both explicit
	// destroys below clear it, and the cleanup returns immediately when it is
	// false.
	//
	// The flag is what lets the safety net stay registered without running a third
	// destroy over state Terraform has already emptied. Such a destroy is not
	// harmless noise: it is a terraform invocation per example whose "No changes"
	// result proves nothing (see the rejected repeat-destroy assertion in the
	// design), and a failure in it would report against an example that had
	// already torn itself down correctly.
	applied := false

	// Read by the end-of-run sweep, after this subtest and its cleanups have
	// finished, to tell debris from a failed teardown apart from a genuine late
	// resurrection.
	teardown := &teardownOutcome{}

	// Registered before the first apply, so a destroy runs even when an
	// assertion below fails, the harness panics, or the test times out. This is
	// the difference between one failed example and an instance full of debris.
	t.Cleanup(func() {
		if !applied {
			return
		}
		teardown.fired = true

		// A fresh context, NOT t.Context(). The testing package cancels a test's
		// context just before its Cleanup functions run, so using it here means
		// every teardown fails instantly with "context canceled" and leaves the
		// instance dirty -- which is what the first live run of this harness did.
		ctx, cancel := context.WithTimeout(context.Background(), cleanupDestroyTimeout)
		defer cancel()

		out, err := env.terraformIn(ctx, t, dir, vars, destroyArgs(ex.serialDestroy)...)
		if err == nil {
			return
		}
		t.Logf("[%s] cleanup destroy failed, retrying once after a settle: %v\n%s", ex.dir, err, out)
		time.Sleep(settleAfterDestroyPause)
		// One retry only. The provider already retries the errors it can
		// recognise; more than one here masks a bug rather than tolerating a
		// race. -parallelism=1 for the nested-domain case removes the
		// concurrency that widens the guard's stale-index window.
		if out2, err2 := env.terraformIn(ctx, t, dir, vars, destroyArgs(ex.serialDestroy)...); err2 != nil {
			teardown.errored = true
			t.Errorf("[%s] destroy failed twice; the instance holds debris from this "+
				"example. First error: %v\nRetry output:\n%s", ex.dir, err, out2)
		}
	})

	for i, phaseVars := range phases {
		merged := mergeVars(vars, phaseVars)
		label := phaseLabel(i, len(phases))

		start := time.Now()
		if out, err := env.terraformIn(t.Context(), t, dir, merged, "apply", "-auto-approve", "-input=false", "-no-color"); err != nil {
			// applied is set BEFORE the error is reported, because a failed apply
			// still creates whatever it managed before failing. Leaving the flag
			// false here would tell the cleanup there is nothing to tear down.
			applied = true
			t.Fatalf("[%s] apply%s failed: %v\n%s", ex.dir, label, err, out)
		}
		applied = true
		t.Logf("[%s] apply%s took %s", ex.dir, label, time.Since(start).Round(time.Second))

		// Assertion 1: no resource changes proposed after apply.
		assertPlanClean(t, env, dir, merged, ex.dir+label)
	}

	// Assertion 3a: every managed URN exists on the server. Harvested once and
	// reused for the post-destroy check, so this costs one GET per resource.
	harvested := harvestURNs(t, env, dir, vars, ex.dir)
	for _, h := range harvested {
		datahubtesting.AssertURNPresent(t, env.client, h.resourceType, h.urn)
	}

	// Assertion 4: outputs exist and none is null. Enforces the house rule that
	// every runnable example exposes something the reader can act on.
	assertOutputsUsable(t, env, dir, vars, ex.dir)

	// Destroy now, explicitly, so the assertions below run against its result.
	// The t.Cleanup above stays registered and becomes a no-op on success.
	if out, err := env.terraformIn(t.Context(), t, dir, vars, destroyArgs(ex.serialDestroy)...); err != nil {
		t.Fatalf("[%s] destroy failed: %v\n%s", ex.dir, err, out)
	}
	applied = false

	remaining := assertDestroyLeftNothing(t, env, ex, harvested, "first destroy", true)

	// Assertion 5: prove re-creation is not blocked, then destroy again and assert
	// that too. A husk is invisible in the UI and carries no content aspects; what
	// it actually does is refuse the next apply with "already exists". Testing that
	// consequence beats inferring it from an aspect probe.
	//
	// On by default. The opt-out is a reason string in the table, not a bool, so
	// the skip list can be audited -- see noReapplyReason.
	if reason := reapplySkipReason(ex, harvested); reason != "" {
		t.Logf("[%s] not re-applying after destroy: %s", ex.dir, reason)
	} else {
		start := time.Now()
		// Every phase, not just the last one. A phased example is phased because
		// the first apply is a precondition for the second -- data-product-simple
		// turns provider defaults on only once the property they reference exists
		// -- so replaying the final phase alone would apply a configuration whose
		// precondition was never established, and report the resulting failure as
		// a blocked re-creation.
		reapplied := true
		for i, phaseVars := range phases {
			label := phaseLabel(i, len(phases))
			if out, err := env.terraformIn(t.Context(), t, dir, mergeVars(vars, phaseVars), "apply", "-auto-approve", "-input=false", "-no-color"); err != nil {
				// Two known server behaviours produce this symptom and they need
				// different responses, so name both rather than the first one found.
				// A CAT-2583 husk is a bug to escalate; a burned structured-property
				// field is confirmed-intended, and the answer there is a
				// noReapplyReason entry citing the issue.
				t.Errorf("[%s] re-apply%s after destroy failed, so destroy left something "+
					"blocking re-creation of a URN this harness proved absent. Two known "+
					"causes: a CAT-2583 husk (entity gone from the UI, still blocking its own "+
					"URN), or an Elasticsearch field burned by a structured-property "+
					"assignment (datahub-project/datahub#18974) -- the error text below "+
					"distinguishes them. %v\n%s",
					ex.dir, label, err, out)
				applied = true
				reapplied = false
				break
			}
			applied = true
		}
		if !reapplied {
			return remaining, teardown
		}
		t.Logf("[%s] re-apply after destroy succeeded in %s", ex.dir, time.Since(start).Round(time.Second))

		// Re-harvest rather than reuse the first harvest, and require the two to
		// agree. Reuse would be cheaper and would quietly check the wrong URNs: a
		// re-created entity landing on a different URN leaves the new one behind
		// with nothing looking for it, while the absence check dutifully confirms
		// the old one is gone. A mismatch is itself worth failing on -- it means
		// URN derivation is not deterministic across re-creation, which is a
		// property every import, every data source lookup and every user's second
		// apply depends on.
		reharvested := harvestURNs(t, env, dir, vars, ex.dir+" (re-apply)")
		assertHarvestsMatch(t, ex.dir, harvested, reharvested)

		// Second-cycle input: the URNs the first destroy proved absent, taken from
		// the re-harvest, plus anything the re-apply produced that the first apply
		// did not. Skipped are the ones already reported as survivors and the
		// mustSurvive addresses -- the first because a survivor belongs to the
		// cycle that found it, the second because this function's return value also
		// feeds the end-of-run sweep, where an entity destroy is supposed to RESTORE
		// would read as a late resurrection. The cost is that the restore assertion
		// runs once per example rather than twice.
		absentAfterFirst := make(map[string]bool, len(remaining))
		for _, h := range remaining {
			absentAfterFirst[h.urn] = true
		}
		firstSeen := make(map[string]bool, len(harvested))
		for _, h := range harvested {
			firstSeen[h.urn] = true
		}
		secondCycle := make([]harvestedURN, 0, len(reharvested))
		for _, h := range reharvested {
			if firstSeen[h.urn] && !absentAfterFirst[h.urn] {
				continue
			}
			secondCycle = append(secondCycle, h)
		}

		// The second destroy, explicit and asserted, mirroring the first. Leaving
		// it to the registered cleanup would have the re-apply prove only that
		// creation is unblocked, while the teardown of what it created went
		// unchecked -- and the teardown is the half that has actually gone wrong
		// here before.
		//
		// t.Errorf and return rather than t.Fatalf: Fatalf abandons the function,
		// so the URNs this example created would never reach the end-of-run sweep
		// and the run's final picture would be missing exactly the example that
		// failed. The registered cleanup still fires, so the debris is still
		// pursued.
		if out, err := env.terraformIn(t.Context(), t, dir, vars, destroyArgs(ex.serialDestroy)...); err != nil {
			t.Errorf("[%s] second destroy failed after a successful re-apply. Destroy worked "+
				"the first time on the same configuration, so this is a delete path that "+
				"cannot remove a re-created entity: %v\n%s", ex.dir, err, out)
			return remaining, teardown
		}
		applied = false

		remaining = assertDestroyLeftNothing(t, env, ex, secondCycle, "second destroy", false)
	}

	return remaining, teardown
}

// awaitPreflightURN blocks until the entity named by ex.waitForURN exists, and
// FAILS the example when it never does. A no-op for an example that names none.
//
// Polling rather than assuming, because DataHub's bootstrap is not one thing.
// Most of the entities the examples lean on -- the root user, the built-in
// roles, the ownership types, the page templates -- are declared blocking and
// synchronous in bootstrap_mcps.yaml, so GMS is not healthy until they are
// written and `datahub docker check` passing means they are there. The ingestion
// recipes are not: ingestion-datahub-gc carries no overrides at all and so takes
// the file's defaults, blocking: false and async: true. A Quickstart that has
// just satisfied the health poll can therefore be missing it for a few seconds.
//
// Failing rather than skipping is the deliberate half. That template is
// optional: false upstream, so its permanent absence is a defect in the server
// or in this example's premise, and a skip would convert that finding into a
// green run with one fewer example in it -- the exact shape of coverage loss the
// classification test exists to prevent.
func awaitPreflightURN(t *testing.T, env liveExampleEnv, ex liveExample) {
	t.Helper()

	if ex.waitForURN == "" {
		return
	}

	start := time.Now()
	deadline := start.Add(waitForURNBudget)
	var lastErr error
	for {
		present, err := datahubtesting.URNPresent(t.Context(), env.client, ex.waitForURN)
		switch {
		case err != nil:
			// Not fatal on its own: a probe can fail while the instance is still
			// settling. Kept so the timeout message can say what went wrong rather
			// than reporting a bare absence.
			lastErr = err
		case present:
			t.Logf("[%s] preflight: %s present after %s", ex.dir, ex.waitForURN, time.Since(start).Round(time.Second))
			return
		default:
			lastErr = nil
		}

		if time.Now().After(deadline) {
			t.Fatalf("[%s] preflight: %s did not appear within %s. This example reads an "+
				"entity it does not create, and that entity is written by an ASYNCHRONOUS "+
				"bootstrap template (bootstrap_mcps.yaml declares ingestion recipes with "+
				"blocking: false and async: true), so it can lag a healthy GMS. Exhausting "+
				"the %s budget means something more than lag: the template is optional: "+
				"false upstream, so permanent absence is a real defect rather than a reason "+
				"to skip. Last probe error: %v",
				ex.dir, ex.waitForURN, waitForURNBudget, waitForURNBudget, lastErr)
		}
		time.Sleep(waitForURNInterval)
	}
}

// reapplySkipReason returns why the re-apply cycle does not run for ex, or the
// empty string to run it. Default-on: only a tabled reason or an empty harvest
// suppresses it.
//
// The empty-harvest case is DERIVED rather than tabled on purpose. An example
// that manages no entity -- provider-install-verification reads data.datahub_me
// and creates nothing, ingestion-source-lookup only looks one up -- has no URN
// whose re-creation could be blocked, so there is nothing for the check to prove.
// Tabling that would be a second opt-out list which says only what the harness
// can already see, and which would go stale the moment such an example grew a
// managed resource: the entry would keep suppressing a check that had become
// meaningful.
func reapplySkipReason(ex liveExample, harvested []harvestedURN) string {
	if ex.noReapplyReason != "" {
		return ex.noReapplyReason
	}
	if len(harvested) == 0 {
		return "the URN harvest is empty, so this configuration manages no entity whose " +
			"re-creation could be blocked (derived, not tabled)"
	}
	return ""
}

// assertHarvestsMatch fails when re-applying the same configuration produced a
// different set of address-to-URN pairs from the first apply.
//
// Both directions matter and they fail for different reasons. A pair present the
// first time and absent the second means the re-apply created something else
// under that address, so the entity the harness is about to assert the absence of
// is not the entity that now exists. A pair present only the second time is
// unaccounted-for debris: nothing harvested it before the first destroy, so no
// absence check and no sweep entry covers it.
//
// Compared as pairs rather than as two URN sets because an address whose URN
// changed and a URN that moved to another address are different defects, and the
// pair naming both is what makes the report actionable.
func assertHarvestsMatch(t *testing.T, label string, first, second []harvestedURN) {
	t.Helper()

	if problem := describeHarvestMismatch(first, second); problem != "" {
		t.Errorf("[%s] %s", label, problem)
	}
}

// describeHarvestMismatch returns the empty string when the two harvests agree,
// and otherwise the report. Pure, and separate from the assertion above, for the
// reason the message builders in datahubtesting are: a message reachable only by
// failing a test is a message no test can read, and a comparison that can only be
// observed passing is indistinguishable from one that always passes.
func describeHarvestMismatch(first, second []harvestedURN) string {
	index := func(hs []harvestedURN) map[string]bool {
		m := make(map[string]bool, len(hs))
		for _, h := range hs {
			m[h.address+" -> "+h.urn] = true
		}
		return m
	}
	firstSet, secondSet := index(first), index(second)

	var lost, gained []string
	for pair := range firstSet {
		if !secondSet[pair] {
			lost = append(lost, pair)
		}
	}
	for pair := range secondSet {
		if !firstSet[pair] {
			gained = append(gained, pair)
		}
	}
	sort.Strings(lost)
	sort.Strings(gained)

	if len(lost) == 0 && len(gained) == 0 {
		return ""
	}
	return fmt.Sprintf("the re-applied configuration harvested a different set of URNs than the "+
		"first apply, so URN derivation is not deterministic across re-creation. Only present "+
		"the first time:\n  %s\nOnly present after the re-apply (debris no absence check and "+
		"no sweep entry covers):\n  %s",
		strings.Join(lost, "\n  "), strings.Join(gained, "\n  "))
}

// TestDescribeHarvestMismatch exercises both directions, which is the whole point
// of the message builder being pure: the live harness only ever reaches this
// comparison against a well-behaved provider, so a version that returned "no
// mismatch" unconditionally would pass every live run there has ever been.
func TestDescribeHarvestMismatch(t *testing.T) {
	t.Parallel()

	a := harvestedURN{address: "datahub_domain.finance", resourceType: "datahub_domain", urn: "urn:li:domain:finance"}
	b := harvestedURN{address: "datahub_tag.pii", resourceType: "datahub_tag", urn: "urn:li:tag:pii"}
	// Same address, different URN: the shape a non-deterministic key produces.
	aMoved := harvestedURN{address: a.address, resourceType: a.resourceType, urn: "urn:li:domain:finance-9f3c"}

	t.Run("identical harvests agree", func(t *testing.T) {
		t.Parallel()
		if problem := describeHarvestMismatch([]harvestedURN{a, b}, []harvestedURN{b, a}); problem != "" {
			t.Errorf("order should not matter, got: %s", problem)
		}
	})

	t.Run("a URN that changed is reported in both directions", func(t *testing.T) {
		t.Parallel()
		problem := describeHarvestMismatch([]harvestedURN{a, b}, []harvestedURN{aMoved, b})
		if problem == "" {
			t.Fatal("no mismatch reported for an address whose URN changed between applies")
		}
		for _, want := range []string{a.urn, aMoved.urn, a.address} {
			if !strings.Contains(problem, want) {
				t.Errorf("message does not name %q: %s", want, problem)
			}
		}
	})

	t.Run("a URN only the re-apply created is reported", func(t *testing.T) {
		t.Parallel()
		problem := describeHarvestMismatch([]harvestedURN{a}, []harvestedURN{a, b})
		if problem == "" {
			t.Fatal("no mismatch reported for a resource that only the second apply created")
		}
		if !strings.Contains(problem, b.urn) {
			t.Errorf("message does not name the unaccounted-for URN %q: %s", b.urn, problem)
		}
	})

	t.Run("an empty harvest against a populated one is a mismatch", func(t *testing.T) {
		t.Parallel()
		if problem := describeHarvestMismatch([]harvestedURN{a}, nil); problem == "" {
			t.Fatal("no mismatch reported for a re-apply that harvested nothing at all")
		}
	})
}

// assertDestroyLeftNothing runs assertion 3b for one destroy cycle: absence for
// every URN the example created, inverted for the addresses listed in
// mustSurvive, plus the stale-expectation guard on the table itself.
//
// Extracted because the harness destroys twice and the second destroy deserves
// the same scrutiny as the first. It is the destroy that follows a re-creation,
// so it is the only one positioned to show a delete path that works on a
// freshly created entity and not on a re-created one.
//
// cycle labels every message with which destroy it belongs to; without it a
// failure report gives a maintainer no way to tell the two apart.
//
// Returns the URNs it proved absent, and the caller feeds only those into the
// next cycle. That is what keeps one survivor to one report: the cycle that
// first saw it survive names it, and neither the later cycle nor the end-of-run
// sweep repeats the accusation over a URN already known to be there.
func assertDestroyLeftNothing(t *testing.T, env liveExampleEnv, ex liveExample, harvested []harvestedURN, cycle string, checkStaleMustSurvive bool) []harvestedURN {
	t.Helper()

	// Deliberately symmetric with the first cycle. The pause exists so the
	// absence check does not race the asynchronous CAT-2583 side effect, and that
	// side effect does not care which destroy fired it -- skipping the settle on
	// the second cycle to save fifteen seconds would buy false failures. It costs
	// nothing today: every example that sets settleAfterDestroy also opts out of
	// the re-apply, so no example currently reaches this branch twice.
	if ex.settleAfterDestroy {
		t.Logf("[%s] %s: settling %s before the absence check (CAT-2583)", ex.dir, cycle, settleAfterDestroyPause)
		time.Sleep(settleAfterDestroyPause)
	}

	survive := make(map[string]bool, len(ex.mustSurvive))
	for _, addr := range ex.mustSurvive {
		survive[addr] = true
	}
	seen := make(map[string]bool, len(ex.mustSurvive))
	absent := make([]harvestedURN, 0, len(harvested))

	for _, h := range harvested {
		if survive[h.address] {
			seen[h.address] = true
			// Inverted deliberately. home-page-layout adopts the instance's
			// default home-page template, so destroy restores the layout it
			// replaced rather than deleting a template the organisation depends
			// on. Absence here would mean the restore did not happen.
			//
			// Its own message rather than AssertURNPresent's: that one is written
			// for the after-apply case and blames Create, which would send a
			// maintainer looking in entirely the wrong place.
			present, err := datahubtesting.URNPresent(context.Background(), env.client, h.urn)
			switch {
			case err != nil:
				t.Errorf("[%s] %s: %s (%s) is expected to survive destroy, but presence "+
					"could not be established: %v", ex.dir, cycle, h.address, h.urn, err)
			case !present:
				t.Errorf("[%s] %s: %s (%s) was deleted by destroy, but this example adopts an "+
					"entity it did not create, so destroy is supposed to RESTORE it. The "+
					"instance has been left without it.", ex.dir, cycle, h.address, h.urn)
			}
			continue
		}
		if problem := datahubtesting.CheckURNAbsent(context.Background(), env.client, h.resourceType, h.urn); problem != "" {
			t.Errorf("[%s] %s: %s", ex.dir, cycle, problem)
			continue
		}
		absent = append(absent, h)
	}

	// A mustSurvive address that matched nothing is a stale expectation, and it
	// would otherwise pass silently while asserting nothing at all.
	//
	// First cycle only. This is table hygiene rather than a check on the server,
	// and its answer cannot change between two destroys of the same state -- so
	// running it twice would present one wrong table entry as two failures.
	if checkStaleMustSurvive {
		for _, addr := range ex.mustSurvive {
			if !seen[addr] {
				t.Errorf("[%s] mustSurvive names %q, which is not a managed resource in the "+
					"harvested state; the restore assertion checked nothing", ex.dir, addr)
			}
		}
	}

	return absent
}

// assertPlanClean fails when terraform plans a change to any resource after a
// successful apply, which is the drift and normalisation class: a server that
// rewrites a value, a Read that maps an aspect back differently from how Create
// wrote it, a Computed attribute the provider forgets to set.
//
// -detailed-exitcode is the cheap first pass, but exit 2 is not the verdict.
// Several examples read plural data sources backed by OpenSearch; between the
// apply and the plan the index catches up, the list grows, and the output value
// changes. That is not a provider defect, so the verdict comes from
// resource_changes in the plan JSON and output-only churn is a log line.
func assertPlanClean(t *testing.T, env liveExampleEnv, dir string, vars map[string]string, label string) {
	t.Helper()

	_, err := env.terraformIn(t.Context(), t, dir, vars, "plan", "-detailed-exitcode", "-input=false", "-no-color")
	if err == nil {
		return
	}

	planFile := filepath.Join(dir, "tfplan.live")
	if out, perr := env.terraformIn(t.Context(), t, dir, vars, "plan", "-out=tfplan.live", "-input=false", "-no-color"); perr != nil {
		t.Errorf("[%s] plan after apply failed: %v\n%s", label, perr, out)
		return
	}
	showOut, serr := env.terraformIn(t.Context(), t, dir, vars, "show", "-json", "tfplan.live")
	_ = os.Remove(planFile)
	if serr != nil {
		t.Errorf("[%s] terraform show -json on the saved plan failed: %v", label, serr)
		return
	}

	var plan struct {
		ResourceChanges []struct {
			Address string `json:"address"`
			Mode    string `json:"mode"`
			Change  struct {
				Actions []string `json:"actions"`
			} `json:"change"`
		} `json:"resource_changes"`
	}
	if jerr := json.Unmarshal([]byte(showOut), &plan); jerr != nil {
		t.Errorf("[%s] parsing plan JSON: %v", label, jerr)
		return
	}

	var changed []string
	for _, rc := range plan.ResourceChanges {
		// Managed resources only, for the same reason harvestURNs filters the
		// same way. Terraform lists data sources in resource_changes too, and a
		// read it defers to apply carries actions ["read"] -- not "no-op", so it
		// would be reported here as though the published example failed to
		// converge, naming a data source as the culprit.
		if rc.Mode != "managed" {
			continue
		}
		for _, a := range rc.Change.Actions {
			if a != "no-op" {
				changed = append(changed, fmt.Sprintf("%s: %s", rc.Address, strings.Join(rc.Change.Actions, ",")))
				break
			}
		}
	}
	if len(changed) == 0 {
		t.Logf("[%s] plan is non-empty but proposes no resource changes; output-only "+
			"churn, expected for index-lagged plural data sources", label)
		return
	}
	t.Errorf("[%s] plan after apply proposes %d resource change(s), so applying the "+
		"published example twice is not a no-op:\n  %s", label, len(changed),
		strings.Join(changed, "\n  "))
}

// harvestURNs reads the URN of every managed resource out of terraform state.
//
// mode == "managed" is what keeps the check honest: it excludes data-source
// URNs, which in some examples resolve to entities the harness must neither
// delete nor expect to disappear (local-iam reads urn:li:corpuser:datahub, the
// account it authenticates as).
func harvestURNs(t *testing.T, env liveExampleEnv, dir string, vars map[string]string, label string) []harvestedURN {
	t.Helper()

	out, err := env.terraformIn(t.Context(), t, dir, vars, "show", "-json")
	if err != nil {
		t.Fatalf("[%s] terraform show -json failed: %v", label, err)
	}

	var state struct {
		Values struct {
			RootModule struct {
				Resources []struct {
					Address string         `json:"address"`
					Mode    string         `json:"mode"`
					Type    string         `json:"type"`
					Values  map[string]any `json:"values"`
				} `json:"resources"`
				ChildModules []json.RawMessage `json:"child_modules"`
			} `json:"root_module"`
		} `json:"values"`
	}
	if err := json.Unmarshal([]byte(out), &state); err != nil {
		t.Fatalf("[%s] parsing state JSON: %v", label, err)
	}

	// No runnable example uses modules today. If one starts, its resources would
	// be invisible to this harvest and the absence check would silently cover
	// less than it claims -- so fail rather than skip.
	if len(state.Values.RootModule.ChildModules) > 0 {
		t.Errorf("[%s] state has child modules, which this harvest does not walk; "+
			"resources inside them would be checked by nothing", label)
	}

	var harvested []harvestedURN
	for _, r := range state.Values.RootModule.Resources {
		if r.Mode != "managed" {
			continue
		}
		if urnlessLiveResources[r.Type] {
			continue
		}

		attr := "urn"
		if a, ok := urnAttribute[r.Type]; ok {
			attr = a
		}

		urn, _ := r.Values[attr].(string)
		if urn == "" {
			if tmpl, ok := urnTemplate[r.Type]; ok {
				if id, _ := r.Values["id"].(string); id != "" {
					urn = fmt.Sprintf(tmpl, id)
				}
			}
		}
		if urn == "" {
			t.Errorf("[%s] %s exposes no URN: attribute %q is empty and no template is "+
				"registered for type %q. Either the resource is missing a computed urn "+
				"attribute, or it belongs in urnlessLiveResources.", label, r.Address, attr, r.Type)
			continue
		}
		harvested = append(harvested, harvestedURN{address: r.Address, resourceType: r.Type, urn: urn})
	}

	t.Logf("[%s] harvested %d managed URNs", label, len(harvested))
	return harvested
}

// assertOutputsUsable enforces the house rule that a runnable example exposes
// something the reader can verify or act on. Shape is deliberately not asserted:
// several outputs derive from index-lagged plural data sources, so anything
// stronger than non-null would be flaky for reasons that are not defects.
func assertOutputsUsable(t *testing.T, env liveExampleEnv, dir string, vars map[string]string, label string) {
	t.Helper()

	out, err := env.terraformIn(t.Context(), t, dir, vars, "output", "-json")
	if err != nil {
		t.Errorf("[%s] terraform output -json failed: %v", label, err)
		return
	}

	var outputs map[string]struct {
		Value any `json:"value"`
	}
	if jerr := json.Unmarshal([]byte(out), &outputs); jerr != nil {
		t.Errorf("[%s] parsing outputs JSON: %v", label, jerr)
		return
	}
	if len(outputs) == 0 {
		t.Errorf("[%s] declares no outputs; every runnable example must expose "+
			"something the reader can verify or act on after apply", label)
		return
	}
	for name, o := range outputs {
		if o.Value == nil {
			t.Errorf("[%s] output %q is null after a successful apply", label, name)
		}
	}
}

// liveVars fills the per-run values the table leaves blank.
//
// Two of the three are randomised for one reason: datahub_local_user_login is
// subject to the OSS signUp guard, which rejects the request whenever the user
// entity exists AT ALL, not merely when it already has credentials. A fixed
// address is therefore poisoned permanently by a single failed destroy, so
// test_user_email (home-page-layout) and new_member_email (local-iam) each get a
// fresh one per run. Note that the examples' published defaults are fine as
// published -- a reader applies once against their own instance -- so this is a
// harness concern and belongs here rather than in the .tf files.
//
// test_user_password exists for an unrelated reason: the attribute is WriteOnly
// and so has no default anywhere. Generating it here keeps it out of state, out
// of the plan file and out of the repository.
func liveVars(t *testing.T, ex liveExample) map[string]string {
	t.Helper()
	if len(ex.vars) == 0 {
		return nil
	}

	vars := make(map[string]string, len(ex.vars))
	for k, v := range ex.vars {
		if v != "" {
			vars[k] = v
			continue
		}
		switch k {
		case "test_user_email":
			vars[k] = fmt.Sprintf("tf-example-live-%s@example.invalid", randomSuffix(t))
		case "new_member_email":
			vars[k] = fmt.Sprintf("tf-example-live-iam-%s@example.invalid", randomSuffix(t))
		case "test_user_password":
			vars[k] = "TFLive-" + randomSuffix(t) + "-" + randomSuffix(t)
		default:
			t.Fatalf("liveExamples[%q] leaves var %q blank and liveVars has no rule "+
				"for filling it", ex.dir, k)
		}
	}
	return vars
}

func randomSuffix(t *testing.T) string {
	t.Helper()
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("generating a random suffix: %v", err)
	}
	return hex.EncodeToString(b)
}

func destroyArgs(serial bool) []string {
	args := []string{"destroy", "-auto-approve", "-input=false", "-no-color"}
	if serial {
		args = append(args, "-parallelism=1")
	}
	return args
}

func mergeVars(base, overlay map[string]string) map[string]string {
	if len(overlay) == 0 {
		return base
	}
	merged := make(map[string]string, len(base)+len(overlay))
	for k, v := range base {
		merged[k] = v
	}
	for k, v := range overlay {
		merged[k] = v
	}
	return merged
}

// phaseLabel names a phase in a message, and returns "" for the single-phase
// case so an unphased example's messages stay uncluttered.
func phaseLabel(i, n int) string {
	if n <= 1 {
		return ""
	}
	return fmt.Sprintf(" (phase %d/%d)", i+1, n)
}

// selectLiveExamples returns the run list, narrowed by the EXAMPLES filter.
func selectLiveExamples(t *testing.T) []liveExample {
	t.Helper()

	filter := strings.TrimSpace(os.Getenv(liveExampleFilterEnvVar))
	if filter == "" {
		return liveExamples
	}

	wanted := make(map[string]bool)
	for _, f := range strings.FieldsFunc(filter, func(r rune) bool { return r == ',' || r == ' ' }) {
		wanted[strings.TrimSpace(f)] = true
	}

	var selected []liveExample
	for _, ex := range liveExamples {
		if wanted[ex.dir] {
			selected = append(selected, ex)
			delete(wanted, ex.dir)
		}
	}
	for name := range wanted {
		t.Fatalf("%s names %q, which is not in liveExamples", liveExampleFilterEnvVar, name)
	}
	if len(selected) == 0 {
		t.Fatalf("%s=%q selected no examples", liveExampleFilterEnvVar, filter)
	}
	t.Logf("%s=%q: running %d of %d examples", liveExampleFilterEnvVar, filter, len(selected), len(liveExamples))
	return selected
}

// liveExampleEnv holds what a live run needs.
type liveExampleEnv struct {
	terraform   string
	cliConfig   string
	examplesDir string
	client      *datahub.Client
}

// setupLiveExamples locates the binaries, writes a CLI config carrying a dev
// override, and builds the API client used for the presence assertions.
//
// The dev override is what makes the whole harness cheap: terraform loads the
// provider straight from disk, so there is no terraform init, no network and no
// Registry involved. Verified empirically -- apply in a copied example reaches
// the provider's credential check without any init.
//
// The corollary must not be forgotten: a dev override makes terraform ignore
// required_providers entirely, so Stage C can never detect a wrong version pin.
// That stays the job of scripts/check-example-versions.sh and the registry smoke
// test.
func setupLiveExamples(t *testing.T) liveExampleEnv {
	t.Helper()

	if os.Getenv(liveExampleEnvVar) == "" {
		t.Skipf("%s not set; run `make test-examples-live` against a DataHub instance", liveExampleEnvVar)
	}

	gmsURL := strings.TrimSpace(os.Getenv("DATAHUB_GMS_URL"))
	token := strings.TrimSpace(os.Getenv("DATAHUB_GMS_TOKEN"))
	if gmsURL == "" || token == "" {
		t.Fatalf("DATAHUB_GMS_URL and DATAHUB_GMS_TOKEN must both be set; these tests " +
			"apply for real and have no mock mode. Try `eval \"$(make quickstart-token)\"`.")
	}

	terraform, err := exec.LookPath("terraform")
	if err != nil {
		t.Fatalf("terraform not found on PATH: %v", err)
	}

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolving repository root: %v", err)
	}

	binDir := filepath.Join(root, "bin")
	if _, err := os.Stat(filepath.Join(binDir, "terraform-provider-datahub")); err != nil {
		t.Fatalf("provider binary not found in %s: %v\nRun `make install` first, or use "+
			"`make test-examples-live` which builds it.", binDir, err)
	}

	cliConfig := filepath.Join(t.TempDir(), "dev.tfrc")
	writeFile(t, cliConfig, fmt.Sprintf(`provider_installation {
  dev_overrides {
    "registry.terraform.io/datahub-project/datahub" = %q
  }
  direct {}
}
`, binDir))

	client, err := datahub.NewClient(gmsURL, token)
	if err != nil {
		t.Fatalf("building DataHub client: %v", err)
	}

	return liveExampleEnv{
		terraform:   terraform,
		cliConfig:   cliConfig,
		examplesDir: filepath.Join(root, "examples"),
		client:      client,
	}
}

// terraformIn runs terraform in dir with an explicit environment.
//
// Explicit rather than inherited, for the same reason the validate harness does
// it: a stray TF_CLI_CONFIG_FILE in the developer's shell (which .mise.env
// exports) must not silently redirect which provider binary is under test.
// DATAHUB_GMS_URL and DATAHUB_GMS_TOKEN are passed through because the provider
// needs them; nothing else is.
// The context is a parameter rather than t.Context() because the teardown path
// cannot use the latter: the testing package cancels a test's context just
// BEFORE its Cleanup functions run, so a destroy registered as cleanup and using
// t.Context() fails instantly with "context canceled" and leaves the instance
// dirty. That is not a hypothetical -- it is what the first live run of this
// harness did, and the failure looked exactly like a broken destroy.
func (e liveExampleEnv) terraformIn(ctx context.Context, t *testing.T, dir string, vars map[string]string, args ...string) (string, error) {
	t.Helper()

	cmd := exec.CommandContext(ctx, e.terraform, args...)
	cmd.Dir = dir
	cmd.Env = []string{
		"HOME=" + os.Getenv("HOME"),
		"PATH=" + os.Getenv("PATH"),
		"TF_CLI_CONFIG_FILE=" + e.cliConfig,
		"TF_IN_AUTOMATION=1",
		"DATAHUB_GMS_URL=" + os.Getenv("DATAHUB_GMS_URL"),
		"DATAHUB_GMS_TOKEN=" + os.Getenv("DATAHUB_GMS_TOKEN"),
	}
	for k, v := range vars {
		cmd.Env = append(cmd.Env, "TF_VAR_"+k+"="+v)
	}

	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// copyExampleDir copies an example into a scratch directory, so a run never
// writes state, plan files or .terraform into the repository.
func copyExampleDir(t *testing.T, src, dst string) {
	t.Helper()

	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatalf("reading %s: %v", src, err)
	}
	for _, e := range entries {
		if e.IsDir() {
			// Examples carry fixtures/ and similar; none of the sixteen in the run
			// list does, and copying a tree here would quietly bring along whatever
			// a future example puts there.
			continue
		}
		if !strings.HasSuffix(e.Name(), ".tf") {
			continue
		}
		copyFile(t, filepath.Join(src, e.Name()), filepath.Join(dst, e.Name()))
	}
}
