# ownership-type-simple

Demonstrates the `datahub_ownership_type` resource, the `datahub_entity_ownership` resource, the `datahub_ownership_type` data source, and the `datahub_ownership_types` plural data source.

Creates two custom ownership types -- Data Quality Lead and Data Producer -- then puts them to work: a glossary term is the owned entity, a group and a user are the owner principals, and three `(owner, ownership type)` pairs are assigned across them. It also enumerates all ownership types via the plural data source; a `for_each` data source resolves each URN to its full attributes, producing the `ownership_types` map output keyed by URN. Because that list is eventually-consistent (GraphQL-backed), newly created types appear in the map on the next `terraform plan` or `terraform refresh` rather than in the same apply.

## What the owner assignments show

An ownership type on its own is just a vocabulary entry, so the assignments are the interesting half. Two shapes are demonstrated deliberately, because both are ordinary in DataHub and both are easy to assume are impossible:

- **Two owners sharing one ownership type.** The group and the user are both Data Quality Leads. `(entity, ownership type)` is not a unique key -- a role is usually held by more than one party.
- **One owner holding two ownership types.** The group is both Data Quality Lead and Data Producer. Each pair is its own entry and its own edge in DataHub.

`datahub_entity_ownership` owns only the pairs it declares. An owner assigned through the DataHub UI on the same term survives `terraform apply` and `terraform destroy` untouched, which is what makes it safe to point at a glossary people already curate by hand. The corollary: removing an `owner` entry from the configuration *does* remove that pair on the next apply.

## Prerequisites

- Terraform >= 1.11
- A running DataHub instance (OSS or Cloud)
- `DATAHUB_GMS_URL` and `DATAHUB_GMS_TOKEN` environment variables set

## Run

```bash
export DATAHUB_GMS_URL=http://localhost:8080
export DATAHUB_GMS_TOKEN=your-token-here

terraform init
terraform apply
```

## Variables

| Variable | Default | Purpose |
|---|---|---|
| `owner_user_urn` | `urn:li:corpuser:datahub` | A corp user to name as the second owner. The default is the built-in admin, present on every Quickstart and Cloud instance. |

The user must already exist: DataHub's `batchAddOwners` resolves every owner URN server-side and refuses the whole write with `Owner with urn ... does not exist.` otherwise. That is why this example takes a URN rather than creating a `datahub_corp_user` -- creating one drives the sign-up endpoint, which refuses an address whose user entity already exists, so a single failed destroy would poison a fixed address permanently.

To point it at a real person:

```bash
terraform apply -var owner_user_urn=urn:li:corpuser:alice@example.com
```

## Verify

After apply, `owner_assignments` lists the three managed pairs as `owner -> role`. Read them back from DataHub itself with:

```bash
eval "$(terraform output -raw verify_owners_command)"
```

That hits `GET /openapi/v3/entity/glossaryterm/{urn}`, the strongly-consistent (MySQL-backed) read path the provider uses, so the owners are visible immediately rather than after an index catches up. Expect three entries in `ownership.value.owners`, each carrying `owner` and `typeUrn`.

In the UI, open the glossary term and read the **Ownership** panel on the right:

```bash
echo "$DATAHUB_GMS_URL$(terraform output -raw term_ui_path)"
```

Navigate to **Govern -> Glossary** and find *TF Example Ownership - Customer LTV*, or to **Settings -> Ownership Types** to confirm the two new types appear alongside the built-in ones.

## Bulk import pattern

Use `datahub_ownership_types` to import pre-existing custom ownership types into Terraform state without recreating them:

```hcl
data "datahub_ownership_types" "existing" {}

import {
  for_each = toset([
    for urn in data.datahub_ownership_types.existing.urns
    : urn if !startswith(urn, "urn:li:ownershipType:__system__")
  ])
  id = each.value
  to = datahub_ownership_type.imported[each.value]
}

resource "datahub_ownership_type" "imported" {
  for_each = toset([
    for urn in data.datahub_ownership_types.existing.urns
    : urn if !startswith(urn, "urn:li:ownershipType:__system__")
  ])
  type_id = trimprefix(each.value, "urn:li:ownershipType:")
  name    = "placeholder"  # replaced on first plan+apply after import
}
```

The filter on `__system__` excludes built-in system types, which cannot be managed by Terraform.

`datahub_entity_ownership` imports the same way, by the owned entity's URN. Use it on an entity of *your own* that already carries hand-assigned owners -- not on `datahub_entity_ownership.customer_ltv`, which this configuration already manages and which Terraform will refuse to import twice:

```hcl
resource "datahub_entity_ownership" "adopted" {
  entity_urn = "urn:li:glossaryTerm:your-existing-term"

  owner = [
    # Fill in from the imported state, then remove the ones you do not
    # want Terraform to manage -- see the caveat below.
  ]
}
```

```bash
terraform import datahub_entity_ownership.adopted urn:li:glossaryTerm:your-existing-term
terraform state show datahub_entity_ownership.adopted
```

Import adopts **every** owner then present on the entity, so write the configuration from what `terraform state show` reports before the next apply -- an owner you leave out of it will be removed.

## Cleanup

```bash
terraform destroy
```

This removes the three owner assignments, the glossary term, the group, and hard-deletes the two custom ownership types. Built-in system types looked up via the data source are not affected, and neither is any owner that was assigned outside this configuration.
