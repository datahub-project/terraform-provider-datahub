// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahubtesting

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

// This file holds scenario step builders for the provider-level defaults
// feature (defaults.custom_properties, auto_properties,
// auto_property_strategy). datahub_domain is the vehicle: the merge, plan,
// read-reconciliation, and import logic is shared code (defaults.go in the
// provider package), so exercising one resource covers the engine; the other
// resources' own lifecycle tests cover their wiring.

// defaultsProviderBlock builds a provider block with the given body (already
// indented HCL attribute lines, or empty for a bare block).
func defaultsProviderBlock(body string) string {
	if body == "" {
		return "\nprovider \"datahub\" {}\n"
	}
	return "\nprovider \"datahub\" {\n" + body + "\n}\n"
}

// defaultsDomainConfig composes a provider block and a single test domain.
// extraAttrs is injected verbatim into the resource body.
func defaultsDomainConfig(providerBody, domainID, extraAttrs string) string {
	return defaultsProviderBlock(providerBody) + fmt.Sprintf(`
resource "datahub_domain" "test" {
  domain_id = %q
  name      = "Defaults Domain"
%s
}
`, domainID, extraAttrs)
}

// DomainDefaultsCustomPropertiesSteps covers defaults.custom_properties:
// merge into custom_properties_all at create, provider-default change
// rippling to the resource as an in-place update, defaults removal clearing
// the default-sourced keys (while the CREATION_ONLY marker carries forward),
// and an import round-trip.
func DomainDefaultsCustomPropertiesSteps(domainID string) []resource.TestStep {
	const addr = "datahub_domain.test"
	resourceCP := `  custom_properties = {
    tier = "gold"
  }`
	defaultsWith := func(env string) string {
		return fmt.Sprintf(`  defaults = {
    custom_properties = {
      team = "platform"
      env  = %q
    }
  }`, env)
	}
	return []resource.TestStep{
		{
			Config: defaultsDomainConfig(defaultsWith("dev"), domainID, resourceCP),
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("custom_properties"), knownvalue.MapExact(map[string]knownvalue.Check{
					"tier": knownvalue.StringExact("gold"),
				})),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("custom_properties_all"), knownvalue.MapExact(map[string]knownvalue.Check{
					"managed-by": knownvalue.StringExact("terraform"),
					"team":       knownvalue.StringExact("platform"),
					"env":        knownvalue.StringExact("dev"),
					"tier":       knownvalue.StringExact("gold"),
				})),
			},
		},
		{
			// Changing only the provider default must ripple to the resource
			// as an in-place update of custom_properties_all.
			Config: defaultsDomainConfig(defaultsWith("prod"), domainID, resourceCP),
			ConfigPlanChecks: resource.ConfigPlanChecks{
				PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(addr, plancheck.ResourceActionUpdate),
				},
			},
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("custom_properties_all"), knownvalue.MapExact(map[string]knownvalue.Check{
					"managed-by": knownvalue.StringExact("terraform"),
					"team":       knownvalue.StringExact("platform"),
					"env":        knownvalue.StringExact("prod"),
					"tier":       knownvalue.StringExact("gold"),
				})),
			},
		},
		{
			// Removing the defaults block clears the default-sourced keys.
			// The managed-by marker survives: CREATION_ONLY carries forward
			// markers already present in state.
			Config: defaultsDomainConfig("", domainID, resourceCP),
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("custom_properties_all"), knownvalue.MapExact(map[string]knownvalue.Check{
					"managed-by": knownvalue.StringExact("terraform"),
					"tier":       knownvalue.StringExact("gold"),
				})),
			},
		},
		{
			ResourceName:      addr,
			ImportState:       true,
			ImportStateId:     domainID,
			ImportStateVerify: true,
		},
	}
}

