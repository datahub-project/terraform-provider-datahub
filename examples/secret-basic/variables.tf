variable "secret_value" {
  description = "Plaintext value to store as the DataHub secret. Pass via TF_VAR_secret_value to avoid shell history exposure."
  type        = string
  sensitive   = true
}
