# Terraform Provider DataHub

Terraform provider to manage DataHub ingestion sources.

This provider is implemented with the Terraform Plugin Framework and talks to DataHub via its OpenAPI endpoints.

## What it supports

- Resources
  - `datahub_ingestion_source`: creates/updates/deletes a DataHub ingestion source from a recipe JSON string.

Generated docs live under `docs/resources/`.

## Requirements

- [Terraform](https://developer.hashicorp.com/terraform/downloads) >= 1.0
- [Go](https://go.dev/doc/install) >= 1.26

## Provider configuration

The provider needs to reach DataHub GMS.

Common approaches:

- Set `DATAHUB_GMS_URL` and `DATAHUB_GMS_TOKEN` environment variables.
- Or configure them in the provider block.

See `examples/provider-install-verification/` for a working development setup.

## Security / Credentials

DataHub ingestion source configurations (including the recipe JSON) are stored in DataHub. If you embed credentials (tokens, passwords, private keys) directly into the recipe/config, they can end up stored in DataHub metadata and exposed to users/services with access to view ingestion source configs. This provider does not “magically” change that behavior.

Recommended approaches:

- Use **DataHub Secrets** (UI Ingestion → Secrets) and reference secrets by name using the `${SECRET_NAME}` convention in your recipe/config.
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
