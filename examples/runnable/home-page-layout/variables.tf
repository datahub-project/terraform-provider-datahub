variable "adopt_default_template" {
  description = <<-EOT
    Manage the template the instance actually renders as everyone's home page,
    instead of creating a separate demonstration template.

    Left false, this example applies and destroys cleanly and changes nothing
    users see. Set to true and the next apply replaces your organisation's home
    page layout, and `terraform destroy` will refuse to remove it -- see the
    README.
  EOT
  type        = bool
  default     = false
}

variable "runbook_url" {
  description = "Target for the link module on the page."
  type        = string
  default     = "https://docs.datahub.com"
}
