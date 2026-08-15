# form-simple

Demonstrates `datahub_form` by creating the two form types side by side, along with everything they need:

- **TF Example Form - Dataset Metadata** (`tf-example-form-metadata`) - a `COMPLETION` form that collects two structured property values (a required sensitivity level and an optional review cycle), assigned to asset owners plus a stewardship group, and automatically applied to every PostgreSQL dataset.
- **TF Example Form - Sensitivity Attestation** (`tf-example-form-attestation`) - a `VERIFICATION` form whose single prompt asks owners to attest that the recorded sensitivity level is still accurate, automatically applied to every MySQL dataset.
- The two structured properties the prompts collect, and the stewardship group the COMPLETION form is assigned to - all created here and referenced via `.urn`, so Terraform orders creation automatically.

The forms only *define* the structured properties; values are written when someone responds to a form in the DataHub UI.

## Things to know before adapting this

- **Assignment is asynchronous and additive.** A background hook assigns the form to matching entities after the filter is written, so newly matching datasets pick the form up on their own. Narrowing or removing the `dynamic_assignment` filter stops *future* assignment but does not retract the form from entities that already carry it; deleting the form does (also asynchronously).
- **Full-state ownership.** Each apply replaces the prompts list, the actors lists and the dynamic assignment filter wholesale. Prompts or actor assignments added in the DataHub UI are removed on the next apply.
- **Omit empty actor lists; never write `users = []` or `groups = []`.** An explicit empty list reads back as null and produces a plan that never converges. Leaving the whole `actors` block out assigns the form to the owners of each matched asset, which is also the default.
- **Prompt ids are required and must be globally unique across all forms.** The server would otherwise mint a random UUID, and completion records are keyed by the id - changing it resets collected responses.

## Prerequisites

- DataHub GMS URL and a personal access token with the **Manage Metadata Forms** (`MANAGE_FORMS`) privilege, plus **Manage Structured Properties** and **Manage Users and Groups** for the supporting resources.
- Terraform >= 1.11.

## Usage

```bash
export DATAHUB_GMS_URL=http://localhost:8080
export DATAHUB_GMS_TOKEN=your-personal-access-token

terraform init
terraform apply
```

After apply, verify the forms. Where depends on the deployment, and this catches people out:

- **DataHub Cloud**: **Govern → Compliance Forms** (the entry is itself behind a feature flag, so it can be hidden even there).
- **OSS, including the Quickstart**: there is no forms page. The nav entry exists only in the Cloud build, so the forms are created, readable through the API, and invisible in the browser. Use `terraform output -raw verify_command`, which reads the form back through the strongly consistent OpenAPI v3 entity endpoint.

Once a PostgreSQL or MySQL dataset exists in the catalog, the background hook assigns the matching form to it and the prompts appear on the dataset page.

## Outputs

| Output | Description |
|---|---|
| `completion_form_urn` | URN of the COMPLETION form |
| `verification_form_urn` | URN of the VERIFICATION form |
| `sensitivity_property_urn` | URN of the structured property both forms collect |
| `stewards_group_urn` | URN of the group assigned the COMPLETION form |
| `all_form_urns` | All form URNs in DataHub (eventually consistent) |

## Cleanup

```bash
terraform destroy
```

This hard-deletes both forms, both structured property definitions and the group. DataHub asynchronously removes form references from any assigned entities; structured property values already collected through the prompts are ordinary metadata on those entities and survive.
