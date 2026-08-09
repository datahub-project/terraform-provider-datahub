variable "runbook_url" {
  description = "Target for the link module on the page."
  type        = string
  default     = "https://docs.datahub.com"
}

variable "test_user_email" {
  description = "Address for the test user. Used as the username too, because DataHub Cloud derives the URN from the email and ignores the username."
  type        = string
  default     = "tf-example-homepage-viewer@example.invalid"
}


variable "test_user_password" {
  description = <<-EOT
    Initial password for the test user. Write-only: passed straight to DataHub
    and never written to Terraform state.

    Pass it via TF_VAR_test_user_password rather than -var, so it does not land
    in shell history. Required for destroy as well as apply, because Terraform
    evaluates variable validation on both.
  EOT
  type        = string
  sensitive   = true
  default     = null

  validation {
    # Deliberately loud. initial_password is Optional on the resource, so a
    # missing value would not fail -- the provider would quietly generate a
    # throwaway and return a reset link instead, and the scripted login below
    # would not work. Better to stop here with instructions than to succeed
    # into a state the example cannot demonstrate.
    condition     = var.test_user_password != null && length(var.test_user_password) >= 16
    error_message = <<-EOT
      test_user_password is required, and must be at least 16 characters.

      This example creates a real login, so it will not invent a credential for
      you: a generated one would have to be stored in Terraform state to be
      shown to you, and state keeps it forever. Supply one instead -- it is
      write-only and never stored:

          export TF_VAR_test_user_password=$(openssl rand -base64 24)
          echo "$TF_VAR_test_user_password"   # note it down; you will need it to log in

      Keep it exported for `terraform destroy` too.
    EOT
  }
}
