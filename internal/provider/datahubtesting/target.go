// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahubtesting

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
)

// TargetKind identifies which DataHub backend an acceptance test is running
// against. The active target is derived at setup time from whether
// DATAHUB_GMS_URL is present in the process environment.
type TargetKind int

const (
	// TargetMock points the provider at an in-memory mock server spun up
	// per-test. This is the default: SetupTarget selects it when
	// DATAHUB_GMS_URL is absent from the environment. The only target used
	// in CI on every PR push.
	TargetMock TargetKind = iota

	// TargetLive points the provider at a real DataHub instance (local
	// Quickstart or remote tenant). SetupTarget selects it when
	// DATAHUB_GMS_URL is present in the environment.
	TargetLive
)

// Target is the active acceptance-test backend chosen at SetupTarget time.
// It encapsulates whether the provider is talking to an in-memory mock or
// a real DataHub instance, and exposes helpers (resource naming, kind
// predicates) that let test functions adapt without branching on env vars
// themselves.
type Target struct {
	Kind    TargetKind
	isCloud bool
}

// IsLive reports whether the target is a real DataHub instance rather than
// the in-memory mock.
func (tg *Target) IsLive() bool {
	return tg.Kind != TargetMock
}

// IsCloud reports whether the target is known to be a DataHub Cloud instance.
// It returns true for the in-memory mock (which always supports Cloud features)
// and for live targets when auto-detection or DATAHUB_CLOUD=1 indicates Cloud.
func (tg *Target) IsCloud() bool {
	return tg.isCloud
}

// RequireCloud skips the calling test if the target is not Cloud-capable.
// Use this on every test that exercises Cloud-only resources such as
// datahub_remote_executor_pool. The test always runs against the mock
// (which simulates Cloud). Against live targets it runs only when the
// instance is detected as DataHub Cloud; set DATAHUB_CLOUD=1 to force
// Cloud mode if auto-detection gives the wrong result.
func (tg *Target) RequireCloud(t *testing.T) {
	t.Helper()
	if !tg.isCloud {
		t.Skip("skipping Cloud-only test: target is not DataHub Cloud; set DATAHUB_CLOUD=1 to force Cloud mode")
	}
}

// RequireOSS skips the calling test unless the target is a live instance
// detected as OSS DataHub. Use this for tests that specifically verify the
// provider's graceful-error behavior when Cloud-only features are absent
// (e.g. datahub_remote_executor_pool reporting "DataHub Cloud Required").
// Skips on mock (which simulates Cloud) and on any live target detected or
// forced as Cloud. Set DATAHUB_CLOUD=0 to force OSS mode on a Cloud instance.
func (tg *Target) RequireOSS(t *testing.T) {
	t.Helper()
	if tg.Kind == TargetMock {
		t.Skip("skipping OSS error-path test: mock target always supports Cloud features; use testacc-local, testacc-quickstart, or testacc-remote against an OSS instance")
	}
	if tg.isCloud {
		t.Skip("skipping OSS error-path test: target is DataHub Cloud; this test requires an OSS instance. Set DATAHUB_CLOUD=0 to force OSS mode.")
	}
}

// Name returns a resource name suitable for the active target.
//
// For the mock target, base is returned unchanged: each test gets a fresh
// in-memory server so collisions are impossible and stable names aid
// debugging.
//
// For live targets, base is suffixed with a short random string. The
// randomness prevents collisions between repeated runs (e.g. when a
// previous run leaked state because Destroy failed mid-test) and between
// concurrent developers sharing a single DataHub instance. The
// "tfprovider-" prefix convention used throughout these tests gives a
// future sweeper a stable substring to match on.
func (tg *Target) Name(base string) string {
	if tg.Kind == TargetMock {
		return base
	}
	return base + "-" + strings.ToLower(acctest.RandString(8))
}

