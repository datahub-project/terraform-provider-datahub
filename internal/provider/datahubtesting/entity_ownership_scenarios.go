// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahubtesting

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/hashicorp/terraform-plugin-testing/config"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
)

const (
	// entityOwnershipAddr is the address of the resource under test in every
	// scenario here.
	entityOwnershipAddr = "datahub_entity_ownership.test"

	// technicalOwnerTypeURN is one of the ownership type entities DataHub
	// bootstraps for the legacy Owner.type enum. Used where a scenario needs an
	// ownership type it did not create.
	technicalOwnerTypeURN = "urn:li:ownershipType:__system__technical_owner"

	// seededOwnerURN stands in for a principal somebody assigned in the DataHub
	// UI. It is one of the mock's pre-seeded users, and on a live instance the
	// built-in admin, so no scenario has to create it.
	seededOwnerURN = "urn:li:corpuser:datahub"

	// secondOwnerURN is the mock's other pre-seeded user.
	secondOwnerURN = "urn:li:corpuser:testuser"
)

// ownerEntry renders one element of the owner set attribute.
func ownerEntry(ownerExpr, typeExpr string) string {
	return fmt.Sprintf("    { owner_urn = %s, ownership_type_urn = %s },\n", ownerExpr, typeExpr)
}

// ownerCheck builds the state check for one expected owner set element.
func ownerCheck(ownerURN, typeURN string) knownvalue.Check {
	return knownvalue.ObjectExact(map[string]knownvalue.Check{
		"owner_urn":          knownvalue.StringExact(ownerURN),
		"ownership_type_urn": knownvalue.StringExact(typeURN),
	})
}

// entityOwnershipFixture emits the entities every ownership scenario needs: a
// glossary term to own, a group to own it, and two custom ownership types.
//
// A glossary term is the target because it is the shape of the real use case
// this resource was built for -- adopting a glossary somebody has already been
// curating -- and because glossaryTerm carries the ownership aspect on both OSS
// and Cloud.
func entityOwnershipFixture(termID, groupID, typeIDA, typeIDB string) string {
	return fmt.Sprintf(`
resource "datahub_glossary_term" "target" {
  term_id = %q
  name    = "Entity Ownership Target"
}

resource "datahub_corp_group" "stewards" {
  group_id = %q
  name     = "Entity Ownership Stewards"
}

resource "datahub_ownership_type" "a" {
  type_id = %q
  name    = "Entity Ownership Type A"
}

resource "datahub_ownership_type" "b" {
  type_id = %q
  name    = "Entity Ownership Type B"
}
`, termID, groupID, typeIDA, typeIDB)
}

// EntityOwnershipLifecycleSteps is the main lifecycle: create, update, import.
//
// The create step covers the three repetition shapes that are all legitimate
// and all easy to break with a well-meaning validator:
//
//   - several owners on one entity across several ownership types;
//   - TWO owners sharing ONE ownership type (in the dataset that drove this
//     work, 92 of 1,782 owner cells hold two owners in the same role, so this
//     is the primary use case rather than an edge case);
//   - ONE owner holding TWO ownership types.
//
// The update step adds one pair, removes one pair and keeps two, in a single
// apply, and asserts the action is an in-place update rather than a replace.
func EntityOwnershipLifecycleSteps(termID, groupID, typeIDA, typeIDB string) []resource.TestStep {
	termURN := "urn:li:glossaryTerm:" + termID
	groupURN := "urn:li:corpGroup:" + groupID
	typeAURN := "urn:li:ownershipType:" + typeIDA
	typeBURN := "urn:li:ownershipType:" + typeIDB

	cfg := func(owners string) string {
		return providerBlock +
			entityOwnershipFixture(termID, groupID, typeIDA, typeIDB) +
			"\nresource \"datahub_entity_ownership\" \"test\" {\n" +
			"  entity_urn = datahub_glossary_term.target.urn\n\n" +
			"  owner = [\n" + owners + "  ]\n}\n"
	}

	initial := ownerEntry("datahub_corp_group.stewards.urn", "datahub_ownership_type.a.urn") +
		ownerEntry(fmt.Sprintf("%q", seededOwnerURN), "datahub_ownership_type.a.urn") +
		ownerEntry("datahub_corp_group.stewards.urn", "datahub_ownership_type.b.urn")

	// Adds (testuser, B), removes (group, B), keeps (group, A) and (datahub, A).
	updated := ownerEntry("datahub_corp_group.stewards.urn", "datahub_ownership_type.a.urn") +
		ownerEntry(fmt.Sprintf("%q", seededOwnerURN), "datahub_ownership_type.a.urn") +
		ownerEntry(fmt.Sprintf("%q", secondOwnerURN), "datahub_ownership_type.b.urn")

	return []resource.TestStep{
		{
			Config: cfg(initial),
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(entityOwnershipAddr, tfjsonpath.New("entity_urn"), knownvalue.StringExact(termURN)),
				statecheck.ExpectKnownValue(entityOwnershipAddr, tfjsonpath.New("id"), knownvalue.StringExact(termURN)),
				statecheck.ExpectKnownValue(entityOwnershipAddr, tfjsonpath.New("owner"), knownvalue.SetExact([]knownvalue.Check{
					ownerCheck(groupURN, typeAURN),
					ownerCheck(seededOwnerURN, typeAURN),
					ownerCheck(groupURN, typeBURN),
				})),
			},
		},
		{
			Config: cfg(updated),
			ConfigPlanChecks: resource.ConfigPlanChecks{
				PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(entityOwnershipAddr, plancheck.ResourceActionUpdate),
				},
			},
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(entityOwnershipAddr, tfjsonpath.New("owner"), knownvalue.SetExact([]knownvalue.Check{
					ownerCheck(groupURN, typeAURN),
					ownerCheck(seededOwnerURN, typeAURN),
					ownerCheck(secondOwnerURN, typeBURN),
				})),
			},
			Check: resource.ComposeAggregateTestCheckFunc(
				// The removed pair is gone from the server, and only that pair.
				CheckOwnerAbsent(termURN, groupURN, typeBURN),
				CheckOwnerPresent(termURN, groupURN, typeAURN),
			),
		},
		{
			// Import by entity URN. The entity carries three owners across two
			// ownership types at this point, all of them declared, so the
			// adopt-everything import must reproduce the state exactly.
			ResourceName:      entityOwnershipAddr,
			ImportState:       true,
			ImportStateVerify: true,
			ImportStateId:     termURN,
		},
	}
}

