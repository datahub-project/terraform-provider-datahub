// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
)

// Every runnable example is a whole configuration that somebody applies for
// real, against a shared or disposable DataHub instance. When two of them name
// the same entity, three things go wrong, in ascending order of nastiness.
//
// A concurrent apply fails outright with "already exists" -- the loud case, and
// the least damaging. A sequential apply succeeds and leaves two Terraform
// states each believing they own one entity, so the first destroy removes it
// and the second reports drift or a 404. And when cleanup fails -- which it
// does; the CAT-2583 resurrection documented in husk_diagnostic.go survives a
// terraform destroy -- an operator sweeping the debris cannot tell which
// example produced the leftover URN, because more than one could have.
//
// These tests make that last question always answerable: given a URN, an id
// string, or a display name found on an instance, exactly one directory under
// examples/runnable can have produced it. Uniqueness is what the tests enforce;
// the naming convention that delivers it (the per-example slug in
// "Example conventions" in CLAUDE.md) is what makes the answer inferable by eye
// rather than by grep.
//
// Scope is examples/runnable only. The snippets under examples/resources and
// examples/data-sources are rendered into the registry documentation and are
// never applied by anything in this repository, so an id repeated between two
// of them costs nothing; several deliberately reuse a runnable example's
// vocabulary for continuity across pages.
//
// These tests parse HCL directly and need neither terraform nor a provider
// binary, so unlike the validate tests they run under a plain `go test ./...`.

// urnKey records how a resource type's configuration turns into a DataHub URN:
// the attribute holding the URN suffix, and the prefix it is appended to.
//
// A URN is namespaced by entity type, so comparing id strings alone both
// over-reports (a domain and a glossary node may share an id and never collide)
// and, historically, under-reports -- the prod-snowflake collision was missed
// for years because the search assumed the "tf-example-" prefix. Resolving to
// the full URN is the only comparison that answers the question being asked.
type urnKey struct {
	attr   string
	prefix string
}

// urnKeyedResources covers every resource type whose URN is derived from
// configuration. Verified against the URN prefix constants and ImportState
// parsers in each resource implementation.
var urnKeyedResources = map[string]urnKey{
	"datahub_action_pipeline":      {"action_id", "urn:li:dataHubAction:"},
	"datahub_connection":           {"connection_id", "urn:li:dataHubConnection:"},
	"datahub_corp_group":           {"group_id", "urn:li:corpGroup:"},
	"datahub_corp_user":            {"username", "urn:li:corpuser:"},
	"datahub_data_product":         {"data_product_id", "urn:li:dataProduct:"},
	"datahub_domain":               {"domain_id", "urn:li:domain:"},
	"datahub_glossary_node":        {"node_id", "urn:li:glossaryNode:"},
	"datahub_glossary_term":        {"term_id", "urn:li:glossaryTerm:"},
	"datahub_ingestion_source":     {"source_id", "urn:li:dataHubIngestionSource:"},
	"datahub_local_user_login":     {"username", "urn:li:corpuser:"},
	"datahub_ownership_type":       {"type_id", "urn:li:ownershipType:"},
	"datahub_page_module":          {"page_module_id", "urn:li:dataHubPageModule:"},
	"datahub_page_template":        {"page_template_id", "urn:li:dataHubPageTemplate:"},
	"datahub_policy":               {"policy_id", "urn:li:dataHubPolicy:"},
	"datahub_remote_executor_pool": {"pool_id", "urn:li:dataHubRemoteExecutorPool:"},
	"datahub_secret":               {"name", "urn:li:dataHubSecret:"},
	"datahub_service_account":      {"service_account_id", "urn:li:corpuser:service_"},
	"datahub_structured_property":  {"property_id", "urn:li:structuredProperty:"},
	"datahub_tag":                  {"tag_id", "urn:li:tag:"},
}

// urnlessResources are the resource types whose URN cannot be predicted from
// configuration, each with the reason. They are exempt from the uniqueness
// check rather than forgotten by it: TestEveryExampleResourceTypeIsClassified
// fails when a type appears in an example and is in neither map.
var urnlessResources = map[string]string{
	"datahub_corp_group_member":              "relationship edge, not an entity: destroy removes a membership from an aspect",
	"datahub_role_assignment":                "relationship edge, not an entity: destroy removes a role from an actor",
	"datahub_structured_property_assignment": "relationship edge, not an entity: destroy removes a property value from an entity",
	"datahub_assertion_assignment_rule":      "server assigns the assertion URNs the rule creates",
	"datahub_custom_assertion":               "server derives the assertion URN from the dataset and assertion shape",
	"datahub_data_contract":                  "server derives the contract URN from the dataset URN",
	"datahub_field_assertion":                "server derives the assertion URN from the dataset and assertion shape",
	"datahub_freshness_assertion":            "server derives the assertion URN from the dataset and assertion shape",
	"datahub_schema_assertion":               "server derives the assertion URN from the dataset and assertion shape",
	"datahub_sql_assertion":                  "server derives the assertion URN from the dataset and assertion shape",
	"datahub_volume_assertion":               "server derives the assertion URN from the dataset and assertion shape",
	"datahub_organization_display_preferences": "singleton: urn:li:globalSettings:0 by construction, " +
		"and only one example manages it",
}

