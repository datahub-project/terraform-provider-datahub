# Import by URN
terraform import datahub_connection.prod_databricks urn:li:dataHubConnection:prod-databricks

# Or import by bare connection ID (provider constructs the URN)
terraform import datahub_connection.prod_databricks prod-databricks

# NOTE: After import, the connection config blob is encrypted at rest and cannot
# be read back from the DataHub API. You must add the appropriate platform block
# to your configuration with the correct credentials and set config_wo_version
# before running terraform apply.
