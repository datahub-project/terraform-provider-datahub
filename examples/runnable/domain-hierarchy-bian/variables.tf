variable "business_areas_filter" {
  description = <<-EOT
    List of BIAN business-area ids to create (e.g. ["customers", "products"]).
    Only the specified areas and all of their business domains and service
    domains are created. Set to [] (the default) to create the entire BIAN
    hierarchy: 8 business areas / 43 business domains / 326 service domains
    = 377 domains total.
  EOT
  type        = list(string)
  default     = []
}
