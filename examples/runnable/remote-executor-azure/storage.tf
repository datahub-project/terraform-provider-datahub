# A tiny storage account with one seeded CSV blob. This is what the ingestion
# source actually ingests, making the example runnable end to end: the abs
# source lists the container, infers the CSV schema, and emits a dataset.
resource "azurerm_storage_account" "data" {
  name                     = "tfexdh${random_string.suffix.result}"
  location                 = azurerm_resource_group.rg.location
  resource_group_name      = azurerm_resource_group.rg.name
  account_tier             = "Standard"
  account_replication_type = "LRS"
  # Shared-key auth stays enabled (the default): Terraform uses it to upload
  # the blob, and the recipe authenticates with the account key.
  tags = local.tags
}

resource "azurerm_storage_container" "data" {
  name                  = local.container_name
  storage_account_id    = azurerm_storage_account.data.id
  container_access_type = "private"
}

resource "azurerm_storage_blob" "customers_csv" {
  name                 = "customers.csv"
  storage_container_id = azurerm_storage_container.data.id
  type                 = "Block"
  content_type         = "text/csv"

  source_content = <<-CSV
    id,name,region,signup_date
    1,Amara Okafor,emea,2025-11-03
    2,Jin Park,apac,2025-12-14
    3,Lucia Alvarez,amer,2026-01-22
    4,Noah Williams,amer,2026-02-05
    5,Priya Sharma,apac,2026-03-18
  CSV
}
