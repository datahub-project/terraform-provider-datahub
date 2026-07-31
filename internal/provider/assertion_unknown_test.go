// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/datahubtesting"
)

// A conditionally-required attribute supplied as a computed value must be
// accepted. See the comment on the scenario builders for the faulty idiom and
// why datahub_volume_assertion and datahub_sql_assertion are unaffected.

func TestAcc_FreshnessAssertion_UnknownFixedIntervalUnit(t *testing.T) {
	datahubtesting.SetupTarget(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps:                    datahubtesting.FreshnessUnknownFixedIntervalSteps(),
	})
}

func TestAcc_FreshnessAssertion_UnknownCronSchedule(t *testing.T) {
	datahubtesting.SetupTarget(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps:                    datahubtesting.FreshnessUnknownCronSteps(),
	})
}

func TestAcc_FieldAssertion_UnknownMetric(t *testing.T) {
	datahubtesting.SetupTarget(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps:                    datahubtesting.FieldUnknownMetricSteps(),
	})
}

// Control. datahub_volume_assertion already guards unknown, so this passes both
// before and after the fix; it fails only if that guard is lost.
func TestAcc_VolumeAssertion_UnknownVolumeType_Control(t *testing.T) {
	datahubtesting.SetupTarget(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps:                    datahubtesting.VolumeUnknownChangeTypeControl(),
	})
}

// The other direction: an unknown attribute in the wrong branch must still be
// rejected. See FreshnessUnknownWrongBranchSteps.
func TestAcc_FreshnessAssertion_UnknownInWrongBranchStillRejected(t *testing.T) {
	datahubtesting.SetupTarget(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps:                    datahubtesting.FreshnessUnknownWrongBranchSteps(),
	})
}
