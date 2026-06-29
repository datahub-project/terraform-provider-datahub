// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

// Package targets registers every DataHub import target with the importtarget
// registry. It is the single source of truth for "which resource types can be
// enumerated and imported, and how a URN maps to an import ID".
//
// Import it for its side effects from both consumers:
//
//		import _ "github.com/datahub-project/terraform-provider-datahub/internal/provider/importtarget/targets"
//
//	  - the provider (internal/provider) imports it so the enumeration data
//	    sources (data.datahub_domains, ...) and the coverage test see every entry;
//	  - the datahub-tf-extract CLI imports it so bulk enumeration covers the same
//	    set of types the provider supports.
//
// Crucially this package depends only on importtarget and pkg/datahub - never on
// the Terraform plugin framework - so the CLI binary stays free of provider
// runtime dependencies. Previously the registrations lived as per-resource
// init() blocks in package provider (framework-coupled, so the CLI could not
// import them) and were *duplicated* by a hand-maintained list in the CLI's own
// reg package, which silently lagged: the CLI enumerated only 4 of the 18 types
// the provider could actually import. Consolidating here removes that drift.
//
// URN prefixes are inlined as literals (matching the historical CLI reg list)
// rather than referencing the package-provider constants, because those
// constants live in the framework-coupled provider package. Each prefix mirrors
// the corresponding <type>URNPrefix constant in the resource files.
package targets

import (
	"context"
	"fmt"
	"strings"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/importtarget"
	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
)