// EntityOwnershipDuplicateEntrySteps documents what an exactly duplicated owner
// entry does, rather than leaving it to be discovered.
//
// Two entries with the same owner_urn AND the same ownership_type_urn are the
// same set element, so Terraform collapses them in the configuration before the
// provider is called at all: the result is one owner edge, no error, and no
// permanent diff. Nothing in the provider enforces this, and nothing needs to --
// which is exactly why it is worth a test. A validator rejecting duplicates
// would be the wrong fix and would have to distinguish this case from the two
// legitimate repetition shapes in EntityOwnershipLifecycleSteps.
//
// The second step re-applies the identical configuration and asserts an empty
// plan, because "collapses to one" would be a hollow claim if the collapsed
// value then disagreed with what was read back.
func EntityOwnershipDuplicateEntrySteps(termID, groupID, typeIDA, typeIDB string) []resource.TestStep {
	groupURN := "urn:li:corpGroup:" + groupID
	typeAURN := "urn:li:ownershipType:" + typeIDA

	cfg := providerBlock +
		entityOwnershipFixture(termID, groupID, typeIDA, typeIDB) + `
resource "datahub_entity_ownership" "test" {
  entity_urn = datahub_glossary_term.target.urn

  owner = [
    { owner_urn = datahub_corp_group.stewards.urn, ownership_type_urn = datahub_ownership_type.a.urn },
    { owner_urn = datahub_corp_group.stewards.urn, ownership_type_urn = datahub_ownership_type.a.urn },
  ]
}
`

	return []resource.TestStep{
		{
			Config: cfg,
			ConfigStateChecks: []statecheck.StateCheck{
				// One element, not two: the duplicate never reaches the provider.
				statecheck.ExpectKnownValue(entityOwnershipAddr, tfjsonpath.New("owner"), knownvalue.SetSizeExact(1)),
				statecheck.ExpectKnownValue(entityOwnershipAddr, tfjsonpath.New("owner"), knownvalue.SetExact([]knownvalue.Check{
					ownerCheck(groupURN, typeAURN),
				})),
			},
			Check: CheckOwnerCount(func() string { return "urn:li:glossaryTerm:" + termID }(), 1),
		},
		{
			Config:             cfg,
			PlanOnly:           true,
			ExpectNonEmptyPlan: false,
		},
	}
}

