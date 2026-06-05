// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/datahubtesting"
)

// TestAcc_GlossaryTerm_Lifecycle exercises create, in-place update of name
// and description, and import (by id and by URN) for datahub_glossary_term.
func TestAcc_GlossaryTerm_Lifecycle(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	nodeID := tg.Name("tfprovider-gnode-lc")
	termID := tg.Name("tfprovider-gterm")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.GlossaryTermCheckDestroy,
		Steps:                    datahubtesting.GlossaryTermLifecycleSteps(nodeID, termID),
	})
}

// TestAcc_GlossaryTerm_Reparent exercises in-place reparenting (removing
// parent_node) via updateParentNode for datahub_glossary_term.
func TestAcc_GlossaryTerm_Reparent(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	nodeID := tg.Name("tfprovider-gnode-rp")
	termID := tg.Name("tfprovider-gterm-rp")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.GlossaryTermCheckDestroy,
		Steps:                    datahubtesting.GlossaryTermReparentSteps(nodeID, termID),
	})
}

// TestAcc_GlossaryTerm_DataSource reads a created glossary term back via the
// singular datahub_glossary_term data source.
func TestAcc_GlossaryTerm_DataSource(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	nodeID := tg.Name("tfprovider-gnode-ds")
	termID := tg.Name("tfprovider-gterm-ds")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.GlossaryTermCheckDestroy,
		Steps:                    datahubtesting.GlossaryTermDataSourceSteps(nodeID, termID),
	})
}

// TestAcc_GlossaryTerms_List verifies a created term's URN appears in the
// datahub_glossary_terms enumeration data source.
func TestAcc_GlossaryTerms_List(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	if tg.IsLive() {
		t.Skip("list data source is OpenSearch-backed and eventually consistent; a just-created term may not be indexed at read time")
	}
	nodeID := tg.Name("tfprovider-gnode-list")
	termID := tg.Name("tfprovider-gterm-list")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.GlossaryTermCheckDestroy,
		Steps:                    datahubtesting.GlossaryTermListSteps(nodeID, termID),
	})
}
