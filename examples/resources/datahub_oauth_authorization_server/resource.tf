# An outbound OAuth client configuration: DataHub obtains tokens from this
# identity provider when calling an external API (e.g. an MCP server behind
# Snowflake OAuth). Referenced from AI plugin configuration via its urn.
resource "datahub_oauth_authorization_server" "snowflake" {
  server_id    = "snowflake-oauth"
  display_name = "Snowflake OAuth"
  description  = "Snowflake security integration for the analytics MCP server"

  client_id                = var.oauth_client_id
  client_secret_wo         = var.oauth_client_secret
  client_secret_wo_version = 1 # bump to rotate in place (no replacement)

  authorization_url = "https://example-account.snowflakecomputing.com/oauth/authorize"
  token_url         = "https://example-account.snowflakecomputing.com/oauth/token-request"
  scopes            = ["refresh_token", "session:role:ANALYTICS_READER"]
}

variable "oauth_client_id" {
  description = "OAuth client ID from the identity provider"
  type        = string
  sensitive   = true
}

variable "oauth_client_secret" {
  description = "OAuth client secret; sent to DataHub (encrypted server-side), never stored in Terraform state"
  type        = string
  sensitive   = true
}