// displayNameAttrs are the attributes carrying a human-readable label. These do
// not collide technically -- nothing keys off them -- but an operator sweeping
// the DataHub UI for debris reads the display name, not the URN, which is the
// exact situation these tests exist to serve. Two examples showing
// "TF Example - Finance" in a list leave that operator no better off than two
// examples sharing a URN would.
var displayNameAttrs = []string{"name", "display_name", "source_name", "full_name"}

// exampleIdentifier is one resolved identifier and where it came from.
type exampleIdentifier struct {
	dir   string // directory under examples/runnable
	file  string // path relative to examples/
	line  int
	value string
}

func (i exampleIdentifier) where() string {
	return fmt.Sprintf("examples/%s:%d", i.file, i.line)
}

// TestRunnableExampleURNsAreUnique is the load-bearing assertion: no DataHub URN
// is produced by two different runnable examples.
func TestRunnableExampleURNsAreUnique(t *testing.T) {
	t.Parallel()

	scan := scanRunnableExamples(t)

	assertOnePerDirectory(t, scan.urns, "URN",
		"Two runnable examples create the same DataHub entity. Applying both leaves "+
			"two Terraform states owning one entity, and an orphan left by either is "+
			"untraceable. Rename one, following the per-example slug convention in "+
			"the \"Example conventions\" section of CLAUDE.md.")

	if len(scan.urns) < 30 {
		t.Errorf("only %d URNs resolved from examples/runnable, expected at least 30; "+
			"the scan is probably not finding them", len(scan.urns))
	}
	t.Logf("%d distinct URNs resolved across %d runnable example directories",
		len(scan.urns), len(scan.dirs))
}

// TestRunnableExampleIDStringsAreUnique is the near-miss guard. Two examples
// sharing an id string under different entity types collide with nothing today,
// but the whole distance between that and a real collision is one rename of an
// entity type -- and that near-miss existed in this tree, with domain-simple and
// glossary-node-term-simple both saying "tf-example-accounting".
func TestRunnableExampleIDStringsAreUnique(t *testing.T) {
	t.Parallel()

	scan := scanRunnableExamples(t)

	assertOnePerDirectory(t, scan.ids, "identifier",
		"Two runnable examples use the same id string. They resolve to different "+
			"URNs today only because their entity types differ, which is one rename "+
			"away from a real collision, and a sweeper grepping for the string gets "+
			"two directories back. Add the owning example's slug to one of them.")
}

// TestRunnableExampleDisplayNamesAreUnique keeps the UI half of the debris sweep
// honest.
func TestRunnableExampleDisplayNamesAreUnique(t *testing.T) {
	t.Parallel()

	scan := scanRunnableExamples(t)

	assertOnePerDirectory(t, scan.displayNames, "display name",
		"Two runnable examples show the same label in the DataHub UI. Nothing "+
			"breaks, but an operator looking at a leftover entity cannot tell which "+
			"example created it, which is the case this convention exists to cover.")
}

// TestRegistrySnippetsDoNotClaimRunnableURNs stops a registry snippet from
// naming the same entity as a runnable example.
//
// Snippets are free to repeat each other -- several deliberately share a
// "data-platform" group or a "finance" domain so the pages read as one estate --
// because nothing in this repository applies them. What they must not do is
// reuse a URN a runnable example creates, because a user who copies the snippet
// off the registry page and applies it then owns an entity that
// examples/runnable also claims, and an orphan bearing that URN could have come
// from either. Exactly one such overlap existed
// (urn:li:dataHubAction:tf-example-dataplex-glossary-sync) and was resolved by
// renaming the runnable example, leaving the published page unchanged.
func TestRegistrySnippetsDoNotClaimRunnableURNs(t *testing.T) {
	t.Parallel()

	runnable := scanRunnableExamples(t)

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolving repository root: %v", err)
	}
	examplesDir := filepath.Join(root, "examples")

	var checked int
	for _, tier := range []string{"resources", "data-sources"} {
		entries, err := os.ReadDir(filepath.Join(examplesDir, tier))
		if err != nil {
			t.Fatalf("reading examples/%s: %v", tier, err)
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			bodies := parseDirectory(t, filepath.Join(examplesDir, tier, entry.Name()))
			ctx := evalContextFor(bodies)

			for path, body := range bodies {
				rel := mustRel(t, examplesDir, path)
				for _, block := range body.Blocks {
					if block.Type != "resource" || len(block.Labels) != 2 {
						continue
					}
					key, ok := urnKeyedResources[block.Labels[0]]
					if !ok {
						continue
					}
					value, line, ok := literalAttr(block, key.attr, ctx)
					if !ok {
						continue
					}
					checked++

					if hits := runnable.urns[key.prefix+value]; len(hits) > 0 {
						t.Errorf("registry snippet examples/%s:%d creates %s, which "+
							"examples/runnable/%s also creates. Rename the runnable "+
							"example's id -- the published snippet is what users read, "+
							"so it keeps the readable name.",
							rel, line, key.prefix+value, hits[0].dir)
					}
				}
			}
		}
	}

	if checked < 20 {
		t.Errorf("only %d snippet identifiers resolved, expected at least 20; the "+
			"scan is probably not finding them", checked)
	}
}