// EntityOwnershipOutOfBandSteps is the merge contract, and the one scenario that
// cannot be written without seeding: there is no other way to have an owner
// present that Terraform never declared.
//
// An owner is injected straight into the stored ownership aspect, standing in
// for somebody assigning it in the DataHub UI. It must then survive create,
// survive an update that adds and removes declared pairs, and survive the
// destroy of the resource itself. The destroy is driven by removing the resource
// from the configuration rather than by the test's own teardown, so the
// surviving owner can still be read back afterwards -- a teardown destroy takes
// the glossary term with it and leaves nothing to ask about.
//
// Mock-only: a live target exposes no /test-control/seed-owner.
func EntityOwnershipOutOfBandSteps(termID, groupID, typeIDA, typeIDB string) []resource.TestStep {
	termURN := "urn:li:glossaryTerm:" + termID
	groupURN := "urn:li:corpGroup:" + groupID
	typeAURN := "urn:li:ownershipType:" + typeIDA
	typeBURN := "urn:li:ownershipType:" + typeIDB

	fixture := entityOwnershipFixture(termID, groupID, typeIDA, typeIDB)

	ownership := func(owners string) string {
		return "\nresource \"datahub_entity_ownership\" \"test\" {\n" +
			"  entity_urn = datahub_glossary_term.target.urn\n\n" +
			"  owner = [\n" + owners + "  ]\n}\n"
	}

	declaredOne := ownerEntry("datahub_corp_group.stewards.urn", "datahub_ownership_type.a.urn")
	declaredTwo := ownerEntry("datahub_corp_group.stewards.urn", "datahub_ownership_type.b.urn") +
		ownerEntry(fmt.Sprintf("%q", secondOwnerURN), "datahub_ownership_type.b.urn")

	seededSurvives := CheckOwnerPresent(termURN, seededOwnerURN, technicalOwnerTypeURN)

	return []resource.TestStep{
		{
			PreConfig: func() {
				SeedEntityOwner(os.Getenv("DATAHUB_GMS_URL"), termURN, seededOwnerURN, technicalOwnerTypeURN)
			},
			Config: providerBlock + fixture + ownership(declaredOne),
			ConfigStateChecks: []statecheck.StateCheck{
				// State holds the declared pair only. If the resource adopted
				// what it found, the seeded owner would appear here and the next
				// destroy would delete somebody else's assignment.
				statecheck.ExpectKnownValue(entityOwnershipAddr, tfjsonpath.New("owner"), knownvalue.SetExact([]knownvalue.Check{
					ownerCheck(groupURN, typeAURN),
				})),
			},
			Check: resource.ComposeAggregateTestCheckFunc(seededSurvives),
		},
		{
			// Add two pairs, remove the one declared before. The seeded owner is
			// touched by neither.
			Config: providerBlock + fixture + ownership(declaredTwo),
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(entityOwnershipAddr, tfjsonpath.New("owner"), knownvalue.SetExact([]knownvalue.Check{
					ownerCheck(groupURN, typeBURN),
					ownerCheck(secondOwnerURN, typeBURN),
				})),
			},
			Check: resource.ComposeAggregateTestCheckFunc(
				seededSurvives,
				CheckOwnerAbsent(termURN, groupURN, typeAURN),
			),
		},
		{
			// Destroy just the ownership resource by dropping it from the
			// configuration. Its declared pairs go; the seeded one stays.
			Config: providerBlock + fixture,
			Check: resource.ComposeAggregateTestCheckFunc(
				seededSurvives,
				CheckOwnerAbsent(termURN, groupURN, typeBURN),
				CheckOwnerAbsent(termURN, secondOwnerURN, typeBURN),
				CheckOwnerCount(termURN, 1),
			),
		},
	}
}

// EntityOwnershipPinnedRemovalSteps covers the optional-ownershipTypeUrn trap.
//
// One owner holds two ownership types; the update removes one of them. DataHub's
// RemoveOwnerInput.ownershipTypeUrn is optional, and OwnerServiceUtils.isOwnerEqual
// short-circuits to true for every edge whose owner URN matches the moment the
// requested type is null -- so a removeOwner that omitted the type would take the
// second ownership type with it. The mock reproduces that behaviour exactly,
// which is what gives this test teeth.
//
// It would fail two ways if the provider stopped pinning the type: the explicit
// server check below, and the framework's post-apply plan, which would be
// non-empty because the surviving declared pair would have been read back absent.
func EntityOwnershipPinnedRemovalSteps(termID, groupID, typeIDA, typeIDB string) []resource.TestStep {
	termURN := "urn:li:glossaryTerm:" + termID
	groupURN := "urn:li:corpGroup:" + groupID
	typeAURN := "urn:li:ownershipType:" + typeIDA
	typeBURN := "urn:li:ownershipType:" + typeIDB

	fixture := entityOwnershipFixture(termID, groupID, typeIDA, typeIDB)
	ownership := func(owners string) string {
		return "\nresource \"datahub_entity_ownership\" \"test\" {\n" +
			"  entity_urn = datahub_glossary_term.target.urn\n\n" +
			"  owner = [\n" + owners + "  ]\n}\n"
	}

	both := ownerEntry("datahub_corp_group.stewards.urn", "datahub_ownership_type.a.urn") +
		ownerEntry("datahub_corp_group.stewards.urn", "datahub_ownership_type.b.urn")
	onlyB := ownerEntry("datahub_corp_group.stewards.urn", "datahub_ownership_type.b.urn")

	return []resource.TestStep{
		{
			Config: providerBlock + fixture + ownership(both),
			Check: resource.ComposeAggregateTestCheckFunc(
				CheckOwnerPresent(termURN, groupURN, typeAURN),
				CheckOwnerPresent(termURN, groupURN, typeBURN),
			),
		},
		{
			Config: providerBlock + fixture + ownership(onlyB),
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(entityOwnershipAddr, tfjsonpath.New("owner"), knownvalue.SetExact([]knownvalue.Check{
					ownerCheck(groupURN, typeBURN),
				})),
			},
			Check: resource.ComposeAggregateTestCheckFunc(
				CheckOwnerAbsent(termURN, groupURN, typeAURN),
				// The whole point: the same owner's other ownership type must
				// still be there.
				CheckOwnerPresent(termURN, groupURN, typeBURN),
				CheckOwnerCount(termURN, 1),
			),
		},
	}
}

