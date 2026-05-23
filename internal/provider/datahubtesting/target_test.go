// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahubtesting_test

import (
	"strings"
	"testing"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/datahubtesting"
)

// TestSetupTarget_mock verifies that an absent (or empty) DATAHUB_GMS_URL
// selects mock mode: Kind=TargetMock, IsLive()=false, Name() unchanged.
func TestSetupTarget_mock(t *testing.T) {
	t.Setenv("DATAHUB_GMS_URL", "") // empty treated same as absent by SetupTarget
	tg := datahubtesting.SetupTarget(t)
	if tg.Kind != datahubtesting.TargetMock {
		t.Fatalf("expected TargetMock, got %v", tg.Kind)
	}
	if tg.IsLive() {
		t.Fatal("IsLive() should be false for TargetMock")
	}
	if got := tg.Name("base"); got != "base" {
		t.Fatalf("Name(%q) = %q, want %q", "base", got, "base")
	}
}

// TestSetupTarget_live verifies that a present DATAHUB_GMS_URL (with token)
// selects live mode: Kind=TargetLive, IsLive()=true, Name() has a random suffix.
func TestSetupTarget_live(t *testing.T) {
	t.Setenv("DATAHUB_GMS_URL", "http://localhost:18080")
	t.Setenv("DATAHUB_GMS_TOKEN", "test-token")
	tg := datahubtesting.SetupTarget(t)
	if tg.Kind != datahubtesting.TargetLive {
		t.Fatalf("expected TargetLive, got %v", tg.Kind)
	}
	if !tg.IsLive() {
		t.Fatal("IsLive() should be true for TargetLive")
	}
	name := tg.Name("pfx")
	if !strings.HasPrefix(name, "pfx-") || len(name) <= len("pfx-") {
		t.Fatalf("Name(%q) = %q, want prefix %q followed by a random suffix", "pfx", name, "pfx-")
	}
}
