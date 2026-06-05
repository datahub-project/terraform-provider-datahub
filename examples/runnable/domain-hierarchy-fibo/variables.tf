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
    Also controls whether a matching root glossary node is created.
  EOT
  type        = bool
  default     = true
}

variable "create_glossary" {
  description = <<-EOT
    When true (default), create a Business Glossary hierarchy alongside the
    domain hierarchy. Each FIBO leaf ontology contributes a glossary node and
    its owl:Class definitions become glossary terms nested beneath it. The
    glossary mirrors the domain tree at all four levels (root, domain, module,
    leaf). Set to false to create only the domain hierarchy without any
    glossary resources.
  EOT
  type        = bool
  default     = true
}
