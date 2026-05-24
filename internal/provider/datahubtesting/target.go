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
// and for live targets when DATAHUB_CLOUD=1 is set in the environment.
func (tg *Target) IsCloud() bool {
	return tg.isCloud
}

// RequireCloud skips the calling test if the target is not Cloud-capable.
// Use this on every test that exercises Cloud-only resources such as
// datahub_remote_executor_pool. The test always runs against the mock
// (which simulates Cloud). Against live targets it runs only when
// DATAHUB_CLOUD=1 is set; otherwise it is skipped.
func (tg *Target) RequireCloud(t *testing.T) {
	t.Helper()
	if !tg.isCloud {
		t.Skip("skipping Cloud-only test: set DATAHUB_CLOUD=1 to include Cloud-only tests against a live Cloud instance")
	}
}

// RequireOSS skips the calling test unless the target is a live instance
// with DATAHUB_CLOUD unset. Use this for tests that specifically verify the
// provider's graceful-error behavior when Cloud-only features are absent
// (e.g. datahub_remote_executor_pool reporting "DataHub Cloud Required").
// Skips on mock (which simulates Cloud) and on any live target when
// DATAHUB_CLOUD=1 is set.
func (tg *Target) RequireOSS(t *testing.T) {
	t.Helper()
	if tg.Kind == TargetMock {
		t.Skip("skipping OSS-error-path test: mock target always supports Cloud features; use testacc-local or testacc-quickstart (OSS DataHub) instead")
	}
	if tg.isCloud {
		t.Skip("skipping OSS-error-path test: DATAHUB_CLOUD=1 is set; this test requires an OSS DataHub instance (testacc-local or testacc-quickstart without DATAHUB_CLOUD=1)")
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
//     Missing creds are a hard failure (not a skip). The Makefile targets
//     testacc-local, testacc-quickstart, and testacc-remote each enforce
//     their own URL constraints before invoking go test; this function trusts
//     the environment they provide.
func SetupTarget(t *testing.T) *Target {
	t.Helper()
	// Capture the initial environment before any t.Setenv calls.
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
	isCloud := strings.TrimSpace(os.Getenv("DATAHUB_CLOUD")) == "1"
	return &Target{Kind: TargetLive, isCloud: isCloud}
}
