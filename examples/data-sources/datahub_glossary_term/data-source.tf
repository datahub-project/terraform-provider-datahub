data "datahub_glossary_term" "revenue" {
  term_id = "revenue"
}

output "revenue_urn" {
  value = data.datahub_glossary_term.revenue.urn
}
