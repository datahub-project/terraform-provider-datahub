// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider_test

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// Stage C classification: which runnable examples are applied against a live
// DataHub Quickstart, and why each of the rest is not.
//
// This file deliberately holds no terraform machinery. Everything here runs
// under a plain `go test ./...` with no provider binary, no terraform and no
// DataHub instance, because the check that matters most -- that no example is
// classified by nothing -- must not be gated behind an opt-in env var. See
// TestEveryRunnableExampleIsClassified.
//
// Design: docs/design/live-example-execution.md.

// liveExample describes one runnable example the harness applies for real.
//
// The flags exist because four examples need something the other two do not,
// and every one of them traces to a documented server behaviour rather than a
// harness convenience. Nothing here is a knob to turn when CI goes red.
type liveExample struct {
	// dir is the directory name under examples/runnable.
	dir string

	// vars are TF_VAR_ values the harness supplies. Used where an example
	// deliberately has no default (a credential), or where its published
	// default is right for a reader and wrong for a harness.
	vars map[string]string

	// phases applies the configuration more than once with different variables.
	// data-product-simple needs it: provider configuration cannot depend on
	// resources the same apply creates, so defaults are off for the first apply
	// and on for the second.
	phases []map[string]string

	// serialDestroy makes the destroy retry use -parallelism=1. Set for nested
	// domains, where the child-delete guard reads a stale OpenSearch index and
	// concurrency widens the window (datahub-project/datahub#17732).
	serialDestroy bool

	// settleAfterDestroy pauses before the absence check. Set where the
	// configuration deletes structured properties, whose delete fires an
	// asynchronous side effect that can resurrect a hard-deleted entity as a
	// husk (CAT-2583).
	settleAfterDestroy bool

	// mustSurvive names resource addresses whose entity must STILL EXIST after
	// destroy, inverting the usual assertion. Only home-page-layout needs it,
	// and it is not an exemption: the assertion is that the restore happened.
	mustSurvive []string

	// reapplyAfterDestroy applies the configuration a second time after the
	// destroy. A CAT-2583 husk carries no content aspects and is invisible in
	// the UI; what it actually does is block re-creation of the same URN. A
	// second apply tests that consequence directly rather than inferring it
	// from an aspect probe.
	reapplyAfterDestroy bool
}

// liveExamples are the examples applied and destroyed against a live
// Quickstart, in run order.
//
// Ordering carries exactly one constraint now: provider-install-verification
// runs first. Everything else that used to constrain the order was an
// identifier collision between two examples, and #114 removed all of them --
// note that uniqueness made the run safe to REORDER, not safe to parallelise.
// Several examples read instance-wide plural data sources and so appear in each
// other's outputs no matter what their entities are called.
//
// This is the first slice, not the intended end state. Ten further examples are
// classified as deferred below; the slice exists to measure real wall-clock and
// exercise both known flake classes before committing to that surface.
var liveExamples = []liveExample{
	// Credential preflight. Creates nothing, and fails in seconds when the PAT
	// or GMS URL is wrong -- before an hour of Quickstart time is spent.
	{dir: "provider-install-verification"},

	// Index-lagged plural data source (data.datahub_tags.all, searchAcrossEntities
	// backed). Between apply and plan the index catches up and the output value
	// changes, which is the false-failure class the plan assertion must tolerate.
	{dir: "tag-simple"},

	// Three-level hierarchy, four children across two parents: hits the
	// child-domain delete race by construction.
	//
	// This is where reapplyAfterDestroy is exercised. Domains are the entity type
	// the provider carries create-time husk repair for (domains.go), so applying
	// this configuration a second time is a direct test of the thing a husk
	// actually does -- block re-creation of a URN whose entity is gone. See the
	// note on structured-and-custom-properties for why the husk-exposed example
	// cannot host this check itself.
	{dir: "domain-simple", serialDestroy: true, reapplyAfterDestroy: true},

	// The worst CAT-2583 shape in the tree: deletes structured properties that
	// are assigned to entities the same destroy removes. CHANGELOG.md records
	// this exact configuration producing husks.
	//
	// Deliberately NOT reapplyAfterDestroy, and the reason is a finding rather
	// than a preference. Deleting a structured property leaves its Elasticsearch
	// field mapping behind: the entity is provably gone (404 on the v3 entity
	// endpoint) but a later create of the same qualifiedName is rejected with
	// "Elasticsearch field 'tf-example_governance_tier' collides with existing
	// property mapping", because DataHub normalises '.' to '_' and the property
	// collides with its own residue. So this configuration cannot be applied
	// twice against one instance at all -- not by this harness, and not by a
	// user. That is a server-side gap, distinct from CAT-2583 (there is no husk
	// here), and it is why the ephemeral Quickstart target is mandatory for this
	// example rather than merely preferable.
	{
		dir:                "structured-and-custom-properties",
		settleAfterDestroy: true,
	},

	// New with #116. Two modules and a template nothing points at, no variables:
	// the boring case, which is what makes it a useful control.
	{dir: "page-template-simple"},

	// New with #116, and the only live coverage of restore-on-destroy anywhere.
	// It adopts the instance's default template, so destroy restores the
	// previous layout instead of deleting it.
	//
	// test_user_email is overridden rather than left at its published default
	// because datahub_local_user_login is subject to the OSS signUp guard, which
	// rejects the request whenever the user entity exists at all. One failed
	// destroy would poison the example permanently on any instance that
	// survives the run. The ephemeral Quickstart makes that self-healing; the
	// override means an accidental run against a persistent instance is
	// survivable too.
	//
	// test_user_password has no default at all (it is WriteOnly, so storing it
	// anywhere is the thing the attribute exists to prevent).
	{
		dir: "home-page-layout",
		vars: map[string]string{
			"test_user_email":    "", // filled per run; see liveVars
			"test_user_password": "", // filled per run; see liveVars
		},
		mustSurvive: []string{"datahub_page_template.home"},
	},
}

