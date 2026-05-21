// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package uid

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestSHA256Hex(t *testing.T) {
	// SHA-256 of the empty byte slice is a well-known constant.
	const emptyHex = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if got := SHA256Hex([]byte{}); got != emptyHex {
		t.Errorf("SHA256Hex(empty) = %q, want %q", got, emptyHex)
	}
	if SHA256Hex(nil) != emptyHex {
		t.Errorf("SHA256Hex(nil) should equal SHA256Hex(empty)")
	}

	// Deterministic.
	if SHA256Hex([]byte("hello")) != SHA256Hex([]byte("hello")) {
		t.Error("SHA256Hex is not deterministic")
	}

	// Different inputs produce different digests.
	if SHA256Hex([]byte("a")) == SHA256Hex([]byte("b")) {
		t.Error("SHA256Hex collision on distinct inputs")
	}

	// Result is 64 lowercase hex characters (256 bits / 4 bits per char).
	h := SHA256Hex([]byte("x"))
	if len(h) != 64 {
		t.Errorf("SHA256Hex length = %d, want 64", len(h))
	}
	for i, c := range h {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("SHA256Hex result[%d] = %q is not lowercase hex", i, c)
			break
		}
	}
}

func TestSanitizeIDPart(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"", ""},
		{"hello", "hello"},
		{"Hello World", "hello-world"},
		{"BigQuery Source", "bigquery-source"},
		{"foo_bar", "foo-bar"},
		{"foo!@#bar", "foo-bar"},
		{"--foo--", "foo"},
		{"  spaces  ", "spaces"},
		{"123abc", "123abc"},
		{"ALL-CAPS", "all-caps"},
		{"a  b  c", "a-b-c"},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got := sanitizeIDPart(tc.input)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("sanitizeIDPart(%q) mismatch (-want +got):\n%s", tc.input, diff)
			}
		})
	}
}

func TestDeriveID(t *testing.T) {
	t.Run("basic_stable", func(t *testing.T) {
		id := DeriveID("my-source", []byte("my-source"), 0)
		// Must have the sanitized prefix and a 12-char hash suffix.
		if !strings.HasPrefix(id, "my-source-") {
			t.Errorf("DeriveID = %q, want prefix my-source-", id)
		}
		suffix := strings.TrimPrefix(id, "my-source-")
		if len(suffix) != 12 {
			t.Errorf("hash suffix length = %d, want 12", len(suffix))
		}
		// Deterministic.
		if DeriveID("my-source", []byte("my-source"), 0) != id {
			t.Error("DeriveID is not deterministic")
		}
	})

	t.Run("different_hash_inputs_differ", func(t *testing.T) {
		a := DeriveID("src", []byte("a"), 0)
		b := DeriveID("src", []byte("b"), 0)
		if a == b {
			t.Errorf("DeriveID collision for different hash inputs: %q == %q", a, b)
		}
	})

	t.Run("empty_prefix_falls_back_to_source", func(t *testing.T) {
		id := DeriveID("", []byte("x"), 0)
		if !strings.HasPrefix(id, "source-") {
			t.Errorf("DeriveID with empty prefix = %q, want source- prefix", id)
		}
	})

	t.Run("whitespace_only_prefix_falls_back", func(t *testing.T) {
		id := DeriveID("   ", []byte("x"), 0)
		if !strings.HasPrefix(id, "source-") {
			t.Errorf("DeriveID with whitespace prefix = %q, want source- prefix", id)
		}
	})

	t.Run("max_prefix_len_truncates", func(t *testing.T) {
		longPrefix := "a-very-long-source-name-that-exceeds-limit"
		id := DeriveID(longPrefix, []byte("x"), 10)
		parts := strings.SplitN(id, "-", -1)
		// The prefix portion (everything before the final 12-char hash) must be short.
		// id = "<truncated-prefix>-<12hash>", last segment is the hash.
		hashPart := parts[len(parts)-1]
		prefixPart := strings.TrimSuffix(id, "-"+hashPart)
		if len(prefixPart) > 10 {
			t.Errorf("prefix after truncation = %q (len %d), want <= 10 chars", prefixPart, len(prefixPart))
		}
	})

	t.Run("suffix_is_valid_base32", func(t *testing.T) {
		id := DeriveID("src", []byte("test"), 0)
		suffix := strings.TrimPrefix(id, "src-")
		for i, c := range suffix {
			if !((c >= 'a' && c <= 'z') || (c >= '2' && c <= '7')) {
				t.Errorf("hash suffix[%d] = %q is not lowercase base32", i, c)
				break
			}
		}
	})
}
