// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahubtesting

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
)

// NeutralizeDevOverride points TF_CLI_CONFIG_FILE at an empty Terraform CLI
// configuration for the duration of the test, so that a dev_overrides block in the
// developer's own CLI config cannot influence how Terraform resolves providers.
//
// Every test step that uses resource.ExternalProvider must call this first.
// dev_overrides makes Terraform serve datahub-project/datahub from a local build
// and ignore required_providers entirely, so the step silently runs against the
// build under test instead of the published version it named -- and the comparison
// the step exists to make evaluates against the wrong provider, passing whatever
// happens. `make dev-override` sets TF_CLI_CONFIG_FILE via .mise.env, which makes
// that vacuous mode the default locally: an upgrade test pointed at a nonexistent
// provider version passed until the override was defeated.
//
// Steps using ProtoV6ProviderFactories do not need this. The framework serves the
// provider in-process via TF_REATTACH_PROVIDERS, which takes precedence over
// dev_overrides -- verified by breaking the provider source without rebuilding the
// binary the override points at, and observing the test still fail.
func NeutralizeDevOverride(t *testing.T) {
	t.Helper()
	empty := filepath.Join(t.TempDir(), "empty.tfrc")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatalf("NeutralizeDevOverride: writing empty CLI config: %v", err)
	}
	t.Setenv("TF_CLI_CONFIG_FILE", empty)
}

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

