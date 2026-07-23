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
