output "finance_domain_urn" {
  description = "URN of the governed (parent) domain."
  value       = datahub_domain.finance.urn
}

output "billing_domain_urn" {
  description = "URN of the sub-domain covered by the cascade."
  value       = datahub_domain.finance_billing.urn
}

output "marketing_domain_urn" {
  description = "URN of the unrelated domain the stewards can read but not edit."
  value       = datahub_domain.marketing.urn
}

output "steward_group_urn" {
  description = "URN of the stewards group the policy targets."
  value       = datahub_corp_group.finance_stewards.urn
}

output "steward_user_urn" {
  description = "URN of the steward test user."
  value       = datahub_local_user_login.steward.user_urn
}

output "policy_urn" {
  description = "URN of the domain-scoped metadata policy."
  value       = datahub_policy.finance_stewards.urn
}

output "reviewed_tag_urn" {
  description = "URN of the pre-existing tag the stewards apply during verification."
  value       = datahub_tag.reviewed.urn
}

# ---------------------------------------------------------------------------
# Seeding scratch datasets (run as the ADMIN token)
# ---------------------------------------------------------------------------

# One batch write via the OpenAPI v3 entity endpoint: three datasets placed in
# the three domains, plus one with no domain at all -- the four fixtures the
# verification walkthrough needs. Run with:
#   eval "$(terraform output -raw seed_dataset_commands)"
output "seed_dataset_commands" {
  description = "Creates the four scratch datasets the verification walkthrough probes. Run as the admin token."
  value = format(
    "curl -sS -X POST \"$DATAHUB_GMS_URL/openapi/v3/entity/dataset\" -H \"Authorization: Bearer $DATAHUB_GMS_TOKEN\" -H 'Content-Type: application/json' -d '%s'",
    jsonencode([
      for name, domain_urn in local.seed_datasets : merge(
        {
          urn = local.dataset_urn[name]
          datasetProperties = {
            value = {
              name        = name
              description = "Scratch dataset seeded by the domain-scoped-write-access example"
            }
          }
        },
        domain_urn == null ? {} : {
          domains = { value = { domains = [domain_urn] } }
        },
      )
    ]),
  )
}

# The scratch datasets are not Terraform-managed, so terraform destroy does not
# remove them. Run with: eval "$(terraform output -raw cleanup_dataset_commands)"
output "cleanup_dataset_commands" {
  description = "Deletes the four scratch datasets. Run as the admin token, after terraform destroy."
  # A dataset URN contains parentheses and commas, so it must be encoded
  # before it can be used as a path segment on the entity endpoint.
  value = join("\n", [
    for name, _ in local.seed_datasets : format(
      "curl -sS -X DELETE \"$DATAHUB_GMS_URL/openapi/v3/entity/dataset/%s\" -H \"Authorization: Bearer $DATAHUB_GMS_TOKEN\"",
      urlencode(local.dataset_urn[name]),
    )
  ])
}

# ---------------------------------------------------------------------------
# Acting as the steward (run these as the TEST USER, not the admin)
# ---------------------------------------------------------------------------

# Log in as the steward and keep the session cookie every probe below uses.
output "steward_login_commands" {
  description = "Logs in as the steward test user, using the password you supplied as TF_VAR_test_user_password."
  value = format(
    <<-EOT
    # NOTE: these calls go to the DataHub FRONTEND, not to GMS. On open-source
    # DataHub they are different hosts -- frontend :9002, GMS :8080 -- and GMS
    # returns 404 for /logIn. On DataHub Cloud one host serves both.
    #
    #   export DATAHUB_FRONTEND_URL=http://localhost:9002   # Quickstart
    #   export DATAHUB_FRONTEND_URL="$DATAHUB_GMS_URL"      # DataHub Cloud
    #
    # jq builds the body rather than string interpolation, so a password
    # containing a quote or backslash cannot break the JSON.
    curl -sS -c cookies.txt -X POST "$DATAHUB_FRONTEND_URL/logIn" \
      -H 'Content-Type: application/json' \
      -d "$(jq -nc --arg u '%s' --arg p "$TF_VAR_test_user_password" '{username:$u,password:$p}')"
    EOT
    , var.test_user_email,
  )
}

# Check 1: the steward reads an asset OUTSIDE their domain. Expect the dataset
# back -- read access is catalog-wide.
output "probe_read_command" {
  description = "As the steward: read a dataset outside the governed domain. Expect success."
  value       = local.probe_graphql_commands.read_marketing
}

# Check 2: the steward tags an asset INSIDE the governed domain.
# Expect {"data":{"addTag":true}}.
output "probe_in_domain_command" {
  description = "As the steward: apply the reviewed tag to the finance dataset. Expect success."
  value       = local.probe_graphql_commands.tag_finance
}

# Check 4: the steward tags an asset in a SUB-DOMAIN of the governed domain.
# Expect {"data":{"addTag":true}} -- the policy names only the parent, and the
# DOMAIN criterion cascades to descendants.
output "probe_subdomain_command" {
  description = "As the steward: apply the reviewed tag to the billing (sub-domain) dataset. Expect success."
  value       = local.probe_graphql_commands.tag_billing
}

# Check 3: the steward tags an asset in a DIFFERENT domain.
# Expect an Unauthorized error.
output "probe_other_domain_command" {
  description = "As the steward: attempt to tag the marketing dataset. Expect Unauthorized."
  value       = local.probe_graphql_commands.tag_marketing
}

# Check 5: the steward tags an asset with NO domain. Expect an Unauthorized
# error -- a domain-scoped policy matches nothing on an unassigned asset.
output "probe_no_domain_command" {
  description = "As the steward: attempt to tag the no-domain dataset. Expect Unauthorized."
  value       = local.probe_graphql_commands.tag_orphan
}

# Check 6: the steward tries to CREATE a tag. Expect an Unauthorized error --
# tag creation is a platform privilege that cannot be domain-scoped, and this
# example deliberately withholds it.
output "probe_create_tag_command" {
  description = "As the steward: attempt to create a brand-new tag. Expect Unauthorized."
  value       = local.probe_graphql_commands.create_tag
}
