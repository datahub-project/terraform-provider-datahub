# Read the branding DataHub shows every user, without taking ownership of it.
# There are no arguments: DataHub stores these settings once per instance.
data "datahub_organization_display_preferences" "current" {}

# Record the current values before adopting the
# datahub_organization_display_preferences resource. Applying that resource
# overwrites the branding for every user of the instance, and destroying it
# resets these fields to DataHub's defaults rather than restoring what was
# there, so this data source is the only way to capture them first.
output "current_branding" {
  description = "Organization name and logo currently branding the DataHub UI."
  value = {
    org_name = data.datahub_organization_display_preferences.current.org_name
    logo_url = data.datahub_organization_display_preferences.current.logo_url
  }
}

# A preference that has never been set comes back as null, so supply a fallback
# wherever the value is reused in a configuration and has to be non-empty.
output "display_name" {
  description = "Organization name to show in reports, with a fallback when unset."
  value       = coalesce(data.datahub_organization_display_preferences.current.org_name, "DataHub")
}