// AssertEntityAbsent fails the test if the entity exists, reading through the
// strongly-consistent OpenAPI v3 entity endpoint so there is no index-lag
// window. entityPath is the lowercase path segment (e.g. "corpgroup").
//
// Its purpose is to catch a resource that was written to the server and then
// abandoned. When a Create writes an entity and only afterwards hits an error,
// it returns without setting state: Terraform holds no state entry, destroy has
// nothing to remove, and the entity is orphaned where neither plan nor state
// can see it. Asserting the error alone cannot detect that; only asking the
// server can.
//
// Works against the mock as well as live targets, since both serve the same
// entity endpoint.
func (tg *Target) AssertEntityAbsent(t *testing.T, entityPath, entityURN string) {
	t.Helper()

	gmsURL := strings.TrimRight(os.Getenv("DATAHUB_GMS_URL"), "/")
	token := os.Getenv("DATAHUB_GMS_TOKEN")

	httpClient := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet,
		fmt.Sprintf("%s/openapi/v3/entity/%s/%s", gmsURL, entityPath, url.PathEscape(entityURN)), nil)
	if err != nil {
		t.Fatalf("AssertEntityAbsent: build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := httpClient.Do(req)
	if err != nil {
		t.Fatalf("AssertEntityAbsent: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return
	}
	if resp.StatusCode >= http.StatusBadRequest {
		t.Fatalf("AssertEntityAbsent: unexpected HTTP %d checking %s", resp.StatusCode, entityURN)
	}
	t.Errorf("entity %s still exists on the server (HTTP %d) after the apply failed; "+
		"it was written and then abandoned, so Terraform holds no state for it and "+
		"destroy cannot remove it", entityURN, resp.StatusCode)
}

// EnsureDatasetEntity creates a minimal dataset entity on a live target so that
// mutations which require the referenced dataset to exist (assertion monitor
// upserts, upsertDataContract) succeed. The entity is guaranteed to exist when
// this returns; see ensureDataset. A t.Cleanup is registered to hard-delete it
// after the test ends. On the in-memory mock this is a no-op: the mock does not
// validate entity existence before accepting GraphQL mutations.
func (tg *Target) EnsureDatasetEntity(t *testing.T, entityURN string) {
	t.Helper()
	if tg.Kind == TargetMock {
		return
	}
	ensureDataset(t, "EnsureDatasetEntity", entityURN, map[string]any{
		"datasetProperties": map[string]any{
			"value": map[string]any{"name": "TF Provider Test Dataset"},
		},
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
	ensureDataset(t, "EnsureDatasetEntityWithSchema", entityURN, map[string]any{
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
	})
}

// ensureDataset is the single write path behind every Ensure*Dataset* fixture.
// It POSTs the aspects for entityURN, requires proof that the entity now
// exists, and registers a hard delete for cleanup. aspects maps aspect name to
// its {"value": ...} envelope.
//
// The write is sent with async=false, and nothing short of HTTP 200 is
// accepted. Without that flag the v3 collection POST hands the proposal to the
// Kafka ingest topic and answers 202 before anything is persisted, so the
// dataset exists only once a consumer gets to it. Every caller of these
// fixtures then makes a call that checks existence -- upsertDataContract
// rejects a missing dataset with "Provided entity ... does not exist!", and
// the assertion monitor upserts likewise -- so an async fixture races the very
// call it exists to satisfy. It usually wins, which is why the failure was
// intermittent and appeared only under consumer lag on a busy instance.
//
// After the write it reads the entity back through the strongly-consistent
// entity endpoint and fails if it is absent. That is the condition the caller
// depends on, so the fixture asserts it directly instead of inferring it from
// a status code.
//
// It takes testing.TB rather than *testing.T so the failure paths can be
// unit-tested without failing the test that exercises them.
func ensureDataset(t testing.TB, caller, entityURN string, aspects map[string]any) {
	t.Helper()

	gmsURL := strings.TrimRight(os.Getenv("DATAHUB_GMS_URL"), "/")
	token := os.Getenv("DATAHUB_GMS_TOKEN")
	httpClient := &http.Client{Timeout: 15 * time.Second}
	entityURL := gmsURL + "/openapi/v3/entity/dataset/" + url.PathEscape(entityURN)

	entity := map[string]any{"urn": entityURN}
	for name, aspect := range aspects {
		entity[name] = aspect
	}
	body, err := json.Marshal([]map[string]any{entity})
	if err != nil {
		t.Fatalf("%s: marshal: %v", caller, err)
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
		gmsURL+"/openapi/v3/entity/dataset?async=false", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("%s: build request: %v", caller, err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		t.Fatalf("%s: POST /openapi/v3/entity/dataset: %v", caller, err)
	}
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	_ = resp.Body.Close()

	// Any 2xx means the server accepted the write, so register the delete
	// before judging the status: a 202 still materialises the entity later,
	// and failing without cleanup would leave it behind on a shared instance.
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		t.Cleanup(func() { deleteFixtureDataset(t, caller, httpClient, entityURL, token, entityURN) })
	}
	switch {
	case resp.StatusCode == http.StatusAccepted:
		t.Fatalf("%s: POST for %q returned HTTP 202: the server queued the write asynchronously "+
			"despite async=false, so the dataset may not exist yet and the dependent call would race it",
			caller, entityURN)
	case resp.StatusCode != http.StatusOK:
		t.Fatalf("%s: POST for %q returned HTTP %d, want 200: %s", caller, entityURN, resp.StatusCode, respBody)
	}

	getReq, err := http.NewRequestWithContext(context.Background(), http.MethodGet, entityURL, nil)
	if err != nil {
		t.Fatalf("%s: build read-back request: %v", caller, err)
	}
	getReq.Header.Set("Authorization", "Bearer "+token)
	getResp, err := httpClient.Do(getReq)
	if err != nil {
		t.Fatalf("%s: read-back GET %q: %v", caller, entityURN, err)
	}
	_ = getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("%s: POST for %q returned 200 but the read-back returned HTTP %d: "+
			"the write did not land", caller, entityURN, getResp.StatusCode)
	}

	t.Logf("%s: created dataset %q (HTTP %d, read back HTTP %d)",
		caller, entityURN, resp.StatusCode, getResp.StatusCode)
}

// deleteFixtureDataset hard-deletes a fixture dataset. A failure is reported as
// a test error rather than a log line: the fixture URNs are fixed strings on a
// shared instance, so a dataset left behind is debris someone has to sweep.
// 404 is success -- the test itself may already have removed it.
func deleteFixtureDataset(t testing.TB, caller string, httpClient *http.Client, entityURL, token, entityURN string) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodDelete, entityURL, nil)
	if err != nil {
		t.Errorf("%s cleanup: build delete request: %v; the fixture dataset %q may be left behind", caller, err, entityURN)
		return
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := httpClient.Do(req)
	if err != nil {
		t.Errorf("%s cleanup: DELETE %q: %v; the fixture dataset may be left behind", caller, entityURN, err)
		return
	}
	_ = resp.Body.Close()
	if resp.StatusCode >= 400 && resp.StatusCode != http.StatusNotFound {
		t.Errorf("%s cleanup: DELETE %q returned HTTP %d; the fixture dataset is left behind",
			caller, entityURN, resp.StatusCode)
		return
	}
	t.Logf("%s cleanup: deleted dataset %q (HTTP %d)", caller, entityURN, resp.StatusCode)
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
