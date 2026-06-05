# FIBO Domain Hierarchy + Glossary Example

Creates the [FIBO (Financial Industry Business Ontology)](https://spec.edmcouncil.org/fibo/) taxonomy as a three-level DataHub domain hierarchy AND a matching Business Glossary drawn from the same ontology:

```
Financial Industry Business Ontology (FIBO)    <- optional root (domain + glossary node)
  Domain (e.g. Securities (SEC))               <- domain node + glossary node
    Module (e.g. Debt)                         <- domain node + glossary node
      Leaf ontology (e.g. Bonds)               <- domain node + glossary node
        bond                                   <- glossary term (owl:Class Bond)
        amortizing bond                        <- glossary term (owl:Class AmortizingBond)
        callable bond                          <- glossary term (owl:Class CallableBond)
        ...
```

**Scale (all domains, Release terms only):** ~103 leaf ontologies, ~147 domain nodes, ~147 matching glossary nodes, ~1500-2000 glossary terms. Start with a single domain (`-var 'domains_filter=["SEC"]'`) for a faster first run.

## How it works

A Python script shallow-clones the FIBO GitHub repository and reads the RDF files to extract two layers of content:

1. **Domain hierarchy** (regex-based): parses `Metadata*.rdf` files for names and descriptions, walks the directory tree to build the domain/module/leaf structure.

2. **Glossary terms** (rdflib-based): parses each leaf `.rdf` ontology file as RDF/XML and extracts `owl:Class` definitions. Each class's `rdfs:label` becomes the term name and its `skos:definition` becomes the term description. Classes flagged with Provisional maturity are excluded by default (matching the DataHub FIBO ingestion default).

The output is a structured JSON file cached locally in `.fibo-cache/` (gitignored). Terraform reads this file at plan time — no network calls during `terraform plan` or `terraform apply`.

**License:** FIBO is published by the [EDM Council](https://edmcouncil.org) under the [MIT License](https://github.com/edmcouncil/fibo/blob/master/LICENSE).

## Prerequisites

- **git** -- used by `make fibo-data` to shallow-clone the FIBO repository (~60 MB download, done once)
- **Python 3.8+** with **rdflib>=6.0** -- install with `make fibo-deps` or `pip3 install rdflib`
- **make** -- drives the data preparation step
- DataHub OSS or DataHub Cloud instance
- Personal access token with **Manage Domains** and **Manage Glossaries** privileges
- Terraform >= 1.11

## Usage

### Step 1 -- install Python dependencies (one-time)

```bash
make fibo-deps
```

This installs `rdflib` (required for OWL class extraction). The `fibo-data` target runs this automatically, so you can skip directly to Step 2 if you prefer.

### Step 2 -- prepare the FIBO data (one-time)

```bash
make fibo-data
```

This clones the FIBO repository, parses the RDF files, and generates `.fibo-cache/fibo.json`. Subsequent runs reuse the cache (refreshed automatically after 7 days, or immediately with `make fibo-update`).

The console output shows a per-domain summary:

```
  SEC: 4 modules, 23 leaves, 462 terms
  DER: 4 modules, 15 leaves, 287 terms
  ...
Total: 9 domains, 35 modules, 103 leaves, 1847 glossary terms
```

### Step 3 -- apply

```bash
export DATAHUB_GMS_URL="https://your-instance.acryl.io/gms"
export DATAHUB_GMS_TOKEN="your-personal-access-token"

terraform init
terraform apply
```

**Recommended for a first run** -- scope to a single domain to verify the setup before loading the full ontology:

```bash
terraform apply -var 'domains_filter=["SEC"]'
```

To scope to multiple domains:

```bash
terraform apply -var 'domains_filter=["SEC","DER","LOAN"]'
```

Available domain codes: `ACTUS`, `BE`, `CAE`, `DER`, `FBC`, `IND`, `LOAN`, `MD`, `SEC`. (`FND` and `BP` are always excluded as ontology scaffolding.)

To create only the domain hierarchy without the glossary:

```bash
terraform apply -var 'create_glossary=false'
```

To omit the top-level FIBO root node (both domain and glossary root):

```bash
terraform apply -var 'create_root_node=false'
```

### Including Provisional-maturity terms

By default only FIBO classes marked Release (or with no maturity annotation) are extracted. To include Provisional classes, regenerate the JSON and apply:

```bash
make fibo-data-provisional   # regenerates .fibo-cache/fibo.json with --include-provisional
terraform apply
```

To revert to Release-only:

```bash
make fibo-update             # regenerates without --include-provisional
terraform apply
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

```bash
# Print entity counts
terraform output

# Browse in the DataHub UI (strip /gms from the URL for UI access)
echo "Domains: ${DATAHUB_GMS_URL%/gms}/domains"
echo "Glossary: ${DATAHUB_GMS_URL%/gms}/glossary"
```

The Domains page shows the three-level FIBO hierarchy. The Glossary page shows a matching hierarchy of term groups with terms beneath each leaf node.

## Cleanup

```bash
terraform destroy
```

Terraform destroys terms before leaf nodes, leaf nodes before modules, and modules before top-level nodes -- in both the domain and glossary hierarchies -- because all `parent_domain` and `parent_node` attributes reference `.urn` outputs, giving Terraform the dependency edges it needs.

Domain destruction respects DataHub's hard-delete child guard (the server refuses to delete a domain with children). Glossary destruction has no server-side guard, so ordering via `.urn` references is the only protection against orphaned nodes.
