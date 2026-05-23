variable "secret_value" {
  description = "Plaintext value to store as the DataHub secret. Pass via TF_VAR_secret_value to avoid shell history exposure. Not required for terraform destroy."
  type        = string
  sensitive   = true
  default     = null
}
