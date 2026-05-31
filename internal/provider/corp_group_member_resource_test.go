// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/datahubtesting"
)

// TestAcc_CorpGroupMember_Lifecycle exercises create, import by composite ID,
// and drift/re-bind for datahub_corp_group_member. It binds the built-in
// "datahub" user (present on both the mock and a live OSS Quickstart) to a
// freshly created group.
func TestAcc_CorpGroupMember_Lifecycle(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	groupID := tg.Name("tfprovider-member-group")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.CorpGroupMemberCheckDestroy,
		Steps:                    datahubtesting.CorpGroupMemberSteps(groupID, "urn:li:corpuser:datahub"),
	})
}