// liveExclusion records why an example is not applied live.
//
// permanent separates "this can never run here" from "not in the first slice".
// Without that distinction every deferred example reads as permanently out of
// scope, and the expansion slice would have to re-derive which is which.
type liveExclusion struct {
	reason    string
	permanent bool
}

var liveExampleExclusions = map[string]liveExclusion{
	// Permanent: Cloud-only. The entity types and mutations do not exist in OSS
	// DataHub, and the Quickstart target is OSS.
	"action-pipeline-dataplex-sync": {
		reason:    "datahub_action_pipeline and datahub_action_pipelines are Cloud-only; the dataHubAction entity type does not exist in OSS",
		permanent: true,
	},
	"assertion-volume-sqlite": {
		reason:    "datahub_volume_assertion is Cloud-only; the monitor service layer does not exist in OSS",
		permanent: true,
	},
	"executor-pool-basic": {
		reason:    "datahub_remote_executor_pool is Cloud-only; the dataHubRemoteExecutorPool entity type does not exist in OSS",
		permanent: true,
	},

	// Permanent: cost and wall-clock. Both would also be excluded as Cloud-only.
	"financial-services": {
		reason:    "generates its inputs by pip-installing dependencies and shallow-cloning a 40-60 MB repository, then creates 147 domains and 1500-2000 glossary terms; also Cloud-only via five assertion resources, and needs ANTHROPIC_API_KEY",
		permanent: true,
	},
	"remote-executor-azure": {
		reason:    "provisions billable Azure infrastructure (roughly USD 280/month per its README) with 10-15 minutes of provisioning; also Cloud-only, and needs three credentials plus a Cloudsmith entitlement",
		permanent: true,
	},

	// Deferred: these run on a Quickstart and are intended for the expansion
	// slice, once the first slice has measured what a live run actually costs.
	"connection-snowflake": {
		reason: "deferred to the expansion slice; runs on Quickstart (DataHub stores the connection blob without ever dialling Snowflake) but needs three synthetic variables",
	},
	"connection-snowflake-ingestion-source": {
		reason: "deferred to the expansion slice; same synthetic variables as connection-snowflake",
	},
	"data-product-simple": {
		reason: "deferred to the expansion slice; needs the two-phase apply (enable_defaults false then true) and carries CAT-2583 exposure",
	},
	"glossary-node-term-simple": {
		reason: "deferred to the expansion slice; no blockers",
	},
	"ingestion-source-csv-enricher": {
		reason: "deferred to the expansion slice; no blockers (the CSV is fetched by the executor at ingestion time, not by terraform)",
	},
	"ingestion-source-lookup": {
		reason: "deferred to the expansion slice; additionally needs a preflight probe for datahub-gc, since whether a freshly healthy Quickstart has finished creating its system ingestion sources is unverified",
	},
	"local-iam": {
		reason: "deferred to the expansion slice; needs a member_username override so the example does not modify the group membership of the account the harness authenticates as, plus a per-run unique email for the OSS signUp guard",
	},
	"ownership-type-simple": {
		reason: "deferred to the expansion slice; no blockers",
	},
	"secret-basic": {
		reason: "deferred to the expansion slice; needs TF_VAR_secret_value, which has no default",
	},
	"structured-property-simple": {
		reason: "deferred to the expansion slice; narrow CAT-2583 exposure (properties assigned to nothing)",
	},
}

