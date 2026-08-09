variable "adopt_default_template" {
  description = <<-EOT
    Manage the template the instance actually renders as everyone's home page,
    instead of creating a separate demonstration template.

    Left false, this example changes nothing users see: it builds a separate
    demonstration template that nothing points at.

    Set to true and the next apply replaces the home page for everyone who has
    not personalised theirs. That is reversible -- destroying restores the
    previous layout rather than deleting the page -- but it is visible to every
    user in the meantime, so the opt-in stays explicit for shared instances.
  EOT
  type        = bool
  default     = false
}

variable "runbook_url" {
  description = "Target for the link module on the page."
  type        = string
  default     = "https://docs.datahub.com"
}

variable "create_alternate_template" {
  description = <<-EOT
    Publish a second GLOBAL template that users can opt into.

    Nothing points at it and DataHub has no query that lists templates, so it is
    invisible until someone is given its URN -- which outputs.tf does.
  EOT
  type        = bool
  default     = true
}

variable "create_test_user" {
  description = <<-EOT
    Create a second user with no personal page, to show that the organisation
    default reaches people other than the account that applied this.

    Requires test_user_password. On open-source DataHub the sign-up is rejected
    if a user entity already exists at that address, so destroy this example
    before re-applying it against the same instance.
  EOT
  type        = bool
  default     = false
}

variable "test_user_email" {
  description = "Address for the test user. Used as the username too, because DataHub Cloud derives the URN from the email and ignores the username."
  type        = string
  default     = "tf-example-homepage-viewer@example.invalid"
}

variable "test_user_password" {
  description = "Initial password for the test user. Write-only: never stored in Terraform state. Required when create_test_user is true."
  type        = string
  default     = null
  sensitive   = true
}
