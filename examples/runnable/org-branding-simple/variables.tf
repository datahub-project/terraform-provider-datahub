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

    The default is a deliberately non-resolving example.com URL, so applying
    this example unmodified shows a broken image rather than quietly branding
    the instance with somebody else's artwork. Override it with your own.
  EOT
  type        = string
  default     = "https://example.com/tf-example-branding-logo.png"
}

variable "primary_color" {
  description = <<-EOT
    Brand colour for the DataHub UI, as a six-digit hex colour such as
    "#EC0016". DataHub derives the UI's brand tokens from it, so it affects
    more than one element.

    Deliberately has NO default. Applying this example replaces the branding
    every user of the instance sees, and destroying it resets that branding to
    DataHub's defaults rather than restoring what was there - so a default here
    would let someone recolour a whole instance without ever having made a
    choice. Terraform prompts for a value instead.

        terraform apply -var "primary_color=#EC0016"

    Keep supplying it for `terraform destroy` too: Terraform evaluates the
    configuration, and therefore this variable's validation, on destroy as well.

    Requires DataHub Cloud v2.2.0 or later. On an older instance the apply
    fails with a message naming the attribute; leaving the line out of main.tf
    manages the other two attributes as before.
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
