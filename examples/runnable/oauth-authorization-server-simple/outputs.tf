output "server_urn" {
  description = "Full DataHub URN of the authorization server. Reference this from AI plugin configuration."
  value       = datahub_oauth_authorization_server.okta.urn
}

output "server_id" {
  description = "The server ID (the URN suffix)."
  value       = datahub_oauth_authorization_server.okta.server_id
}

output "has_client_secret" {
  description = "Whether a client secret is currently configured server-side. The secret value itself is never readable."
  value       = datahub_oauth_authorization_server.okta.has_client_secret
}

output "client_secret_urn" {
  description = "URN of the dataHubSecret entity holding the encrypted client secret. A new URN appearing here is the visible sign of a rotation."
  value       = datahub_oauth_authorization_server.okta.client_secret_urn
}

output "verify_command" {
  description = "Reads the entity back via the strongly-consistent OpenAPI v3 endpoint. Run with: eval \"$(terraform output -raw verify_command)\" -- the shell expands DATAHUB_GMS_URL and DATAHUB_GMS_TOKEN at that point."
  value       = "curl -sS -H \"Authorization: Bearer $DATAHUB_GMS_TOKEN\" \"$DATAHUB_GMS_URL/openapi/v3/entity/oauthauthorizationserver/${datahub_oauth_authorization_server.okta.urn}\""
}
