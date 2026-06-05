data "datahub_glossary_nodes" "all" {}

output "glossary_node_urns" {
  value = data.datahub_glossary_nodes.all.urns
}
