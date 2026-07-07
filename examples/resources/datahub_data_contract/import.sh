# Import by full data contract URN.
terraform import datahub_data_contract.orders urn:li:dataContract:b28e16460efef1059ed3749e0de03755

# The bare contract id (URN suffix) is also accepted.
terraform import datahub_data_contract.orders b28e16460efef1059ed3749e0de03755