// EnsureDatasetEntity creates a minimal dataset entity on a live target so that
// assertion monitor mutations (which require the referenced dataset to exist in
// DataHub) succeed. A t.Cleanup is registered to hard-delete the entity after
// the test ends. On the in-memory mock this is a no-op: the mock does not
// validate entity existence before accepting GraphQL mutations.
func (tg *Target) EnsureDatasetEntity(t *testing.T, entityURN string) {
	t.Helper()
	if tg.Kind == TargetMock {
		return
	}

	gmsURL := strings.TrimRight(os.Getenv("DATAHUB_GMS_URL"), "/")
	token := os.Getenv("DATAHUB_GMS_TOKEN")

	body, err := json.Marshal([]map[string]any{
		{
			"urn": entityURN,
			"datasetProperties": map[string]any{
				"value": map[string]any{
					"name": "TF Provider Test Dataset",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("EnsureDatasetEntity: marshal: %v", err)
	}

	httpClient := &http.Client{Timeout: 15 * time.Second}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
		gmsURL+"/openapi/v3/entity/dataset", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("EnsureDatasetEntity: build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		t.Fatalf("EnsureDatasetEntity: POST /openapi/v3/entity/dataset: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		t.Fatalf("EnsureDatasetEntity: POST returned HTTP %d for URN %q", resp.StatusCode, entityURN)
	}
	t.Logf("EnsureDatasetEntity: created dataset entity %q (HTTP %d)", entityURN, resp.StatusCode)

	t.Cleanup(func() {
		deleteURL := gmsURL + "/openapi/v3/entity/dataset/" + url.PathEscape(entityURN)
		delReq, delErr := http.NewRequestWithContext(context.Background(), http.MethodDelete, deleteURL, nil)
		if delErr != nil {
			t.Logf("EnsureDatasetEntity cleanup: build delete request: %v", delErr)
			return
		}
		delReq.Header.Set("Authorization", "Bearer "+token)
		delResp, delErr := httpClient.Do(delReq)
		if delErr != nil {
			t.Logf("EnsureDatasetEntity cleanup: DELETE %q: %v", entityURN, delErr)
			return
		}
		defer delResp.Body.Close()
		t.Logf("EnsureDatasetEntity cleanup: deleted dataset entity %q (HTTP %d)", entityURN, delResp.StatusCode)
	})
}

// EnsureDatasetEntityWithSchema is like EnsureDatasetEntity but also attaches a
// schemaMetadata aspect with two columns (id NUMBER/INTEGER, email STRING/VARCHAR)
// so that schema and field (column) assertion acceptance tests have a schema to
// target. No-op on the mock target. The dataset is hard-deleted on cleanup.
func (tg *Target) EnsureDatasetEntityWithSchema(t *testing.T, entityURN string) {
	t.Helper()
	if tg.Kind == TargetMock {
		return
	}

	gmsURL := strings.TrimRight(os.Getenv("DATAHUB_GMS_URL"), "/")
	token := os.Getenv("DATAHUB_GMS_TOKEN")

	body, err := json.Marshal([]map[string]any{
		{
			"urn":               entityURN,
			"datasetProperties": map[string]any{"value": map[string]any{"name": "TF Provider Test Dataset"}},
			"schemaMetadata": map[string]any{"value": map[string]any{
				"schemaName":     "tf_test",
				"platform":       "urn:li:dataPlatform:sqlite",
				"version":        0,
				"hash":           "",
				"platformSchema": map[string]any{"com.linkedin.schema.OtherSchema": map[string]any{"rawSchema": ""}},
				"fields": []map[string]any{
					{"fieldPath": "id", "nativeDataType": "INTEGER", "type": map[string]any{"type": map[string]any{"com.linkedin.schema.NumberType": map[string]any{}}}},
					{"fieldPath": "email", "nativeDataType": "VARCHAR", "type": map[string]any{"type": map[string]any{"com.linkedin.schema.StringType": map[string]any{}}}},
				},
			}},
		},
	})
	if err != nil {
		t.Fatalf("EnsureDatasetEntityWithSchema: marshal: %v", err)
	}

	httpClient := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
		gmsURL+"/openapi/v3/entity/dataset", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("EnsureDatasetEntityWithSchema: build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		t.Fatalf("EnsureDatasetEntityWithSchema: POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		t.Fatalf("EnsureDatasetEntityWithSchema: POST returned HTTP %d for URN %q", resp.StatusCode, entityURN)
	}
	t.Logf("EnsureDatasetEntityWithSchema: created dataset %q (HTTP %d)", entityURN, resp.StatusCode)

	t.Cleanup(func() {
		deleteURL := gmsURL + "/openapi/v3/entity/dataset/" + url.PathEscape(entityURN)
		delReq, delErr := http.NewRequestWithContext(context.Background(), http.MethodDelete, deleteURL, nil)
		if delErr != nil {
			return
		}
		delReq.Header.Set("Authorization", "Bearer "+token)
		if delResp, e := httpClient.Do(delReq); e == nil {
			delResp.Body.Close()
		}
	})
}

// CleanupOrphanedMonitors searches for and deletes any DataHub monitor entities
// whose URN references the given dataset URN. Orphaned monitors (left behind
// when a previous test run's Delete failed to locate the monitor URN via the
// assertion link) block future assertion creation for the same dataset and
// assertion type, because DataHub Cloud enforces a one-active-monitor-per-
// dataset-per-type constraint. Call this before EnsureDatasetEntity in any
// test that creates Cloud-only assertion monitors. This is a no-op on the mock
// target.
func (tg *Target) CleanupOrphanedMonitors(t *testing.T, datasetURN string) {
	t.Helper()
	if tg.Kind == TargetMock {
		return
	}

	gmsURL := strings.TrimRight(os.Getenv("DATAHUB_GMS_URL"), "/")
	token := os.Getenv("DATAHUB_GMS_TOKEN")
	httpClient := &http.Client{Timeout: 15 * time.Second}

	gqlBody, _ := json.Marshal(map[string]any{
		"query": `query {
  searchAcrossEntities(input: {types: [MONITOR], query: "*", count: 50}) {
    searchResults {
      entity { urn }
    }
  }
}`,
		"variables": map[string]any{},
	})

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
		gmsURL+"/api/graphql", bytes.NewReader(gqlBody))
	if err != nil {
		t.Logf("CleanupOrphanedMonitors: build request: %v", err)
		return
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		t.Logf("CleanupOrphanedMonitors: graphql search: %v", err)
		return
	}
	defer resp.Body.Close()

	var result struct {
		Data struct {
			SearchAcrossEntities struct {
				SearchResults []struct {
					Entity struct {
						URN string `json:"urn"`
					} `json:"entity"`
				} `json:"searchResults"`
			} `json:"searchAcrossEntities"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Logf("CleanupOrphanedMonitors: decode response: %v", err)
		return
	}

	deleted := 0
	for _, sr := range result.Data.SearchAcrossEntities.SearchResults {
		urn := sr.Entity.URN
		if !strings.Contains(urn, datasetURN) {
			continue
		}
		deleteURL := gmsURL + "/openapi/v3/entity/monitor/" + url.PathEscape(urn)
		delReq, delErr := http.NewRequestWithContext(context.Background(), http.MethodDelete, deleteURL, nil)
		if delErr != nil {
			t.Logf("CleanupOrphanedMonitors: build delete request for %q: %v", urn, delErr)
			continue
		}
		delReq.Header.Set("Authorization", "Bearer "+token)
		delResp, delErr := httpClient.Do(delReq)
		if delErr != nil {
			t.Logf("CleanupOrphanedMonitors: DELETE monitor %q: %v", urn, delErr)
			continue
		}
		delResp.Body.Close()
		t.Logf("CleanupOrphanedMonitors: deleted monitor %q (HTTP %d)", urn, delResp.StatusCode)
		deleted++
	}
	if deleted > 0 {
		t.Logf("CleanupOrphanedMonitors: deleted %d orphaned monitor(s) for dataset %q", deleted, datasetURN)
	}
}

// SetupTarget selects the acceptance-test backend from the process
// environment and prepares it for use. Call this from each acceptance test
// before resource.Test.
//
// Selection logic:
//
//   - DATAHUB_GMS_URL absent or empty: spin up an in-memory mock server and
//     inject DATAHUB_GMS_URL / DATAHUB_GMS_TOKEN via t.Setenv. The mock is
//     torn down via t.Cleanup when the test ends.
//
//   - DATAHUB_GMS_URL present: treat as a live DataHub instance (local
//     Quickstart or remote tenant). DATAHUB_GMS_TOKEN must also be set.
//     Missing creds are a hard failure (not a skip).
//
// Cloud vs OSS detection (live targets only):
//
//   - DATAHUB_CLOUD=1: force Cloud mode without probing.
//   - DATAHUB_CLOUD=0: force OSS mode without probing.
//   - DATAHUB_CLOUD unset: auto-detect by probing GET /config (or /api/gms/config
//     if behind a frontend proxy) and reading datahub.serverEnv. Falls back to
//     OSS on probe failure, with a warning logged to the test.
func SetupTarget(t *testing.T) *Target {
	t.Helper()
	gmsURL, live := os.LookupEnv("DATAHUB_GMS_URL")
	if !live || strings.TrimSpace(gmsURL) == "" {
		srv := NewServer(t)
		t.Setenv("DATAHUB_GMS_URL", srv.URL)
		t.Setenv("DATAHUB_GMS_TOKEN", "test-token")
		// Mock always simulates Cloud: it supports all Cloud-only operations.
		return &Target{Kind: TargetMock, isCloud: true}
	}
	if strings.TrimSpace(os.Getenv("DATAHUB_GMS_TOKEN")) == "" {
		t.Fatalf("DATAHUB_GMS_TOKEN must be set when DATAHUB_GMS_URL is set")
	}

	gmsURL = strings.TrimSpace(gmsURL)
	token := strings.TrimSpace(os.Getenv("DATAHUB_GMS_TOKEN"))

	// Explicit overrides take priority over auto-detection.
	switch strings.TrimSpace(os.Getenv("DATAHUB_CLOUD")) {
	case "1":
		return &Target{Kind: TargetLive, isCloud: true}
	case "0":
		return &Target{Kind: TargetLive, isCloud: false}
	}

	// Auto-detect via /config probe.
	isCloud, err := detectIsCloud(gmsURL, token)
	if err != nil {
		t.Logf("WARNING: Cloud/OSS auto-detection failed (%v); treating as OSS. Set DATAHUB_CLOUD=1 or DATAHUB_CLOUD=0 to override.", err)
		return &Target{Kind: TargetLive, isCloud: false}
	}
	if isCloud {
		t.Logf("auto-detected DataHub Cloud (serverEnv=cloud)")
	} else {
		t.Logf("auto-detected OSS DataHub (serverEnv=core)")
	}
	return &Target{Kind: TargetLive, isCloud: isCloud}
}

// detectIsCloud probes the GMS /config endpoint to determine whether the
// target is DataHub Cloud (serverEnv="cloud") or OSS (serverEnv="core").
//
// Self-describing probe strategy (from Bart Bot):
//  1. GET /config: if the response contains "datahub.serverEnv" -> direct GMS
//     hit; read serverEnv and return.
//  2. If the response contains "config" (frontend shape) -> we hit the Play
//     frontend; retry GET /api/gms/config with the bearer token, then read
//     datahub.serverEnv from that response.
func detectIsCloud(gmsURL, token string) (bool, error) {
	gmsURL = strings.TrimRight(gmsURL, "/")

	body, err := getConfigBody(gmsURL+"/config", token)
	if err != nil {
		return false, fmt.Errorf("GET /config: %w", err)
	}

	// Direct GMS hit: has top-level "datahub" key.
	if dh, ok := body["datahub"].(map[string]any); ok {
		env, _ := dh["serverEnv"].(string)
		return env == "cloud", nil
	}

	// Frontend hit: has top-level "config" key. Try proxied GMS path.
	if _, ok := body["config"]; ok {
		body2, err := getConfigBody(gmsURL+"/api/gms/config", token)
		if err != nil {
			return false, fmt.Errorf("GET /api/gms/config: %w", err)
		}
		if dh, ok := body2["datahub"].(map[string]any); ok {
			env, _ := dh["serverEnv"].(string)
			return env == "cloud", nil
		}
		return false, fmt.Errorf("GET /api/gms/config: response missing datahub.serverEnv")
	}

	return false, fmt.Errorf("GET /config: response missing both 'datahub' and 'config' keys")
}

// getConfigBody performs a GET request and decodes the JSON body into a map.
func getConfigBody(url, token string) (map[string]any, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("do: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return body, nil
}
