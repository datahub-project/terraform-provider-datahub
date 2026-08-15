# local-iam

Sets up end-to-end DataHub IAM for a team, covering user provisioning, group management, role assignment, and access policies:

- a native group (`datahub_corp_group`) representing the team,
- a new **login user** (`datahub_local_user_login`) provisioned with native credentials and added to the group,
- a **catalog-only user** (`datahub_corp_user`) representing a pipeline bot or ingestion-discovered owner -- the same kind of entity DataHub creates when a source like dbt or BigQuery finds a dataset owner. Appears in the Users UI as inactive (no credentials, by design), and added to the group,
- a lookup of an existing user (`datahub_corp_user` data source) also added to the group,
- a role assignment (`datahub_role_assignment`) granting the group the built-in `Editor` role,
- an access policy (`datahub_policy`) granting the group specific platform privileges, and
- an enumeration of all policies (`datahub_policies` data source), filtered down to the ones this configuration owns by id prefix -- the allowlist shape to feed a bulk-import `import {}` block.

## Username format: OSS vs Cloud

Use an **email address** as the username for `datahub_local_user_login`. This works identically on both platforms:

- **OSS DataHub:** any string is valid; email format is accepted as-is.
- **DataHub Cloud:** the user URN is always derived from the email field, so username and email must match. Email-format usernames satisfy this automatically.

After apply, the created user has no usable password. Send the reset link to the user:

```bash
terraform output -raw team_member_reset_url
```

The link is single-use and expires in 24 hours.

## Prerequisites

- A running DataHub instance (OSS or DataHub Cloud)
- `DATAHUB_GMS_URL` and `DATAHUB_GMS_TOKEN` set in the shell
- The token must belong to a principal with the `MANAGE_USERS_AND_GROUPS` and `MANAGE_USER_CREDENTIALS` privileges, plus `MANAGE_POLICIES` for the access policy it creates and the policy enumeration it reads

## Configure

```bash
cp terraform.tfvars.example terraform.tfvars
# Edit terraform.tfvars to set new_member_email and optionally member_username
```

## Apply

```bash
export DATAHUB_GMS_URL=https://your-instance.acryl.io
export DATAHUB_GMS_TOKEN=<personal-access-token>

terraform init
terraform apply
```

## Onboard the new user

```bash
# Copy the reset link and send it to the new team member (expires in 24h):
terraform output -raw team_member_reset_url
```

## Verify

```bash
terraform output next_steps
```

The policy enumeration is also worth a look. `all_policy_urns` lists every policy the token can see, including DataHub's built-in defaults; `managed_policy_urns` filters that down to the ones this configuration created. A just-created policy can be missing from the first read -- the backing query is eventually consistent -- so re-run `terraform refresh` if it has not appeared yet:

```bash
terraform output managed_policy_urns
```

Then open the DataHub UI:

- Groups: `$DATAHUB_GMS_URL/settings/identities/groups`
- Users: `$DATAHUB_GMS_URL/settings/identities/users`

## The automatic managed-by marker

When you apply this example, a `managed-by = "terraform"` custom property will appear on the `pipeline_bot` user. That is the provider's `auto_properties` marker, which is on by default for every custom-property-capable resource -- see the "Provider-level defaults" guide. To apply the example without it, add `auto_properties = []` to the provider block.

## Clean up

```bash
terraform destroy
```

> **Warning:** `terraform destroy` hard-deletes the created user entities (group, login user, pipeline bot). The existing user looked up via `member_username` is only removed from the group membership — the user entity itself is not deleted.
