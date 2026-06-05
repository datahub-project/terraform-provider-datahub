# FIBO Domain Hierarchy Example

Creates the [FIBO (Financial Industry Business Ontology)](https://spec.edmcouncil.org/fibo/) taxonomy as a three-level DataHub domain hierarchy:

```
Financial Industry Business Ontology (FIBO)   ← optional root node
  Domain (e.g. Securities (SEC))
    Module (e.g. Debt)
      Leaf ontology (e.g. Bonds, Mortgage Backed Securities)
```

## How it works

A Python script shallow-clones the FIBO GitHub repository and reads the RDF metadata files (`Metadata*.rdf`, individual ontology files) to extract names and descriptions. The output is a structured JSON file cached locally in `.fibo-cache/` (gitignored). Terraform reads this file at plan time — no network calls during `terraform plan` or `terraform apply`.

**License:** FIBO is published by the [EDM Council](https://edmcouncil.org) under the [MIT License](https://github.com/edmcouncil/fibo/blob/master/LICENSE).

## Prerequisites

- **git** — used by `make fibo-data` to shallow-clone the FIBO repository (~40 MB download, done once)
- **Python 3.8+** — standard library only, no extra packages required
- **make** — drives the data preparation step
- DataHub OSS or DataHub Cloud instance
- Personal access token with **Manage Domains** privilege
- Terraform >= 1.11

## Usage

### Step 1 — prepare the FIBO data (one-time)

```bash
make fibo-data
```

This clones the FIBO repository and generates `.fibo-cache/fibo.json`. Subsequent runs reuse the cache (refreshed automatically after 7 days, or immediately with `make fibo-update`).

### Step 2 — apply

```bash
export DATAHUB_GMS_URL="https://your-instance.acryl.io/gms"
export DATAHUB_GMS_TOKEN="your-personal-access-token"

terraform init
terraform apply
```

To restrict to a subset of FIBO domains:

```bash
terraform apply -var 'domains_filter=["SEC"]'
terraform apply -var 'domains_filter=["SEC","DER","LOAN"]'
```

Available domain codes: `ACTUS`, `BE`, `CAE`, `DER`, `FBC`, `IND`, `LOAN`, `MD`, `SEC`. (`FND` and `BP` are always excluded as ontology scaffolding.)

To omit the top-level FIBO root node:

```bash
terraform apply -var 'create_root_node=false'
```

### Refreshing FIBO data

To pick up a new FIBO release without re-cloning:

```bash
make fibo-update   # regenerates JSON from existing clone
```

To start completely fresh (re-clones the repository):

```bash
make clean-fibo
make fibo-data
```

## Verify

Open the Domains page in the DataHub UI at `$DATAHUB_GMS_URL/domains` (strip the `/gms` suffix for UI access) to browse the hierarchy.

## Cleanup

```bash
terraform destroy
```

Terraform destroys leaf nodes before modules, and modules before top-level domains — the correct order for DataHub's hard-delete child guard — because the `parent_domain` references create the dependency edges automatically.
