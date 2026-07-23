// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/datahubtesting"
)

func TestAcc_StructuredPropertyAssignment_Lifecycle(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	propertyID := tg.Name("tfprovider-spa")
	domainID := tg.Name("tfprovider-spa-dom")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.StructuredPropertyAssignmentCheckDestroy,
		Steps:                    datahubtesting.StructuredPropertyAssignmentSteps(propertyID, domainID),
	})
}

// TestAcc_StructuredPropertyAssignment_NewTargets exercises assignments
// against the corpGroup, corpUser, and dataContract target types.
func TestAcc_StructuredPropertyAssignment_NewTargets(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	propertyID := tg.Name("tfprovider-spa-nt")
	groupID := tg.Name("tfprovider-spa-nt-grp")
	username := tg.Name("tfprovider-spa-nt-usr")
	datasetURN := "urn:li:dataset:(urn:li:dataPlatform:hive,tfprovider_spa_nt.table,PROD)"
	if tg.IsLive() {
		tg.EnsureDatasetEntity(t, datasetURN)
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.StructuredPropertyAssignmentCheckDestroy,
		Steps:                    datahubtesting.StructuredPropertyAssignmentNewTargetsSteps(propertyID, groupID, username, datasetURN),
	})
}

func TestAcc_StructuredPropertyAssignment_UnsupportedTarget(t *testing.T) {
	// Guard #1 (CAT-2562): unsupported target type rejected at plan time.
	_ = datahubtesting.SetupTarget(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps:                    datahubtesting.StructuredPropertyAssignmentUnsupportedTargetSteps(),
	})
}

func TestAcc_StructuredPropertyAssignment_TypeMismatch(t *testing.T) {
	// Guard #2 (CAT-2563): property not applicable to the target entity type,
	// rejected at apply time.
	tg := datahubtesting.SetupTarget(t)
	propertyID := tg.Name("tfprovider-spa-mismatch")
	domainID := tg.Name("tfprovider-spa-mismatch-dom")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps:                    datahubtesting.StructuredPropertyAssignmentTypeMismatchSteps(propertyID, domainID),
	})
}

func TestAcc_StructuredPropertyAssignment_Reorder(t *testing.T) {
	// values is an unordered set: reordering must not produce a diff.
	tg := datahubtesting.SetupTarget(t)
	propertyID := tg.Name("tfprovider-spa-reorder")
	domainID := tg.Name("tfprovider-spa-reorder-dom")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.StructuredPropertyAssignmentCheckDestroy,
		Steps:                    datahubtesting.StructuredPropertyAssignmentReorderSteps(propertyID, domainID),
	})
}

func TestAcc_StructuredPropertyAssignment_NonAllowedValue(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	propertyID := tg.Name("tfprovider-spa-av")
	domainID := tg.Name("tfprovider-spa-av-dom")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps:                    datahubtesting.StructuredPropertyAssignmentNonAllowedValueSteps(propertyID, domainID),
	})
}

func TestAcc_StructuredPropertyAssignment_Cardinality(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	propertyID := tg.Name("tfprovider-spa-card")
	domainID := tg.Name("tfprovider-spa-card-dom")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps:                    datahubtesting.StructuredPropertyAssignmentCardinalitySteps(propertyID, domainID),
	})
}

func TestAcc_StructuredPropertyAssignment_PropertyNotFound(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	domainID := tg.Name("tfprovider-spa-missing-dom")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps:                    datahubtesting.StructuredPropertyAssignmentPropertyNotFoundSteps(domainID),
	})
}

func TestAcc_StructuredPropertyAssignment_InvalidNumber(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	propertyID := tg.Name("tfprovider-spa-badnum")
	domainID := tg.Name("tfprovider-spa-badnum-dom")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps:                    datahubtesting.StructuredPropertyAssignmentInvalidNumberSteps(propertyID, domainID),
	})
}

func TestAcc_StructuredPropertyAssignment_NumberValues(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	propertyID := tg.Name("tfprovider-spa-num")
	domainID := tg.Name("tfprovider-spa-num-dom")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.StructuredPropertyAssignmentCheckDestroy,
		Steps:                    datahubtesting.StructuredPropertyAssignmentNumberSteps(propertyID, domainID),
	})
}
