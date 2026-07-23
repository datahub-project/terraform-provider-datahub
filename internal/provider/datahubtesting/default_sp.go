// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahubtesting

import (
	"fmt"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

// Scenario step builders for provider-level default structured properties
// (defaults.structured_properties and the computed
// structured_properties_defaults per-property latch). The referenced property
// definition is created by a datahub_structured_property resource in an
// earlier step of the same scenario (defaults off), satisfying the
// create-before-reference requirement against both mock and live targets.
//
// Destroy-ordering rule (CAT-2583): every scenario's final applied state has
// the default values removed (latch released) BEFORE the framework's destroy.
// Destroying a property definition while entities still carry its values
// races DataHub's async property-delete side effect, which can resurrect a
// concurrently hard-deleted entity as a husk.

// spDefaultsProviderBlock builds a provider block with
// defaults.structured_properties mapping propURN to the given values, or a
// bare provider block when propURN is empty.
func spDefaultsProviderBlock(propURN string, values ...string) string {
	if propURN == "" {
		return "\nprovider \"datahub\" {}\n"
	}
	quoted := ""
	for i, v := range values {
		if i > 0 {
			quoted += ", "
		}
		quoted += fmt.Sprintf("%q", v)
	}
	return fmt.Sprintf("\nprovider \"datahub\" {\n  defaults = {\n    structured_properties = {\n      %q = [%s]\n    }\n  }\n}\n", propURN, quoted)
}

// spDefinitionConfig declares the marker property definition used by the
// scenarios, targeting the given entity types.
func spDefinitionConfig(propertyID string, entityTypes string) string {
	return fmt.Sprintf(`
resource "datahub_structured_property" "marker" {
  property_id  = %q
  value_type   = "string"
  entity_types = [%s]
}
`, propertyID, entityTypes)
}

// DomainSPDefaultsLifecycleSteps covers the full per-property latch lifecycle
// on datahub_domain: unlatched create (defaults off), latch-on via provider
// defaults, plan idempotency, value-change ripple, import while latched, and
// unlatch (values removed) before destroy.
func DomainSPDefaultsLifecycleSteps(domainID, propertyID string) []resource.TestStep {
	const addr = "datahub_domain.test"
	propURN := "urn:li:structuredProperty:" + propertyID
	domain := fmt.Sprintf(`
resource "datahub_domain" "test" {
  domain_id = %q
  name      = "SP Defaults Domain"
}
`, domainID)
	definition := spDefinitionConfig(propertyID, `"domain"`)
	without := spDefaultsProviderBlock("") + definition + domain
	with := func(value string) string {
		return spDefaultsProviderBlock(propURN, value) + definition + domain
	}
	spCheck := func(value string) statecheck.StateCheck {
		return statecheck.ExpectKnownValue(addr, tfjsonpath.New("structured_properties_defaults"), knownvalue.MapExact(map[string]knownvalue.Check{
			propURN: knownvalue.SetExact([]knownvalue.Check{knownvalue.StringExact(value)}),
		}))
	}

	return []resource.TestStep{
		{
			// Defaults off: definition + domain exist, latch released.
			Config: without,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("structured_properties_defaults"), knownvalue.Null()),
			},
		},
		{
			// Defaults on: the existing domain is latched and stamped.
			Config:            with("gold"),
			ConfigStateChecks: []statecheck.StateCheck{spCheck("gold")},
		},
		{
			Config:   with("gold"),
			PlanOnly: true,
		},
		{
			// Provider-default value change ripples as an in-place update.
			Config:            with("platinum"),
			ConfigStateChecks: []statecheck.StateCheck{spCheck("platinum")},
		},
		{
			ResourceName:      addr,
			ImportState:       true,
			ImportStateId:     domainID,
			ImportStateVerify: true,
		},
		{
			// Unlatch before destroy (see destroy-ordering rule above).
			Config: without,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("structured_properties_defaults"), knownvalue.Null()),
			},
		},
	}
}

// SPDefaultsEntityTypeFilteringSteps proves the entityTypes filter: a default
// property applicable only to domains stamps the domain and silently skips
// the corp group in the same config.
func SPDefaultsEntityTypeFilteringSteps(domainID, groupID, propertyID string) []resource.TestStep {
	const domainAddr = "datahub_domain.test"
	const groupAddr = "datahub_corp_group.test"
	propURN := "urn:li:structuredProperty:" + propertyID
	resources := spDefinitionConfig(propertyID, `"domain"`) + fmt.Sprintf(`
resource "datahub_domain" "test" {
  domain_id = %q
  name      = "SP Filtering Domain"
}

resource "datahub_corp_group" "test" {
  group_id = %q
  name     = "SP Filtering Group"
}
`, domainID, groupID)
	without := spDefaultsProviderBlock("") + resources
	with := spDefaultsProviderBlock(propURN, "prod") + resources

	return []resource.TestStep{
		{
			Config: without,
		},
		{
			Config: with,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(domainAddr, tfjsonpath.New("structured_properties_defaults"), knownvalue.MapExact(map[string]knownvalue.Check{
					propURN: knownvalue.SetExact([]knownvalue.Check{knownvalue.StringExact("prod")}),
				})),
				// corpGroup is not in the definition's entityTypes: skipped.
				statecheck.ExpectKnownValue(groupAddr, tfjsonpath.New("structured_properties_defaults"), knownvalue.Null()),
			},
		},
		{
			Config:   with,
			PlanOnly: true,
		},
		{
			// Unlatch before destroy.
			Config: without,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(domainAddr, tfjsonpath.New("structured_properties_defaults"), knownvalue.Null()),
			},
		},
	}
}

