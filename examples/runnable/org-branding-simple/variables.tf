variable "org_name" {
  description = <<-EOT
    Organization name branding the DataHub UI (browser title and navigation).

    A variable rather than a literal so that someone adopting this example
    against a real instance can supply their own organization's name instead of
    renaming it to a test string:

        terraform apply -var "org_name=Acme Corporation"
  EOT
  type        = string
  default     = "TF Example Branding"
}

variable "logo_url" {
  description = <<-EOT
    URL of the organization logo shown in the DataHub UI. It must be reachable
    by the browsers of everyone viewing DataHub, not by Terraform.

    Defaults to "", which DataHub treats as clear-to-default. Read that as an
    action, not as leaving things alone: on an instance with no custom logo it
    is a no-op and the UI keeps its own mark, but on one that already has a
    logo, applying this REMOVES it. No custom logo is the usual starting
    point, which is why the default is safe for a first look and why it is
    the wrong thing to leave in place against an instance you care about.

    Do not swap the default for a placeholder URL either - an unreachable one
    renders as a broken image beside the organization name and reads as a bug
    in the example rather than as something to replace.

    Set it to your own logo when adopting this against a real instance.
  EOT
  type        = string
  default     = ""
}

variable "primary_color" {
  description = <<-EOT
    Brand colour for the DataHub UI, as a six-digit hex colour such as
    "#EC0016".

        terraform apply -var "primary_color=#EC0016"

    WHEN DESTROYING, THIS VALUE IS IGNORED. Terraform evaluates the
    configuration on destroy as well, so it asks for a colour either way - but
    the answer is discarded and the branding is reset to DataHub's defaults
    regardless of what you type. Any valid colour gets you past the prompt:

        terraform destroy -var "primary_color=#EC0016"

    There is deliberately no default. Applying this example replaces the
    branding every user of the instance sees, and destroying it resets that
    branding to DataHub's defaults rather than restoring what was there, so a
    default would let someone recolour a whole instance without ever having
    made a choice. Terraform asks instead.

    DataHub derives the UI's brand tokens from this one value, so it affects
    more than one element. Requires DataHub Cloud v2.2.0 or later; on an older
    instance the apply fails with a message naming the attribute, and leaving
    the line out of main.tf manages the other two attributes as before.
  EOT
  type        = string

  validation {
    # Same rule as the provider's own validator (six hex digits, "#RRGGBB"),
    # applied here so the prompt is rejected immediately rather than after a
    # plan. Three-digit CSS shorthand such as "#E01" is not accepted, by the
    # server or by the provider.
    #
    # The provider additionally accepts "" as DataHub's clear-to-default
    # sentinel. This variable does not: an empty string is what an accidental
    # empty prompt answer produces, and silently wiping the brand colour is the
    # outcome this variable exists to prevent. To clear the colour instead,
    # set primary_color = "" directly on the resource in main.tf.
    condition     = can(regex("^#[0-9A-Fa-f]{6}$", var.primary_color))
    error_message = <<-EOT
      primary_color must be a six-digit hex colour code starting with "#", for
      example "#EC0016". Three-digit shorthand ("#E01") is not accepted.

      This value is required and has no default on purpose: applying this
      example rebrands the DataHub UI for every user of the instance, and
      destroying it resets the branding to DataHub's defaults rather than
      restoring what was there. Choose the colour deliberately.
    EOT
  }
}
