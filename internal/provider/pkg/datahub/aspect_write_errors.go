// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahub

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ErrVersionConflict is matched by errors.Is for any VersionConflictError.
var ErrVersionConflict = errors.New("aspect changed during write")

// VersionConflictError is DataHub's HTTP 412 answer to a conditional aspect
// write: the aspect's stored version no longer matched the version the
// provider read before building the write, so another producer wrote it in
// between. Nothing in the request was written. Attempts counts the writes
// tried, including the re-reads and retries writeAspects made before giving
// up; Elapsed is the time they took.
type VersionConflictError struct {
	URN      string
	Aspect   string
	Expected string
	Actual   string
	Attempts int
	Elapsed  time.Duration
	Message  string
}

func (e *VersionConflictError) Error() string {
	aspect := e.Aspect
	if aspect == "" {
		aspect = "an aspect"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "DataHub refused to write %s on %s because the aspect changed while Terraform was writing it", aspect, e.URN)
	if e.Expected != "" && e.Actual != "" {
		fmt.Fprintf(&b, ": Terraform read version %s but DataHub holds version %s", e.Expected, e.Actual)
	}
	if e.Attempts > 1 {
		fmt.Fprintf(&b, ". Terraform re-read and retried %d times over %s and the aspect kept changing", e.Attempts-1, e.Elapsed.Round(time.Millisecond))
	}
	b.WriteString(". Nothing was written: DataHub rolls back the whole request on a version mismatch, so it is safe to run terraform apply again. " +
		"If this keeps happening, something else is rewriting the aspect continuously (an ingestion run, a sync job or another Terraform run); pause it and apply again")
	if e.Message != "" {
		fmt.Fprintf(&b, ". DataHub said: %s", e.Message)
	}
	return b.String()
}

// Is lets errors.Is(err, ErrVersionConflict) match.
func (e *VersionConflictError) Is(target error) bool { return target == ErrVersionConflict }

// EntityExistsError is what a create-only write returns when the entity it
// was told must not exist already does: found by the read before the write,
// or by the server's 412 when it appeared in between. SoftDeleted is set
// when the read saw it soft-deleted, which is the usual reason an id is
// taken by something the UI no longer shows.
type EntityExistsError struct {
	URN         string
	What        string
	SoftDeleted bool
	Message     string
}

func (e *EntityExistsError) Error() string {
	what := e.What
	if what == "" {
		what = "an entity"
	}
	var s string
	if e.SoftDeleted {
		s = fmt.Sprintf("%s already exists at %s, soft-deleted; restore it in DataHub and import it with terraform import, or choose another id", what, e.URN)
	} else {
		s = fmt.Sprintf("%s already exists at %s; import it with terraform import, or choose another id", what, e.URN)
	}
	if e.Message != "" {
		s += ". DataHub said: " + e.Message
	}
	return s
}

// RetryAfterError is a 429 or 503 from DataHub. After is the server's
// Retry-After, zero when it sent none.
type RetryAfterError struct {
	Status  int
	After   time.Duration
	Waits   int
	Message string
}

func (e *RetryAfterError) Error() string {
	what := "DataHub is temporarily unavailable"
	if e.Status == http.StatusTooManyRequests {
		what = "DataHub rate-limited the write"
	}
	s := fmt.Sprintf("%s (HTTP %d)", what, e.Status)
	if e.After > 0 {
		s += fmt.Sprintf(" and asked for %s between retries", e.After.Round(time.Second))
	}
	if e.Waits > 0 {
		s += fmt.Sprintf("; the provider waited %d times and gave up", e.Waits)
	}
	s += ". Re-run terraform apply, or lower -parallelism"
	if e.Message != "" {
		s += ". DataHub said: " + e.Message
	}
	return s
}

var (
	// The server's 412 message ends "... msg=Expected version 3, actual
	// version 4)". Matched loosely: a different build may quote or reorder.
	conflictVersionsRe = regexp.MustCompile(`(?i)expected version\s*:?\s*"?(-?\d+)"?.{0,40}?actual version\s*:?\s*"?(-?\d+)"?`)
	conflictAspectRe   = regexp.MustCompile(`aspectName=([A-Za-z0-9_]+)`)
)

// parseVersionConflict extracts what it can from a 412 body. Every field may
// be empty; the caller already knows the write was refused.
func parseVersionConflict(body string, declared []string) (expected, actual, aspect string) {
	if m := conflictVersionsRe.FindStringSubmatch(body); m != nil {
		expected, actual = m[1], m[2]
	}
	if m := conflictAspectRe.FindStringSubmatch(body); m != nil {
		for _, d := range declared {
			if d == m[1] {
				aspect = d
			}
		}
	}
	return expected, actual, aspect
}

// looksLikeVersionConflict reports whether an error body from a server that
// predates the 412 mapping (DataHub 0.14 to 1.0) describes a precondition
// failure. The phrases are specific to ConditionalWriteValidator.
func looksLikeVersionConflict(body string) bool {
	lower := strings.ToLower(body)
	return strings.Contains(lower, "expected version") || strings.Contains(lower, "subtype=precondition")
}

// parseRetryAfter reads a Retry-After header given as seconds or as an HTTP
// date. Zero when absent or unparsable.
func parseRetryAfter(h string) time.Duration {
	h = strings.TrimSpace(h)
	if h == "" {
		return 0
	}
	if secs, err := strconv.Atoi(h); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(h); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}