// EntityOwnershipReplaceSteps moves the assignment to a different entity, which
// entity_urn's RequiresReplace turns into a destroy-and-create.
//
// Worth its own scenario for two reasons. The id attribute is the entity URN and
// carries UseStateForUnknown, so a replacement that planned id from the OLD
// state would fail the apply with "Provider produced inconsistent result after
// apply" -- the plan modifier is only correct because Terraform re-plans the
// create half against a null prior state. And the destroy half must still remove
// the pairs from the first entity, which no state check can observe once the
// resource address has been reused.
func EntityOwnershipReplaceSteps(termID, groupID, typeIDA, typeIDB string) []resource.TestStep {
	firstURN := "urn:li:glossaryTerm:" + termID
	secondURN := "urn:li:glossaryTerm:" + termID + "-moved"
	groupURN := "urn:li:corpGroup:" + groupID
	typeAURN := "urn:li:ownershipType:" + typeIDA

	fixture := entityOwnershipFixture(termID, groupID, typeIDA, typeIDB) + fmt.Sprintf(`
resource "datahub_glossary_term" "other" {
  term_id = "%s-moved"
  name    = "Entity Ownership Other Target"
}
`, termID)

	cfg := func(termRef string) string {
		return providerBlock + fixture +
			"\nresource \"datahub_entity_ownership\" \"test\" {\n" +
			"  entity_urn = " + termRef + ".urn\n\n" +
			"  owner = [\n" +
			ownerEntry("datahub_corp_group.stewards.urn", "datahub_ownership_type.a.urn") +
			"  ]\n}\n"
	}

	return []resource.TestStep{
		{
			Config: cfg("datahub_glossary_term.target"),
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(entityOwnershipAddr, tfjsonpath.New("id"), knownvalue.StringExact(firstURN)),
			},
		},
		{
			Config: cfg("datahub_glossary_term.other"),
			ConfigPlanChecks: resource.ConfigPlanChecks{
				PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(entityOwnershipAddr, plancheck.ResourceActionDestroyBeforeCreate),
				},
			},
			ConfigStateChecks: []statecheck.StateCheck{
				// id tracks the new entity, not the one held in the old state.
				statecheck.ExpectKnownValue(entityOwnershipAddr, tfjsonpath.New("id"), knownvalue.StringExact(secondURN)),
				statecheck.ExpectKnownValue(entityOwnershipAddr, tfjsonpath.New("entity_urn"), knownvalue.StringExact(secondURN)),
			},
			Check: resource.ComposeAggregateTestCheckFunc(
				CheckOwnerPresent(secondURN, groupURN, typeAURN),
				// The destroy half ran: the pair is gone from the entity it left,
				// which still exists.
				CheckOwnerAbsent(firstURN, groupURN, typeAURN),
				CheckOwnerCount(firstURN, 0),
			),
		},
	}
}

// EntityOwnershipFromVariableSteps drives the owner set from an input variable
// rather than from literals.
//
// This is mandatory for any resource with a nested attribute, and the reason is
// that a literal resolves at plan time and never produces an unknown value: the
// whole suite can pass while every module consumer is broken with "Received
// unknown value, however the target type cannot handle unknown values". Two
// resources shipped with exactly that defect before the rule was written down.
//
// Two shapes are exercised, because they fail differently. The variable makes
// the whole set unknown during the validate walk (root module inputs are
// unknown regardless of a default), and terraform_data.seed.output makes the
// element attribute values unknown at plan time -- neither exists until apply.
func EntityOwnershipFromVariableSteps(termID, groupID, typeIDA, typeIDB string) []resource.TestStep {
	groupURN := "urn:li:corpGroup:" + groupID
	typeAURN := "urn:li:ownershipType:" + typeIDA

	fromVariable := providerBlock + `
variable "owners" {
  type = set(object({
    owner_urn          = string
    ownership_type_urn = string
  }))
}
` + entityOwnershipFixture(termID, groupID, typeIDA, typeIDB) + `
resource "datahub_entity_ownership" "test" {
  entity_urn = datahub_glossary_term.target.urn
  owner      = var.owners
}
`

	fromComputed := providerBlock +
		entityOwnershipFixture(termID, groupID, typeIDA, typeIDB) + `
# terraform_data.output is not resolvable until apply, so every attribute fed
# from it is unknown during the plan. Unlike a variable this needs no test
# harness support, which is what makes it the better regression guard.
resource "terraform_data" "seed" {
  input = {
    owner_urn          = datahub_corp_group.stewards.urn
    ownership_type_urn = datahub_ownership_type.a.urn
  }
}

resource "datahub_entity_ownership" "test" {
  entity_urn = datahub_glossary_term.target.urn

  owner = [
    {
      owner_urn          = terraform_data.seed.output.owner_urn
      ownership_type_urn = terraform_data.seed.output.ownership_type_urn
    },
  ]
}
`

	return []resource.TestStep{
		{
			Config: fromVariable,
			ConfigVariables: config.Variables{
				"owners": config.SetVariable(
					config.ObjectVariable(map[string]config.Variable{
						"owner_urn":          config.StringVariable(seededOwnerURN),
						"ownership_type_urn": config.StringVariable(typeAURN),
					}),
					config.ObjectVariable(map[string]config.Variable{
						"owner_urn":          config.StringVariable(secondOwnerURN),
						"ownership_type_urn": config.StringVariable(typeAURN),
					}),
				),
			},
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(entityOwnershipAddr, tfjsonpath.New("owner"), knownvalue.SetExact([]knownvalue.Check{
					ownerCheck(seededOwnerURN, typeAURN),
					ownerCheck(secondOwnerURN, typeAURN),
				})),
			},
		},
		{
			Config: fromComputed,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(entityOwnershipAddr, tfjsonpath.New("owner"), knownvalue.SetExact([]knownvalue.Check{
					ownerCheck(groupURN, typeAURN),
				})),
			},
		},
	}
}

