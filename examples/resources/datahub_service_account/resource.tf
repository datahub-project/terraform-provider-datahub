# Manage a DataHub service account: a non-human identity for programmatic access
# (CI/CD, ingestion, automation). Requires DataHub Core >= 1.4.0 or DataHub Cloud.
#
# This manages the identity only. Mint an access token separately in the UI
# (Settings -> Access Tokens, token type "Service Account") or via the API;
# tokens are write-once and are not managed here.
resource "datahub_service_account" "ci_bot" {
  service_account_id = "ci-bot" # becomes urn:li:corpuser:service_ci-bot
  display_name       = "CI Bot"
  description        = "Automation account for the CI/CD pipeline"
}

output "ci_bot_urn" {
  value = datahub_service_account.ci_bot.urn
}
