// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahubtesting

import (
	"os"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
)

// TargetKind identifies which DataHub backend an acceptance test is running
// against. The active target is chosen at test setup time via the
// DATAHUB_TEST_TARGET environment variable.
type TargetKind int

const (
	// TargetMock points the provider at an in-memory mock server spun up
	// per-test. This is the default and the only target used in CI.
	TargetMock TargetKind = iota

	// TargetLocalLive points the provider at a real DataHub started locally
	// via `datahub docker quickstart`. Throw-away environment, low stakes.
	TargetLocalLive

	// TargetCloudLive points the provider at a real DataHub cloud instance.
	// Higher stakes: requires explicit DATAHUB_TEST_ALLOW_CLOUD=1
	// acknowledgement. Intended only for tenants set up specifically for
	// smoke-testing the provider.
	TargetCloudLive
)

// Target is the active acceptance-test backend chosen at SetupTarget time.
// It encapsulates whether the provider is talking to an in-memory mock or
// a real DataHub instance, and exposes helpers (resource naming, kind
// predicates) that let test functions adapt without branching on env vars
// themselves.
type Target struct {
	Kind TargetKind
}

// IsLive reports whether the target is a real DataHub instance (local or
// cloud) rather than the in-memory mock.
func (t *Target) IsLive() bool {
	return t.Kind != TargetMock
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
func (t *Target) Name(base string) string {
	if t.Kind == TargetMock {
		return base
	}
	return base + "-" + strings.ToLower(acctest.RandString(8))
}

// SetupTarget reads DATAHUB_TEST_TARGET and configures the provider for the
// requested backend. Call this from each acceptance test before resource.Test.
//
// Behavior by env-var value:
//
//   - unset / "mock":  spin up an in-memory mock server and point
//     DATAHUB_GMS_URL / DATAHUB_GMS_TOKEN at it via t.Setenv. The mock is
//     torn down via t.Cleanup when the test ends.
//   - "local":         expect DATAHUB_GMS_URL and DATAHUB_GMS_TOKEN to point
//     at a Quickstart instance running locally. Env vars must be set in the
//     shell; this function does not call t.Setenv. Missing creds are a hard
//     failure (not a skip).
//   - "cloud":         same as local, plus DATAHUB_TEST_ALLOW_CLOUD must be
//     set to "1" to confirm intentional use against a real cloud tenant.
//
// Any unrecognized value is a hard failure.
func SetupTarget(t *testing.T) *Target {
	t.Helper()
	raw := strings.ToLower(strings.TrimSpace(os.Getenv("DATAHUB_TEST_TARGET")))
	switch raw {
	case "", "mock":
		srv := NewServer(t)
		t.Setenv("DATAHUB_GMS_URL", srv.URL)
		t.Setenv("DATAHUB_GMS_TOKEN", "test-token")
		return &Target{Kind: TargetMock}
	case "local", "local-live":
		requireLiveEnv(t)
		return &Target{Kind: TargetLocalLive}
	case "cloud", "cloud-live":
		requireLiveEnv(t)
		if strings.TrimSpace(os.Getenv("DATAHUB_TEST_ALLOW_CLOUD")) == "" {
			t.Fatalf("DATAHUB_TEST_TARGET=cloud requires DATAHUB_TEST_ALLOW_CLOUD=1 to acknowledge running against a real cloud instance")
		}
		return &Target{Kind: TargetCloudLive}
	default:
		t.Fatalf("unrecognized DATAHUB_TEST_TARGET=%q (want one of: mock, local, cloud)", raw)
		return nil
	}
}

func requireLiveEnv(t *testing.T) {
	t.Helper()
	if strings.TrimSpace(os.Getenv("DATAHUB_GMS_URL")) == "" {
		t.Fatalf("DATAHUB_GMS_URL must be set for live targets")
	}
	if strings.TrimSpace(os.Getenv("DATAHUB_GMS_TOKEN")) == "" {
		t.Fatalf("DATAHUB_GMS_TOKEN must be set for live targets")
	}
}
