// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The provider resolves credentials from the configuration, then the
// environment, and nowhere else. Earlier versions had a third tier reading
// gms.server / gms.token from the DataHub CLI's ~/.datahubenv, which meant a
// configuration supplying neither still authenticated - against whichever
// instance the operator's CLI had last been pointed at.
//
// These tests pin the removal. Without them the fallback could be reintroduced
// as a convenience by someone who never saw why it went, which is exactly how it
// arrived in the first place: present in the initial commit, with no recorded
// rationale.

// TestNoDatahubEnvReferenceInCredentialResolution asserts that no production
// source file reads the CLI configuration file. A behavioural test cannot
// express this well, because proving a file is *not* read means controlling
// HOME for the whole process; asserting the code contains no such read is both
// stronger and cheaper.
func TestNoDatahubEnvReferenceInCredentialResolution(t *testing.T) {
	t.Parallel()

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("reading package directory: %v", err)
	}

	var offenders []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		content, err := os.ReadFile(filepath.Clean(name))
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}
		// The removal hint deliberately names the file in prose, so match only
		// on the Go string literal form a real read would need.
		if strings.Contains(string(content), `".datahubenv"`) {
			offenders = append(offenders, name)
		}
	}

	if len(offenders) > 0 {
		t.Errorf("production code references \".datahubenv\" in %v; the CLI-config credential "+
			"fallback was removed because it made the target instance depend on the "+
			"machine rather than the configuration. See docs/design/provider-credential-resolution.md.",
			offenders)
	}
}

// TestRemovedCLIConfigFallbackHintIsActionable guards the upgrade message. It is
// the only place an affected user learns what happened, so it has to name the
// file, say the fallback was removed, and give the replacement.
func TestRemovedCLIConfigFallbackHintIsActionable(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"~/.datahubenv",    // what was being read
		"has been removed", // that this is deliberate, not a bug
		"CHANGELOG",        // where to find which release, since the message must not hardcode it
		"DATAHUB_GMS_URL",  // the replacement
		"DATAHUB_GMS_TOKEN",
		"production", // why it mattered
	} {
		if !strings.Contains(removedCLIConfigFallbackHint, want) {
			t.Errorf("upgrade hint is missing %q; an affected user has no other source for this", want)
		}
	}
}
