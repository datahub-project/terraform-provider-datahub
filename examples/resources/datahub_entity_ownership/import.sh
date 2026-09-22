# Import by the owned entity's URN. Every owner then present on the entity is
# adopted into the resource, so drop an entry from the configuration afterwards
# and the next apply removes that pair from DataHub.
terraform import datahub_entity_ownership.revenue 'urn:li:glossaryTerm:revenue'