// TestEveryRunnableExampleIsClassified is the point of the whole mechanism: an
// example in neither table is a build failure, not an absence.
//
// Stage B learned this the hard way. examples/provider was validated by nothing
// because it is neither a per-resource snippet nor a runnable example, and the
// omission was invisible until someone reconciled the file count by hand --
// TestEveryExampleIsValidated exists to make that impossible to repeat. Stage C
// reintroduces exactly the same hazard at directory granularity: an author
// adding examples/runnable/foo and not touching this file would otherwise get
// silence, and silence is indistinguishable from a deliberate decision.
//
// Deliberately not gated behind the live env var. It needs no terraform, no
// provider binary and no DataHub instance, so it runs under a plain
// `go test ./...` -- which is the only way it catches the omission at the time
// the example is added rather than the next time somebody runs a nightly.
func TestEveryRunnableExampleIsClassified(t *testing.T) {
	t.Parallel()

	runnableDir := filepath.Join("..", "..", "examples", "runnable")
	entries, err := os.ReadDir(runnableDir)
	if err != nil {
		t.Fatalf("reading examples/runnable: %v", err)
	}

	inRunList := make(map[string]bool, len(liveExamples))
	for _, ex := range liveExamples {
		if inRunList[ex.dir] {
			t.Errorf("%q appears twice in liveExamples", ex.dir)
		}
		inRunList[ex.dir] = true
	}

	var dirs []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dirs = append(dirs, e.Name())

		name := e.Name()
		_, excluded := liveExampleExclusions[name]

		switch {
		case inRunList[name] && excluded:
			t.Errorf("examples/runnable/%s is in both liveExamples and "+
				"liveExampleExclusions; it must be in exactly one", name)
		case !inRunList[name] && !excluded:
			t.Errorf("examples/runnable/%s is classified by nothing. Stage C neither "+
				"applies it nor records why it is skipped, so it is covered by no live "+
				"test and nobody can tell whether that was a decision. Add it to "+
				"liveExamples, or to liveExampleExclusions with a reason.", name)
		}
	}

	// The reverse direction: an entry naming a directory that no longer exists
	// is stale, and a stale exclusion is how a renamed example quietly loses its
	// coverage while both tables still look complete.
	present := make(map[string]bool, len(dirs))
	for _, d := range dirs {
		present[d] = true
	}
	for _, ex := range liveExamples {
		if !present[ex.dir] {
			t.Errorf("liveExamples names %q, which is not a directory under "+
				"examples/runnable", ex.dir)
		}
	}
	stale := make([]string, 0, len(liveExampleExclusions))
	for name := range liveExampleExclusions {
		if !present[name] {
			stale = append(stale, name)
		}
	}
	sort.Strings(stale)
	for _, name := range stale {
		t.Errorf("liveExampleExclusions names %q, which is not a directory under "+
			"examples/runnable", name)
	}

	// A floor, not an assertion about the exact count: the walk silently passing
	// over an empty directory listing would make every check above vacuous.
	const wantAtLeast = 15
	if len(dirs) < wantAtLeast {
		t.Errorf("only %d directories under examples/runnable, expected at least %d; "+
			"the directory read is probably not working", len(dirs), wantAtLeast)
	}

	var deferredCount int
	for _, ex := range liveExampleExclusions {
		if !ex.permanent {
			deferredCount++
		}
	}
	t.Logf("%d runnable examples: %d applied live, %d excluded (%d permanently, %d deferred)",
		len(dirs), len(liveExamples), len(liveExampleExclusions),
		len(liveExampleExclusions)-deferredCount, deferredCount)
}

// TestLiveExampleExclusionsHaveReasons keeps the exclusion table honest. An
// empty reason is worse than no entry: it satisfies the completeness test above
// while telling the next reader nothing.
func TestLiveExampleExclusionsHaveReasons(t *testing.T) {
	t.Parallel()

	for name, ex := range liveExampleExclusions {
		if len(ex.reason) < 20 {
			t.Errorf("liveExampleExclusions[%q] has reason %q, which is too short to "+
				"explain anything; say what specifically prevents a live run", name, ex.reason)
		}
	}
}
