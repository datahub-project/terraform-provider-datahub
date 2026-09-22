variable "owner_user_urn" {
  description = <<-EOT
    URN of a corp user to name as a second owner of the glossary term, e.g.
    "urn:li:corpuser:alice@example.com". The user must already exist in
    DataHub: batchAddOwners resolves every owner URN server-side and rejects
    one that does not. The default is the built-in admin account, which is
    present on every Quickstart and DataHub Cloud instance.
  EOT
  type        = string
  default     = "urn:li:corpuser:datahub"

  validation {
    condition     = startswith(var.owner_user_urn, "urn:li:corpuser:")
    error_message = "owner_user_urn must be a corp user URN starting with \"urn:li:corpuser:\"."
  }
}
