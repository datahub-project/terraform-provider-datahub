# examples/

This directory contains two distinct types of content.

## Doc fragments

Consumed by `make generate` (tfplugindocs) to produce the `docs/` directory.
These are code snippets only -- no `terraform {}` block, not runnable standalone.

- `provider/` -- provider configuration snippet embedded in `docs/index.md`
- `data-sources/<name>/data-source.tf` -- embedded in `docs/data-sources/<name>.md`
- `resources/<name>/resource.tf` -- embedded in `docs/resources/<name>.md`

## Runnable examples

Self-contained Terraform configurations you can `terraform apply` directly.
Each has its own README with prerequisites and instructions, except
`provider-install-verification/`, which is a single `main.tf`.

Entries marked **DataHub Cloud only** use resources that do not exist in
open-source DataHub and will fail there with a clear error; everything else
runs against OSS Quickstart and DataHub Cloud alike. Two examples --
`remote-executor-azure/` and `financial-services/` -- are substantially
heavier than the rest; their entries say so.

All runnable examples live under `runnable/`:

- `runnable/provider-install-verification/` -- smoke test: verifies the provider binary loads and credentials are valid
- `runnable/tag-simple/` -- create three tags with display colours, read one back by ID, and enumerate every tag as a bulk-import source
- `runnable/domain-simple/` -- build a two-level domain hierarchy, using `.urn` references so Terraform creates parents first and destroys children first
- `runnable/glossary-node-term-simple/` -- build a two-level Business Glossary: nested term groups with terms attached at two different depths
- `runnable/ownership-type-simple/` -- create custom ownership types, then resolve every ownership type in the instance to its full attributes with a `for_each` data source
- `runnable/structured-property-simple/` -- define two typed structured properties: a single-valued number, and a string constrained to three allowed values
- `runnable/structured-and-custom-properties/` -- contrast free-form custom properties with defined-and-validated structured properties on the same glossary entities, covering assignment, UI grouping by dotted qualified name, and `defaults.custom_properties`
- `runnable/data-product-simple/` -- create two data products in a domain, read them back individually and in bulk, then enable provider-level tag and structured-property defaults on a second apply
- `runnable/secret-basic/` -- manage a secret as a Terraform resource and reference it from an ingestion source recipe
- `runnable/ingestion-source-csv-enricher/` -- create an ingestion source using the csv-enricher connector
- `runnable/ingestion-source-lookup/` -- look up an existing ingestion source with a data source
- `runnable/connection-snowflake/` -- create a reusable Snowflake connection
- `runnable/connection-snowflake-ingestion-source/` -- create a Snowflake connection wired to an ingestion source
- `runnable/local-iam/` -- set up local-auth users, group membership, a role assignment, and an access policy for a team
- `runnable/assertion-volume-sqlite/` -- profile a local SQLite table into DataHub and assert a minimum row count against the ingested profile; the README walks a pass/fail/pass cycle by reseeding the table (DataHub Cloud only)
- `runnable/action-pipeline-dataplex-sync/` -- create an action pipeline that propagates DataHub glossary terms to the Google Dataplex catalog, and enumerate all pipelines (DataHub Cloud only)
- `runnable/executor-pool-basic/` -- provision a remote executor pool and route an ingestion source to it (DataHub Cloud only)
- `runnable/remote-executor-azure/` -- full Remote Executor deployment on Azure AKS: executor pool, worker via Helm, and an ingestion source using both DataHub-managed and Key Vault CSI-mounted secrets (DataHub Cloud only; **creates billable Azure infrastructure** -- roughly USD 280/month for the AKS cluster alone, billing from `terraform apply` until `terraform destroy`)
- `runnable/financial-services/` -- the largest example: a `make` step shallow-clones the FIBO ontology repository (~60 MB) and runs a Python/rdflib pipeline to generate a three-level domain hierarchy and matching Business Glossary, up to ~147 domains and ~1500-2000 terms unless you narrow it with `-var 'domains_filter=["SEC"]'`. An optional second layer downloads ISO 20022 XSD schemas, converts them to Avro, emits Kafka/PostgreSQL/Looker datasets with three-tier lineage, optionally tags them against FIBO via the Anthropic API, and creates five kinds of data-quality assertion (that assertion layer is DataHub Cloud only, and is skipped entirely until its generated config exists). Needs `git`, `make`, Python 3.8+ and network access; not a quick first run
