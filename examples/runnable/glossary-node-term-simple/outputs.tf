output "finance_node_urn" {
  description = "URN of the Finance root term group."
  value       = datahub_glossary_node.finance.urn
}

output "accounting_node_urn" {
  description = "URN of the Accounting sub-node (child of Finance)."
  value       = datahub_glossary_node.accounting.urn
}

output "customer_node_urn" {
  description = "URN of the Customer root term group."
  value       = datahub_glossary_node.customer.urn
}

output "segmentation_node_urn" {
  description = "URN of the Segmentation sub-node (child of Customer)."
  value       = datahub_glossary_node.segmentation.urn
}

output "revenue_term_urn" {
  description = "URN of the Revenue term (direct child of Finance)."
  value       = datahub_glossary_term.revenue.urn
}

output "accrual_term_urn" {
  description = "URN of the Accrual term (child of Accounting)."
  value       = datahub_glossary_term.accrual.urn
}

output "churn_term_urn" {
  description = "URN of the Churn term (direct child of Customer)."
  value       = datahub_glossary_term.churn.urn
}

output "cohort_term_urn" {
  description = "URN of the Cohort term (child of Segmentation)."
  value       = datahub_glossary_term.cohort.urn
}

output "summary" {
  description = "Post-apply summary of all created glossary entities."
  value       = <<-EOT

  Glossary hierarchy created (4 term groups, 4 terms):

    TF Example Glossary - Finance            ${datahub_glossary_node.finance.urn}
      |- TF Example Glossary - Revenue       ${datahub_glossary_term.revenue.urn}
      +- TF Example Glossary - Accounting    ${datahub_glossary_node.accounting.urn}
           +- TF Example Glossary - Accrual  ${datahub_glossary_term.accrual.urn}

    TF Example Glossary - Customer           ${datahub_glossary_node.customer.urn}
      |- TF Example Glossary - Churn         ${datahub_glossary_term.churn.urn}
      +- TF Example Glossary - Segmentation  ${datahub_glossary_node.segmentation.urn}
           +- TF Example Glossary - Cohort   ${datahub_glossary_term.cohort.urn}

  View in DataHub UI:
    $DATAHUB_GMS_URL/glossary

  To remove all resources:
    terraform destroy

  EOT
}
