terraform {
  required_version = ">= 1.11"
  required_providers {
    datahub = {
      source  = "datahub-project/datahub"
      version = "0.22.0"
    }
  }
}

provider "datahub" {
  # Credentials from environment:
  #   DATAHUB_GMS_URL   - your DataHub Cloud instance URL
  #   DATAHUB_GMS_TOKEN - personal access token with the Manage Connections privilege
}

# An outbound OAuth client configuration: DataHub obtains tokens from this
# identity provider (an Okta authorization server here) when calling an
# external API on a user's behalf -- typically an MCP server registered as an
# AI plugin. Reference it from plugin configuration via its urn attribute.
resource "datahub_oauth_authorization_server" "okta" {
  server_id    = "tf-example-oauth-okta"
  display_name = "TF Example OAuth - Okta"
  description  = "Okta authorization server for the TF Example analytics MCP server"

  # The client id is not a secret (DataHub returns it on read), but the client
  # secret never belongs in configuration: it arrives through a sensitive
  # variable and the WriteOnly attribute below, so it is never written to
  # terraform.tfstate. See README.md for how to supply it.
  client_id                = var.oauth_client_id
  client_secret_wo         = var.oauth_client_secret
  client_secret_wo_version = 1 # increment to rotate the secret IN PLACE (no replacement)

  # Endpoints of the identity provider, not of DataHub.
  authorization_url = "https://example.okta.com/oauth2/default/v1/authorize"
  token_url         = "https://example.okta.com/oauth2/default/v1/token"

  # The provider owns the complete list: scopes added outside Terraform are
  # removed on the next apply.
  scopes = ["mcp:read", "offline_access"]

  # How DataHub authenticates at token_url. Okta's default for web apps is
  # client_secret_basic, so this overrides the server default (POST_BODY).
  token_auth_method = "BASIC"

  # How the obtained access token is injected into API requests. These three
  # match the server defaults and are spelled out only to show the shape:
  # Authorization: Bearer <token>.
  auth_location    = "HEADER"
  auth_header_name = "Authorization"
  auth_scheme      = "Bearer"
}
