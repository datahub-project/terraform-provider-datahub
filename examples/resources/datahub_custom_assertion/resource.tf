# Registers an assertion that something outside DataHub evaluates, so an
# external check appears alongside native ones on the dataset's Quality tab.
#
# DataHub does not run this. The owning system reports each result through the
# reportAssertionResult API; the provider manages only the registration.
resource "datahub_custom_assertion" "nightly_reconciliation" {
  entity_urn     = "urn:li:dataset:(urn:li:dataPlatform:snowflake,analytics.orders,PROD)"
  assertion_type = "CUSTOM_SQL"
  platform_urn   = "urn:li:dataPlatform:great-expectations"
  description    = "Nightly ledger reconciliation, evaluated by the finance pipeline and reported back to DataHub."
}
