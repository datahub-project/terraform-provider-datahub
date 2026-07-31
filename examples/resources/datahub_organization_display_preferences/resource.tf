# Brand the DataHub UI for every user in the organization.
#
# This is a singleton: DataHub stores these settings once per instance, so
# declare at most one of these resources and manage it from a single place.
#
# Applying this changes what every user of the instance sees, and it is not
# reversible by removing the resource: destroy resets these fields to DataHub's
# defaults rather than restoring whatever branding was there before. Capture the
# existing values first (see the data source below) if you might want them back.
resource "datahub_organization_display_preferences" "main" {
  org_name = "TF Example Org"
  logo_url = "https://example.com/tf-example-logo.png"
}

# Read the current branding without managing it, which is also how you record
# what was set before adopting the resource above.
data "datahub_organization_display_preferences" "current" {}

output "organization_name" {
  description = "Organization name currently branding the DataHub UI."
  value       = data.datahub_organization_display_preferences.current.org_name
}
