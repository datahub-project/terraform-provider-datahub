# Brand the DataHub UI for every user in the organization.
#
# This is a singleton: DataHub stores these settings once per instance, so
# declare at most one of these resources and manage it from a single place.
resource "datahub_organization_display_preferences" "main" {
  org_name = "TF Example Org"
  logo_url = "https://example.com/tf-example-logo.png"
}

# Read the current branding without managing it. Useful for reusing the
# organization name elsewhere, or for inspecting what is set before adopting
# the resource above.
data "datahub_organization_display_preferences" "current" {}

output "organization_name" {
  description = "Organization name currently branding the DataHub UI."
  value       = data.datahub_organization_display_preferences.current.org_name
}
