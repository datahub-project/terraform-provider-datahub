locals {
  previous = data.datahub_organization_display_preferences.previous

  # A preference that was never set, or that was set and later reset, reads
  # back as null. Normalising null to "" here does two things: it keeps the
  # generated snippet below valid HCL, and it keeps the restore faithful, since
  # "" is DataHub's clear-to-default sentinel and is exactly what the null
  # meant. primary_color is additionally null on a DataHub Cloud instance older
  # than v2.2.0, which has no brand colour to report.
  previous_org_name      = local.previous.org_name == null ? "" : local.previous.org_name
  previous_logo_url      = local.previous.logo_url == null ? "" : local.previous.logo_url
  previous_primary_color = local.previous.primary_color == null ? "" : local.previous.primary_color
}

output "settings_urn" {
  description = "URN of the DataHub global settings singleton these preferences live on."
  value       = datahub_organization_display_preferences.branding.urn
}

output "applied_branding" {
  description = "Branding this configuration put on the instance. Every user of the instance sees it."
  value = {
    org_name      = datahub_organization_display_preferences.branding.org_name
    logo_url      = datahub_organization_display_preferences.branding.logo_url
    primary_color = datahub_organization_display_preferences.branding.primary_color
  }
}

# The record of what was there before, as read during the plan phase of the
# first apply. Null means the preference was not set.
#
# Read this on the FIRST apply and keep it somewhere outside Terraform. The
# next plan or refresh re-reads the data source and these values become this
# example's own, at which point nothing anywhere remembers the originals:
# destroy resets the fields to DataHub's defaults, and DataHub stores no
# history.
output "previous_branding" {
  description = "Branding the instance had before this example was applied, as read during the first plan. Null means unset."
  value = {
    org_name      = local.previous.org_name
    logo_url      = local.previous.logo_url
    primary_color = local.previous.primary_color
  }
}

# The same values as a configuration block, ready to paste into a directory of
# your own to put the instance back the way it was:
#
#   terraform output -raw restore_snippet > branding-restore.tf
#
# Empty strings are meaningful rather than filler: DataHub cannot remove one of
# these fields, only blank it, so "" is how you ask for the default back.
output "restore_snippet" {
  description = "A datahub_organization_display_preferences block that restores the branding recorded in previous_branding."
  value       = <<-EOT
    resource "datahub_organization_display_preferences" "branding" {
      org_name      = ${jsonencode(local.previous_org_name)}
      logo_url      = ${jsonencode(local.previous_logo_url)}
      primary_color = ${jsonencode(local.previous_primary_color)}
    }
  EOT
}

# The same restore expressed against this example's own variables, so it can be
# run from this directory without editing anything.
#
# Emitted only when the instance had a brand colour to restore. This example's
# primary_color variable requires a real six-digit hex value, so there is no
# -var form that restores "no colour set" - use restore_snippet for that case,
# where "" asks DataHub for its default brand back.
output "restore_command" {
  description = "Command that re-applies the previous branding from this directory, or instructions when no -var form can express it."
  value = local.previous_primary_color == "" ? trimspace(<<-EOT
    # The instance had no brand colour set, and this example's primary_color
    # variable will not accept the empty string that asks for DataHub's
    # default back. Restore from the snippet instead:
    #   terraform output -raw restore_snippet > branding-restore.tf
  EOT
    ) : join(" ", [
      "terraform apply",
      "-var ${jsonencode("org_name=${local.previous_org_name}")}",
      "-var ${jsonencode("logo_url=${local.previous_logo_url}")}",
      "-var ${jsonencode("primary_color=${local.previous_primary_color}")}",
  ])
}
