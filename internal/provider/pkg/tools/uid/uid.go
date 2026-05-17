// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

// Package uid provides small helpers to derive stable IDs used throughout the provider.
package uid

import (
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"strings"
)

// SHA256Hex returns the hex-encoded sha256 digest of b.
func SHA256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// DeriveID produces a stable ID of the form "<sanitized-prefix>-<hash>".
//
// The hash suffix is the lower-case, no-padding base32 encoding of SHA-256(hashInput),
// truncated to 12 characters.
//
// If maxPrefixLen > 0, the sanitized prefix is truncated to that length before joining.
// This is used to keep IDs readable (e.g., when deriving IDs from long names).
func DeriveID(prefixInput string, hashInput []byte, maxPrefixLen int) string {
	prefix := sanitizeIDPart(prefixInput)
	if prefix == "" {
		prefix = "source"
	}

	if maxPrefixLen > 0 && len(prefix) > maxPrefixLen {
		prefix = prefix[:maxPrefixLen]
		prefix = strings.Trim(prefix, "-")
		if prefix == "" {
			prefix = "source"
		}
	}

	sum := sha256.Sum256(hashInput)
	enc := base32.StdEncoding.WithPadding(base32.NoPadding)
	// 12 base32 chars ~= 60 bits; plenty for uniqueness while keeping IDs readable.
	hashSuffix := strings.ToLower(enc.EncodeToString(sum[:]))
	if len(hashSuffix) > 12 {
		hashSuffix = hashSuffix[:12]
	}

	return prefix + "-" + hashSuffix
}

func sanitizeIDPart(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return ""
	}

	var b strings.Builder
	b.Grow(len(s))
	prevDash := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		isLower := c >= 'a' && c <= 'z'
		isDigit := c >= '0' && c <= '9'
		if isLower || isDigit {
			b.WriteByte(c)
			prevDash = false
			continue
		}
		if !prevDash {
			b.WriteByte('-')
			prevDash = true
		}
	}

	out := strings.Trim(b.String(), "-")
	return out
}
