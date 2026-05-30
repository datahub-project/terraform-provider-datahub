// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/datahubtesting"
)

// TestAcc_Role_DataSource reads the built-in Admin role.
func TestAcc_Role_DataSource(t *testing.T) {
	datahubtesting.SetupTarget(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps:                    datahubtesting.RoleDataSourceSteps(),
	})
}

// TestAcc_Roles_List verifies the three built-in roles are returned by the
// datahub_roles data source.
func TestAcc_Roles_List(t *testing.T) {
	datahubtesting.SetupTarget(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps:                    datahubtesting.RolesListSteps(),
	})
}

// TestAcc_RoleAssignment_Lifecycle covers assign, in-place reassign, import, and
// delete/unassign for datahub_role_assignment, using a freshly created group as
// the actor.
func TestAcc_RoleAssignment_Lifecycle(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	groupID := tg.Name("tfprovider-roleassign-group")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.RoleAssignmentCheckDestroy,
		Steps:                    datahubtesting.RoleAssignmentSteps(groupID),
	})
}