// EntityOwnershipAllTargetsSteps assigns an owner to one entity of each of the
// eight accepted target types in a single apply.
//
// The allowlist is the only thing standing between a practitioner and either a
// silent no-op (an entity type without the ownership aspect) or an overwrite of
// hand-curated asset metadata, so "the eight we say we accept are accepted" is
// worth asserting rather than assuming. Written as one config so a type
// accidentally dropped from the allowlist fails here rather than nowhere.
func EntityOwnershipAllTargetsSteps(prefix string) []resource.TestStep {
	typeURN := "urn:li:ownershipType:" + prefix + "-type"

	cfg := providerBlock + fmt.Sprintf(`
resource "datahub_ownership_type" "t" {
  type_id = "%[1]s-type"
  name    = "All Targets Ownership Type"
}

resource "datahub_domain" "d" {
  domain_id = "%[1]s-domain"
  name      = "All Targets Domain"
}

resource "datahub_glossary_node" "gn" {
  node_id = "%[1]s-node"
  name    = "All Targets Node"
}

resource "datahub_glossary_term" "gt" {
  term_id = "%[1]s-term"
  name    = "All Targets Term"
}

resource "datahub_data_product" "dp" {
  data_product_id = "%[1]s-product"
  name            = "All Targets Product"
}

resource "datahub_corp_group" "cg" {
  group_id = "%[1]s-group"
  name     = "All Targets Group"
}

resource "datahub_tag" "tg" {
  tag_id = "%[1]s-tag"
  name   = "All Targets Tag"
}

resource "datahub_form" "fm" {
  form_id = "%[1]s-form"
  name    = "All Targets Form"
}

resource "datahub_ingestion_source" "is" {
  source_id   = "%[1]s-source"
  source_name = "All Targets Source"
  recipe      = jsonencode({ source = { type = "demo-data", config = {} } })
}

# One resource per target type rather than a for_each: the acceptance test
# framework's state shim rejects string-indexed resource addresses, so a
# for_each here would fail before any assertion ran.
resource "datahub_entity_ownership" "on_domain" {
  entity_urn = datahub_domain.d.urn
  owner      = [{ owner_urn = %[2]q, ownership_type_urn = datahub_ownership_type.t.urn }]
}

resource "datahub_entity_ownership" "on_glossary_node" {
  entity_urn = datahub_glossary_node.gn.urn
  owner      = [{ owner_urn = %[2]q, ownership_type_urn = datahub_ownership_type.t.urn }]
}

resource "datahub_entity_ownership" "on_glossary_term" {
  entity_urn = datahub_glossary_term.gt.urn
  owner      = [{ owner_urn = %[2]q, ownership_type_urn = datahub_ownership_type.t.urn }]
}

resource "datahub_entity_ownership" "on_data_product" {
  entity_urn = datahub_data_product.dp.urn
  owner      = [{ owner_urn = %[2]q, ownership_type_urn = datahub_ownership_type.t.urn }]
}

resource "datahub_entity_ownership" "on_corp_group" {
  entity_urn = datahub_corp_group.cg.urn
  owner      = [{ owner_urn = %[2]q, ownership_type_urn = datahub_ownership_type.t.urn }]
}

resource "datahub_entity_ownership" "on_tag" {
  entity_urn = datahub_tag.tg.urn
  owner      = [{ owner_urn = %[2]q, ownership_type_urn = datahub_ownership_type.t.urn }]
}

resource "datahub_entity_ownership" "on_form" {
  entity_urn = datahub_form.fm.urn
  owner      = [{ owner_urn = %[2]q, ownership_type_urn = datahub_ownership_type.t.urn }]
}

resource "datahub_entity_ownership" "on_ingestion_source" {
  entity_urn = "urn:li:dataHubIngestionSource:${datahub_ingestion_source.is.source_id}"
  owner      = [{ owner_urn = %[2]q, ownership_type_urn = datahub_ownership_type.t.urn }]
}
`, prefix, seededOwnerURN)

	checks := make([]resource.TestCheckFunc, 0, 8)
	for _, urn := range []string{
		"urn:li:domain:" + prefix + "-domain",
		"urn:li:glossaryNode:" + prefix + "-node",
		"urn:li:glossaryTerm:" + prefix + "-term",
		"urn:li:dataProduct:" + prefix + "-product",
		"urn:li:corpGroup:" + prefix + "-group",
		"urn:li:tag:" + prefix + "-tag",
		"urn:li:form:" + prefix + "-form",
		"urn:li:dataHubIngestionSource:" + prefix + "-source",
	} {
		checks = append(checks, CheckOwnerPresent(urn, seededOwnerURN, typeURN))
	}

	return []resource.TestStep{
		{
			Config: cfg,
			Check:  resource.ComposeAggregateTestCheckFunc(checks...),
		},
	}
}

