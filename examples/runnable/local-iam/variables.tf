variable "new_member_email" {
  description = <<-EOT
    Email address for the new team member created by this example.
    Used as both the DataHub username and the email field.

    Use an email address on both OSS and Cloud:
    - OSS DataHub: any string works as a username; email format is accepted.
    - DataHub Cloud: the URN is always derived from the email field, so the
      username and email must match.

    Avoid using a real address; this is a demo account that will be deleted
    on terraform destroy.
  EOT
  type        = string
  default     = "tf-example-member@example.com"
}

variable "member_username" {
  description = "Username of an existing DataHub user to add to the group. Must already exist (provisioned via SSO/JIT or separately). Defaults to the bootstrap admin present on an OSS Quickstart."
  type        = string
  default     = "datahub"
}
