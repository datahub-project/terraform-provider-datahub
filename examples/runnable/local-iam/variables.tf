variable "member_username" {
  description = "Username of an existing DataHub user to add to the group. Must already exist; the provider does not create users. Defaults to the bootstrap admin user present on an OSS Quickstart."
  type        = string
  default     = "datahub"
}