// EntityOwnershipRejectedTargetSteps asserts a rejected target entity type
// fails at plan time with a message that says why.
//
// The three reasons need three messages, because they call for three different
// responses: a data asset should be enriched in the UI, an entity type without
// the aspect cannot be made to work at all, and an unrecognised type is a typo.
// Asserting only "it was rejected" would let the messages collapse into one.
func EntityOwnershipRejectedTargetSteps(entityURN string, wantError *regexp.Regexp) []resource.TestStep {
	return []resource.TestStep{
		{
			Config: providerBlock + fmt.Sprintf(`
resource "datahub_entity_ownership" "test" {
  entity_urn = %q

  owner = [
    { owner_urn = %q, ownership_type_urn = %q },
  ]
}
`, entityURN, seededOwnerURN, technicalOwnerTypeURN),
			ExpectError: wantError,
		},
	}
}

// EntityOwnershipInvalidOwnerURNSteps asserts an owner URN that is neither a
// corp user nor a corp group is rejected at plan time. The provider derives
// DataHub's required OwnerEntityType from this URN, so an unrecognised kind has
// no derivable answer and cannot be sent at all.
func EntityOwnershipInvalidOwnerURNSteps(termID string) []resource.TestStep {
	return []resource.TestStep{
		{
			Config: providerBlock + fmt.Sprintf(`
resource "datahub_entity_ownership" "test" {
  entity_urn = "urn:li:glossaryTerm:%s"

  owner = [
    { owner_urn = "urn:li:dataPlatform:snowflake", ownership_type_urn = %q },
  ]
}
`, termID, technicalOwnerTypeURN),
			ExpectError: regexp.MustCompile(`(?s)Invalid owner URN`),
		},
	}
}

// EntityOwnershipAbsentAspectSteps proves the read path tolerates an entity
// whose ownership aspect is absent, which is how an entity with no owners
// actually reads: the aspect key is missing from the JSON entirely rather than
// present with an empty array.
//
// Every declared owner is dropped out of band between the two steps, leaving the
// entity in exactly that state. Two wrong behaviours are distinguished by the
// expected plan action. If Read errored, the step fails outright. If Read treated
// the absent aspect as a missing resource and called RemoveResource, the plan
// would be a Create; an Update is the only outcome consistent with "the entity
// is there, it just has no owners, so re-add mine".
//
// Mock-only: a live target exposes no /test-control/drop-owners.
func EntityOwnershipAbsentAspectSteps(termID, groupID, typeIDA, typeIDB string) []resource.TestStep {
	termURN := "urn:li:glossaryTerm:" + termID
	groupURN := "urn:li:corpGroup:" + groupID
	typeAURN := "urn:li:ownershipType:" + typeIDA

	cfg := providerBlock +
		entityOwnershipFixture(termID, groupID, typeIDA, typeIDB) +
		"\nresource \"datahub_entity_ownership\" \"test\" {\n" +
		"  entity_urn = datahub_glossary_term.target.urn\n\n" +
		"  owner = [\n" +
		ownerEntry("datahub_corp_group.stewards.urn", "datahub_ownership_type.a.urn") +
		"  ]\n}\n"

	return []resource.TestStep{
		{Config: cfg},
		{
			PreConfig: func() {
				DropEntityOwners(os.Getenv("DATAHUB_GMS_URL"), termURN)
			},
			Config: cfg,
			ConfigPlanChecks: resource.ConfigPlanChecks{
				PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(entityOwnershipAddr, plancheck.ResourceActionUpdate),
				},
			},
			Check: resource.ComposeAggregateTestCheckFunc(
				CheckOwnerPresent(termURN, groupURN, typeAURN),
			),
		},
	}
}

