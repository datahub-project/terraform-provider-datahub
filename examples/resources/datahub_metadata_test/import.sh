# Import by bare test_id (provider constructs the URN).
terraform import datahub_metadata_test.prod_datasets_have_owners tf-example-prod-dataset-owners

# Import by full URN. Tests created in the DataHub Cloud UI have a random
# UUID id, so import is how they come under Terraform management.
terraform import datahub_metadata_test.prod_datasets_have_owners urn:li:test:tf-example-prod-dataset-owners
