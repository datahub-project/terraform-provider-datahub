variable "oauth_client_id" {
  description = "OAuth client ID issued by the identity provider. Not a secret (DataHub returns it on read), so a placeholder default keeps the example self-contained."
  type        = string
  default     = "0oa-tf-example-client-id"
}

variable "oauth_client_secret" {
  description = "OAuth client secret issued by the identity provider. Deliberately has no default: supply it via TF_VAR_oauth_client_secret, a .tfvars file outside version control, or a secrets manager (see README.md). Sent to DataHub, which encrypts it server-side; never written to Terraform state."
  type        = string
  sensitive   = true
}
