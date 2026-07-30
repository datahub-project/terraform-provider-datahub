// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/datahubtesting"
)

// TestAcc_Policy_CriteriaFromVariable guards against literal-only consumption of
// resources.filter.criteria. See PolicyCriteriaFromVariableSteps for the
// reported failure and why the Go-native model types cause it.
func TestAcc_Policy_CriteriaFromVariable(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	policyID := tg.Name("tfprovider-pol-unk")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.PolicyCheckDestroy,
		Steps:                    datahubtesting.PolicyCriteriaFromVariableSteps(policyID),
	})
}

// TestAcc_Policy_ReportedRepro is the reporter's config as written, rather than a
// paraphrase of it: value from the variable's own default, no actors block, and
// their criterion. See PolicyReportedReproSteps for the two deviations the test
// harness forces.
func TestAcc_Policy_ReportedRepro(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	policyID := tg.Name("tfprovider-pol-repro")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.PolicyCheckDestroy,
		Steps:                    datahubtesting.PolicyReportedReproSteps(policyID),
	})
}

// TestAcc_Policy_ActorsFromVariable establishes whether the same defect predates
// the criteria feature. actors is backed by a Go pointer and holds three
// Optional+Computed booleans, so it should fail identically if the cause is the
// model types rather than anything specific to resources.filter.
func TestAcc_Policy_ActorsFromVariable(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	policyID := tg.Name("tfprovider-pol-act")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.PolicyCheckDestroy,
		Steps:                    datahubtesting.PolicyActorsFromVariableSteps(policyID),
	})
}

// TestAcc_StructuredProperty_SettingsFromVariable covers the same defect on an
// unrelated resource, to establish that the cause is the Go-native model types
// rather than anything about datahub_policy.
func TestAcc_StructuredProperty_SettingsFromVariable(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	propertyID := tg.Name("tfprovider-sp-unk")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps:                    datahubtesting.StructuredPropertySettingsFromVariableSteps(propertyID),
	})
}
