terraform {
  required_version = ">= 1.11"

  required_providers {
    datahub = {
      source  = "datahub-project/datahub"
      version = "0.25.0"
    }
  }
}

# Credentials come from the environment:
#   DATAHUB_GMS_URL   - your DataHub Cloud instance URL
#   DATAHUB_GMS_TOKEN - personal access token whose principal holds the
#                       Manage Organization Display Preferences privilege
provider "datahub" {}

# ---------------------------------------------------------------------------
# Record the branding that is on the instance right now.
#
# This data source has no dependency on the resource below, so Terraform reads
# it during the plan phase, before anything is written. On the FIRST
# `terraform apply` the outputs therefore describe the branding as it was
# before this example replaced it - and that is the only chance to capture it:
# `terraform destroy` resets these fields to DataHub's defaults rather than
# restoring whatever was there, and DataHub keeps no history of the old values.
#
# The next `terraform plan` or `terraform refresh` re-reads this data source
# and the outputs flip to this example's own values. Copy restore_snippet out
# of the first apply, before that happens. See README.md.
# ---------------------------------------------------------------------------
data "datahub_organization_display_preferences" "previous" {}

# ---------------------------------------------------------------------------
# Organization-wide branding: name, logo and brand colour.
#
# This is a singleton. DataHub stores these settings once per instance, on the
# global settings object, so there is no id to supply, at most one of these
# resources belongs in a configuration, and applying it changes what EVERY user
# of the instance sees.
# ---------------------------------------------------------------------------
resource "datahub_organization_display_preferences" "branding" {
  # All three attributes are declared on purpose, and org_name and logo_url in
  # particular are not optional in practice. The provider sends both on every
  # write whatever the configuration says, so a configuration naming only
  # primary_color would blank the organization name and the logo rather than
  # leave them alone. Declaring all three is the only way to know what the
  # instance ends up with.
  org_name = var.org_name
  logo_url = var.logo_url

  # primary_color is the one exception to that rule, and only while it has
  # never been set: until this resource writes it once, it is left out of the
  # mutation entirely and a colour set in the DataHub UI survives. Naming it
  # here - which this example deliberately does - takes ownership from now on,
  # including being reset if the line is later removed or the resource
  # destroyed.
  #
  # The variable has no default, so Terraform asks for a value rather than
  # picking one. See variables.tf for why.
  primary_color = var.primary_color
}
