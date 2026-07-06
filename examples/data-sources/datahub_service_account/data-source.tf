# Resolve a service account id to its URN for use as a policy actor or owner.
data "datahub_service_account" "ci_bot" {
  service_account_id = "ci-bot"
}

output "ci_bot_urn" {
  value = data.datahub_service_account.ci_bot.urn
}
