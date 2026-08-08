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
