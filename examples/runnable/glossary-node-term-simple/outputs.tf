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

output "recurring_revenue_term_urn" {
  description = "URN of the Recurring Revenue term (direct child of Finance)."
  value       = datahub_glossary_term.recurring_revenue.urn
}

output "recurring_revenue_is_a_revenue_id" {
  description = "Composite id of the isA edge: Recurring Revenue inherits Revenue."
  value       = datahub_glossary_term_relationship.recurring_revenue_is_a_revenue.id
}

output "cohort_has_a_churn_id" {
  description = "Composite id of the hasA edge: Cohort contains Churn."
  value       = datahub_glossary_term_relationship.cohort_has_a_churn.id
}

output "summary" {
  description = "Post-apply summary of all created glossary entities."
  value       = <<-EOT

  Glossary hierarchy created (4 term groups, 5 terms):

    TF Example Glossary - Finance                     ${datahub_glossary_node.finance.urn}
      |- TF Example Glossary - Revenue                ${datahub_glossary_term.revenue.urn}
      |- TF Example Glossary - Recurring Revenue      ${datahub_glossary_term.recurring_revenue.urn}
      +- TF Example Glossary - Accounting             ${datahub_glossary_node.accounting.urn}
           +- TF Example Glossary - Accrual           ${datahub_glossary_term.accrual.urn}

    TF Example Glossary - Customer                    ${datahub_glossary_node.customer.urn}
      |- TF Example Glossary - Churn                  ${datahub_glossary_term.churn.urn}
      +- TF Example Glossary - Segmentation           ${datahub_glossary_node.segmentation.urn}
           +- TF Example Glossary - Cohort            ${datahub_glossary_term.cohort.urn}

  Term relationships created (2 edges):

    Recurring Revenue --isA-->  Revenue   (UI: "Inherits" / "Inherited By")
    Cohort            --hasA--> Churn     (UI: "Contains" / "Contained By")

  View in DataHub UI:
    $DATAHUB_GMS_URL/glossary

  To remove all resources:
    terraform destroy

  EOT
}