// EntityOwnershipLegacyTypeImportSteps imports an entity whose owners were
// written straight to the aspect, one of them carrying only the legacy
// Owner.type enum and no typeUrn -- the shape an SDK-written aspect, or one
// written before typeUrn existed, still has on disk.
//
// The provider reproduces the server's own mapping (OwnerUtils.mapOwnershipTypeToEntity:
// __system__ plus the lowercased enum) so such an edge resolves to the same pair
// the configuration would declare. Without that, the pair reads as absent and
// the resource shows a diff it can never settle.
//
// Mock-only: it needs /test-control/seed-owner.
func EntityOwnershipLegacyTypeImportSteps(termID, groupID, typeIDA, typeIDB string) []resource.TestStep {
	termURN := "urn:li:glossaryTerm:" + termID

	cfg := providerBlock +
		entityOwnershipFixture(termID, groupID, typeIDA, typeIDB) + fmt.Sprintf(`
resource "datahub_entity_ownership" "test" {
  entity_urn = datahub_glossary_term.target.urn

  owner = [
    { owner_urn = %q, ownership_type_urn = %q },
  ]
}
`, seededOwnerURN, technicalOwnerTypeURN)

	return []resource.TestStep{
		{
			// The entity has to exist before it can be imported, and an import
			// step does not apply its configuration. So the fixture is applied
			// first, WITHOUT the ownership resource, leaving the seeded aspect as
			// the only thing the import can read.
			Config: providerBlock + entityOwnershipFixture(termID, groupID, typeIDA, typeIDB),
		},
		{
			PreConfig: func() {
				SeedLegacyTypeOwner(os.Getenv("DATAHUB_GMS_URL"), termURN, seededOwnerURN, "TECHNICAL_OWNER")
			},
			Config:             cfg,
			ResourceName:       entityOwnershipAddr,
			ImportState:        true,
			ImportStateId:      termURN,
			ImportStatePersist: true,
			ImportStateCheck: func(states []*terraform.InstanceState) error {
				// The slice carries every instance in the persisted state, not
				// just the imported one, because the fixture was applied first.
				// Pick ours by the attribute only this resource type has.
				attrs, err := onlyOwnershipInstance(states)
				if err != nil {
					return err
				}
				if got := attrs["owner.#"]; got != "1" {
					return fmt.Errorf("imported owner.# = %q, want %q", got, "1")
				}
				pairs := ownerPairsFromAttrs(attrs)
				want := seededOwnerURN + "|" + technicalOwnerTypeURN
				if len(pairs) != 1 || pairs[0] != want {
					return fmt.Errorf("imported owner pairs = %v, want [%s]; the legacy Owner.type "+
						"enum was not mapped back to its system ownership type entity", pairs, want)
				}
				return nil
			},
		},
		{
			// An empty plan against the equivalent configuration is what proves
			// the mapping agrees in both directions.
			Config:             cfg,
			PlanOnly:           true,
			ExpectNonEmptyPlan: false,
		},
	}
}

// onlyOwnershipInstance picks the single datahub_entity_ownership instance out
// of a persisted-state instance list, identified by the two attributes only
// this resource type carries together.
func onlyOwnershipInstance(states []*terraform.InstanceState) (map[string]string, error) {
	var found []map[string]string
	for _, st := range states {
		if st == nil {
			continue
		}
		if _, ok := st.Attributes["entity_urn"]; !ok {
			continue
		}
		if _, ok := st.Attributes["owner.#"]; !ok {
			continue
		}
		found = append(found, st.Attributes)
	}
	if len(found) != 1 {
		return nil, fmt.Errorf("expected exactly 1 datahub_entity_ownership instance in the imported state, got %d (of %d instances)", len(found), len(states))
	}
	return found[0], nil
}

// ownerPairsFromAttrs collects "<owner_urn>|<ownership_type_urn>" for every
// element of the owner set in a flatmap attribute bag, sorted. Set element
// indices are hashes, so the values have to be gathered rather than indexed.
func ownerPairsFromAttrs(attrs map[string]string) []string {
	owners := map[string]string{}
	types := map[string]string{}
	for k, v := range attrs {
		rest, ok := strings.CutPrefix(k, "owner.")
		if !ok {
			continue
		}
		idx, field, ok := strings.Cut(rest, ".")
		if !ok {
			continue
		}
		switch field {
		case "owner_urn":
			owners[idx] = v
		case "ownership_type_urn":
			types[idx] = v
		}
	}
	out := make([]string, 0, len(owners))
	for idx, owner := range owners {
		out = append(out, owner+"|"+types[idx])
	}
	sort.Strings(out)
	return out
}

// ownerEdgesFor reads an entity's owner pairs through the same
// strongly-consistent path the provider uses.
func ownerEdgesFor(entityURN string) ([]datahub.OwnerEdge, error) {
	client, err := datahub.NewClient(os.Getenv("DATAHUB_GMS_URL"), os.Getenv("DATAHUB_GMS_TOKEN"))
	if err != nil {
		return nil, fmt.Errorf("building DataHub client: %w", err)
	}
	edges, found, err := client.GetEntityOwners(context.Background(), entityURN)
	if err != nil {
		return nil, fmt.Errorf("reading owners of %q: %w", entityURN, err)
	}
	if !found {
		return nil, fmt.Errorf("entity %q does not exist", entityURN)
	}
	return edges, nil
}

