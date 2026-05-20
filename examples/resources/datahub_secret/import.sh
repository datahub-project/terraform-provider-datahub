# Import by full URN
terraform import datahub_secret.bq_creds urn:li:dataHubSecret:bq-service-account-json

# Or equivalently, by bare name
terraform import datahub_secret.bq_creds bq-service-account-json
