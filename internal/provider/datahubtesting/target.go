// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahubtesting

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
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
