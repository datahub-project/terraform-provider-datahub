# Domain-scoped write access

Answers a common field question: **"Can everyone have read-only access to ALL of DataHub, with write privileges limited to a given domain?"**

Yes -- but not as a single named role. DataHub roles (Admin/Editor/Reader) are platform-wide and cannot be narrowed to a domain, and custom roles do not exist. Policies can be scoped to a domain, and privileges from every policy that matches a user are added together. So the permission is built from two pieces:

1. The built-in **Reader** role -- read access across the whole catalog, no edit rights.
2. A **domain-scoped METADATA policy** -- a curated "steward" privilege set, filtered to one domain.

This example creates everything needed to stand the pattern up and verify it end to end:

- Three domains: `Finance` (governed), `Finance Billing` (a sub-domain, to prove the cascade), and `Marketing` (unrelated, to prove the deny boundary)
- A `Finance Stewards` group and a native-login test user in it
- A Reader role assignment on the test user
- The domain-scoped metadata policy targeting the group
- A pre-existing tag for the stewards to apply (they can apply tags, not create them)
- Outputs that seed four scratch datasets and then run every verification check as the test user, copy-paste ready

## Prerequisites

- Terraform >= 1.11
- `DATAHUB_GMS_URL` and `DATAHUB_GMS_TOKEN` exported, where the token has `MANAGE_POLICIES`, `MANAGE_USERS_AND_GROUPS`, `MANAGE_USER_CREDENTIALS`, `CREATE_DOMAINS` and `CREATE_TAGS` (an Admin token has all of these)
- `jq` on `PATH` (the verification commands use it to build JSON safely)
- Works on both open-source DataHub and DataHub Cloud

## Apply

```sh
export DATAHUB_GMS_URL=...
export DATAHUB_GMS_TOKEN=...

# The test user needs a real password. Write-only: never stored in state.
export TF_VAR_test_user_password=$(openssl rand -base64 24)
echo "$TF_VAR_test_user_password"   # note it down

terraform init
terraform apply
```

## Verify

The rollout guide's six checks, in run order. First seed the scratch datasets (as the **admin** token) and log in (as the **test user**):

```sh
eval "$(terraform output -raw seed_dataset_commands)"

# The login goes to the DataHub FRONTEND, not GMS. On OSS they differ:
export DATAHUB_FRONTEND_URL=http://localhost:9002   # Quickstart
# export DATAHUB_FRONTEND_URL="$DATAHUB_GMS_URL"    # DataHub Cloud

eval "$(terraform output -raw steward_login_commands)"
```

Then run each probe and compare with the expected result:

| # | Check | Command | Expect |
|---|---|---|---|
| 1 | Read an asset outside the domain | `eval "$(terraform output -raw probe_read_command)"` | the dataset |
| 2 | Edit inside the assigned domain | `eval "$(terraform output -raw probe_in_domain_command)"` | `{"data":{"addTag":true}}` |
| 3 | Edit in a different domain | `eval "$(terraform output -raw probe_other_domain_command)"` | `Unauthorized` error |
| 4 | Edit in a sub-domain (the cascade) | `eval "$(terraform output -raw probe_subdomain_command)"` | `{"data":{"addTag":true}}` |
| 5 | Edit an asset with no domain | `eval "$(terraform output -raw probe_no_domain_command)"` | `Unauthorized` error |
| 6 | Create a new tag (platform right) | `eval "$(terraform output -raw probe_create_tag_command)"` | `Unauthorized` error |

The same checks can be run in the UI by signing in as the test user (email from `terraform output`, the password you exported) and confirming the edit controls are active on Finance assets and read-only elsewhere.

A note on timing: GMS caches policies and refreshes the cache every 120 seconds (`POLICY_CACHE_REFRESH_INTERVAL_SECONDS`), so a probe run immediately after `terraform apply` can be denied once and succeed on retry. Wait out the interval before concluding anything.

## What to know before adapting this

**The domain filter cascades.** A `DOMAIN` criterion matches the selected domain and every sub-domain nested beneath it -- that is why check 4 passes with a policy that names only `Finance`. Scope the policy to the specific child domain when you want the narrower grant.

**Assets with no domain match nothing.** A domain-scoped policy cannot reach an asset that carries no domain (check 5). Before rolling out, search for assets with no domain and work through the backlog, or the first steward complaint will be about an asset nobody can touch.

**Creating tags and terms is a platform right, not a domain one.** Applying an existing tag or glossary term scopes to the domain cleanly; creating one is platform-wide and cannot be filtered by domain (check 6). Keep creation with a central governance group and let stewards request new terms through the proposal workflow.

**`EDIT_ENTITY_DOMAINS` is deliberately withheld.** It would let a steward change which domain an asset belongs to -- including moving it *out* of their own domain and beyond the reach of the policy that granted the right. Grant it only when stewards are responsible for classifying incoming assets.

**The pattern repeats per domain.** Each governed domain gets its own group and policy. Where several domains share an identical privilege set, list them in one policy's `values` (they combine as OR) rather than multiplying policies -- the authorization layer evaluates every matching policy on each check, so policy count carries a latency cost.

**Reader vs. OSS defaults.** On DataHub Cloud, the Reader role is what grants catalog-wide read. Fresh open-source instances additionally bootstrap an ACTIVE "All Users - View Entity Page" policy, so on a Quickstart everyone can read everything before the role does anything. The role assignment is kept here because it is the piece that matters where this pattern is actually deployed.

**This controls write access, not read access.** The pattern assumes everyone should read everything. Restricting what a group can *see* is a separate configuration with different considerations -- DataHub policies are additive ALLOW rules with no deny, so read restriction is not a small variation on this example.

## Cleanup

```sh
# The scratch datasets are not Terraform-managed; remove them first (admin token).
eval "$(terraform output -raw cleanup_dataset_commands)"

terraform destroy
```

Destroy removes the policy, role assignment, membership, user, group, tag and domains. The domains are destroyed child-first because `parent_domain` references the parent's URN, giving Terraform the ordering edge. If you ran check 6 as an admin by mistake, also delete `urn:li:tag:tf-example-steward-denied`.