// TestEveryExampleResourceTypeIsClassified fails when a resource type used by an
// example is in neither urnKeyedResources nor urnlessResources.
//
// Without it the uniqueness tests degrade silently: a new resource type simply
// contributes no identifiers, every assertion keeps passing over the types it
// already knew about, and the new one is checked by nothing. That failure mode
// is not hypothetical here -- examples/provider was validated by nothing for
// exactly this reason until TestEveryExampleIsValidated reconciled the file
// count.
func TestEveryExampleResourceTypeIsClassified(t *testing.T) {
	t.Parallel()

	scan := scanRunnableExamples(t)

	for _, rt := range sortedKeys(scan.resourceTypes) {
		_, keyed := urnKeyedResources[rt]
		_, urnless := urnlessResources[rt]
		switch {
		case keyed && urnless:
			t.Errorf("%s is in both urnKeyedResources and urnlessResources; it must be "+
				"in exactly one", rt)
		case !keyed && !urnless:
			t.Errorf("%s is used by examples/runnable but classified by neither "+
				"urnKeyedResources nor urnlessResources, so none of its identifiers "+
				"are checked for uniqueness. Add the attribute and URN prefix to "+
				"urnKeyedResources, or add it to urnlessResources with the reason its "+
				"URN cannot be derived from configuration.", rt)
		}
	}

	if len(scan.resourceTypes) < 15 {
		t.Errorf("only %d resource types found in examples/runnable, expected at "+
			"least 15; the scan is probably not finding them", len(scan.resourceTypes))
	}
}

// assertOnePerDirectory fails for every value claimed by more than one directory.
func assertOnePerDirectory(t *testing.T, index map[string][]exampleIdentifier, kind, advice string) {
	t.Helper()

	for _, value := range sortedKeys(index) {
		occurrences := index[value]

		dirs := map[string]bool{}
		for _, o := range occurrences {
			dirs[o.dir] = true
		}
		if len(dirs) < 2 {
			continue
		}

		locations := make([]string, 0, len(occurrences))
		for _, o := range occurrences {
			locations = append(locations, o.where())
		}
		sort.Strings(locations)
		t.Errorf("%s %q is claimed by %d runnable examples (%s):\n  %s\n\n%s",
			kind, value, len(dirs), strings.Join(sortedKeys(dirs), ", "),
			strings.Join(locations, "\n  "), advice)
	}
}

// exampleScan is everything the uniqueness tests read out of examples/runnable.
type exampleScan struct {
	urns          map[string][]exampleIdentifier
	ids           map[string][]exampleIdentifier
	displayNames  map[string][]exampleIdentifier
	resourceTypes map[string]bool
	dirs          map[string]bool
}

