// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahubtesting

import (
	"context"
	"fmt"
	"os"
	"regexp"

	"github.com/hashicorp/terraform-plugin-testing/config"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
)

// glossaryTermRelationshipConfig renders two terms and two typed edges on the
// same source term (isA and hasA). Two edges on one source exercises
// coexistence within the single glossaryRelatedTerms aspect, and both edges
// take their URNs as expression inputs, so term_urn/related_term_urn arrive
// unknown at plan time.
func glossaryTermRelationshipConfig(sourceID, relatedID string) string {
	return providerBlock + fmt.Sprintf(`
resource "datahub_glossary_term" "source" {
  term_id = %q
  name    = "Net Revenue"
}

resource "datahub_glossary_term" "related" {
  term_id = %q
  name    = "Revenue Concept"
}

resource "datahub_glossary_term_relationship" "inherits" {
  term_urn          = datahub_glossary_term.source.urn
  relationship_type = "isA"
  related_term_urn  = datahub_glossary_term.related.urn
}

resource "datahub_glossary_term_relationship" "contains" {
  term_urn          = datahub_glossary_term.source.urn
  relationship_type = "hasA"
  related_term_urn  = datahub_glossary_term.related.urn
}
`, sourceID, relatedID)
}

// GlossaryTermRelationshipSteps returns test steps covering create (both
// relationship types on one source term), import by composite ID, and
// out-of-band removal being re-created, for datahub_glossary_term_relationship.
func GlossaryTermRelationshipSteps(sourceID, relatedID string) []resource.TestStep {
	const inheritsAddr = "datahub_glossary_term_relationship.inherits"
	const containsAddr = "datahub_glossary_term_relationship.contains"
	sourceURN := "urn:li:glossaryTerm:" + sourceID
	relatedURN := "urn:li:glossaryTerm:" + relatedID
	cfg := glossaryTermRelationshipConfig(sourceID, relatedID)

	return []resource.TestStep{
		{
			Config: cfg,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(inheritsAddr, tfjsonpath.New("term_urn"), knownvalue.StringExact(sourceURN)),
				statecheck.ExpectKnownValue(inheritsAddr, tfjsonpath.New("relationship_type"), knownvalue.StringExact("isA")),
				statecheck.ExpectKnownValue(inheritsAddr, tfjsonpath.New("related_term_urn"), knownvalue.StringExact(relatedURN)),
				statecheck.ExpectKnownValue(inheritsAddr, tfjsonpath.New("id"), knownvalue.StringExact(sourceURN+"|isA|"+relatedURN)),
				statecheck.ExpectKnownValue(containsAddr, tfjsonpath.New("relationship_type"), knownvalue.StringExact("hasA")),
				statecheck.ExpectKnownValue(containsAddr, tfjsonpath.New("id"), knownvalue.StringExact(sourceURN+"|hasA|"+relatedURN)),
			},
		},
		{
			// Import by composite ID.
			ResourceName:      inheritsAddr,
			ImportState:       true,
			ImportStateVerify: true,
		},
		{
			// Out-of-band removal: Read detects the missing edge and the next
			// apply re-creates it. The sibling hasA edge must survive both the
			// removal and the re-create (edges are owned one per resource).
			PreConfig: func() {
				client, err := datahub.NewClient(os.Getenv("DATAHUB_GMS_URL"), os.Getenv("DATAHUB_GMS_TOKEN"))
				if err != nil {
					panic(fmt.Sprintf("GlossaryTermRelationshipSteps PreConfig: %v", err))
				}
				if delErr := client.RemoveRelatedTerm(context.Background(), sourceURN, datahub.TermRelationshipIsA, relatedURN); delErr != nil {
					panic(fmt.Sprintf("GlossaryTermRelationshipSteps PreConfig: remove failed: %v", delErr))
				}
			},
			Config: cfg,
			ConfigPlanChecks: resource.ConfigPlanChecks{
				PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(inheritsAddr, plancheck.ResourceActionCreate),
					plancheck.ExpectResourceAction(containsAddr, plancheck.ResourceActionNoop),
				},
			},
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(inheritsAddr, tfjsonpath.New("id"), knownvalue.StringExact(sourceURN+"|isA|"+relatedURN)),
				statecheck.ExpectKnownValue(containsAddr, tfjsonpath.New("id"), knownvalue.StringExact(sourceURN+"|hasA|"+relatedURN)),
			},
		},
	}
}

// GlossaryTermRelationshipFromVariableSteps drives relationship_type from an
// input variable rather than a literal, per the design doc's "one non-literal
// input per resource" rule: a variable-fed attribute is unknown during the
// validate walk, so this catches any presence check shaped
// !x.IsNull() && x.ValueString() != "" that would misread unknown as absent
// (and any validator that fails to skip unknown).
func GlossaryTermRelationshipFromVariableSteps(sourceID, relatedID string) []resource.TestStep {
	cfg := providerBlock + `
variable "rel_type" {
  type = string
}
` + fmt.Sprintf(`
resource "datahub_glossary_term" "source" {
  term_id = %q
  name    = "Operating Margin"
}

resource "datahub_glossary_term" "related" {
  term_id = %q
  name    = "Margin Concept"
}

resource "datahub_glossary_term_relationship" "test" {
  term_urn          = datahub_glossary_term.source.urn
  relationship_type = var.rel_type
  related_term_urn  = datahub_glossary_term.related.urn
}
`, sourceID, relatedID)

	return []resource.TestStep{
		{
			Config: cfg,
			ConfigVariables: config.Variables{
				"rel_type": config.StringVariable("isA"),
			},
		},
	}
}

// GlossaryTermRelationshipSelfSteps asserts that a self-relationship is
// rejected at validate time, before any API call (so no terms are created).
func GlossaryTermRelationshipSelfSteps(termID string) []resource.TestStep {
	cfg := providerBlock + fmt.Sprintf(`
resource "datahub_glossary_term_relationship" "test" {
  term_urn          = "urn:li:glossaryTerm:%s"
  relationship_type = "isA"
  related_term_urn  = "urn:li:glossaryTerm:%s"
}
`, termID, termID)

	return []resource.TestStep{
		{
			Config:      cfg,
			ExpectError: regexp.MustCompile(`Self-relationship not allowed`),
		},
	}
}

// GlossaryTermRelationshipCheckDestroy verifies every
// datahub_glossary_term_relationship in the post-destroy state no longer
// exists as an edge in DataHub.
func GlossaryTermRelationshipCheckDestroy(s *terraform.State) error {
	client, err := datahub.NewClient(os.Getenv("DATAHUB_GMS_URL"), os.Getenv("DATAHUB_GMS_TOKEN"))
	if err != nil {
		return fmt.Errorf("CheckDestroy: failed to build DataHub client: %w", err)
	}
	ctx := context.Background()
	for _, rs := range s.RootModule().Resources {
		if rs.Type != "datahub_glossary_term_relationship" {
			continue
		}
		termURN := rs.Primary.Attributes["term_urn"]
		relationshipType := rs.Primary.Attributes["relationship_type"]
		relatedTermURN := rs.Primary.Attributes["related_term_urn"]
		exists, getErr := client.RelatedTermExists(ctx, termURN, relationshipType, relatedTermURN)
		if getErr != nil {
			return fmt.Errorf("CheckDestroy: unexpected error checking relationship %q -%s-> %q: %w", termURN, relationshipType, relatedTermURN, getErr)
		}
		if exists {
			return fmt.Errorf("relationship %q -%s-> %q still exists after destroy", termURN, relationshipType, relatedTermURN)
		}
	}
	return nil
}
