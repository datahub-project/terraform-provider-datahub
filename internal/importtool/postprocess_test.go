// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package importtool

import (
	"strings"
	"testing"
)

// secretGenerated is the generated.tf fragment that Terraform produces for a
// datahub_secret import -- empirically captured from a real generate-config-out run.
const secretGenerated = `# __generated__ by Terraform
# Please review these resources and move them into your main configuration files.

# __generated__ by Terraform from "urn:li:dataHubSecret:DBT_CLOUD_SECRET_FICTION_BANK"
resource "datahub_secret" "dbt_cloud_secret_fiction_bank" {
  description      = null
  name             = "DBT_CLOUD_SECRET_FICTION_BANK"
  value            = null # sensitive
  value_wo_version = null
}
`

// connectionGenerated is the generated.tf fragment for a datahub_connection import.
const connectionGenerated = `# __generated__ by Terraform from "urn:li:dataHubConnection:da45c888-ef22-4a16-8a4e-85c0ee539c80"
resource "datahub_connection" "r_da45c888_ef22_4a16_8a4e_85c0ee539c80" {
  config_wo_version = null
  connection_id     = "da45c888-ef22-4a16-8a4e-85c0ee539c80"
  name              = "databricks"
}
`

func TestPostProcess_Secret(t *testing.T) {
	out, vars, err := PostProcess([]byte(secretGenerated))
	if err != nil {
		t.Fatalf("PostProcess error: %v", err)
	}

	// Should have one variable for "value".
	if len(vars) != 1 {
		t.Fatalf("got %d variables; want 1 (got: %v)", len(vars), vars)
	}
	v := vars[0]
	if v.Attr != "value" {
		t.Errorf("variable attr = %q; want \"value\"", v.Attr)
	}
	if !strings.Contains(v.Name, "value") {
		t.Errorf("variable name %q should contain \"value\"", v.Name)
	}

	content := string(out)

	// value should now reference var.X
	if !strings.Contains(content, "var.") {
		t.Error("generated output should contain var. reference for WriteOnly attr")
	}
	// value_wo_version should be set to 1
	if !strings.Contains(content, "value_wo_version = 1") {
		t.Errorf("expected value_wo_version = 1 in output:\n%s", content)
	}
	// null # sensitive should be gone
	if strings.Contains(content, "null # sensitive") {
		t.Error("null # sensitive should have been replaced")
	}
}

func TestPostProcess_Connection(t *testing.T) {
	out, vars, err := PostProcess([]byte(connectionGenerated))
	if err != nil {
		t.Fatalf("PostProcess error: %v", err)
	}

	// No WriteOnly attrs for connection top-level.
	if len(vars) != 0 {
		t.Errorf("got %d variables for connection; want 0", len(vars))
	}

	content := string(out)

	// config_wo_version should be set to 1
	if !strings.Contains(content, "config_wo_version = 1") {
		t.Errorf("expected config_wo_version = 1 in output:\n%s", content)
	}
	// Platform stub comment should appear
	if !strings.Contains(content, "# Add ONE of the following platform blocks") {
		t.Errorf("expected platform stub comment in output:\n%s", content)
	}
}

func TestPostProcess_IngestionSource(t *testing.T) {
	// Ingestion source has no WriteOnly attrs -- PostProcess should be a near-no-op.
	src := `resource "datahub_ingestion_source" "bigquery" {
  recipe      = jsonencode({source = {type = "bigquery"}})
  source_id   = "abc123"
  source_name = "BigQuery"
}
`
	out, vars, err := PostProcess([]byte(src))
	if err != nil {
		t.Fatalf("PostProcess error: %v", err)
	}
	if len(vars) != 0 {
		t.Errorf("got %d variables for ingestion source; want 0", len(vars))
	}
	// Output should be valid and contain the recipe.
	if !strings.Contains(string(out), "bigquery") {
		t.Error("ingestion source name should be preserved")
	}
}