// DomainAutoPropertiesLifecycleSteps covers the auto-property markers: the
// built-in managed-by default with a bare provider block, plan idempotency,
// enabling provider-version under PROACTIVE, and disabling markers entirely
// via auto_properties = [] (removal regardless of strategy).
func DomainAutoPropertiesLifecycleSteps(domainID string) []resource.TestStep {
	const addr = "datahub_domain.test"
	return []resource.TestStep{
		{
			Config: defaultsDomainConfig("", domainID, ""),
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("custom_properties"), knownvalue.Null()),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("custom_properties_all"), knownvalue.MapExact(map[string]knownvalue.Check{
					"managed-by": knownvalue.StringExact("terraform"),
				})),
			},
		},
		{
			// Idempotency: replanning the identical config must be empty.
			Config:   defaultsDomainConfig("", domainID, ""),
			PlanOnly: true,
		},
		{
			// PROACTIVE adds the provider-version marker to the existing
			// resource with its live value (the test provider version).
			Config: defaultsDomainConfig(
				`  auto_properties        = ["managed-by", "provider-version"]
  auto_property_strategy = "PROACTIVE"`, domainID, ""),
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("custom_properties_all"), knownvalue.MapExact(map[string]knownvalue.Check{
					"managed-by":       knownvalue.StringExact("terraform"),
					"provider-version": knownvalue.StringExact("test"),
				})),
			},
		},
		{
			// Explicit disable removes all markers estate-wide, even under
			// the default CREATION_ONLY strategy.
			Config: defaultsDomainConfig(`  auto_properties = []`, domainID, ""),
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("custom_properties_all"), knownvalue.Null()),
			},
		},
	}
}

// DomainAutoPropertiesDisabledSteps covers the plain opt-out journey: a user
// who only wants to disable the markers. Created with auto_properties = [],
// nothing extra is ever written - resource-level custom_properties behave
// exactly as before the feature existed, replans stay empty, and an import
// round-trips cleanly.
func DomainAutoPropertiesDisabledSteps(domainID string) []resource.TestStep {
	const addr = "datahub_domain.test"
	disabled := `  auto_properties = []`
	resourceCP := `  custom_properties = {
    tier = "gold"
  }`
	return []resource.TestStep{
		{
			// No resource properties either: nothing is written at all.
			Config: defaultsDomainConfig(disabled, domainID, ""),
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("custom_properties"), knownvalue.Null()),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("custom_properties_all"), knownvalue.Null()),
			},
		},
		{
			// Resource-level custom properties pass through untouched: _all
			// is exactly the resource map, no marker mixed in.
			Config: defaultsDomainConfig(disabled, domainID, resourceCP),
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("custom_properties"), knownvalue.MapExact(map[string]knownvalue.Check{
					"tier": knownvalue.StringExact("gold"),
				})),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("custom_properties_all"), knownvalue.MapExact(map[string]knownvalue.Check{
					"tier": knownvalue.StringExact("gold"),
				})),
			},
		},
		{
			// Stability: replanning the identical disabled config is empty.
			Config:   defaultsDomainConfig(disabled, domainID, resourceCP),
			PlanOnly: true,
		},
		{
			ResourceName:      addr,
			ImportState:       true,
			ImportStateId:     domainID,
			ImportStateVerify: true,
		},
	}
}

// DomainAutoPropertyStrategySteps covers the CREATION_ONLY upgrade fence: a
// resource created without markers (simulating an estate from before the
// feature) sees an empty plan when markers become enabled, gets stamped by a
// one-time PROACTIVE pass, and then stays stable when the strategy returns to
// CREATION_ONLY (value carry-forward).
func DomainAutoPropertyStrategySteps(domainID string) []resource.TestStep {
	const addr = "datahub_domain.test"
	return []resource.TestStep{
		{
			// Simulate a pre-feature estate: no markers stamped.
			Config: defaultsDomainConfig(`  auto_properties = []`, domainID, ""),
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("custom_properties_all"), knownvalue.Null()),
			},
		},
		{
			// Markers on (built-in default) + CREATION_ONLY (built-in
			// default): the existing, unstamped resource must plan EMPTY -
			// this is the upgrade-silence guarantee.
			Config:   defaultsDomainConfig("", domainID, ""),
			PlanOnly: true,
		},
		{
			// One-time PROACTIVE convergence pass stamps the estate.
			Config: defaultsDomainConfig(`  auto_property_strategy = "PROACTIVE"`, domainID, ""),
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("custom_properties_all"), knownvalue.MapExact(map[string]knownvalue.Check{
					"managed-by": knownvalue.StringExact("terraform"),
				})),
			},
		},
		{
			// Back to CREATION_ONLY: the stamped marker carries forward, so
			// the plan is empty again.
			Config:   defaultsDomainConfig("", domainID, ""),
			PlanOnly: true,
		},
	}
}