// CheckOwnerPresent asserts one (owner, ownership type) pair is on the entity.
//
// Asking the server is not redundant with a state check. State records what the
// resource believes it owns; only the server can say what is actually on the
// aspect, which is the whole question for an owner Terraform never declared.
func CheckOwnerPresent(entityURN, ownerURN, ownershipTypeURN string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		edges, err := ownerEdgesFor(entityURN)
		if err != nil {
			return err
		}
		for _, e := range edges {
			if e.OwnerURN == ownerURN && e.OwnershipTypeURN == ownershipTypeURN {
				return nil
			}
		}
		return fmt.Errorf("owner (%s, %s) is not present on %s; owners are %v",
			ownerURN, ownershipTypeURN, entityURN, edges)
	}
}

// CheckOwnerAbsent asserts one (owner, ownership type) pair is NOT on the entity.
func CheckOwnerAbsent(entityURN, ownerURN, ownershipTypeURN string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		edges, err := ownerEdgesFor(entityURN)
		if err != nil {
			return err
		}
		for _, e := range edges {
			if e.OwnerURN == ownerURN && e.OwnershipTypeURN == ownershipTypeURN {
				return fmt.Errorf("owner (%s, %s) is still present on %s; owners are %v",
					ownerURN, ownershipTypeURN, entityURN, edges)
			}
		}
		return nil
	}
}

// CheckOwnerCount asserts how many owner pairs the entity carries in total,
// declared or not. This is what catches a removal that took more than it was
// asked for, or one that took nothing.
func CheckOwnerCount(entityURN string, want int) resource.TestCheckFunc {
	return func(*terraform.State) error {
		edges, err := ownerEdgesFor(entityURN)
		if err != nil {
			return err
		}
		if len(edges) != want {
			return fmt.Errorf("entity %s carries %d owner(s), want %d: %v", entityURN, len(edges), want, edges)
		}
		return nil
	}
}

// EntityOwnershipCheckDestroy verifies every pair each datahub_entity_ownership
// in the post-destroy state declared has been removed from its entity. A
// destroyed target entity counts as removed: its owners cannot outlive it.
func EntityOwnershipCheckDestroy(s *terraform.State) error {
	client, err := datahub.NewClient(os.Getenv("DATAHUB_GMS_URL"), os.Getenv("DATAHUB_GMS_TOKEN"))
	if err != nil {
		return fmt.Errorf("CheckDestroy: failed to build DataHub client: %w", err)
	}
	ctx := context.Background()
	for _, rs := range s.RootModule().Resources {
		if rs.Type != "datahub_entity_ownership" {
			continue
		}
		entityURN := rs.Primary.Attributes["entity_urn"]
		edges, found, getErr := client.GetEntityOwners(ctx, entityURN)
		if getErr != nil || !found {
			// The target entity is gone, which removes its owners with it.
			continue
		}
		declared := ownerPairsFromAttrs(rs.Primary.Attributes)
		for _, want := range declared {
			ownerURN, typeURN, _ := strings.Cut(want, "|")
			for _, e := range edges {
				if e.OwnerURN == ownerURN && e.OwnershipTypeURN == typeURN {
					return fmt.Errorf("owner (%s, %s) still assigned to %s after destroy",
						ownerURN, typeURN, entityURN)
				}
			}
		}
	}
	return nil
}

// SeedEntityOwner injects an owner edge into the mock's stored ownership aspect
// at baseURL, bypassing the provider's write path entirely. Stands in for an
// owner assigned in the DataHub UI. Mock-only.
func SeedEntityOwner(baseURL, entityURN, ownerURN, ownershipTypeURN string) {
	body := fmt.Sprintf(`{"entityUrn":%q,"ownerUrn":%q,"ownershipTypeUrn":%q}`, entityURN, ownerURN, ownershipTypeURN)
	postTestControl(baseURL, "/test-control/seed-owner", body, "SeedEntityOwner")
}

// SeedLegacyTypeOwner injects an owner edge carrying only the legacy
// Owner.type enum, with typeUrn absent. Mock-only.
func SeedLegacyTypeOwner(baseURL, entityURN, ownerURN, legacyType string) {
	body := fmt.Sprintf(`{"entityUrn":%q,"ownerUrn":%q,"legacyType":%q,"omitTypeUrn":true}`, entityURN, ownerURN, legacyType)
	postTestControl(baseURL, "/test-control/seed-owner", body, "SeedLegacyTypeOwner")
}

// DropEntityOwners clears every owner from an entity's stored ownership aspect,
// leaving the aspect absent -- how an entity that never had an owner reads.
// Mock-only.
func DropEntityOwners(baseURL, entityURN string) {
	body := fmt.Sprintf(`{"entityUrn":%q}`, entityURN)
	postTestControl(baseURL, "/test-control/drop-owners", body, "DropEntityOwners")
}

func postTestControl(baseURL, path, body, caller string) {
	resp, err := http.Post(baseURL+path, "application/json", strings.NewReader(body)) //nolint:noctx
	if err != nil {
		panic(fmt.Sprintf("%s: %v", caller, err))
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		panic(fmt.Sprintf("%s: unexpected status %d", caller, resp.StatusCode))
	}
}
