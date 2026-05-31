# authorization-team-access

Sets up DataHub authorization for a team. This example grows across the IAM resource series; it currently provisions:

- a native group (`datahub_corp_group`) representing a team, and
- a lookup of that group via the `datahub_corp_group` data source.

Later additions to this example: group membership, a role assignment, and an access policy.

## Prerequisites

- A running DataHub instance (OSS or DataHub Cloud)
- `DATAHUB_GMS_URL` and `DATAHUB_GMS_TOKEN` set in the shell
- The token must belong to a principal with the `MANAGE_USERS_AND_GROUPS` privilege

## Apply

```bash
export DATAHUB_GMS_URL=https://your-instance.acryl.io
export DATAHUB_GMS_TOKEN=<personal-access-token>

terraform init
terraform apply
```

## Verify

```bash
terraform output group_urn
```

Then open the group in the DataHub UI: `$DATAHUB_GMS_URL/settings/identities/groups`.

## Clean up

```bash
terraform destroy
```
