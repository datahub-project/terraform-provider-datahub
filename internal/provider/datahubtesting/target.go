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
	Kind TargetKind
}

// IsLive reports whether the target is a real DataHub instance rather than
// the in-memory mock.
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
		return &Target{Kind: TargetMock}
	}
	if strings.TrimSpace(os.Getenv("DATAHUB_GMS_TOKEN")) == "" {
		t.Fatalf("DATAHUB_GMS_TOKEN must be set when DATAHUB_GMS_URL is set")
	}
	return &Target{Kind: TargetLive}
}
