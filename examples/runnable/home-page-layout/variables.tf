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

