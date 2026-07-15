// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/datahubtesting"
)

// TestAcc_Defaults_CustomProperties covers provider-level
// defaults.custom_properties: merge into custom_properties_all at create, a
// provider-default change rippling as an in-place update, defaults removal
// (with CREATION_ONLY marker carry-forward), and an import round-trip.
func TestAcc_Defaults_CustomProperties(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	domainID := tg.Name("tfprovider-defaults-cp")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.DomainCheckDestroy,
		Steps:                    datahubtesting.DomainDefaultsCustomPropertiesSteps(domainID),
	})
}

// TestAcc_Defaults_AutoProperties covers the auto-property markers: built-in
// managed-by default, plan idempotency, provider-version under PROACTIVE, and
// disable via auto_properties = [].
func TestAcc_Defaults_AutoProperties(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	domainID := tg.Name("tfprovider-defaults-auto")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.DomainCheckDestroy,
		Steps:                    datahubtesting.DomainAutoPropertiesLifecycleSteps(domainID),
	})
}

// TestAcc_Defaults_AutoPropertiesDisabled covers the plain opt-out journey:
// auto_properties = [] from the start. Nothing is written, resource-level
// custom_properties behave as before the feature, replans are empty, and
// import round-trips.
func TestAcc_Defaults_AutoPropertiesDisabled(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	domainID := tg.Name("tfprovider-defaults-disabled")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.DomainCheckDestroy,
		Steps:                    datahubtesting.DomainAutoPropertiesDisabledSteps(domainID),
	})
}

// TestAcc_Defaults_AutoPropertyStrategy covers the CREATION_ONLY upgrade
// fence (empty plan on an unstamped estate) and the one-time PROACTIVE
// convergence pass.
func TestAcc_Defaults_AutoPropertyStrategy(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	domainID := tg.Name("tfprovider-defaults-strategy")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.DomainCheckDestroy,
		Steps:                    datahubtesting.DomainAutoPropertyStrategySteps(domainID),
	})
}

// TestAcc_Defaults_Collisions covers resource-vs-default key collisions:
// same-value overlap must be perfectly stable, differing values resolve to
// the resource (with a plan-time warning covered by unit tests).
func TestAcc_Defaults_Collisions(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	domainID := tg.Name("tfprovider-defaults-collision")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.DomainCheckDestroy,
		Steps:                    datahubtesting.DomainDefaultsCollisionSteps(domainID),
	})
}

// TestAcc_DefaultTags_CorpGroupLifecycle covers the tags_all ownership latch
// on datahub_corp_group: unlatched create, latching an existing resource when
// defaults.tags appears, idempotency, import while latched, and unlatching.
func TestAcc_DefaultTags_CorpGroupLifecycle(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	groupID := tg.Name("tfprovider-dtags-group")
	tagID := tg.Name("tfprovider-dtags-marker")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps:                    datahubtesting.CorpGroupDefaultTagsLifecycleSteps(groupID, tagID),
	})
}

// TestAcc_DefaultTags_CorpUserAtCreate covers tagging at create time via the
// corpuser entity path (shared with datahub_service_account).
func TestAcc_DefaultTags_CorpUserAtCreate(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	username := tg.Name("tfprovider-dtags-user")
	tagID := tg.Name("tfprovider-dtags-umarker")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps:                    datahubtesting.CorpUserDefaultTagsAtCreateSteps(username, tagID),
	})
}

// TestAcc_DefaultTags_DataProductAtCreate covers tagging at create time via
// the dataproduct entity path, coexisting with the managed-by marker.
func TestAcc_DefaultTags_DataProductAtCreate(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	dataProductID := tg.Name("tfprovider-dtags-dp")
	tagID := tg.Name("tfprovider-dtags-dpmarker")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps:                    datahubtesting.DataProductDefaultTagsAtCreateSteps(dataProductID, tagID),
	})
}

// TestAcc_DefaultTags_ExternalEdits covers both sides of the latch against
// external tag edits: invisible while unlatched, stomped while latched.
// Mock-only (the simulated edit writes the raw globalTags aspect).
func TestAcc_DefaultTags_ExternalEdits(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	if tg.IsLive() {
		t.Skip("external-edit simulation writes the raw globalTags aspect; mock-only")
	}
	groupID := tg.Name("tfprovider-dtags-ext-group")
	tagID := tg.Name("tfprovider-dtags-ext-marker")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps:                    datahubtesting.CorpGroupExternalTagSteps(groupID, tagID),
	})
}

// TestAcc_DefaultTags_NonexistentTag asserts a clear apply-time error when
// defaults.tags references a tag that does not exist.
func TestAcc_DefaultTags_NonexistentTag(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	groupID := tg.Name("tfprovider-dtags-missing")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps:                    datahubtesting.DefaultTagsNonexistentSteps(groupID),
	})
}

// TestAcc_Defaults_ExternalEditStomped covers full-map ownership: a property
// added outside Terraform surfaces as drift on custom_properties_all and is
// removed by the next apply. Mock-only (the simulated external edit writes
// the raw aspect, which is not safe against a live server).
func TestAcc_Defaults_ExternalEditStomped(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	if tg.IsLive() {
		t.Skip("external-edit simulation writes the raw domainProperties aspect; mock-only")
	}
	domainID := tg.Name("tfprovider-defaults-external")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.DomainCheckDestroy,
		Steps:                    datahubtesting.DomainDefaultsExternalEditSteps(domainID),
	})
}
