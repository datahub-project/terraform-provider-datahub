variable "domains_filter" {
  description = <<-EOT
    List of FIBO domain codes to include (e.g. ["SEC", "DER"]). Leave empty
    (the default) to create the full hierarchy across all available domains.
    Available codes: ACTUS, BE, CAE, DER, FBC, IND, LOAN, MD, SEC.
    FND and BP are always excluded as ontology infrastructure.
  EOT
  type        = list(string)
  default     = []
}

variable "create_root_node" {
  description = <<-EOT
    When true (default), create a single top-level DataHub domain named
    "Financial Industry Business Ontology (FIBO)" and nest all domain nodes
    under it. When false, FIBO domain nodes are created as root-level domains.
  EOT
  type        = bool
  default     = true
}
