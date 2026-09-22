// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider_test

import (
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/datahubtesting"
)

// entityOwnershipFixtureIDs mints the four ids every ownership scenario needs.
// Each scenario gets its own set so a leaked entity from one cannot affect
// another on a live target.
func entityOwnershipFixtureIDs(tg *datahubtesting.Target, base string) (termID, groupID, typeIDA, typeIDB string) {
	return tg.Name(base + "-term"), tg.Name(base + "-grp"), tg.Name(base + "-ta"), tg.Name(base + "-tb")
}

// TestAcc_EntityOwnership_Lifecycle covers create, update and import, and with
// them the three repetition shapes that must all work: several owners across
// several ownership types, two owners sharing one ownership type, and one owner
// holding two ownership types.
func TestAcc_EntityOwnership_Lifecycle(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	termID, groupID, typeIDA, typeIDB := entityOwnershipFixtureIDs(tg, "tfprovider-eo-life")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.EntityOwnershipCheckDestroy,
		Steps:                    datahubtesting.EntityOwnershipLifecycleSteps(termID, groupID, typeIDA, typeIDB),
	})
}

// TestAcc_EntityOwnership_DuplicateEntry documents the semantics of an exactly
// duplicated owner entry: the set collapses it to one edge, with no error and no
// permanent diff. It is a test rather than a comment because the tempting fix --
// a validator rejecting repeated owners or repeated ownership types -- would
// break the two legitimate repetition shapes the lifecycle test covers.
func TestAcc_EntityOwnership_DuplicateEntry(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	termID, groupID, typeIDA, typeIDB := entityOwnershipFixtureIDs(tg, "tfprovider-eo-dup")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.EntityOwnershipCheckDestroy,
		Steps:                    datahubtesting.EntityOwnershipDuplicateEntrySteps(termID, groupID, typeIDA, typeIDB),
	})
}

// TestAcc_EntityOwnership_OutOfBandOwnerSurvives is the merge contract: an owner
// assigned outside Terraform survives create, update and destroy.
func TestAcc_EntityOwnership_OutOfBandOwnerSurvives(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	if tg.IsLive() {
		t.Skip("seeding an owner straight into the stored aspect needs /test-control/seed-owner, which only the mock exposes")
	}
	termID, groupID, typeIDA, typeIDB := entityOwnershipFixtureIDs(tg, "tfprovider-eo-oob")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.EntityOwnershipCheckDestroy,
		Steps:                    datahubtesting.EntityOwnershipOutOfBandSteps(termID, groupID, typeIDA, typeIDB),
	})
}

// TestAcc_EntityOwnership_RemovalPinnedToType catches the optional-ownershipTypeUrn
// trap: removing one of an owner's two ownership types must leave the other in
// place. A removeOwner call without the type would silently take both.
func TestAcc_EntityOwnership_RemovalPinnedToType(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	termID, groupID, typeIDA, typeIDB := entityOwnershipFixtureIDs(tg, "tfprovider-eo-pin")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.EntityOwnershipCheckDestroy,
		Steps:                    datahubtesting.EntityOwnershipPinnedRemovalSteps(termID, groupID, typeIDA, typeIDB),
	})
}

// TestAcc_EntityOwnership_ReplaceOnEntityChange moves the assignment to another
// entity. entity_urn forces replacement, and the id attribute is the entity URN
// with UseStateForUnknown on it, so this is the one path where a stale plan
// value would surface as "Provider produced inconsistent result after apply".
func TestAcc_EntityOwnership_ReplaceOnEntityChange(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	termID, groupID, typeIDA, typeIDB := entityOwnershipFixtureIDs(tg, "tfprovider-eo-move")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.EntityOwnershipCheckDestroy,
		Steps:                    datahubtesting.EntityOwnershipReplaceSteps(termID, groupID, typeIDA, typeIDB),
	})
}

// TestAcc_EntityOwnership_FromNonLiteral feeds the owner set from an input
// variable and from a computed attribute, the two everyday ways a value arrives
// unknown. Mandatory for any resource with a nested attribute: a literal
// resolves at plan time, so a suite of literal-only tests can pass while every
// module consumer of the resource is broken.
func TestAcc_EntityOwnership_FromNonLiteral(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	termID, groupID, typeIDA, typeIDB := entityOwnershipFixtureIDs(tg, "tfprovider-eo-unk")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.EntityOwnershipCheckDestroy,
		Steps:                    datahubtesting.EntityOwnershipFromVariableSteps(termID, groupID, typeIDA, typeIDB),
	})
}

