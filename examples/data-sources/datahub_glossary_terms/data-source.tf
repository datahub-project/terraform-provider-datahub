data "datahub_glossary_terms" "all" {}

output "glossary_term_urns" {
  value = data.datahub_glossary_terms.all.urns
}
