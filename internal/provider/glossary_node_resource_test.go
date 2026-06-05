// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/datahubtesting"
)

// TestAcc_GlossaryNode_Lifecycle exercises create, in-place update of name
// and description, and import (by id and by URN) for datahub_glossary_node.
func TestAcc_GlossaryNode_Lifecycle(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	nodeID := tg.Name("tfprovider-gnode")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.GlossaryNodeCheckDestroy,
		Steps:                    datahubtesting.GlossaryNodeLifecycleSteps(nodeID),
	})
}

// TestAcc_GlossaryNode_ParentChild exercises parent-child creation and
// in-place reparenting via updateParentNode for datahub_glossary_node.
func TestAcc_GlossaryNode_ParentChild(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	parentID := tg.Name("tfprovider-gnode-parent")
	childID := tg.Name("tfprovider-gnode-child")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.GlossaryNodeCheckDestroy,
		Steps:                    datahubtesting.GlossaryNodeParentChildSteps(parentID, childID),
	})
}

// TestAcc_GlossaryNode_DataSource reads a created glossary node back via the
// singular datahub_glossary_node data source.
func TestAcc_GlossaryNode_DataSource(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	nodeID := tg.Name("tfprovider-gnode-ds")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.GlossaryNodeCheckDestroy,
		Steps:                    datahubtesting.GlossaryNodeDataSourceSteps(nodeID),
	})
}

// TestAcc_GlossaryNodes_List verifies a created node's URN appears in the
// datahub_glossary_nodes enumeration data source.
func TestAcc_GlossaryNodes_List(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	if tg.IsLive() {
		t.Skip("list data source is OpenSearch-backed and eventually consistent; a just-created node may not be indexed at read time")
	}
	nodeID := tg.Name("tfprovider-gnode-list")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.GlossaryNodeCheckDestroy,
		Steps:                    datahubtesting.GlossaryNodeListSteps(nodeID),
	})
}