// TestAcc_EntityOwnership_AllTargetTypes assigns an owner to one entity of each
// of the eight accepted target types in a single apply.
func TestAcc_EntityOwnership_AllTargetTypes(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	prefix := tg.Name("tfprovider-eo-all")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.EntityOwnershipCheckDestroy,
		Steps:                    datahubtesting.EntityOwnershipAllTargetsSteps(prefix),
	})
}

// TestAcc_EntityOwnership_RejectedTargets asserts each rejected target type
// fails at plan time with the message its own reason deserves.
//
// The corpuser and dataContract cases are the ones worth stating separately:
// both are valid datahub_structured_property_assignment targets, so a reader who
// knows that resource would reasonably expect them to work here. They do not,
// because neither entity type declares the ownership aspect at all -- a
// different reason from the deny-list one, with a different remedy (none).
func TestAcc_EntityOwnership_RejectedTargets(t *testing.T) {
	for name, tc := range map[string]struct {
		entityURN string
		wantError *regexp.Regexp
	}{
		"dataset": {
			entityURN: "urn:li:dataset:(urn:li:dataPlatform:hive,foo.bar,PROD)",
			wantError: regexp.MustCompile(`(?s)ingested data asset.*out of scope for this provider`),
		},
		"chart": {
			entityURN: "urn:li:chart:(looker,baz)",
			wantError: regexp.MustCompile(`(?s)ingested data asset`),
		},
		"corpuser": {
			entityURN: "urn:li:corpuser:alice@example.com",
			wantError: regexp.MustCompile(`(?s)corpuser.*aspect at all`),
		},
		"dataContract": {
			entityURN: "urn:li:dataContract:abc123",
			wantError: regexp.MustCompile(`(?s)dataContract.*aspect at all`),
		},
		"unrecognised": {
			entityURN: "urn:li:mysteryEntity:whatever",
			wantError: regexp.MustCompile(`(?s)not a supported owner`),
		},
	} {
		t.Run(name, func(t *testing.T) {
			_ = datahubtesting.SetupTarget(t)
			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
				Steps:                    datahubtesting.EntityOwnershipRejectedTargetSteps(tc.entityURN, tc.wantError),
			})
		})
	}
}

// TestAcc_EntityOwnership_InvalidOwnerURN asserts an owner URN that is neither a
// corp user nor a corp group is rejected at plan time.
func TestAcc_EntityOwnership_InvalidOwnerURN(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	termID := tg.Name("tfprovider-eo-badowner")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps:                    datahubtesting.EntityOwnershipInvalidOwnerURNSteps(termID),
	})
}

// TestAcc_EntityOwnership_AbsentAspect proves Read tolerates an entity whose
// ownership aspect is absent entirely, which is how an entity with no owners
// reads, and plans to re-add the declared pairs rather than erroring or deciding
// the resource is gone.
func TestAcc_EntityOwnership_AbsentAspect(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	if tg.IsLive() {
		t.Skip("clearing an entity's owners out of band needs /test-control/drop-owners, which only the mock exposes")
	}
	termID, groupID, typeIDA, typeIDB := entityOwnershipFixtureIDs(tg, "tfprovider-eo-empty")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.EntityOwnershipCheckDestroy,
		Steps:                    datahubtesting.EntityOwnershipAbsentAspectSteps(termID, groupID, typeIDA, typeIDB),
	})
}

// TestAcc_EntityOwnership_ImportLegacyType imports an owner stored with only the
// legacy Owner.type enum and no typeUrn, and asserts it resolves to the system
// ownership type entity the server would have derived. Without that mapping the
// pair reads as absent and the resource shows a diff it can never settle.
func TestAcc_EntityOwnership_ImportLegacyType(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	if tg.IsLive() {
		t.Skip("writing a typeUrn-less owner edge needs /test-control/seed-owner, which only the mock exposes")
	}
	termID, groupID, typeIDA, typeIDB := entityOwnershipFixtureIDs(tg, "tfprovider-eo-legacy")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.EntityOwnershipCheckDestroy,
		Steps:                    datahubtesting.EntityOwnershipLegacyTypeImportSteps(termID, groupID, typeIDA, typeIDB),
	})
}
