// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/datahubtesting"
)

// TestAcc_GlossaryTermRelationship_Lifecycle exercises create (both
// relationship types on one source term), import by composite ID, and
// out-of-band removal being re-created without disturbing the sibling edge.
func TestAcc_GlossaryTermRelationship_Lifecycle(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	sourceID := tg.Name("tfprovider-rel-source")
	relatedID := tg.Name("tfprovider-rel-related")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.GlossaryTermRelationshipCheckDestroy,
		Steps:                    datahubtesting.GlossaryTermRelationshipSteps(sourceID, relatedID),
	})
}

// TestAcc_GlossaryTermRelationship_FromVariable drives relationship_type from
// an input variable rather than a literal, so the attribute is unknown during
// the validate walk (the non-literal coverage the design checklist requires).
func TestAcc_GlossaryTermRelationship_FromVariable(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	sourceID := tg.Name("tfprovider-relvar-source")
	relatedID := tg.Name("tfprovider-relvar-related")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.GlossaryTermRelationshipCheckDestroy,
		Steps:                    datahubtesting.GlossaryTermRelationshipFromVariableSteps(sourceID, relatedID),
	})
}

// TestAcc_GlossaryTermRelationship_SelfRejected asserts a self-relationship is
// rejected at validate time, before any API call.
func TestAcc_GlossaryTermRelationship_SelfRejected(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	termID := tg.Name("tfprovider-rel-self")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps:                    datahubtesting.GlossaryTermRelationshipSelfSteps(termID),
	})
}
