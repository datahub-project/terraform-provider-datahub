data "datahub_data_contracts" "all" {}

output "data_contract_urns" {
  description = "URNs of all DataHub data contracts."
  value       = data.datahub_data_contracts.all.urns
}