func init() {
	// Ordering matters for the CLI's `terraform plan -generate-config-out` pass:
	// datahub_secret's value attribute is Required + WriteOnly, which makes that
	// command exit non-zero as soon as it reaches a secret. Terraform stops
	// generating after the first such error, so every other type must be
	// registered before datahub_secret to have its config block generated.
	// datahub_secret is therefore registered last.

	// --- Enumerable, no Required+WriteOnly attributes ---

	importtarget.Register(importtarget.Target{
		ResourceTypeName:   "datahub_ingestion_source",
		DataSourceTypeName: "datahub_ingestion_sources",
		Enumerate: func(ctx context.Context, c *datahub.Client) ([]string, error) {
			return c.ListIngestionSourceURNs(ctx)
		},
		IDFromURN:     func(urn string) string { return strings.TrimPrefix(urn, "urn:li:dataHubIngestionSource:") },
		OSSCompatible: true,
	})

	importtarget.Register(importtarget.Target{
		ResourceTypeName:   "datahub_connection",
		DataSourceTypeName: "datahub_connections",
		Enumerate: func(ctx context.Context, c *datahub.Client) ([]string, error) {
			all, err := c.ListConnectionURNs(ctx)
			if err != nil {
				return nil, fmt.Errorf("listing connection URNs: %w", err)
			}
			// Skip system and OAuth connections that the provider cannot manage.
			// DataHub Cloud creates internal connections with IDs like
			// "urn_li_corpuser_alice@example.com__urn_li_service_<uuid>" (OAuth)
			// and "__system_teams-0" (system). These have unknown platform types
			// that the datahub_connection resource cannot model, so importing them
			// fails with a provider error and aborts generate-config-out.
			const prefix = "urn:li:dataHubConnection:"
			var filtered []string
			for _, urn := range all {
				id := strings.TrimPrefix(urn, prefix)
				if strings.HasPrefix(id, "urn_li_") || strings.HasPrefix(id, "__") {
					continue
				}
				filtered = append(filtered, urn)
			}
			return filtered, nil
		},
		IDFromURN:     func(urn string) string { return strings.TrimPrefix(urn, "urn:li:dataHubConnection:") },
		OSSCompatible: true,
	})

	importtarget.Register(importtarget.Target{
		ResourceTypeName:   "datahub_domain",
		DataSourceTypeName: "datahub_domains",
		Enumerate: func(ctx context.Context, c *datahub.Client) ([]string, error) {
			urns, err := c.ListDomainURNs(ctx)
			if err != nil {
				return nil, fmt.Errorf("listing domain URNs: %w", err)
			}
			return urns, nil
		},
		IDFromURN:     func(urn string) string { return strings.TrimPrefix(urn, "urn:li:domain:") },
		OSSCompatible: true,
	})

	importtarget.Register(importtarget.Target{
		ResourceTypeName:   "datahub_tag",
		DataSourceTypeName: "datahub_tags",
		Enumerate: func(ctx context.Context, c *datahub.Client) ([]string, error) {
			urns, err := c.ListTagURNs(ctx)
			if err != nil {
				return nil, fmt.Errorf("listing tag URNs: %w", err)
			}
			return urns, nil
		},
		IDFromURN:     func(urn string) string { return strings.TrimPrefix(urn, "urn:li:tag:") },
		OSSCompatible: true,
	})

	importtarget.Register(importtarget.Target{
		ResourceTypeName:   "datahub_glossary_node",
		DataSourceTypeName: "datahub_glossary_nodes",
		Enumerate: func(ctx context.Context, c *datahub.Client) ([]string, error) {
			urns, err := c.ListGlossaryNodeURNs(ctx)
			if err != nil {
				return nil, fmt.Errorf("listing glossary node URNs: %w", err)
			}
			return urns, nil
		},
		IDFromURN:     func(urn string) string { return strings.TrimPrefix(urn, "urn:li:glossaryNode:") },
		OSSCompatible: true,
	})

	importtarget.Register(importtarget.Target{
		ResourceTypeName:   "datahub_glossary_term",
		DataSourceTypeName: "datahub_glossary_terms",
		Enumerate: func(ctx context.Context, c *datahub.Client) ([]string, error) {
			urns, err := c.ListGlossaryTermURNs(ctx)
			if err != nil {
				return nil, fmt.Errorf("listing glossary term URNs: %w", err)
			}
			return urns, nil
		},
		IDFromURN:     func(urn string) string { return strings.TrimPrefix(urn, "urn:li:glossaryTerm:") },
		OSSCompatible: true,
	})

	importtarget.Register(importtarget.Target{
		ResourceTypeName:   "datahub_structured_property",
		DataSourceTypeName: "datahub_structured_properties",
		Enumerate: func(ctx context.Context, c *datahub.Client) ([]string, error) {
			urns, err := c.ListStructuredPropertyURNs(ctx)
			if err != nil {
				return nil, fmt.Errorf("listing structured property URNs: %w", err)
			}
			return urns, nil
		},
		IDFromURN:     func(urn string) string { return strings.TrimPrefix(urn, "urn:li:structuredProperty:") },
		OSSCompatible: true,
	})

	importtarget.Register(importtarget.Target{
		ResourceTypeName:   "datahub_data_product",
		DataSourceTypeName: "datahub_data_products",
		Enumerate: func(ctx context.Context, c *datahub.Client) ([]string, error) {
			urns, err := c.ListDataProductURNs(ctx)
			if err != nil {
				return nil, fmt.Errorf("listing data product URNs: %w", err)
			}
			return urns, nil
		},
		IDFromURN:     func(urn string) string { return strings.TrimPrefix(urn, "urn:li:dataProduct:") },
		OSSCompatible: true,
	})

	importtarget.Register(importtarget.Target{
		ResourceTypeName:   "datahub_ownership_type",
		DataSourceTypeName: "datahub_ownership_types",
		Enumerate: func(ctx context.Context, c *datahub.Client) ([]string, error) {
			urns, err := c.ListOwnershipTypeURNs(ctx)
			if err != nil {
				return nil, fmt.Errorf("listing ownership type URNs: %w", err)
			}
			return urns, nil
		},
		IDFromURN:     func(urn string) string { return strings.TrimPrefix(urn, "urn:li:ownershipType:") },
		OSSCompatible: true,
	})

	importtarget.Register(importtarget.Target{
		ResourceTypeName:   "datahub_policy",
		DataSourceTypeName: "datahub_policies",
		Enumerate: func(ctx context.Context, c *datahub.Client) ([]string, error) {
			urns, err := c.ListPolicyURNs(ctx)
			if err != nil {
				return nil, fmt.Errorf("listing policy URNs: %w", err)
			}
			return urns, nil
		},
		IDFromURN:     func(urn string) string { return strings.TrimPrefix(urn, "urn:li:dataHubPolicy:") },
		OSSCompatible: true,
	})

	importtarget.Register(importtarget.Target{
		ResourceTypeName:   "datahub_corp_group",
		DataSourceTypeName: "datahub_corp_groups",
		Enumerate: func(ctx context.Context, c *datahub.Client) ([]string, error) {
			urns, err := c.ListGroupURNs(ctx)
			if err != nil {
				return nil, fmt.Errorf("listing group URNs: %w", err)
			}
			return urns, nil
		},
		IDFromURN:     func(urn string) string { return strings.TrimPrefix(urn, "urn:li:corpGroup:") },
		OSSCompatible: true,
	})

	importtarget.Register(importtarget.Target{
		ResourceTypeName:   "datahub_corp_user",
		DataSourceTypeName: "datahub_corp_user",
		Enumerate: func(ctx context.Context, c *datahub.Client) ([]string, error) {
			urns, err := c.ListCorpUserURNs(ctx)
			if err != nil {
				return nil, fmt.Errorf("listing corp user URNs: %w", err)
			}
			return urns, nil
		},
		IDFromURN:     func(urn string) string { return strings.TrimPrefix(urn, "urn:li:corpuser:") },
		OSSCompatible: true,
	})

	// Custom assertions are OSS-compatible and support enumeration for bulk
	// import. Enumerate CUSTOM-type assertions only: the `assertion` entity type
	// is shared by monitor (freshness/volume/sql) and native (dataset/field)
	// assertions, which datahub_custom_assertion does not model. The import ID is
	// the full assertion URN.
	importtarget.Register(importtarget.Target{
		ResourceTypeName:   "datahub_custom_assertion",
		DataSourceTypeName: "datahub_assertions",
		Enumerate: func(ctx context.Context, c *datahub.Client) ([]string, error) {
			urns, err := c.ListCustomAssertionURNs(ctx)
			if err != nil {
				return nil, fmt.Errorf("listing custom assertion URNs: %w", err)
			}
			return urns, nil
		},
		IDFromURN:     func(urn string) string { return urn },
		OSSCompatible: true,
	})

	// --- Cloud-only, no auto-enumeration (import by explicit URN) ---

	// The Cloud-only monitor assertion types share the `assertion` entity type
	// with custom, ingested (EXTERNAL), and auto (INFERRED) assertions, so each
	// enumerator filters to source==NATIVE and the sub-shape its resource models
	// (see List*AssertionURNs). EXTERNAL (e.g. dbt tests) and INFERRED (smart/AI)
	// assertions are owned by the producing system and are intentionally never
	// enumerated for import. The import ID is the full assertion URN.
	importtarget.Register(importtarget.Target{
		ResourceTypeName:   "datahub_freshness_assertion",
		DataSourceTypeName: "datahub_assertions",
		Enumerate: func(ctx context.Context, c *datahub.Client) ([]string, error) {
			urns, err := c.ListFreshnessAssertionURNs(ctx)
			if err != nil {
				return nil, fmt.Errorf("listing freshness assertion URNs: %w", err)
			}
			return urns, nil
		},
		IDFromURN:     func(urn string) string { return urn },
		OSSCompatible: false,
	})

	importtarget.Register(importtarget.Target{
		ResourceTypeName:   "datahub_volume_assertion",
		DataSourceTypeName: "datahub_assertions",
		Enumerate: func(ctx context.Context, c *datahub.Client) ([]string, error) {
			urns, err := c.ListVolumeAssertionURNs(ctx)
			if err != nil {
				return nil, fmt.Errorf("listing volume assertion URNs: %w", err)
			}
			return urns, nil
		},
		IDFromURN:     func(urn string) string { return urn },
		OSSCompatible: false,
	})

	importtarget.Register(importtarget.Target{
		ResourceTypeName:   "datahub_sql_assertion",
		DataSourceTypeName: "datahub_assertions",
		Enumerate: func(ctx context.Context, c *datahub.Client) ([]string, error) {
			urns, err := c.ListSQLAssertionURNs(ctx)
			if err != nil {
				return nil, fmt.Errorf("listing sql assertion URNs: %w", err)
			}
			return urns, nil
		},
		IDFromURN:     func(urn string) string { return urn },
		OSSCompatible: false,
	})

	importtarget.Register(importtarget.Target{
		ResourceTypeName:   "datahub_field_assertion",
		DataSourceTypeName: "datahub_assertions",
		Enumerate: func(ctx context.Context, c *datahub.Client) ([]string, error) {
			urns, err := c.ListFieldAssertionURNs(ctx)
			if err != nil {
				return nil, fmt.Errorf("listing field assertion URNs: %w", err)
			}
			return urns, nil
		},
		IDFromURN:     func(urn string) string { return urn },
		OSSCompatible: false,
	})

	importtarget.Register(importtarget.Target{
		ResourceTypeName:   "datahub_schema_assertion",
		DataSourceTypeName: "datahub_assertions",
		Enumerate: func(ctx context.Context, c *datahub.Client) ([]string, error) {
			urns, err := c.ListSchemaAssertionURNs(ctx)
			if err != nil {
				return nil, fmt.Errorf("listing schema assertion URNs: %w", err)
			}
			return urns, nil
		},
		IDFromURN:     func(urn string) string { return urn },
		OSSCompatible: false,
	})

	// Action pipelines (dataHubAction) are Cloud-only and enumerable via
	// listActionPipelines. The import ID is the bare action_id (URN suffix);
	// ImportState also accepts the full URN. The shared-instance/selective-
	// extraction caveat applies (a shared instance may hold pipelines created
	// elsewhere).
	importtarget.Register(importtarget.Target{
		ResourceTypeName:   "datahub_action_pipeline",
		DataSourceTypeName: "datahub_action_pipelines",
		Enumerate: func(ctx context.Context, c *datahub.Client) ([]string, error) {
			urns, err := c.ListActionPipelineURNs(ctx)
			if err != nil {
				return nil, fmt.Errorf("listing action pipeline URNs: %w", err)
			}
			return urns, nil
		},
		IDFromURN:     func(urn string) string { return strings.TrimPrefix(urn, "urn:li:dataHubAction:") },
		OSSCompatible: false,
	})

	// Remote executor pools are Cloud-only with no list API reachable via OSS
	// GraphQL; users supply pool IDs manually when importing.
	importtarget.Register(importtarget.Target{
		ResourceTypeName:   "datahub_remote_executor_pool",
		DataSourceTypeName: "",
		Enumerate:          nil,
		IDFromURN:          func(urn string) string { return strings.TrimPrefix(urn, "urn:li:dataHubRemoteExecutorPool:") },
		OSSCompatible:      false,
	})

	// --- Relationship / composite resources: no single-URN import identity ---

	// datahub_corp_group_member identifies a (group, member) pair, not a single
	// URN, so IDFromURN does not apply and there is no enumeration data source.
	importtarget.Register(importtarget.Target{
		ResourceTypeName:   "datahub_corp_group_member",
		DataSourceTypeName: "",
		Enumerate:          nil,
		IDFromURN:          nil,
		OSSCompatible:      true,
	})

	// datahub_role_assignment identifies a (role, actor) pair, so the
	// single-URN IDFromURN extraction does not apply.
	importtarget.Register(importtarget.Target{
		ResourceTypeName:   "datahub_role_assignment",
		DataSourceTypeName: "",
		Enumerate:          nil,
		IDFromURN:          nil,
		OSSCompatible:      true,
	})

	// datahub_local_user_login manages native credentials for a corp user; it
	// has no list API of its own and imports by the user's URN.
	importtarget.Register(importtarget.Target{
		ResourceTypeName:   "datahub_local_user_login",
		DataSourceTypeName: "",
		Enumerate:          nil,
		IDFromURN:          func(urn string) string { return strings.TrimPrefix(urn, "urn:li:corpuser:") },
		OSSCompatible:      true,
	})

	// --- Required+WriteOnly: must be registered LAST (see ordering note above) ---

	importtarget.Register(importtarget.Target{
		ResourceTypeName:   "datahub_secret",
		DataSourceTypeName: "datahub_secrets",
		Enumerate: func(ctx context.Context, c *datahub.Client) ([]string, error) {
			return c.ListSecretURNs(ctx)
		},
		// ImportState accepts both the full URN and the bare name; pass the full
		// URN so the provider can validate the prefix.
		IDFromURN:     func(urn string) string { return urn },
		OSSCompatible: true,
	})
}
