# Import by bare id (the provider adds the service_ prefix)
terraform import datahub_service_account.ci_bot ci-bot

# Or by the service_-prefixed username
terraform import datahub_service_account.ci_bot service_ci-bot

# Or by full URN
terraform import datahub_service_account.ci_bot urn:li:corpuser:service_ci-bot
