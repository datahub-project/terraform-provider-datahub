// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahubtesting_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/datahubtesting"
)

// gmsConfigHandler returns an HTTP handler that responds with the standard
// GMS /config shape, using the given serverEnv value ("cloud" or "core").
func gmsConfigHandler(serverEnv string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"datahub": map[string]any{
				"serverEnv":  serverEnv,
				"serverType": "prod",
			},
		})
	})
}

// frontendConfigHandler returns an HTTP handler that simulates the Play
// frontend's /config endpoint (frontend shape, no datahub.serverEnv) and a
// proxied /api/gms/config endpoint that returns the GMS shape.
func frontendConfigHandler(serverEnv string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/gms/config" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"datahub": map[string]any{
					"serverEnv":  serverEnv,
					"serverType": "prod",
				},
			})
			return
		}
		// /config: frontend shape
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "ok",
			"config": map[string]any{
				"application": "datahub-frontend",
				"appVersion":  "v0.3.17.5-acryl",
			},
		})
	})
}

// TestSetupTarget_mock verifies that an absent (or empty) DATAHUB_GMS_URL
// selects mock mode: Kind=TargetMock, IsLive()=false, Name() unchanged, IsCloud()=true.
func TestSetupTarget_mock(t *testing.T) {
	t.Setenv("DATAHUB_GMS_URL", "")
	tg := datahubtesting.SetupTarget(t)
	if tg.Kind != datahubtesting.TargetMock {
		t.Fatalf("expected TargetMock, got %v", tg.Kind)
	}
	if tg.IsLive() {
		t.Fatal("IsLive() should be false for TargetMock")
	}
	if !tg.IsCloud() {
		t.Fatal("IsCloud() should be true for TargetMock (mock simulates Cloud)")
	}
	if got := tg.Name("base"); got != "base" {
		t.Fatalf("Name(%q) = %q, want %q", "base", got, "base")
	}
}

// TestSetupTarget_live_explicit_cloud verifies that DATAHUB_CLOUD=1 forces
// Cloud mode without making a probe request.
func TestSetupTarget_live_explicit_cloud(t *testing.T) {
	t.Setenv("DATAHUB_GMS_URL", "http://localhost:18080")
	t.Setenv("DATAHUB_GMS_TOKEN", "test-token")
	t.Setenv("DATAHUB_CLOUD", "1")
	tg := datahubtesting.SetupTarget(t)
	if tg.Kind != datahubtesting.TargetLive {
		t.Fatalf("expected TargetLive, got %v", tg.Kind)
	}
	if !tg.IsCloud() {
		t.Fatal("IsCloud() should be true when DATAHUB_CLOUD=1")
	}
	name := tg.Name("pfx")
	if !strings.HasPrefix(name, "pfx-") || len(name) <= len("pfx-") {
		t.Fatalf("Name(%q) = %q, want prefix %q followed by a random suffix", "pfx", name, "pfx-")
	}
}

// TestSetupTarget_live_explicit_oss verifies that DATAHUB_CLOUD=0 forces
// OSS mode without making a probe request.
func TestSetupTarget_live_explicit_oss(t *testing.T) {
	t.Setenv("DATAHUB_GMS_URL", "http://localhost:18080")
	t.Setenv("DATAHUB_GMS_TOKEN", "test-token")
	t.Setenv("DATAHUB_CLOUD", "0")
	tg := datahubtesting.SetupTarget(t)
	if tg.Kind != datahubtesting.TargetLive {
		t.Fatalf("expected TargetLive, got %v", tg.Kind)
	}
	if tg.IsCloud() {
		t.Fatal("IsCloud() should be false when DATAHUB_CLOUD=0")
	}
}

// TestSetupTarget_live_autodetect_cloud verifies that when DATAHUB_CLOUD is
// unset, SetupTarget probes /config and returns IsCloud()=true when the GMS
// reports serverEnv=cloud.
func TestSetupTarget_live_autodetect_cloud(t *testing.T) {
	srv := httptest.NewServer(gmsConfigHandler("cloud"))
	defer srv.Close()
	t.Setenv("DATAHUB_GMS_URL", srv.URL)
	t.Setenv("DATAHUB_GMS_TOKEN", "test-token")
	t.Setenv("DATAHUB_CLOUD", "")
	tg := datahubtesting.SetupTarget(t)
	if tg.Kind != datahubtesting.TargetLive {
		t.Fatalf("expected TargetLive, got %v", tg.Kind)
	}
	if !tg.IsCloud() {
		t.Fatal("IsCloud() should be true when probe returns serverEnv=cloud")
	}
}

// TestSetupTarget_live_autodetect_oss verifies that when DATAHUB_CLOUD is
// unset, SetupTarget probes /config and returns IsCloud()=false when the GMS
// reports serverEnv=core.
func TestSetupTarget_live_autodetect_oss(t *testing.T) {
	srv := httptest.NewServer(gmsConfigHandler("core"))
	defer srv.Close()
	t.Setenv("DATAHUB_GMS_URL", srv.URL)
	t.Setenv("DATAHUB_GMS_TOKEN", "test-token")
	t.Setenv("DATAHUB_CLOUD", "")
	tg := datahubtesting.SetupTarget(t)
	if tg.Kind != datahubtesting.TargetLive {
		t.Fatalf("expected TargetLive, got %v", tg.Kind)
	}
	if tg.IsCloud() {
		t.Fatal("IsCloud() should be false when probe returns serverEnv=core")
	}
}

// TestSetupTarget_live_autodetect_frontend_proxy verifies that when /config
// returns the frontend shape (no datahub.serverEnv), SetupTarget retries
// /api/gms/config and reads serverEnv from that response.
func TestSetupTarget_live_autodetect_frontend_proxy(t *testing.T) {
	srv := httptest.NewServer(frontendConfigHandler("cloud"))
	defer srv.Close()
	t.Setenv("DATAHUB_GMS_URL", srv.URL)
	t.Setenv("DATAHUB_GMS_TOKEN", "test-token")
	t.Setenv("DATAHUB_CLOUD", "")
	tg := datahubtesting.SetupTarget(t)
	if tg.Kind != datahubtesting.TargetLive {
		t.Fatalf("expected TargetLive, got %v", tg.Kind)
	}
	if !tg.IsCloud() {
		t.Fatal("IsCloud() should be true when proxied /api/gms/config returns serverEnv=cloud")
	}
}

// TestSetupTarget_live_autodetect_probe_failure verifies that a probe failure
// (unreachable server) falls back to OSS mode with a warning rather than
// fatally failing the test.
func TestSetupTarget_live_autodetect_probe_failure(t *testing.T) {
	// Use a server that is immediately closed so the probe gets a connection error.
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	srv.Close()
	t.Setenv("DATAHUB_GMS_URL", srv.URL)
	t.Setenv("DATAHUB_GMS_TOKEN", "test-token")
	t.Setenv("DATAHUB_CLOUD", "")
	tg := datahubtesting.SetupTarget(t)
	if tg.Kind != datahubtesting.TargetLive {
		t.Fatalf("expected TargetLive, got %v", tg.Kind)
	}
	if tg.IsCloud() {
		t.Fatal("IsCloud() should be false when probe fails (fall back to OSS)")
	}
}
