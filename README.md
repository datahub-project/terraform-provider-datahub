# Terraform Provider DataHub

[![CI](https://github.com/datahub-project/terraform-provider-datahub/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/datahub-project/terraform-provider-datahub/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/datahub-project/terraform-provider-datahub/graph/badge.svg)](https://codecov.io/gh/datahub-project/terraform-provider-datahub)
[![Terraform Registry Downloads](https://img.shields.io/terraform/provider/dw/1896655?logo=terraform&logoColor=white&labelColor=623CE4&color=4D27AA)](https://registry.terraform.io/providers/datahub-project/datahub/latest/docs)

Terraform provider for DataHub. Manage ingestion, governance taxonomy, access control, and more as code.

This provider is implemented with the Terraform Plugin Framework and talks to DataHub via its OpenAPI v3 and GraphQL APIs.

## What it supports

**Ingestion**

| Component | Type | Notes |
|---|---|---|
| `datahub_ingestion_source` | resource + data source | Ingestion source from a recipe JSON string. |
| `datahub_secret` | resource | Named encrypted secret; reference as `${SECRET_NAME}` in recipes. |
| `datahub_connection` | resource | Reusable, encrypted credential config for data platforms. |
| `datahub_ingestion_sources` | data source | Enumerate all ingestion source URNs. |
| `datahub_secrets` | data source | Enumerate all secret URNs (values are never returned). |
| `datahub_connections` | data source | Enumerate all connection URNs. |
| `datahub_remote_executor_pool` | resource + data source | Private executor pool for VPC-hosted sources. **DataHub Cloud only.** |

**Governance taxonomy**

| Component | Type | Notes |
|---|---|---|
| `datahub_domain` | resource + data source | DataHub domains; nestable via `parent_domain`. |
| `datahub_domains` | data source | Enumerate all domain URNs for bulk import. |
| `datahub_glossary_node` | resource + data source | Glossary term groups; nestable via `parent_node`. |
| `datahub_glossary_nodes` | data source | Enumerate all glossary node URNs for bulk import. |
| `datahub_glossary_term` | resource + data source | Glossary terms; associated to a node via `parent_node`. |
| `datahub_glossary_terms` | data source | Enumerate all glossary term URNs for bulk import. |

**Access control**

| Component | Type | Notes |
|---|---|---|
| `datahub_corp_group` | resource + data source | Native DataHub groups. |
| `datahub_corp_groups` | data source | Enumerate all group URNs for bulk import. |
| `datahub_corp_group_member` | resource | Membership edge (one user in one group). |
| `datahub_corp_user` | resource + data source | User catalog profile (`corpUserInfo` aspects). |
| `datahub_local_user_login` | resource | Native-auth login credentials via the signUp flow. |
| `datahub_role` | data source | Resolve a built-in role name to its URN. |
| `datahub_roles` | data source | Enumerate all built-in role URNs. |
| `datahub_role_assignment` | resource | Assign a built-in role to a user or group. |
| `datahub_policy` | resource | PLATFORM and METADATA access policies with full actor/privilege/resource control. |
| `datahub_policies` | data source | Enumerate all policy URNs for bulk import. |

**Identity**

| Component | Type | Notes |
|---|---|---|
| `datahub_me` | data source | Authenticated user's identity; useful for smoke-testing provider credentials at plan time. |

Generated docs live under `docs/`.

## Requirements

- [Terraform](https://developer.hashicorp.com/terraform/downloads) >= 1.11
- [Go](https://go.dev/doc/install) >= 1.26

## Provider configuration

The provider needs to reach DataHub GMS.

Common approaches:

- Set `DATAHUB_GMS_URL` and `DATAHUB_GMS_TOKEN` environment variables.
- Or configure them in the provider block.

See `examples/runnable/provider-install-verification/` for a working development setup.

## Security / Credentials

DataHub ingestion source configurations (including the recipe JSON) are stored in DataHub. If you embed credentials (tokens, passwords, private keys) directly into the recipe/config, they can end up stored in DataHub metadata and exposed to users/services with access to view ingestion source configs. This provider does not “magically” change that behavior.

Recommended approaches:

- Use **`datahub_secret`** to manage secrets as Terraform resources, then reference them by name in recipes as `${SECRET_NAME}`. This keeps secret values out of your recipe config and out of source control. See `examples/runnable/secret-basic/` for a working example.
- Use **DataHub Secrets via the UI** (Ingestion → Secrets) if you prefer to manage them outside Terraform, then reference them the same way.
- Use **environment variable substitution** in recipes (DataHub expands `${VAR_NAME}` in config).

Terraform note: if you need a literal `${VAR_NAME}` to reach DataHub (for DataHub substitution), write it as `$${VAR_NAME}` in Terraform strings to prevent Terraform interpolation.

References:

- https://docs.datahub.com/docs/ui-ingestion/#configuring-secrets
- https://docs.datahub.com/docs/metadata-ingestion/recipe_overview#handling-sensitive-information-in-recipes
- https://docs.datahub.com/docs/metadata-ingestion/recipe_overview#loading-sensitive-data-as-files-in-recipes

## Building and contributing

See [BUILDING.md](BUILDING.md) for build instructions, test commands, coverage reports, linting, and doc generation.

## License

This project is licensed under the Apache License, Version 2.0. See [LICENSE](LICENSE) for the full license text.

A small number of files derived from the HashiCorp [terraform-provider-scaffolding-framework](https://github.com/hashicorp/terraform-provider-scaffolding-framework) template remain under the Mozilla Public License, Version 2.0. See [LICENSE.mpl-2.0](LICENSE.mpl-2.0) for that license text and [NOTICE](NOTICE) for the list of affected files. Each source file's `SPDX-License-Identifier` header declares its license authoritatively.
