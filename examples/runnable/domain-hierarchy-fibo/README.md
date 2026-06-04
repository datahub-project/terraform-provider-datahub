# FIBO Domain Hierarchy Example

Creates the [FIBO (Financial Industry Business Ontology)](https://spec.edmcouncil.org/fibo/) taxonomy as a three-level DataHub domain hierarchy:

```
Domain (e.g. SEC - Securities)
  Module (e.g. Debt)
    Leaf ontology (e.g. Bonds, MortgageBackedSecurities)
```

## How it works

The hierarchy is fetched live from the FIBO GitHub repository's file tree — no data file is committed. The `hashicorp/http` provider calls the GitHub tree API at plan/apply time and Terraform's `jsondecode` + `for` expressions derive the 3-level structure directly from the `Domain/Module/Leaf.rdf` path pattern.

**License:** FIBO is published by the [EDM Council](https://edmcouncil.org) under the [MIT License](https://github.com/edmcouncil/fibo/blob/master/LICENSE).

## Prerequisites

- DataHub OSS or DataHub Cloud instance
- Personal access token with **Manage Domains** privilege
- Terraform >= 1.11

## Usage

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

Available domain codes (after excluding ontology scaffolding): `BE`, `CAE`, `DER`, `FBC`, `IND`, `LOAN`, `MD`, `SEC`.

## GitHub API rate limits

The example makes one unauthenticated request to the GitHub API per `terraform plan`. The unauthenticated rate limit is 60 requests/hour, which is sufficient for interactive use. For CI or repeated runs, add a token header:

```hcl
# In main.tf, update the data "http" "fibo_tree" block:
request_headers = {
  Accept        = "application/vnd.github.v3+json"
  User-Agent    = "terraform-datahub-fibo-example"
  Authorization = "Bearer ${var.github_token}"
}
```

## Verify

Open the Domains page in the DataHub UI at `$DATAHUB_GMS_URL/domains` (strip the `/gms` suffix for UI access) to browse the hierarchy.

## Cleanup

```bash
terraform destroy
```

Terraform destroys leaf nodes before modules, and modules before top-level domains — the correct order for DataHub's hard-delete child guard — because the `parent_domain` references create the dependency edges automatically.
