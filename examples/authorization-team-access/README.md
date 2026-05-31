# authorization-team-access

Sets up DataHub authorization for a team. This example grows across the IAM resource series; it currently provisions:

- a native group (`datahub_corp_group`) representing a team,
- a lookup of that group via the `datahub_corp_group` data source, and
- a membership (`datahub_corp_group_member`) adding an existing user to the group.

Later additions to this example: a role assignment and an access policy.

The user added to the group is controlled by the `member_username` variable (default `datahub`, the bootstrap admin on an OSS Quickstart). The provider does not create users, so this user must already exist.

## Prerequisites

- A running DataHub instance (OSS or DataHub Cloud)
- `DATAHUB_GMS_URL` and `DATAHUB_GMS_TOKEN` set in the shell
- The token must belong to a principal with the `MANAGE_USERS_AND_GROUPS` privilege

## Testing against an unreleased provider build

This example uses resources not yet in the published release. Build and install the provider locally, then point Terraform at the local binary using a dev override:

```bash
# From the repo root:
make install       # builds bin/terraform-provider-datahub
make dev-override  # writes dev.tfrc + .mise.env (both gitignored)
```

Then, with mise active (which picks up `TF_CLI_CONFIG_FILE` from `.mise.env`):

```bash
cd examples/authorization-team-access
terraform init   # skips registry download; uses local bin
terraform apply
```

Without mise, set `TF_CLI_CONFIG_FILE` explicitly:

```bash
TF_CLI_CONFIG_FILE=../../dev.tfrc terraform init
TF_CLI_CONFIG_FILE=../../dev.tfrc terraform apply
```

## Apply (released version)

Once these resources are included in a published release, the standard flow applies:

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
