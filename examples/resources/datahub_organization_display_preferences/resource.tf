# Brand the DataHub UI for every user in the organization.
#
# This is a singleton: DataHub stores these settings once per instance, so
# declare at most one of these resources and manage it from a single place.
#
# Applying this changes what every user of the instance sees, and it is not
# reversible by removing the resource: destroy resets these fields to DataHub's
# defaults rather than restoring whatever branding was there before. Capture the
# existing values first (see the data source below) if you might want them back.
# primary_color needs DataHub Cloud v2.2.0 or later. Leaving it out is safe on
# any version and, uniquely among these attributes, is not a reset while you
# have never set it: a colour set in the DataHub UI survives. Setting it once -
# to "" if what you want is the default brand - takes ownership from then on.
resource "datahub_organization_display_preferences" "main" {
  org_name      = "TF Example Org"
  logo_url      = "https://example.com/tf-example-logo.png"
  primary_color = "#EC0016"
}

# Read the current branding without managing it, which is also how you record
# what was set before adopting the resource above.
data "datahub_organization_display_preferences" "current" {}

output "organization_name" {
  description = "Organization name currently branding the DataHub UI."
  value       = data.datahub_organization_display_preferences.current.org_name
}

output "brand_color" {
  description = "Brand colour the DataHub UI derives its theme from, or null when unset."
  value       = data.datahub_organization_display_preferences.current.primary_color
}
