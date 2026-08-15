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

// TestAcc_GlossaryTermRelationship_ImportErrors asserts every malformed
// composite import ID, and a well-formed ID for a nonexistent edge, surfaces a
// specific diagnostic instead of importing a corrupt or phantom resource.
func TestAcc_GlossaryTermRelationship_ImportErrors(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	sourceID := tg.Name("tfprovider-relimperr-source")
	relatedID := tg.Name("tfprovider-relimperr-related")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.GlossaryTermRelationshipCheckDestroy,
		Steps:                    datahubtesting.GlossaryTermRelationshipImportErrorSteps(sourceID, relatedID),
	})
}

// TestAcc_GlossaryTermRelationship_NonexistentTarget asserts the server's
// referential rejection of an edge to a missing term is surfaced as an apply
// error rather than swallowed.
func TestAcc_GlossaryTermRelationship_NonexistentTarget(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	sourceID := tg.Name("tfprovider-relmisstgt-source")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps:                    datahubtesting.GlossaryTermRelationshipNonexistentTargetSteps(sourceID),
	})
}

// TestAcc_GlossaryTermRelationship_InvalidURNRejected asserts the plan-time URN
// validator rejects a non-term URN and the bare "urn:li:glossaryTerm:" prefix
// (the shape an empty string interpolation produces), before any API call.
func TestAcc_GlossaryTermRelationship_InvalidURNRejected(t *testing.T) {
	datahubtesting.SetupTarget(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps:                    datahubtesting.GlossaryTermRelationshipInvalidURNSteps(),
	})
}
