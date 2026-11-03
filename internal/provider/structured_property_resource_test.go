// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider_test

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/datahubtesting"
	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
)

func TestAcc_StructuredProperty_Lifecycle(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	propertyID := tg.Name("tfprovider-sp")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.StructuredPropertyCheckDestroy,
		Steps:                    datahubtesting.StructuredPropertyLifecycleSteps(propertyID),
	})
}

func TestAcc_StructuredProperty_Version(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	propertyID := tg.Name("tfprovider-sp-version")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.StructuredPropertyCheckDestroy,
		Steps:                    datahubtesting.StructuredPropertyVersionSteps(propertyID),
	})
}

func TestAcc_StructuredProperty_VersionedCreate(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	propertyID := tg.Name("tfprovider-sp-vcreate")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.StructuredPropertyCheckDestroy,
		Steps:                    datahubtesting.StructuredPropertyVersionedCreateSteps(propertyID),
	})
}

func TestAcc_StructuredProperty_InvalidVersion(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	propertyID := tg.Name("tfprovider-sp-badversion")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.StructuredPropertyCheckDestroy,
		Steps:                    datahubtesting.StructuredPropertyInvalidVersionSteps(propertyID),
	})
}

func TestAcc_StructuredProperty_DataSource(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	propertyID := tg.Name("tfprovider-sp-ds")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.StructuredPropertyCheckDestroy,
		Steps:                    datahubtesting.StructuredPropertyDataSourceSteps(propertyID),
	})
}

func TestAcc_StructuredProperty_List(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	if tg.IsLive() {
		t.Skip("list data source is OpenSearch-backed and eventually consistent; a just-created property may not be indexed at read time")
	}
	propertyID := tg.Name("tfprovider-sp-list")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.StructuredPropertyCheckDestroy,
		Steps:                    datahubtesting.StructuredPropertyListSteps(propertyID),
	})
}

// TestAcc_StructuredProperty_SoftDeletedInState covers the state a destroy
// leaves when DataHub accepts the soft delete and then refuses the hard
// delete, which is also what deleting the property in the DataHub UI does:
// the property is still in state but soft-deleted in DataHub. A refresh must
// still succeed (with a warning, since DataHub rejects writes to the
// property), and the next destroy must finish the job, which is what the
// delete error tells the practitioner to do. Runs on the mock and on a live
// target alike.
func TestAcc_StructuredProperty_SoftDeletedInState(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	propertyID := tg.Name("tfprovider-sp-softdel")
	urn := "urn:li:structuredProperty:" + propertyID
	config := fmt.Sprintf(`
provider "datahub" {}

resource "datahub_structured_property" "test" {
  property_id  = %q
  value_type   = "string"
  entity_types = ["dataset"]
}
`, propertyID)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.StructuredPropertyCheckDestroy,
		Steps: []resource.TestStep{
			{Config: config},
			{
				// Soft-delete it behind Terraform's back, then refresh.
				PreConfig: func() {
					c, err := datahub.NewClient(os.Getenv("DATAHUB_GMS_URL"), os.Getenv("DATAHUB_GMS_TOKEN"))
					if err != nil {
						t.Fatalf("client: %v", err)
					}
					req, err := c.NewRequest(context.Background(), http.MethodPost, "/openapi/v3/entity/structuredproperty?async=false",
						[]map[string]any{{"urn": urn, "status": map[string]any{"value": map[string]any{"removed": true}}}})
					if err != nil {
						t.Fatalf("request: %v", err)
					}
					res, err := c.Do(req)
					if err != nil {
						t.Fatalf("soft delete: %v", err)
					}
					res.Body.Close()
					if res.StatusCode >= http.StatusBadRequest {
						t.Fatalf("soft delete: HTTP %d", res.StatusCode)
					}
				},
				RefreshState: true,
			},
			// The post-test destroy then deletes a soft-deleted property, and
			// CheckDestroy confirms it is gone.
		},
	})
}
