variable "domains_filter" {
  description = <<-EOT
    List of FIBO domain codes to include (e.g. ["SEC", "DER"]). Leave empty
    (the default) to create the full hierarchy across all available domains.
    Available codes after scaffolding exclusions: BE, CAE, DER, FBC, IND,
    LOAN, MD, SEC (ACTUS if present). FND and BP are always excluded as
    ontology infrastructure rather than business domains.
  EOT
  type        = list(string)
  default     = []
}
