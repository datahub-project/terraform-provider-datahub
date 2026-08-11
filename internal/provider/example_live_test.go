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

	var swept []harvestedURN

	for _, ex := range selectLiveExamples(t) {
		// Not t.Parallel: see the comment above.
		t.Run(ex.dir, func(t *testing.T) {
			urns := runLiveExample(t, env, ex)
			swept = append(swept, urns...)
		})
	}

	// The end-of-run sweep. This is the only assertion that catches a late
	// CAT-2583 resurrection: the side effect is asynchronous, so an entity that
	// was provably absent when its own example finished can reappear while a
	// later example runs. Reported separately from the per-example checks
	// because the two have different causes and different fixes -- a URN failing
	// only here means the delete worked and something put it back.
	t.Run("end-of-run-sweep", func(t *testing.T) {
		if len(swept) == 0 {
			t.Skip("no URNs harvested; nothing to sweep")
		}
		for _, h := range swept {
			present, err := datahubtesting.URNPresent(context.Background(), env.client, h.urn)
			if err != nil {
				t.Errorf("sweep: probing %s (%s): %v", h.urn, h.address, err)
				continue
			}
			if present {
				t.Errorf("sweep: %s (%s) is present again at the end of the run, having "+
					"passed its own post-destroy check. The destroy worked and something "+
					"put it back -- the CAT-2583 side effect is asynchronous and can land "+
					"after a later example has started.", h.urn, h.address)
			}
		}
		t.Logf("swept %d URNs from %d examples", len(swept), len(liveExamples))
	})
}

// runLiveExample drives one example through apply, assertions and destroy,
// returning the URNs it created so the end-of-run sweep can re-check them.
func runLiveExample(t *testing.T, env liveExampleEnv, ex liveExample) []harvestedURN {
	t.Helper()

	dir := t.TempDir()
	copyExampleDir(t, filepath.Join(env.examplesDir, "runnable", ex.dir), dir)

	vars := liveVars(t, ex)
	phases := ex.phases
	if len(phases) == 0 {
		phases = []map[string]string{nil}
	}

	// Registered before the first apply, so a destroy runs even when an
	// assertion below fails, the harness panics, or the test times out. This is
	// the difference between one failed example and an instance full of debris.
	var harvested []harvestedURN
	t.Cleanup(func() {
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
			t.Errorf("[%s] destroy failed twice; the instance holds debris from this "+
				"example. First error: %v\nRetry output:\n%s", ex.dir, err, out2)
		}
	})

	for i, phaseVars := range phases {
		merged := mergeVars(vars, phaseVars)
		label := ""
		if len(phases) > 1 {
			label = fmt.Sprintf(" (phase %d/%d)", i+1, len(phases))
		}

		start := time.Now()
		if out, err := env.terraformIn(t.Context(), t, dir, merged, "apply", "-auto-approve", "-input=false", "-no-color"); err != nil {
			t.Fatalf("[%s] apply%s failed: %v\n%s", ex.dir, label, err, out)
		}
		t.Logf("[%s] apply%s took %s", ex.dir, label, time.Since(start).Round(time.Second))

		// Assertion 1: no resource changes proposed after apply.
		assertPlanClean(t, env, dir, merged, ex.dir+label)
	}

	// Assertion 3a: every managed URN exists on the server. Harvested once and
	// reused for the post-destroy check, so this costs one GET per resource.
	harvested = harvestURNs(t, env, dir, vars, ex.dir)
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

	if ex.settleAfterDestroy {
		t.Logf("[%s] settling %s before the absence check (CAT-2583)", ex.dir, settleAfterDestroyPause)
		time.Sleep(settleAfterDestroyPause)
	}

	// Assertion 3b: absence, except where the example's whole point is that the
	// entity survives.
	survive := make(map[string]bool, len(ex.mustSurvive))
	for _, addr := range ex.mustSurvive {
		survive[addr] = true
	}
	seen := make(map[string]bool, len(ex.mustSurvive))
	remaining := make([]harvestedURN, 0, len(harvested))

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
				t.Errorf("[%s] %s (%s) is expected to survive destroy, but presence "+
					"could not be established: %v", ex.dir, h.address, h.urn, err)
			case !present:
				t.Errorf("[%s] %s (%s) was deleted by destroy, but this example adopts an "+
					"entity it did not create, so destroy is supposed to RESTORE it. The "+
					"instance has been left without it.", ex.dir, h.address, h.urn)
			}
			continue
		}
		datahubtesting.AssertURNAbsent(t, env.client, h.resourceType, h.urn)
		remaining = append(remaining, h)
	}

	// A mustSurvive address that matched nothing is a stale expectation, and it
	// would otherwise pass silently while asserting nothing at all.
	for _, addr := range ex.mustSurvive {
		if !seen[addr] {
			t.Errorf("[%s] mustSurvive names %q, which is not a managed resource in the "+
				"harvested state; the restore assertion checked nothing", ex.dir, addr)
		}
	}

	// Assertion 5: prove re-creation is not blocked. A husk is invisible in the
	// UI and carries no content aspects; what it actually does is refuse the next
	// apply with "already exists". Testing that consequence beats inferring it.
	if ex.reapplyAfterDestroy {
		start := time.Now()
		if out, err := env.terraformIn(t.Context(), t, dir, mergeVars(vars, lastPhase(phases)), "apply", "-auto-approve", "-input=false", "-no-color"); err != nil {
			t.Errorf("[%s] re-apply after destroy failed, which is what a CAT-2583 husk "+
				"does: the entity is gone from the UI but still blocks its own URN. %v\n%s",
				ex.dir, err, out)
		} else {
			t.Logf("[%s] re-apply after destroy succeeded in %s", ex.dir, time.Since(start).Round(time.Second))
		}
		// Leave the teardown to the registered Cleanup.
	}

	return remaining
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
// Both are for home-page-layout. The email is randomised because
// datahub_local_user_login is subject to the OSS signUp guard, which rejects the
// request whenever the user entity exists at all -- so one failed destroy would
// poison a fixed address permanently. The password exists because the attribute
// is WriteOnly and therefore has no default anywhere; generating it here keeps
// it out of state, out of the plan file and out of the repository.
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

func lastPhase(phases []map[string]string) map[string]string {
	if len(phases) == 0 {
		return nil
	}
	return phases[len(phases)-1]
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
			// Examples carry fixtures/ and similar; none of the six in the first
			// slice does, and copying a tree here would quietly bring along
			// whatever a future example puts there.
			continue
		}
		if !strings.HasSuffix(e.Name(), ".tf") {
			continue
		}
		copyFile(t, filepath.Join(src, e.Name()), filepath.Join(dst, e.Name()))
	}
}
