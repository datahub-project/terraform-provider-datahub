# oauth-authorization-server-simple

Creates a DataHub OAuth Authorization Server: an outbound OAuth client configuration DataHub uses to obtain tokens from an external identity provider (Okta in this example) when calling an external API on a user's behalf. Its primary consumer is the Ask DataHub AI plugin registry, where a plugin's `USER_OAUTH` mode references a server like this one by URN.

**DataHub Cloud only.** The `oauthAuthorizationServer` entity type and its mutations do not exist in OSS DataHub, so this example cannot be applied against an OSS instance (including the local Quickstart).

## Prerequisites

- Terraform CLI 1.11 or later (required for WriteOnly attribute support -- `client_secret_wo` depends on it)
- A DataHub Cloud instance
- A personal access token with the **Manage Connections** (`MANAGE_CONNECTIONS`) privilege
- An OAuth client registered with your identity provider (the example uses placeholder Okta endpoints -- substitute your own)

```bash
export DATAHUB_GMS_URL=https://your-instance.acryl.io/gms
export DATAHUB_GMS_TOKEN=<personal-access-token>
```

## Supplying the client secret

The secret never appears in `main.tf` and is never written to Terraform state: it flows through the sensitive variable `oauth_client_secret` into the WriteOnly attribute `client_secret_wo`, and DataHub encrypts it server-side. The variable deliberately has no default, so Terraform will not run without a value. Supply it one of these ways:

- **Environment variable**, without leaving it in shell history:

  ```bash
  read -s TF_VAR_oauth_client_secret && export TF_VAR_oauth_client_secret
  ```

- **A `.tfvars` file kept outside version control** (add it to `.gitignore` before creating it):

  ```bash
  echo 'oauth_client_secret = "..."' > secrets.auto.tfvars
  ```

- **A secrets manager**, wired in however your pipeline injects variables (e.g. Vault provider data source, or the CI system's secret-to-environment mapping emitting `TF_VAR_oauth_client_secret`).

Because the variable has no default, it is also required for `terraform destroy` -- Terraform evaluates the configuration on destroy too.

## Apply

```bash
terraform init
terraform apply
```

## Verify

```bash
terraform output server_urn
terraform output has_client_secret   # true confirms the secret was stored

# Read the entity back through the OpenAPI v3 endpoint:
eval "$(terraform output -raw verify_command)"
```

The response includes every non-secret field; the secret itself is never returned by any API.

## Rotating the client secret

Rotation is **in place**: unlike `datahub_secret`, bumping the version updates the existing server rather than replacing it.

1. Obtain the new secret from your identity provider and update the source of `TF_VAR_oauth_client_secret`.
2. Increment `client_secret_wo_version` in `main.tf` (e.g. `1` -> `2`). The integer is arbitrary; only the change matters.
3. `terraform apply`.

Two things about rotation that are easy to assume wrongly:

- **Rotation does not cascade to referencing AI plugins.** Plugins reference the server by URN, and the URN does not change -- they pick up the new secret transparently and need no reconfiguration. (Contrast with *destroying* the server, which does cascade: see Cleanup.)
- **Each rotation leaves an orphaned `dataHubSecret` entity.** DataHub stores the new secret as a new encrypted entity and nothing deletes the old one. The orphans are inert but permanent; `client_secret_urn` changing between reads is the visible sign a rotation happened (yours or someone else's).

To **clear** the secret instead, remove `client_secret_wo` from config and increment the version; when the version is unchanged, the stored secret is preserved and never re-sent.

## Importing an existing server

```bash
terraform import datahub_oauth_authorization_server.okta urn:li:oauthAuthorizationServer:tf-example-oauth-okta
# or equivalently, by bare server ID:
terraform import datahub_oauth_authorization_server.okta tf-example-oauth-okta
```

Unlike `datahub_secret`, an imported server can be updated without re-supplying the secret: leave `client_secret_wo` and `client_secret_wo_version` unset and the stored secret is untouched (`has_client_secret = true` confirms it is there).

## Cleanup

```bash
terraform destroy
```

Deletion is a **hard delete and it cascades**: DataHub rewrites every AI plugin that references this server (auth flipped to `NONE`, OAuth configuration dropped) and removes per-user OAuth connections for those plugins. Check for referencing plugins before destroying anything you did not just create.
