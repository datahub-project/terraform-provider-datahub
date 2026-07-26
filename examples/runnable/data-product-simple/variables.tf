variable "enable_defaults" {
  type        = bool
  default     = false
  description = "Turn on the provider's defaults.tags and defaults.structured_properties. Leave false for the first apply, which only creates the bootstrap tag and structured property (provider configuration cannot depend on resources created in the same apply -- see the \"Provider-level defaults\" guide's bootstrap ordering section). Set to true and re-apply once they exist."
}