// SPDefaultsAllResourcesSteps exercises the SP-defaults latch on every
// SP-capable resource type other than domain (covered by the lifecycle
// scenario) in one config: glossary node + term, corp user, service account,
// corp group, data product, and data contract. The definition is created
// alone first (create-before-reference), then every resource is CREATED with
// defaults already on - the stamped-at-create path, which the latch-on-update
// scenarios cannot reach - replans clean, imports with the latch intact, and
// unlatches before destroy.
func SPDefaultsAllResourcesSteps(ids map[string]string, contractDatasetURN string) []resource.TestStep {
	propURN := "urn:li:structuredProperty:" + ids["prop"]
	definition := spDefinitionConfig(ids["prop"],
		`"glossaryTerm", "glossaryNode", "corpuser", "corpGroup", "dataProduct", "dataContract"`)
	resources := definition + fmt.Sprintf(`
resource "datahub_glossary_node" "test" {
  node_id = %q
  name    = "SP All Node"
}

resource "datahub_glossary_term" "test" {
  term_id     = %q
  name        = "SP All Term"
  parent_node = datahub_glossary_node.test.urn
}

resource "datahub_corp_user" "test" {
  username     = %q
  display_name = "SP All User"
}

resource "datahub_service_account" "test" {
  service_account_id = %q
  display_name       = "SP All Service Account"
}

resource "datahub_corp_group" "test" {
  group_id = %q
  name     = "SP All Group"
}

resource "datahub_domain" "home" {
  domain_id = %q
  name      = "SP All Home Domain"
}

# The domain also makes deleteDataProduct authorize via the domain path on
# servers without the domain-less-delete fix (OSS #18446).
resource "datahub_data_product" "test" {
  data_product_id = %q
  name            = "SP All Product"
  domain          = datahub_domain.home.urn
}

resource "datahub_custom_assertion" "dq" {
  entity_urn     = %q
  assertion_type = "CUSTOM"
  description    = "TF Example - SP all resources DQ"
  platform_urn   = "urn:li:dataPlatform:dbt"
}

resource "datahub_data_contract" "test" {
  dataset_urn                 = %q
  data_quality_assertion_urns = [datahub_custom_assertion.dq.urn]
}
`, ids["node"], ids["term"], ids["user"], ids["sa"], ids["group"], ids["domain"], ids["dp"], contractDatasetURN, contractDatasetURN)
	without := spDefaultsProviderBlock("") + resources
	with := spDefaultsProviderBlock(propURN, "finale") + resources

	addrs := []string{
		"datahub_glossary_node.test",
		"datahub_glossary_term.test",
		"datahub_corp_user.test",
		"datahub_service_account.test",
		"datahub_corp_group.test",
		"datahub_data_product.test",
		"datahub_data_contract.test",
	}
	stamped := make([]statecheck.StateCheck, 0, len(addrs))
	cleared := make([]statecheck.StateCheck, 0, len(addrs))
	for _, a := range addrs {
		stamped = append(stamped, statecheck.ExpectKnownValue(a, tfjsonpath.New("structured_properties_defaults"), knownvalue.MapExact(map[string]knownvalue.Check{
			propURN: knownvalue.SetExact([]knownvalue.Check{knownvalue.StringExact("finale")}),
		})))
		cleared = append(cleared, statecheck.ExpectKnownValue(a, tfjsonpath.New("structured_properties_defaults"), knownvalue.Null()))
	}

	steps := []resource.TestStep{
		{
			// The definition alone: provider configuration cannot depend on a
			// property created in the same apply.
			Config: spDefaultsProviderBlock("") + definition,
		},
		{
			// Every resource is created with defaults already on: the
			// stamped-at-create path.
			Config:            with,
			ConfigStateChecks: stamped,
		},
		{
			Config:   with,
			PlanOnly: true,
		},
	}
	// Import each resource while latched: import attribution must
	// reconstruct structured_properties_defaults on every type.
	for _, a := range addrs {
		steps = append(steps, resource.TestStep{
			ResourceName:      a,
			ImportState:       true,
			ImportStateVerify: true,
		})
	}
	return append(steps, resource.TestStep{
		// Unlatch before destroy (destroy-ordering rule above).
		Config:            without,
		ConfigStateChecks: cleared,
	})
}