// DomainDefaultsCollisionSteps covers key collisions between resource-level
// custom_properties and provider defaults: same-value overlap must be
// perfectly stable (the AWS default_tags perpetual-diff trap), and a
// differing resource value wins over the default.
func DomainDefaultsCollisionSteps(domainID string) []resource.TestStep {
	const addr = "datahub_domain.test"
	defaults := `  defaults = {
    custom_properties = {
      env = "prod"
    }
  }`
	sameValue := `  custom_properties = {
    env = "prod"
  }`
	differing := `  custom_properties = {
    env = "dev"
  }`
	return []resource.TestStep{
		{
			Config: defaultsDomainConfig(defaults, domainID, sameValue),
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("custom_properties_all"), knownvalue.MapExact(map[string]knownvalue.Check{
					"managed-by": knownvalue.StringExact("terraform"),
					"env":        knownvalue.StringExact("prod"),
				})),
			},
		},
		{
			// Same key, same value at both levels: no perpetual diff.
			Config:   defaultsDomainConfig(defaults, domainID, sameValue),
			PlanOnly: true,
		},
		{
			// Differing value: the resource wins (a plan-time warning fires;
			// warnings are asserted in the engine's unit tests).
			Config: defaultsDomainConfig(defaults, domainID, differing),
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("custom_properties_all"), knownvalue.MapExact(map[string]knownvalue.Check{
					"managed-by": knownvalue.StringExact("terraform"),
					"env":        knownvalue.StringExact("dev"),
				})),
			},
		},
	}
}

// DomainDefaultsExternalEditSteps covers full-map ownership against external
// edits: a property added outside Terraform surfaces as drift on
// custom_properties_all and is removed by the next apply. Mock-only: the
// simulated external edit writes the raw domainProperties aspect, which would
// need the entity's full current aspect state to be safe against a live
// server.
func DomainDefaultsExternalEditSteps(domainID string) []resource.TestStep {
	const addr = "datahub_domain.test"
	resourceCP := `  custom_properties = {
    tier = "gold"
  }`
	cfg := defaultsDomainConfig("", domainID, resourceCP)
	checks := []statecheck.StateCheck{
		statecheck.ExpectKnownValue(addr, tfjsonpath.New("custom_properties_all"), knownvalue.MapExact(map[string]knownvalue.Check{
			"managed-by": knownvalue.StringExact("terraform"),
			"tier":       knownvalue.StringExact("gold"),
		})),
	}
	return []resource.TestStep{
		{
			Config:            cfg,
			ConfigStateChecks: checks,
		},
		{
			// Simulate a UI user adding a property outside Terraform, then
			// re-apply: refresh surfaces the intruder on _all, apply stomps it.
			PreConfig: func() {
				url := os.Getenv("DATAHUB_GMS_URL") + "/openapi/v3/entity/domain"
				body := fmt.Sprintf(`[{"urn":"urn:li:domain:%s","domainProperties":{"value":{"name":"Defaults Domain","customProperties":{"managed-by":"terraform","tier":"gold","intruder":"external"}}}}]`, domainID)
				resp, err := http.Post(url, "application/json", strings.NewReader(body)) //nolint:noctx
				if err != nil {
					panic(fmt.Sprintf("DomainDefaultsExternalEditSteps PreConfig: POST external edit: %v", err))
				}
				defer func() { _ = resp.Body.Close() }()
				if resp.StatusCode >= 300 {
					panic(fmt.Sprintf("DomainDefaultsExternalEditSteps PreConfig: unexpected status %d", resp.StatusCode))
				}
			},
			Config: cfg,
			ConfigPlanChecks: resource.ConfigPlanChecks{
				PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(addr, plancheck.ResourceActionUpdate),
				},
			},
			ConfigStateChecks: checks,
		},
	}
}