// scanRunnableExamples parses every .tf file under examples/runnable and resolves
// each managed resource's identifiers.
//
// Only `resource` blocks are read. A `data` block naming an id refers to an
// entity somebody else created -- examples/runnable/ingestion-source-lookup
// reads the built-in "datahub-gc" source -- and counting those would report
// collisions between examples that share nothing but a lookup.
//
// Values that cannot be resolved statically are skipped, not failed. The
// financial-services example derives thousands of ids from a generated JSON file
// through for_each, so its identifiers exist only at plan time; they are
// namespaced by a "tf-example-fibo-" / "tf-fibo-" prefix no other example uses.
func scanRunnableExamples(t *testing.T) exampleScan {
	t.Helper()

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolving repository root: %v", err)
	}
	examplesDir := filepath.Join(root, "examples")
	runnableDir := filepath.Join(examplesDir, "runnable")

	entries, err := os.ReadDir(runnableDir)
	if err != nil {
		t.Fatalf("reading examples/runnable: %v", err)
	}

	scan := exampleScan{
		urns:          map[string][]exampleIdentifier{},
		ids:           map[string][]exampleIdentifier{},
		displayNames:  map[string][]exampleIdentifier{},
		resourceTypes: map[string]bool{},
		dirs:          map[string]bool{},
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := entry.Name()
		scan.dirs[dir] = true

		bodies := parseDirectory(t, filepath.Join(runnableDir, dir))
		ctx := evalContextFor(bodies)

		for path, body := range bodies {
			rel := mustRel(t, examplesDir, path)

			for _, block := range body.Blocks {
				if block.Type != "resource" || len(block.Labels) != 2 {
					continue
				}
				resourceType := block.Labels[0]
				if !strings.HasPrefix(resourceType, "datahub_") {
					continue
				}
				scan.resourceTypes[resourceType] = true

				if key, ok := urnKeyedResources[resourceType]; ok {
					if value, line, ok := literalAttr(block, key.attr, ctx); ok {
						id := exampleIdentifier{dir: dir, file: rel, line: line, value: value}
						scan.urns[key.prefix+value] = append(scan.urns[key.prefix+value], id)
						scan.ids[value] = append(scan.ids[value], id)
					}
				}

				for _, attr := range displayNameAttrs {
					if value, line, ok := literalAttr(block, attr, ctx); ok {
						scan.displayNames[value] = append(scan.displayNames[value],
							exampleIdentifier{dir: dir, file: rel, line: line, value: value})
					}
				}
			}
		}
	}

	return scan
}

// parseDirectory parses every .tf file in dir, keyed by path.
func parseDirectory(t *testing.T, dir string) map[string]*hclsyntax.Body {
	t.Helper()

	matches, err := filepath.Glob(filepath.Join(dir, "*.tf"))
	if err != nil {
		t.Fatalf("globbing %s: %v", dir, err)
	}

	parser := hclparse.NewParser()
	bodies := make(map[string]*hclsyntax.Body, len(matches))
	for _, path := range matches {
		file, diags := parser.ParseHCLFile(path)
		if diags.HasErrors() {
			t.Fatalf("parsing %s: %v", path, diags)
		}
		body, ok := file.Body.(*hclsyntax.Body)
		if !ok {
			t.Fatalf("parsing %s: not native HCL syntax", path)
		}
		bodies[path] = body
	}
	return bodies
}

// evalContextFor builds the scope an example's identifiers are written against:
// its own variable defaults and locals. Both matter -- connection-snowflake sets
// its connection_id from a variable default and data-product-simple sets its
// marker tag id from a local, so a scan that read only literals would miss the
// prod-snowflake collision entirely, which is how it stayed unnoticed.
func evalContextFor(bodies map[string]*hclsyntax.Body) *hcl.EvalContext {
	ctx := &hcl.EvalContext{Variables: map[string]cty.Value{}}

	vars := map[string]cty.Value{}
	for _, body := range bodies {
		for _, block := range body.Blocks {
			if block.Type != "variable" || len(block.Labels) != 1 {
				continue
			}
			def, ok := block.Body.Attributes["default"]
			if !ok {
				continue
			}
			if value, diags := def.Expr.Value(nil); !diags.HasErrors() {
				vars[block.Labels[0]] = value
			}
		}
	}
	ctx.Variables["var"] = cty.ObjectVal(vars)

	// Locals may reference variables and each other. Two passes settle every
	// chain in the current tree; an unresolvable local simply stays absent and
	// the identifiers depending on it are skipped.
	locals := map[string]cty.Value{}
	for pass := 0; pass < 2; pass++ {
		ctx.Variables["local"] = cty.ObjectVal(locals)
		for _, body := range bodies {
			for _, block := range body.Blocks {
				if block.Type != "locals" {
					continue
				}
				for name, attr := range block.Body.Attributes {
					if value, diags := attr.Expr.Value(ctx); !diags.HasErrors() {
						locals[name] = value
					}
				}
			}
		}
	}
	ctx.Variables["local"] = cty.ObjectVal(locals)

	return ctx
}

// literalAttr resolves one attribute to a string, reporting whether it resolved.
// An attribute referencing a resource, each.value or a function does not resolve
// and is reported as absent.
func literalAttr(block *hclsyntax.Block, name string, ctx *hcl.EvalContext) (string, int, bool) {
	attr, ok := block.Body.Attributes[name]
	if !ok {
		return "", 0, false
	}
	value, diags := attr.Expr.Value(ctx)
	if diags.HasErrors() || value.IsNull() || !value.IsKnown() || value.Type() != cty.String {
		return "", 0, false
	}
	return value.AsString(), attr.SrcRange.Start.Line, true
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
