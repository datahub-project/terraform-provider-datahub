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

	// technicalOwnerTypeURN and businessOwnerTypeURN are ownership type entities
	// DataHub bootstraps for the legacy Owner.type enum. Used where a scenario
	// needs an ownership type it did not create, and so cannot order a
	// dependency on.
	technicalOwnerTypeURN = "urn:li:ownershipType:__system__technical_owner"
	businessOwnerTypeURN  = "urn:li:ownershipType:__system__business_owner"

	// adminOwnerURN is the ONLY owner URN a scenario may use without creating
	// the principal first: the built-in admin, which every DataHub has and which
	// is also the Quickstart PAT actor. Every other owner in these scenarios is
	// a corp group the configuration creates.
	//
	// The rule exists because breaking it is invisible against the mock. Five of
	// these scenarios named urn:li:corpuser:testuser -- a mock fixture with no
	// counterpart on any real instance -- and passed, while every one of them
	// failed live with "Owner with urn urn:li:corpuser:testuser does not exist."
	// The mock now refuses its own fixtures as owners, so the shortcut is closed
	// rather than merely documented.
	adminOwnerURN = BuiltinAdminOwnerURN
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
func entityOwnershipFixture(termID, groupID, group2ID, typeIDA, typeIDB string) string {
	return fmt.Sprintf(`
resource "datahub_glossary_term" "target" {
  term_id = %q
  name    = "Entity Ownership Target"
}

resource "datahub_corp_group" "stewards" {
  group_id = %q
  name     = "Entity Ownership Stewards"
}

resource "datahub_corp_group" "reviewers" {
  group_id = %q
  name     = "Entity Ownership Reviewers"
}

resource "datahub_ownership_type" "a" {
  type_id = %q
  name    = "Entity Ownership Type A"
}

resource "datahub_ownership_type" "b" {
  type_id = %q
  name    = "Entity Ownership Type B"
}
`, termID, groupID, group2ID, typeIDA, typeIDB)
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
func EntityOwnershipLifecycleSteps(termID, groupID, group2ID, typeIDA, typeIDB string) []resource.TestStep {
	termURN := "urn:li:glossaryTerm:" + termID
	groupURN := "urn:li:corpGroup:" + groupID
	group2URN := "urn:li:corpGroup:" + group2ID
	typeAURN := "urn:li:ownershipType:" + typeIDA
	typeBURN := "urn:li:ownershipType:" + typeIDB

	cfg := func(owners string) string {
		return providerBlock +
			entityOwnershipFixture(termID, groupID, group2ID, typeIDA, typeIDB) +
			"\nresource \"datahub_entity_ownership\" \"test\" {\n" +
			"  entity_urn = datahub_glossary_term.target.urn\n\n" +
			"  owner = [\n" + owners + "  ]\n}\n"
	}

	// Three owners in type A -- two groups and the admin user -- and the stewards
	// group additionally in type B. Keeping one corpuser owner alongside the
	// groups is deliberate: without it nothing would exercise the CORP_USER
	// branch of the ownerEntityType derivation against a real server, since
	// every other owner here is a group.
	initial := ownerEntry("datahub_corp_group.stewards.urn", "datahub_ownership_type.a.urn") +
		ownerEntry("datahub_corp_group.reviewers.urn", "datahub_ownership_type.a.urn") +
		ownerEntry(fmt.Sprintf("%q", adminOwnerURN), "datahub_ownership_type.a.urn") +
		ownerEntry("datahub_corp_group.stewards.urn", "datahub_ownership_type.b.urn")

	// Adds (reviewers, B), removes (stewards, B), keeps the three type-A pairs.
	updated := ownerEntry("datahub_corp_group.stewards.urn", "datahub_ownership_type.a.urn") +
		ownerEntry("datahub_corp_group.reviewers.urn", "datahub_ownership_type.a.urn") +
		ownerEntry(fmt.Sprintf("%q", adminOwnerURN), "datahub_ownership_type.a.urn") +
		ownerEntry("datahub_corp_group.reviewers.urn", "datahub_ownership_type.b.urn")

	return []resource.TestStep{
		{
			Config: cfg(initial),
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(entityOwnershipAddr, tfjsonpath.New("entity_urn"), knownvalue.StringExact(termURN)),
				statecheck.ExpectKnownValue(entityOwnershipAddr, tfjsonpath.New("id"), knownvalue.StringExact(termURN)),
				statecheck.ExpectKnownValue(entityOwnershipAddr, tfjsonpath.New("owner"), knownvalue.SetExact([]knownvalue.Check{
					ownerCheck(groupURN, typeAURN),
					ownerCheck(group2URN, typeAURN),
					ownerCheck(adminOwnerURN, typeAURN),
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
					ownerCheck(group2URN, typeAURN),
					ownerCheck(adminOwnerURN, typeAURN),
					ownerCheck(group2URN, typeBURN),
				})),
			},
			Check: resource.ComposeAggregateTestCheckFunc(
				// The removed pair is gone from the server, and only that pair.
				CheckOwnerAbsent(termURN, groupURN, typeBURN),
				CheckOwnerPresent(termURN, groupURN, typeAURN),
			),
		},
		{
			// Import by entity URN.
			//
			// Deliberately NOT ImportStateVerify. Import adopts every owner the
			// entity carries, and on a live instance that is a strict superset
			// of what this resource declared: DataHub assigns the creating actor
			// as TECHNICAL_OWNER, so the import returns that pair too and an
			// equality check fails on an entity the provider handled perfectly.
			// The honest assertion is containment -- every declared pair comes
			// back -- which is also what the resource documents import to do.
			ResourceName:  entityOwnershipAddr,
			ImportState:   true,
			ImportStateId: termURN,
			ImportStateCheck: func(states []*terraform.InstanceState) error {
				attrs, err := onlyOwnershipInstance(states)
				if err != nil {
					return err
				}
				return assertImportedOwnersContain(attrs, []string{
					groupURN + "|" + typeAURN,
					group2URN + "|" + typeAURN,
					adminOwnerURN + "|" + typeAURN,
					group2URN + "|" + typeBURN,
				})
			},
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
func EntityOwnershipDuplicateEntrySteps(termID, groupID, group2ID, typeIDA, typeIDB string) []resource.TestStep {
	groupURN := "urn:li:corpGroup:" + groupID
	typeAURN := "urn:li:ownershipType:" + typeIDA

	cfg := providerBlock +
		entityOwnershipFixture(termID, groupID, group2ID, typeIDA, typeIDB) + `
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
			// The claim is multiplicity, not exclusivity: the duplicated entry
			// produced ONE edge. A total would also count the owner DataHub
			// assigns itself at entity creation.
			Check: CheckOwnerOccurrences("urn:li:glossaryTerm:"+termID, groupURN, typeAURN, 1),
		},
		{
			Config:             cfg,
			PlanOnly:           true,
			ExpectNonEmptyPlan: false,
		},
	}
}

// EntityOwnershipOutOfBandSteps is the merge contract -- the single most
// important behavioural claim this resource makes -- and it now runs against a
// live instance as well as the mock.
//
// The out-of-band owner is assigned by calling DataHub's own batchAddOwners
// from the test, before Terraform is given a configuration that mentions the
// entity at all. That is what somebody using the DataHub UI does, and unlike
// writing to the mock's store it works on any target. It costs one restructure:
// batchAddOwners validates that the target entity exists, so the fixture has to
// be applied first and the assignment cannot be made in the first step's
// PreConfig.
//
// The owner is the built-in admin under a bootstrapped system ownership type,
// so the seeding needs nothing created for it and cannot itself be the thing
// that fails.
//
// The seeded owner must then survive create, survive an update that adds and
// removes declared pairs, and survive the destroy of the resource. The destroy
// is driven by dropping the resource from the configuration rather than by the
// test's own teardown, because a teardown destroy takes the glossary term with
// it and leaves nothing to read back.
func EntityOwnershipOutOfBandSteps(termID, groupID, group2ID, typeIDA, typeIDB string) []resource.TestStep {
	termURN := "urn:li:glossaryTerm:" + termID
	groupURN := "urn:li:corpGroup:" + groupID
	group2URN := "urn:li:corpGroup:" + group2ID
	typeAURN := "urn:li:ownershipType:" + typeIDA
	typeBURN := "urn:li:ownershipType:" + typeIDB

	fixture := entityOwnershipFixture(termID, groupID, group2ID, typeIDA, typeIDB)

	ownership := func(owners string) string {
		return "\nresource \"datahub_entity_ownership\" \"test\" {\n" +
			"  entity_urn = datahub_glossary_term.target.urn\n\n" +
			"  owner = [\n" + owners + "  ]\n}\n"
	}

	declaredOne := ownerEntry("datahub_corp_group.stewards.urn", "datahub_ownership_type.a.urn")
	declaredTwo := ownerEntry("datahub_corp_group.stewards.urn", "datahub_ownership_type.b.urn") +
		ownerEntry("datahub_corp_group.reviewers.urn", "datahub_ownership_type.b.urn")

	outOfBandSurvives := CheckOwnerPresent(termURN, adminOwnerURN, technicalOwnerTypeURN)

	// Owners the entity carries before any datahub_entity_ownership resource
	// exists. On a live instance this is not empty: DataHub assigns the creating
	// actor as TECHNICAL_OWNER (OwnerUtils.addCreatorAsOwner), so the merge
	// contract has something real to preserve without the test arranging it.
	var baseline []datahub.OwnerEdge

	return []resource.TestStep{
		{
			// Fixture only: the entity has to exist before an owner can be
			// assigned to it out of band.
			Config: providerBlock + fixture,
			Check:  CaptureOwners(termURN, &baseline),
		},
		{
			PreConfig: func() {
				AssignOwnerOutOfBand(termURN, adminOwnerURN, technicalOwnerTypeURN)
			},
			Config: providerBlock + fixture + ownership(declaredOne),
			ConfigStateChecks: []statecheck.StateCheck{
				// State holds the declared pair only. If the resource adopted
				// what it found, the out-of-band owner would appear here and the
				// next destroy would delete somebody else's assignment.
				statecheck.ExpectKnownValue(entityOwnershipAddr, tfjsonpath.New("owner"), knownvalue.SetExact([]knownvalue.Check{
					ownerCheck(groupURN, typeAURN),
				})),
			},
			Check: resource.ComposeAggregateTestCheckFunc(
				outOfBandSurvives,
				CheckOwnersPreserved(termURN, &baseline),
			),
		},
		{
			// Add two pairs, remove the one declared before. The out-of-band
			// owner is touched by neither.
			Config: providerBlock + fixture + ownership(declaredTwo),
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(entityOwnershipAddr, tfjsonpath.New("owner"), knownvalue.SetExact([]knownvalue.Check{
					ownerCheck(groupURN, typeBURN),
					ownerCheck(group2URN, typeBURN),
				})),
			},
			Check: resource.ComposeAggregateTestCheckFunc(
				outOfBandSurvives,
				CheckOwnerAbsent(termURN, groupURN, typeAURN),
			),
		},
		{
			// Destroy just the ownership resource by dropping it from the
			// configuration. Its declared pairs go; the out-of-band one stays.
			Config: providerBlock + fixture,
			Check: resource.ComposeAggregateTestCheckFunc(
				outOfBandSurvives,
				CheckOwnersPreserved(termURN, &baseline),
				CheckOwnerAbsent(termURN, groupURN, typeBURN),
				CheckOwnerAbsent(termURN, group2URN, typeBURN),
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
func EntityOwnershipPinnedRemovalSteps(termID, groupID, group2ID, typeIDA, typeIDB string) []resource.TestStep {
	termURN := "urn:li:glossaryTerm:" + termID
	groupURN := "urn:li:corpGroup:" + groupID
	typeAURN := "urn:li:ownershipType:" + typeIDA
	typeBURN := "urn:li:ownershipType:" + typeIDB

	fixture := entityOwnershipFixture(termID, groupID, group2ID, typeIDA, typeIDB)
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
func EntityOwnershipReplaceSteps(termID, groupID, group2ID, typeIDA, typeIDB string) []resource.TestStep {
	firstURN := "urn:li:glossaryTerm:" + termID
	secondURN := "urn:li:glossaryTerm:" + termID + "-moved"
	groupURN := "urn:li:corpGroup:" + groupID
	typeAURN := "urn:li:ownershipType:" + typeIDA

	fixture := entityOwnershipFixture(termID, groupID, group2ID, typeIDA, typeIDB) + fmt.Sprintf(`
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

	// What the first entity owned before this resource touched it. DataHub
	// assigns the creating actor as TECHNICAL_OWNER, so on a live instance this
	// is non-empty and the destroy half has something to leave alone.
	var firstBaseline []datahub.OwnerEdge

	return []resource.TestStep{
		{
			// Fixture only, so the baseline is what the entity owned BEFORE this
			// resource declared anything. Capturing it after the first apply
			// would fold the declared pair into the baseline and then assert the
			// destroy half failed to remove it.
			Config: providerBlock + fixture,
			Check:  CaptureOwners(firstURN, &firstBaseline),
		},
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
				// which still exists -- and whatever else that entity owned is
				// untouched.
				CheckOwnerAbsent(firstURN, groupURN, typeAURN),
				CheckOwnersPreserved(firstURN, &firstBaseline),
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
func EntityOwnershipFromVariableSteps(termID, groupID, group2ID, typeIDA, typeIDB string) []resource.TestStep {
	groupURN := "urn:li:corpGroup:" + groupID
	typeAURN := "urn:li:ownershipType:" + typeIDA

	fromVariable := providerBlock + `
variable "owners" {
  type = set(object({
    owner_urn          = string
    ownership_type_urn = string
  }))
}
` + entityOwnershipFixture(termID, groupID, group2ID, typeIDA, typeIDB) + `
resource "datahub_entity_ownership" "test" {
  entity_urn = datahub_glossary_term.target.urn
  owner      = var.owners
}
`

	fromComputed := providerBlock +
		entityOwnershipFixture(termID, groupID, group2ID, typeIDA, typeIDB) + `
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
			// Both pairs name only things that exist on any target without
			// being created: the built-in admin and two bootstrapped system
			// ownership types. That is not incidental. A URN arriving through a
			// variable is an opaque string, so Terraform's dependency graph
			// cannot order the owner write after the resource that creates the
			// principal or the ownership type -- the documented cost of raw URN
			// inputs. Naming the fixture's own custom ownership type here would
			// be a race, passing or failing on the order Terraform happened to
			// pick. It also keeps the test about the unknown-value mechanism
			// rather than about dependency ordering.
			ConfigVariables: config.Variables{
				"owners": config.SetVariable(
					config.ObjectVariable(map[string]config.Variable{
						"owner_urn":          config.StringVariable(adminOwnerURN),
						"ownership_type_urn": config.StringVariable(technicalOwnerTypeURN),
					}),
					config.ObjectVariable(map[string]config.Variable{
						"owner_urn":          config.StringVariable(adminOwnerURN),
						"ownership_type_urn": config.StringVariable(businessOwnerTypeURN),
					}),
				),
			},
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(entityOwnershipAddr, tfjsonpath.New("owner"), knownvalue.SetExact([]knownvalue.Check{
					ownerCheck(adminOwnerURN, technicalOwnerTypeURN),
					ownerCheck(adminOwnerURN, businessOwnerTypeURN),
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
`, prefix, adminOwnerURN)

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
		checks = append(checks, CheckOwnerPresent(urn, adminOwnerURN, typeURN))
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
`, entityURN, adminOwnerURN, technicalOwnerTypeURN),
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

// EntityOwnershipNoOwnersSteps proves the read path tolerates an entity that
// exists and has no owners, and plans to re-add the declared pairs rather than
// erroring or deciding the resource is gone.
//
// The owners are cleared between the two steps by calling DataHub's own
// removeOwner from the test, which is what somebody clearing them in the UI
// does and which works on any target. It also produces the shape a real server
// produces: OwnerUtils writes the aspect back with an empty owners array rather
// than deleting it, so the entity reads as ownership.value.owners = [] --
// present and empty. The mock reproduces that exactly. The other shape, an
// aspect absent from the JSON altogether, belongs to an entity that never had
// an owner and is covered directly at the client level in
// TestGetEntityOwnersReadShapes.
//
// Two wrong behaviours are distinguished by the expected plan action. If Read
// errored, the step fails outright. If Read treated no-owners as a missing
// resource and called RemoveResource, the plan would be a Create; an Update is
// the only outcome consistent with "the entity is there, it just has no owners,
// so re-add mine".
func EntityOwnershipNoOwnersSteps(termID, groupID, group2ID, typeIDA, typeIDB string) []resource.TestStep {
	termURN := "urn:li:glossaryTerm:" + termID
	groupURN := "urn:li:corpGroup:" + groupID
	typeAURN := "urn:li:ownershipType:" + typeIDA

	cfg := providerBlock +
		entityOwnershipFixture(termID, groupID, group2ID, typeIDA, typeIDB) +
		"\nresource \"datahub_entity_ownership\" \"test\" {\n" +
		"  entity_urn = datahub_glossary_term.target.urn\n\n" +
		"  owner = [\n" +
		ownerEntry("datahub_corp_group.stewards.urn", "datahub_ownership_type.a.urn") +
		"  ]\n}\n"

	return []resource.TestStep{
		{
			Config: cfg,
			Check:  CheckOwnerPresent(termURN, groupURN, typeAURN),
		},
		{
			PreConfig: func() {
				ClearAllOwners(termURN)
			},
			Config: cfg,
			ConfigPlanChecks: resource.ConfigPlanChecks{
				PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(entityOwnershipAddr, plancheck.ResourceActionUpdate),
				},
			},
			Check: CheckOwnerPresent(termURN, groupURN, typeAURN),
		},
	}
}

// EntityOwnershipFixturePrincipalSteps is the regression test for the defect
// this whole fixture rework exists to fix: naming an owner that only the mock
// has must fail, and must fail the same way on both targets.
//
// urn:li:corpuser:testuser is one of seedUsers' fixtures. It has no counterpart
// on any real DataHub, and OwnerUtils.validateOwners refuses it. Before the
// mock was hardened it accepted the URN because the fixture was in its users
// map, so five scenarios passed against the mock and failed against a
// Quickstart with exactly the message asserted here.
//
// Running on both targets is the point. Against live it asserts the server's
// behaviour; against the mock it asserts the mock still agrees with the server,
// which is the property that was missing.
func EntityOwnershipFixturePrincipalSteps(termID, groupID, group2ID, typeIDA, typeIDB string) []resource.TestStep {
	cfg := providerBlock +
		entityOwnershipFixture(termID, groupID, group2ID, typeIDA, typeIDB) + fmt.Sprintf(`
resource "datahub_entity_ownership" "test" {
  entity_urn = datahub_glossary_term.target.urn

  owner = [
    { owner_urn = "urn:li:corpuser:testuser", ownership_type_urn = %q },
  ]
}
`, technicalOwnerTypeURN)

	return []resource.TestStep{
		{
			Config: cfg,
			// Wrapped across lines by Terraform, so match a short fragment.
			ExpectError: regexp.MustCompile(`(?s)Owner with urn`),
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
func EntityOwnershipLegacyTypeImportSteps(termID, groupID, group2ID, typeIDA, typeIDB string) []resource.TestStep {
	termURN := "urn:li:glossaryTerm:" + termID

	cfg := providerBlock +
		entityOwnershipFixture(termID, groupID, group2ID, typeIDA, typeIDB) + fmt.Sprintf(`
resource "datahub_entity_ownership" "test" {
  entity_urn = datahub_glossary_term.target.urn

  owner = [
    { owner_urn = %q, ownership_type_urn = %q },
  ]
}
`, adminOwnerURN, technicalOwnerTypeURN)

	return []resource.TestStep{
		{
			// The entity has to exist before it can be imported, and an import
			// step does not apply its configuration. So the fixture is applied
			// first, WITHOUT the ownership resource, leaving the seeded aspect as
			// the only thing the import can read.
			Config: providerBlock + entityOwnershipFixture(termID, groupID, group2ID, typeIDA, typeIDB),
		},
		{
			PreConfig: func() {
				SeedLegacyTypeOwner(os.Getenv("DATAHUB_GMS_URL"), termURN, adminOwnerURN, "TECHNICAL_OWNER")
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
				// The seeded typeUrn-less edge must come back resolved to the
				// system ownership type entity the server would have derived.
				// Containment rather than equality, because the entity also
				// carries the owner DataHub assigned at creation.
				if err := assertImportedOwnersContain(attrs, []string{
					adminOwnerURN + "|" + technicalOwnerTypeURN,
				}); err != nil {
					return fmt.Errorf("%w; the legacy Owner.type enum was not mapped back to its "+
						"system ownership type entity", err)
				}
				return nil
			},
		},
		// There is deliberately no empty-plan step after this import, and the
		// reason is the adopt-everything contract rather than a gap.
		//
		// Import brings in EVERY owner on the entity. The entity also carries
		// the owner DataHub assigned at creation, which this configuration does
		// not declare and must not declare -- which actor that is depends on the
		// instance. So the plan after import legitimately proposes removing it,
		// and an ExpectNonEmptyPlan: false here would assert the opposite of
		// what the resource is documented to do. That is exactly why the docs
		// tell a practitioner to write the configuration from
		// `terraform state show` after importing rather than from memory.
		//
		// The claim the empty plan was standing in for -- that the legacy type
		// maps back to the same pair a configuration would declare -- is proved
		// by the ImportStateCheck above and pinned directly in
		// TestGetEntityOwnersReadShapes.
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

// assertImportedOwnersContain checks that every "<owner>|<type>" pair in want
// appears among the owners of an imported instance.
//
// Containment, not equality, and the distinction is the resource's documented
// import semantics rather than laxness: import adopts EVERY owner on the
// entity, including ones no configuration declared, so the imported set is a
// superset of what any test arranged.
func assertImportedOwnersContain(attrs map[string]string, want []string) error {
	got := ownerPairsFromAttrs(attrs)
	present := make(map[string]struct{}, len(got))
	for _, p := range got {
		present[p] = struct{}{}
	}
	for _, w := range want {
		if _, ok := present[w]; !ok {
			return fmt.Errorf("imported owners %v do not include %q", got, w)
		}
	}
	return nil
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

// CheckOwnerOccurrences asserts one pair appears exactly want times on the
// entity. Used where the question is about multiplicity rather than presence --
// specifically, that an exactly duplicated owner entry produces one edge.
func CheckOwnerOccurrences(entityURN, ownerURN, ownershipTypeURN string, want int) resource.TestCheckFunc {
	return func(*terraform.State) error {
		edges, err := ownerEdgesFor(entityURN)
		if err != nil {
			return err
		}
		got := 0
		for _, e := range edges {
			if e.OwnerURN == ownerURN && e.OwnershipTypeURN == ownershipTypeURN {
				got++
			}
		}
		if got != want {
			return fmt.Errorf("pair (%s, %s) appears %d time(s) on %s, want %d; owners are %v",
				ownerURN, ownershipTypeURN, got, entityURN, want, edges)
		}
		return nil
	}
}

// There is deliberately no check for an entity's TOTAL owner count, and the
// reason is a DataHub behaviour worth recording rather than an omission.
//
// A live instance hands a newly created entity an owner nobody asked for:
// CreateGlossaryTermResolver calls OwnerUtils.addCreatorAsOwner, which assigns
// the creating actor as TECHNICAL_OWNER. So a term this suite creates already
// has one owner before any datahub_entity_ownership resource exists. Worse for
// an absolute assertion, WHICH actor is environment-dependent: on an
// auth-disabled Quickstart every request is attributed to
// urn:li:corpuser:__datahub_system, while an authenticated instance records the
// real user.
//
// Five scenarios asserted absolute totals and failed live for exactly that
// reason -- each off by one, each naming __datahub_system, and in every case the
// provider had behaved correctly. An absolute count encodes an assumption about
// the instance rather than a claim about the provider.
//
// CaptureOwners plus CheckOwnersPreserved express the claim that was actually
// wanted: whatever was there before this resource existed is still there
// afterwards. That is the merge contract, it needs no knowledge of who the
// platform assigned, and the platform-assigned owner makes it stronger on live
// than any seeding could.

// CaptureOwners records the entity's current owners into dst so a later step can
// assert they survived. Pair it with CheckOwnersPreserved.
func CaptureOwners(entityURN string, dst *[]datahub.OwnerEdge) resource.TestCheckFunc {
	return func(*terraform.State) error {
		edges, err := ownerEdgesFor(entityURN)
		if err != nil {
			return err
		}
		*dst = edges
		return nil
	}
}

// CheckOwnersPreserved asserts every pair CaptureOwners recorded earlier is
// still on the entity. It says nothing about pairs added since, which is the
// point: the resource may add its own, it may not remove anybody else's.
func CheckOwnersPreserved(entityURN string, baseline *[]datahub.OwnerEdge) resource.TestCheckFunc {
	return func(*terraform.State) error {
		edges, err := ownerEdgesFor(entityURN)
		if err != nil {
			return err
		}
		present := make(map[datahub.OwnerEdge]struct{}, len(edges))
		for _, e := range edges {
			present[e] = struct{}{}
		}
		if len(*baseline) == 0 {
			return fmt.Errorf("CheckOwnersPreserved: no baseline was captured for %s; "+
				"CaptureOwners must run in an earlier step", entityURN)
		}
		for _, want := range *baseline {
			if _, ok := present[want]; !ok {
				return fmt.Errorf("owner (%s, %s) was present before this resource managed %s and is now gone; "+
					"the resource removed a pair it never declared. Owners are %v",
					want.OwnerURN, want.OwnershipTypeURN, entityURN, edges)
			}
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

// AssignOwnerOutOfBand assigns an owner to an entity by calling DataHub's own
// batchAddOwners mutation from the test, outside Terraform. It stands in for
// somebody using the DataHub UI, and unlike writing to the mock's store it
// works against a live instance, which is what lets the merge contract be
// tested on a real server rather than only in the mock.
//
// The target entity must already exist: batchAddOwners validates the resource.
func AssignOwnerOutOfBand(entityURN, ownerURN, ownershipTypeURN string) {
	client, err := datahub.NewClient(os.Getenv("DATAHUB_GMS_URL"), os.Getenv("DATAHUB_GMS_TOKEN"))
	if err != nil {
		panic(fmt.Sprintf("AssignOwnerOutOfBand: building client: %v", err))
	}
	err = client.BatchAddOwners(context.Background(), entityURN, []datahub.OwnerEdge{
		{OwnerURN: ownerURN, OwnershipTypeURN: ownershipTypeURN},
	})
	if err != nil {
		panic(fmt.Sprintf("AssignOwnerOutOfBand: assigning (%s, %s) to %s: %v",
			ownerURN, ownershipTypeURN, entityURN, err))
	}
}

// ClearAllOwners removes every owner from an entity through DataHub's own
// removeOwner mutation, leaving it existing with no owners. Stands in for
// somebody clearing the Ownership panel in the UI, and works on any target.
func ClearAllOwners(entityURN string) {
	client, err := datahub.NewClient(os.Getenv("DATAHUB_GMS_URL"), os.Getenv("DATAHUB_GMS_TOKEN"))
	if err != nil {
		panic(fmt.Sprintf("ClearAllOwners: building client: %v", err))
	}
	ctx := context.Background()
	edges, found, err := client.GetEntityOwners(ctx, entityURN)
	if err != nil {
		panic(fmt.Sprintf("ClearAllOwners: reading owners of %s: %v", entityURN, err))
	}
	if !found {
		panic(fmt.Sprintf("ClearAllOwners: entity %s does not exist", entityURN))
	}
	for _, e := range edges {
		if err := client.RemoveOwner(ctx, entityURN, e.OwnerURN, e.OwnershipTypeURN); err != nil {
			panic(fmt.Sprintf("ClearAllOwners: removing (%s, %s) from %s: %v",
				e.OwnerURN, e.OwnershipTypeURN, entityURN, err))
		}
	}
}

// SeedLegacyTypeOwner injects an owner edge carrying only the legacy
// Owner.type enum, with typeUrn absent. Mock-only.
func SeedLegacyTypeOwner(baseURL, entityURN, ownerURN, legacyType string) {
	body := fmt.Sprintf(`{"entityUrn":%q,"ownerUrn":%q,"legacyType":%q,"omitTypeUrn":true}`, entityURN, ownerURN, legacyType)
	postTestControl(baseURL, "/test-control/seed-owner", body, "SeedLegacyTypeOwner")
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