// SPDefaultsAssignmentOverlapSteps exercises the deliberate-overlap path: an
// explicit assignment manages the SAME property URN the provider defaults
// manage, with matching values. The assignment resource emits a plan-time
// warning (warnings do not fail steps); because both sides write the same
// values the combined config still converges and replans clean.
func SPDefaultsAssignmentOverlapSteps(domainID, propertyID string) []resource.TestStep {
	const domainAddr = "datahub_domain.test"
	propURN := "urn:li:structuredProperty:" + propertyID
	resources := spDefinitionConfig(propertyID, `"domain"`) + fmt.Sprintf(`
resource "datahub_domain" "test" {
  domain_id = %q
  name      = "SP Overlap Domain"
}

resource "datahub_structured_property_assignment" "overlap" {
  entity_urn              = datahub_domain.test.urn
  structured_property_urn = datahub_structured_property.marker.urn
  values                  = ["shared-value"]
}
`, domainID)
	base := spDefinitionConfig(propertyID, `"domain"`) + fmt.Sprintf(`
resource "datahub_domain" "test" {
  domain_id = %q
  name      = "SP Overlap Domain"
}
`, domainID)
	with := spDefaultsProviderBlock(propURN, "shared-value") + resources

	return []resource.TestStep{
		{
			// The definition alone: provider configuration cannot depend on a
			// property created in the same apply.
			Config: spDefaultsProviderBlock("") + spDefinitionConfig(propertyID, `"domain"`),
		},
		{
			// Domain and assignment are created with defaults already on:
			// stamped at create, warning fires on the overlapping assignment.
			Config: with,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(domainAddr, tfjsonpath.New("structured_properties_defaults"), knownvalue.MapExact(map[string]knownvalue.Check{
					propURN: knownvalue.SetExact([]knownvalue.Check{knownvalue.StringExact("shared-value")}),
				})),
			},
		},
		{
			// Same values on both sides: no flip-flop, clean replan.
			Config:   with,
			PlanOnly: true,
		},
		{
			// Unlatch AND drop the assignment in the same apply: removing only
			// the defaults would delete the value out from under the still-
			// managed assignment and the post-apply refresh would show drift.
			Config: spDefaultsProviderBlock("") + base,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(domainAddr, tfjsonpath.New("structured_properties_defaults"), knownvalue.Null()),
			},
		},
	}
}

// SPDefaultsAssignmentCoexistenceSteps proves per-property ownership: a
// provider default (property A) and an explicit
// datahub_structured_property_assignment (property B) manage different
// properties on the same domain without fighting - both persist and replans
// are empty.
func SPDefaultsAssignmentCoexistenceSteps(domainID, defaultPropID, assignedPropID string) []resource.TestStep {
	const domainAddr = "datahub_domain.test"
	const assignAddr = "datahub_structured_property_assignment.explicit"
	defaultPropURN := "urn:li:structuredProperty:" + defaultPropID
	resources := fmt.Sprintf(`
resource "datahub_structured_property" "marker" {
  property_id  = %q
  value_type   = "string"
  entity_types = ["domain"]
}

resource "datahub_structured_property" "assigned" {
  property_id  = %q
  value_type   = "string"
  entity_types = ["domain"]
}

resource "datahub_domain" "test" {
  domain_id = %q
  name      = "SP Coexistence Domain"
}

resource "datahub_structured_property_assignment" "explicit" {
  entity_urn              = datahub_domain.test.urn
  structured_property_urn = datahub_structured_property.assigned.urn
  values                  = ["assigned-value"]
}
`, defaultPropID, assignedPropID, domainID)
	without := spDefaultsProviderBlock("") + resources
	with := spDefaultsProviderBlock(defaultPropURN, "default-value") + resources

	return []resource.TestStep{
		{
			Config: without,
		},
		{
			Config: with,
			ConfigStateChecks: []statecheck.StateCheck{
				// The default tracks ONLY its own property.
				statecheck.ExpectKnownValue(domainAddr, tfjsonpath.New("structured_properties_defaults"), knownvalue.MapExact(map[string]knownvalue.Check{
					defaultPropURN: knownvalue.SetExact([]knownvalue.Check{knownvalue.StringExact("default-value")}),
				})),
				// The explicit assignment is untouched by the default's writes.
				statecheck.ExpectKnownValue(assignAddr, tfjsonpath.New("values"), knownvalue.SetExact([]knownvalue.Check{knownvalue.StringExact("assigned-value")})),
			},
		},
		{
			// No fight: replanning the combined config is empty.
			Config:   with,
			PlanOnly: true,
		},
		{
			// Unlatch before destroy.
			Config: without,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(domainAddr, tfjsonpath.New("structured_properties_defaults"), knownvalue.Null()),
			},
		},
	}
}
