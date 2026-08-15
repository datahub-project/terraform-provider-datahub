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
// The flags exist because some examples need something the others do not, and
// every one of them traces to a documented server behaviour rather than a harness
// convenience. Nothing here is a knob to turn when CI goes red.
//
// Every field defaults to the stronger behaviour: the zero value asserts more,
// not less. noReapplyReason in particular is an opt-OUT, so an author who adds an
// example and thinks about none of this gets the full set of checks.
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

	// waitForURN is an entity the example READS but does not create, polled for
	// before the first apply. Set only where the entity is written by an
	// asynchronous bootstrap template, so a Quickstart that has just passed
	// `datahub docker check` may not hold it yet.
	//
	// It is a wait, never a skip. Exhausting the budget fails the example, because
	// every entity worth naming here is optional: false upstream and its permanent
	// absence is therefore a defect rather than a reason to run one example fewer.
	waitForURN string

	// noReapplyReason opts an example out of the re-apply-and-destroy-again cycle,
	// which otherwise runs for every example. Empty means it runs; a non-empty
	// value both suppresses it and says why, because an opt-out nobody can audit
	// is how a temporary workaround becomes permanent.
	//
	// The check is on by default because it costs about a second against a
	// three-minute Quickstart boot, and because it is the only thing that tests
	// what a CAT-2583 husk actually DOES: the husk carries no content aspects and
	// is invisible in the UI, so an aspect probe reports the entity absent while
	// the next apply is refused with "already exists".
	//
	// **Cite the upstream issue number** where one exists. The value of this field
	// is that someone can sweep it and ask which of the tickets have moved; a
	// reason describing a server bug without naming it cannot be swept.
	//
	// There is exactly ONE admissible category: a server behaviour that makes
	// applying the same configuration twice against one instance impossible, so
	// the check can never pass. Two categories that look admissible and are not:
	//
	//   - Cost. The measured figure is ~1s per example. Nothing here is worth
	//     buying a second back.
	//   - "The check is vacuous because these writes are upserts." That is a claim
	//     about the provider's own code, not a law of nature. Leaving the check on
	//     turns it into a regression guard on exactly that claim, which is worth
	//     more than the second it costs.
	//
	// An example that manages no entity at all needs no entry: the harness derives
	// the skip from an empty URN harvest, so the data-source-only examples cannot
	// go stale here.
	noReapplyReason string
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
// This is now every runnable example that can run against an OSS Quickstart.
// The first slice ran six and left ten deferred so that real wall-clock could be
// measured before committing to the surface; the measurement came back at about
// a second per example against a 178-second boot, which removed the argument for
// staging. Every remaining entry in the exclusion table below is permanent.
var liveExamples = []liveExample{
	// Credential preflight. Creates nothing, and fails in seconds when the PAT
	// or GMS URL is wrong -- before an hour of Quickstart time is spent.
	{dir: "provider-install-verification"},

	// Index-lagged plural data source (data.datahub_tags.all, searchAcrossEntities
	// backed). Between apply and plan the index catches up and the output value
	// changes, which is the false-failure class the plan assertion must tolerate.
	{dir: "tag-simple"},

	// Two ownership types, and the only example that feeds a GraphQL list
	// straight into a strongly-consistent singular lookup: for_each over
	// data.datahub_ownership_types.all (listOwnershipTypes, OpenSearch-backed)
	// keying data.datahub_ownership_type.details (OpenAPI v3, which hard-errors
	// on absence rather than returning nothing).
	//
	// That pairing was expected to break the re-apply: right after a destroy the
	// list should still name the deleted types while the lookup 404s. It does
	// not, and the reason is measurable rather than lucky -- against v1.7.0 the
	// lag is entirely on the CREATE side (about 2s before a new type appears in
	// the list), while deleteOwnershipType drops the search document in under
	// 100ms, with the v3 endpoint returning 404 at the same instant. There is no
	// window in which the two disagree in the direction that hurts. Deleting
	// before the create's index write lands leaves nothing stranded either.
	//
	// So no flags, and deliberately no noReapplyReason: that check is what would
	// report a future release making the delete path asynchronous, in the one
	// configuration positioned to notice. See flakiness item 9 in the design.
	{dir: "ownership-type-simple"},

	// Three-level hierarchy, four children across two parents: hits the
	// child-domain delete race by construction.
	//
	// The most informative host for the re-apply check, though no longer the only
	// one now that it is on by default. Domains are the entity type the provider
	// carries create-time husk repair for (domains.go), so applying this
	// configuration a second time exercises that repair path rather than merely
	// hoping not to need it.
	{dir: "domain-simple", serialDestroy: true},

	// Four glossary nodes and four terms, two levels deep. No flags, and the
	// absence of serialDestroy is the deliberate half: unlike domains, DataHub
	// enforces no child guard on glossary node deletion, so there is no
	// stale-index race for -parallelism=1 to narrow. Ordering here comes from the
	// parent_node .urn references in the configuration itself, which is exactly
	// what the example is teaching.
	{dir: "glossary-node-term-simple"},

	// The worst CAT-2583 shape in the tree: deletes structured properties that
	// are assigned to entities the same destroy removes. CHANGELOG.md records
	// this exact configuration producing husks.
	//
	// The one opt-out from the re-apply check, and the reason is a finding rather
	// than a preference. Assigning a structured property to an entity creates an
	// Elasticsearch field for it, and deleting the property does not remove that
	// field. The entity is provably gone -- 404 on the v3 endpoint -- but a later
	// create of the same qualifiedName is rejected with "Elasticsearch field
	// 'tf-example_governance_tier' collides with existing property mapping". So
	// this configuration cannot be applied twice against one instance at all,
	// not by this harness and not by a user.
	//
	// ASSIGNING the property is the trigger, not defining it, and not the dots
	// in the name. Established by controlled experiment against v1.7.0:
	// dotted-unassigned and dotless-unassigned both re-apply cleanly, while
	// dotless-assigned fails with a dot-free field name in the message. That
	// matches structuredProperties being a dynamic mapping -- the field appears
	// only once a document carries a value. Ignore what the error says about '.'
	// versus '_': that describes two distinct names colliding on one field, and
	// DataHub reuses the wording here.
	//
	// The practical rule for a new entry: an example that DEFINES structured
	// properties can re-apply (see structured-property-simple, verified); one
	// that ASSIGNS them cannot. Distinct from CAT-2583 -- there is no husk here
	// -- and it is why the ephemeral Quickstart target is mandatory for this
	// example rather than merely preferable.
	{
		dir:                "structured-and-custom-properties",
		settleAfterDestroy: true,
		noReapplyReason: "assigning a structured property burns its Elasticsearch field name, " +
			"and deleting the property does not release it, so this configuration cannot be " +
			"applied twice against one instance by anyone -- harness or user. Confirmed as " +
			"intended behaviour with reclaim deferred to system-update on " +
			"datahub-project/datahub#18974. Sweep condition: re-enable this check when that " +
			"issue reports the mapping being reclaimed on delete.",
	},

	// The other end of the structured-property spectrum: it DEFINES two
	// properties and assigns them to nothing.
	//
	// settleAfterDestroy is deliberately FALSE, and the asymmetry with the entry
	// above is the point rather than an oversight. The CAT-2583 side effect
	// scrolls the search index for entities CARRYING the property and JSON-PATCHes
	// each hit; the resurrection is a patch landing on a concurrently hard-deleted
	// carrier. With zero carriers there is no scroll result, no patch, and nothing
	// to race -- so a pause would buy fifteen seconds against a mechanism this
	// configuration cannot trigger. The end-of-run sweep re-checks every URN
	// anyway, so if that reasoning is ever wrong the run still says so.
	//
	// It also keeps the re-apply check for the same underlying reason: a
	// definition writes no Elasticsearch field (structuredProperties is a dynamic
	// mapping, so the field appears only once a document carries a value), so
	// there is nothing here to burn.
	{dir: "structured-property-simple"},

	// Two forms (one COMPLETION, one VERIFICATION), the two structured
	// properties their prompts collect, and a stewardship group. The example
	// DEFINES structured properties and assigns them to nothing -- a form
	// prompt references a property by URN without writing a value to any
	// entity -- so no Elasticsearch field is burned and the re-apply check
	// stays on, per the defining-vs-assigning rule in the design doc.
	//
	// settleAfterDestroy is false for the same reason it is false on
	// structured-property-simple: the CAT-2583 resurrection needs entities
	// CARRYING the property, and there are none. The forms' own dynamic
	// assignment matches postgres and mysql datasets, of which a Quickstart
	// has zero, so the async assignment hook has nothing to assign and the
	// destroy has nothing to retract.
	{dir: "form-simple"},

	// The only phased example, and the only one whose provider block is part of
	// what is under test. main.tf puts the marker tag and structured-property URNs
	// inside provider "datahub" { defaults = ... }, and provider configuration is
	// evaluated before any resource in the same apply -- so the first apply must
	// run with defaults OFF to create them, and the second with defaults ON to
	// exercise them. Replaying only the last phase would apply a configuration
	// whose precondition was never established.
	//
	// The phases are also why assertOutputsUsable can be strict here: several
	// outputs are documented as null while defaults are off, and that assertion
	// runs once after the phase loop, so it only ever sees phase-2 values.
	//
	// settleAfterDestroy because the marker property is assigned -- through
	// provider defaults rather than an explicit assignment resource, but assigned
	// -- to two data products the same destroy removes. That is the CAT-2583
	// shape, one entity type removed from structured-and-custom-properties.
	//
	// The re-apply opt-out is established empirically, not inferred from that
	// shape: promoted into the run list against a fresh v1.7.0 Quickstart, its
	// re-apply fails in PHASE 1, on datahub_structured_property.managed_by -- the
	// definition -- even though it was the phase-2 assignment that burned the
	// field. Assignment writes the Elasticsearch field; the next attempt to define
	// the same qualifiedName is what gets rejected.
	{
		dir: "data-product-simple",
		phases: []map[string]string{
			{"enable_defaults": "false"},
			{"enable_defaults": "true"},
		},
		settleAfterDestroy: true,
		noReapplyReason: "the provider defaults assign io.example.terraform.dpManagedBy to both " +
			"data products, which burns that Elasticsearch field name; deleting the property " +
			"does not release it, so phase 1 of any second apply is rejected with " +
			"\"Elasticsearch field 'io_example_terraform_dpManagedBy' collides with existing " +
			"property mapping\". Confirmed as intended behaviour with reclaim deferred to " +
			"system-update on datahub-project/datahub#18974. Sweep condition: re-enable this " +
			"check when that issue reports the mapping being reclaimed on delete.",
	},

	// datahub_secret plus an ingestion source referencing it by name.
	//
	// value is Required and WriteOnly on the resource while var.secret_value
	// defaults to null, so the variable is needed on apply -- and on destroy too,
	// since terraform still evaluates the configuration there. The harness passes
	// vars to every invocation, so nothing extra is required for that.
	//
	// The value itself is inert: DataHub encrypts it server-side and nothing in
	// this run ever decrypts it. Naming it so it reads as a harness artefact
	// matters more than making it look plausible.
	{
		dir:  "secret-basic",
		vars: map[string]string{"secret_value": "tf-example-live-not-a-real-secret"},
	},

	// One ingestion source and nothing else. Its interest is in the harvest
	// rather than the configuration: datahub_ingestion_source is the only entity
	// resource in the provider with no urn attribute, so this is the only example
	// that exercises urnTemplate. Break that template and the presence check after
	// apply is the first thing to notice.
	//
	// The CSV at main.tf is fetched by the executor when a run is triggered, not
	// by terraform, so apply needs no network.
	{dir: "ingestion-source-csv-enricher"},

	// Three variables have no default because they are credentials, and synthetic
	// values suffice: upsertConnection stores an encrypted JSON blob and DataHub
	// never dials Snowflake with it (connections.go), so a connection built from
	// nonsense applies, reads back and destroys exactly like a real one. Delete
	// goes through OpenAPI v3 rather than GraphQL precisely because the mutation
	// is absent in OSS, which is a path only a live run covers.
	{
		dir: "connection-snowflake",
		vars: map[string]string{
			"snowflake_account_id": "tf-example-live.us-east-1",
			"snowflake_username":   "tf-example-live",
			"snowflake_password":   "tf-example-live-not-a-real-secret",
		},
	},

	// The same connection wired into an ingestion source whose recipe references
	// it by URN. Same synthetic credentials, same reason they are sufficient.
	{
		dir: "connection-snowflake-ingestion-source",
		vars: map[string]string{
			"snowflake_account_id": "tf-example-live.us-east-1",
			"snowflake_username":   "tf-example-live",
			"snowflake_password":   "tf-example-live-not-a-real-secret",
		},
	},

	// Read-only: one data.datahub_ingestion_source. It creates nothing, so the
	// re-apply check skips itself off the empty harvest -- derived, not tabled,
	// which is what keeps it from going stale if the example ever grows a resource.
	//
	// waitForURN is the one thing it does need. The example asserts datahub-gc is
	// "present on every DataHub instance", and that is true of a settled instance
	// but not necessarily of one that has just passed `datahub docker check`:
	// bootstrap_mcps.yaml declares ingestion-datahub-gc with no blocking/async
	// overrides, so it takes the file's defaults (blocking: false, async: true),
	// unlike root-user, roles, ownership-types and page-templates which are all
	// explicitly blocking and synchronous. It is optional: false, so it does
	// arrive -- the poll waits for it and fails if it never does.
	{dir: "ingestion-source-lookup", waitForURN: "urn:li:dataHubIngestionSource:datahub-gc"},

	// Two metadata tests and the listTests-backed plural data source. On OSS the
	// definitions are stored verbatim and nothing evaluates them, but the whole
	// CRUD surface is OSS -- which is exactly what a live run covers. No flags:
	// createTest's already-exists guard reads the entity store rather than the
	// search index, and deleteTest is a synchronous hard delete with no async
	// side effect, so the default re-apply-after-destroy check is expected to
	// pass and left on. The plural data source output is index-lagged
	// (listTests), the same false-failure class tag-simple documents, which the
	// plan assertion already tolerates.
	{dir: "metadata-test-simple"},

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

	// The broadest example in the tree: a group, a native login, a catalog-only
	// user, three memberships, a role assignment and a policy. It is also the only
	// one that touches the identity the harness authenticates as, which is why it
	// runs last -- not a constraint the harness depends on, just the cheapest way
	// to bound the blast radius if that ever turns out to matter.
	//
	// It does NOT override member_username, and the design's preferred mitigation
	// (point it at a throwaway user) is retired rather than deferred. That
	// mitigation existed to route around an unanswered question -- whether a
	// group's Editor assignment could NARROW the Admin the harness's own token
	// relies on -- and the answer is that it cannot. DataHub authorization is
	// ALLOW-only and additive, and the Quickstart datahub account's Admin does not
	// come from role matching at all: boot/policies.json names
	// urn:li:corpuser:datahub directly as an actor on the platform policies, which
	// no group assignment can reach. The mitigation was also unimplementable as
	// written, since a Quickstart has no throwaway user to point at.
	//
	// new_member_email IS overridden, for the unrelated OSS signUp guard: it
	// rejects signUp whenever the user entity exists at all, so a fixed address is
	// poisoned permanently by one failed destroy. Left blank here and randomised
	// per run; see liveVars.
	{
		dir: "local-iam",
		vars: map[string]string{
			"new_member_email": "", // filled per run; see liveVars
		},
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

	// No deferred entries remain: every Quickstart-capable example is in the run
	// list. The permanent flag stays load-bearing rather than vestigial -- it is
	// what a future reader needs to tell "this can never run here" from "not yet",
	// and the next example that is merely awkward will want it again.
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

// TestNoReapplyReasonsAreSubstantial is the same guard one level down, on the
// per-example opt-out from the re-apply check.
//
// It exists because that opt-out used to be a bool. A bool records that somebody
// decided; it cannot record why, so the decision could not be reviewed, and a
// skip put in for a server bug was indistinguishable from one put in because a
// run went red. The reason string is only worth the trouble if it is a reason --
// "flaky", "N/A" or "see above" satisfies a non-empty check and tells the next
// reader nothing -- so there is a floor on it, matching the exclusions table.
//
// The floor is length, not content. A test cannot tell whether an issue number
// is the right one, and one demanding a link would push a genuine no-ticket case
// into inventing a plausible URL. Citing the upstream issue is a documented
// requirement on the field (see noReapplyReason) enforced by review; what this
// catches is the empty gesture.
func TestNoReapplyReasonsAreSubstantial(t *testing.T) {
	t.Parallel()

	// Long enough to have named a mechanism. The shortest genuine reason anyone
	// has needed so far runs to several lines.
	const minReasonLen = 40

	for _, ex := range liveExamples {
		if ex.noReapplyReason == "" {
			continue
		}
		if len(ex.noReapplyReason) < minReasonLen {
			t.Errorf("liveExamples[%q] opts out of the re-apply check with reason %q (%d "+
				"chars, minimum %d), which is too short to say what makes applying this "+
				"configuration twice impossible. Name the server behaviour and cite the "+
				"upstream issue.", ex.dir, ex.noReapplyReason, len(ex.noReapplyReason), minReasonLen)
		}
	}
}
