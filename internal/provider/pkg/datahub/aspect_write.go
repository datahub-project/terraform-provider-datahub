// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahub

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// An OpenAPI v3 aspect write replaces the whole stored aspect. A writer that
// manages some of an aspect's fields therefore reads the aspect, sets its
// fields on what it read, and writes the result back; the rest of the aspect
// survives. That read-modify-write has a window: anything written to the
// aspect between the read and the write (the DataHub UI, the CLI, an
// ingestion run, another Terraform run) is overwritten, with HTTP 200 and no
// trace.
//
// DataHub closes the window with a per-aspect precondition. Every stored
// aspect carries systemMetadata.version ("1" on its first write, advanced on
// every write that changes it), a read with ?systemMetadata=true returns it,
// and a write whose per-aspect "headers" object carries
// If-Version-Match lands only if the stored version still equals it. On a
// mismatch the whole request fails with HTTP 412 and nothing in it is
// written. "-1" means the aspect must not exist yet, which makes a create
// atomic: two creators racing for one URN get one success and one 412.
//
// writeAspects is the one path for every such write in this package. It
// reads the aspects a writer declares as preserved, hands the writer their
// unmanaged fields as values that carry their version, builds the write with
// the precondition on every one of them, and on a 412 re-reads, re-merges and
// retries a bounded number of times before failing with a diagnosis. The
// version cannot be dropped on the way: only readPreservedAspects constructs
// a preservedAspect, only preservedAspect.entry produces a versioned entry,
// and writeAspects refuses a merge that returns a declared aspect without
// one. The aspect-write guard tests then check the result on the wire.

// aspectVersion is a stored aspect's systemMetadata.version as DataHub
// reports it: a decimal string, compared as a string.
type aspectVersion string

// versionAbsent is the version DataHub assigns an aspect that does not
// exist; as a precondition it means "must not exist yet".
const versionAbsent aspectVersion = "-1"

// legacyAspectVersion is what DataHub's validator resolves for a stored
// aspect that carries no systemMetadata.version (written before 0.14).
const legacyAspectVersion = "1"

// preservedAspect is one aspect as read for a read-modify-write: the fields
// the writer does not manage, and the version they were read at.
type preservedAspect struct {
	name    string
	fields  map[string]any
	version aspectVersion
}

// entry binds the merged fields to the version they were read at. The
// writer sets its managed fields on p.fields first.
func (p preservedAspect) entry() aspectEntry {
	v := p.version
	return aspectEntry{name: p.name, value: p.fields, version: &v}
}

// preservedAspects is one attempt's read, keyed by aspect name.
type preservedAspects map[string]preservedAspect

// existing reports, for a create-only write, the entity the read found,
// as an EntityExistsError naming whether it is soft-deleted; nil when the
// read found none of the declared aspects. A leftover holding only its key
// aspect or a status (a CAT-2583 husk, or a soft-deleted entity whose
// content aspects were since removed) is therefore not "existing": the
// declared aspects really are absent, the write carries "-1" for each, and
// the server accepts it, which repairs the leftover rather than refusing
// a URN nothing visible occupies.
func (p preservedAspects) existing(spec aspectWriteSpec) error {
	found := false
	for _, d := range spec.preserved {
		if _, ok := p[d.aspect]; ok {
			found = true
		}
	}
	if !found {
		return nil
	}
	removed := false
	if status, ok := p[statusAspect]; ok {
		removed, _ = status.fields["removed"].(bool)
	}
	return &EntityExistsError{URN: spec.urn, What: spec.what, SoftDeleted: removed}
}

// statusAspect is read alongside a create-only write's aspects so an
// existing entity can be reported as soft-deleted.
const statusAspect = "status"

// get returns the read aspect, or, for a declared aspect the entity does not
// hold, an empty one at versionAbsent.
func (p preservedAspects) get(name string) preservedAspect {
	if a, ok := p[name]; ok {
		return a
	}
	return preservedAspect{name: name, fields: map[string]any{}, version: versionAbsent}
}

// aspectEntry is one aspect of the collection POST body.
type aspectEntry struct {
	name  string
	value any
	// version is the precondition; nil for aspects the writer owns whole,
	// which it did not read and replaces unconditionally.
	version *aspectVersion
}

// ownedEntry is an aspect the writer replaces whole: a key aspect, or one
// whose every field the provider manages (classified ownedWhole in the
// aspect-write guard).
func ownedEntry(name string, value any) aspectEntry {
	return aspectEntry{name: name, value: value}
}

// preservedSpec names an aspect the writer reads before writing, and the
// fields it manages, which are removed from the read value before the merge
// sees it so that a field the configuration no longer sets is cleared.
type preservedSpec struct {
	aspect  string
	managed []string
}

// aspectWriteSpec describes one writer's write.
type aspectWriteSpec struct {
	// entityPath is the lowercase entity type in the OpenAPI v3 path.
	entityPath string
	urn        string
	// what names the entity in errors, e.g. "corp user".
	what string
	// preserved is read before every attempt and must appear, versioned, in
	// every merge result.
	preserved []preservedSpec
	// createOnly makes the write a create: every preserved aspect carries
	// versionAbsent, so an entity that appears between the read and the
	// write is reported, atomically on the server. The read is kept, with
	// the status aspect, so an entity that already exists is reported
	// before anything is sent, as soft-deleted when that is why its id is
	// taken, and so that a server predating the precondition (which would
	// ignore "-1" and overwrite) is covered by the same check. A 412 on a
	// create is an EntityExistsError and is never retried.
	createOnly bool
	// privilege is named in the 401/403 message.
	privilege string
	// explain, when set, rewrites the body of any other 4xx/5xx into a
	// practitioner-facing message.
	explain func(body string) string
}

// mergeFunc builds the aspects to write from what the read returned. It runs
// once per attempt and must depend only on prev and the writer's inputs: on
// a version conflict the read is repeated and the function re-run on fresh
// maps, so anything it computes from prev must be recomputed, not cached.
type mergeFunc func(prev preservedAspects) ([]aspectEntry, error)

// aspectWriteRetry tunes the retry loop. Variables so tests can shorten them.
var aspectWriteRetry = struct {
	// maxAttempts is the number of writes tried on repeated version
	// conflicts: one plus the re-read/re-merge retries.
	maxAttempts int
	baseDelay   time.Duration
	maxDelay    time.Duration
	// maxRetryAfterWaits bounds the 429/503 waits per write, counted
	// separately so a busy server never consumes the conflict budget.
	maxRetryAfterWaits int
	// retryAfterCap is the longest Retry-After the provider honours.
	retryAfterCap time.Duration
}{
	maxAttempts:        4,
	baseDelay:          200 * time.Millisecond,
	maxDelay:           2 * time.Second,
	maxRetryAfterWaits: 3,
	retryAfterCap:      30 * time.Second,
}

// writeAspects performs one conditional aspect write: read the preserved
// aspects, merge, POST with If-Version-Match on every preserved aspect, and
// retry the whole sequence on a version conflict. It returns the response
// body of the successful POST.
func (c *Client) writeAspects(ctx context.Context, spec aspectWriteSpec, merge mergeFunc) ([]byte, error) {
	if c == nil {
		return nil, errors.New("client is nil")
	}
	if spec.entityPath == "" || spec.urn == "" {
		return nil, errors.New("entityPath and urn are required")
	}
	start := time.Now()
	waits := 0
	var last error
	for attempt := 1; attempt <= aspectWriteRetry.maxAttempts; attempt++ {
		prev, err := c.readPreservedAspects(ctx, spec)
		if err == nil && spec.createOnly {
			if exists := prev.existing(spec); exists != nil {
				return nil, exists
			}
			prev = preservedAspects{}
		}
		if err != nil {
			var ra *RetryAfterError
			if errors.As(err, &ra) && waits < aspectWriteRetry.maxRetryAfterWaits {
				waits++
				if err := sleepCtx(ctx, backoff(ra.After, waits)); err != nil {
					return nil, fmt.Errorf("%w (while waiting after: %w)", err, ra)
				}
				attempt--
				continue
			}
			return nil, err
		}
		entries, err := merge(prev)
		if err != nil {
			return nil, err
		}
		if err := checkPreservedVersions(spec, prev, entries); err != nil {
			return nil, err
		}
		body, err := aspectWriteBody(spec.urn, entries)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if e.version != nil {
				tflog.Debug(ctx, "DataHub conditional aspect write", map[string]any{
					"urn": spec.urn, "aspect": e.name, "if_version_match_present": true,
					"aspect_version": string(*e.version), "attempt": attempt,
				})
			}
		}
		resp, err := c.postAspects(ctx, spec, body)
		if err == nil {
			return resp, nil
		}
		last = err
		var conflict *VersionConflictError
		var ra *RetryAfterError
		switch {
		case errors.As(err, &conflict):
			conflict.Attempts = attempt
			conflict.Elapsed = time.Since(start)
			if attempt == aspectWriteRetry.maxAttempts {
				return nil, conflict
			}
			tflog.Warn(ctx, "DataHub aspect changed during write, re-reading and retrying", map[string]any{
				"urn": spec.urn, "aspect": conflict.Aspect, "attempt": attempt,
				"expected_version": conflict.Expected, "actual_version": conflict.Actual,
			})
			if err := sleepCtx(ctx, backoff(0, attempt)); err != nil {
				return nil, fmt.Errorf("%w (while retrying after: %w)", err, conflict)
			}
		case errors.As(err, &ra):
			if waits >= aspectWriteRetry.maxRetryAfterWaits {
				ra.Waits = waits
				return nil, ra
			}
			waits++
			if err := sleepCtx(ctx, backoff(ra.After, waits)); err != nil {
				return nil, fmt.Errorf("%w (while waiting after: %w)", err, ra)
			}
			attempt--
		default:
			return nil, err
		}
	}
	return nil, last
}

// backoff returns the wait before a retry: the server's Retry-After when it
// gave one, else exponential from baseDelay with full jitter, capped.
func backoff(retryAfter time.Duration, n int) time.Duration {
	if retryAfter > 0 {
		if retryAfter > aspectWriteRetry.retryAfterCap {
			return aspectWriteRetry.retryAfterCap
		}
		return retryAfter
	}
	d := aspectWriteRetry.baseDelay
	for i := 1; i < n && d < aspectWriteRetry.maxDelay; i++ {
		d *= 2
	}
	if d > aspectWriteRetry.maxDelay {
		d = aspectWriteRetry.maxDelay
	}
	if d <= 0 {
		return 0
	}
	return time.Duration(rand.Int63n(int64(d)) + 1)
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("aspect write cancelled: %w", err)
		}
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return fmt.Errorf("aspect write cancelled: %w", ctx.Err())
	case <-t.C:
		return nil
	}
}

// checkPreservedVersions is the runtime fence: every declared preserved
// aspect must come back from the merge carrying the version it was read at.
func checkPreservedVersions(spec aspectWriteSpec, prev preservedAspects, entries []aspectEntry) error {
	for _, p := range spec.preserved {
		found := false
		for _, e := range entries {
			if e.name != p.aspect {
				continue
			}
			found = true
			if e.version == nil {
				return fmt.Errorf("provider bug: %s was read for a read-modify-write but returned without its version precondition", p.aspect)
			}
			if *e.version != prev.get(p.aspect).version {
				return fmt.Errorf("provider bug: %s was read at version %s but the write carries %s", p.aspect, prev.get(p.aspect).version, *e.version)
			}
		}
		if !found {
			return fmt.Errorf("provider bug: %s was read for a read-modify-write but the write does not include it", p.aspect)
		}
	}
	return nil
}

// readPreservedAspects reads every declared aspect in one GET, with
// systemMetadata, and returns the unmanaged fields and version of each.
// Only the version is kept from systemMetadata: echoing the rest back would
// tie the write to the runId of whatever last wrote the aspect.
func (c *Client) readPreservedAspects(ctx context.Context, spec aspectWriteSpec) (preservedAspects, error) {
	out := preservedAspects{}
	if len(spec.preserved) == 0 {
		return out, nil
	}
	names := make([]string, 0, len(spec.preserved)+1)
	for _, p := range spec.preserved {
		names = append(names, p.aspect)
	}
	declared := spec.preserved
	if spec.createOnly {
		names = append(names, statusAspect)
		declared = append(append([]preservedSpec(nil), spec.preserved...), preservedSpec{aspect: statusAspect})
	}
	path := fmt.Sprintf("/openapi/v3/entity/%s/%s?aspects=%s&systemMetadata=true",
		spec.entityPath, url.PathEscape(spec.urn), url.QueryEscape(strings.Join(names, ",")))
	req, err := c.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("building %s read request: %w", spec.what, err)
	}
	res, err := c.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s read request failed: %w", spec.what, err)
	}
	defer res.Body.Close()

	switch {
	case res.StatusCode == http.StatusNotFound:
		return out, nil
	case res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden:
		return nil, fmt.Errorf("DataHub rejected the request (HTTP %d) reading %s for %s: the calling principal needs read access to the entity it manages",
			res.StatusCode, strings.Join(names, ","), spec.urn)
	case res.StatusCode == http.StatusTooManyRequests || res.StatusCode == http.StatusServiceUnavailable:
		respBody, _ := io.ReadAll(res.Body)
		return nil, &RetryAfterError{Status: res.StatusCode, After: parseRetryAfter(res.Header.Get("Retry-After")), Message: trimBody(respBody)}
	case res.StatusCode >= http.StatusBadRequest:
		respBody, _ := io.ReadAll(res.Body)
		return nil, fmt.Errorf("unexpected HTTP %d reading %s for %s: %s", res.StatusCode, strings.Join(names, ","), spec.urn, respBody)
	}

	var entity map[string]json.RawMessage
	if err := json.NewDecoder(res.Body).Decode(&entity); err != nil {
		return nil, fmt.Errorf("parsing %s response: %w", spec.what, err)
	}
	for _, p := range declared {
		raw, ok := entity[p.aspect]
		if !ok {
			continue
		}
		var aspect struct {
			Value          map[string]any `json:"value"`
			SystemMetadata struct {
				Version string `json:"version"`
			} `json:"systemMetadata"`
		}
		// UseNumber keeps numbers as json.Number, which writes back the exact
		// literal it read. Plain decoding goes through float64 and rounds PDL
		// longs above 2^53, corrupting fields the caller never touched.
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		if err := dec.Decode(&aspect); err != nil {
			return nil, fmt.Errorf("parsing %s aspect: %w", p.aspect, err)
		}
		if aspect.Value == nil {
			continue
		}
		version := aspect.SystemMetadata.Version
		if version == "" {
			// A row last written before DataHub 0.14 has no stored version.
			// The server's ConditionalWriteValidator resolves such a row
			// to max(1, rowVersion), and the latest row is version 0, so
			// "1" is what it will compare against. A server with no
			// validator ignores the header, so "1" costs nothing there.
			version = legacyAspectVersion
		}
		for _, k := range p.managed {
			delete(aspect.Value, k)
		}
		out[p.aspect] = preservedAspect{name: p.aspect, fields: aspect.Value, version: aspectVersion(version)}
	}
	return out, nil
}

// aspectWriteBody builds the OpenAPI v3 collection POST body: one entity
// with every entry as an aspect, and a "headers" object carrying
// If-Version-Match on each versioned entry. It never emits systemMetadata.
func aspectWriteBody(urn string, entries []aspectEntry) ([]map[string]any, error) {
	entity := map[string]any{"urn": urn}
	for _, e := range entries {
		if e.name == "" || e.name == "urn" {
			return nil, fmt.Errorf("provider bug: aspect entry with name %q", e.name)
		}
		if _, dup := entity[e.name]; dup {
			return nil, fmt.Errorf("provider bug: aspect %s appears twice in one write", e.name)
		}
		a := map[string]any{"value": e.value}
		if e.version != nil {
			a["headers"] = map[string]string{"If-Version-Match": string(*e.version)}
		}
		entity[e.name] = a
	}
	return []map[string]any{entity}, nil
}

// postAspects sends the collection POST synchronously and classifies the
// answer. async=false is what makes a precondition failure arrive on this
// response rather than on a Kafka topic.
func (c *Client) postAspects(ctx context.Context, spec aspectWriteSpec, body []map[string]any) ([]byte, error) {
	path := fmt.Sprintf("/openapi/v3/entity/%s?async=false", spec.entityPath)
	req, err := c.NewRequest(ctx, http.MethodPost, path, body)
	if err != nil {
		return nil, fmt.Errorf("building %s write request: %w", spec.what, err)
	}
	res, err := c.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s write request failed: %w", spec.what, err)
	}
	defer res.Body.Close()
	respBody, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("reading %s write response: %w", spec.what, err)
	}
	if res.StatusCode < http.StatusBadRequest {
		return respBody, nil
	}
	declared := make([]string, 0, len(spec.preserved))
	for _, p := range spec.preserved {
		declared = append(declared, p.aspect)
	}
	switch {
	case res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden:
		if spec.privilege != "" {
			return nil, fmt.Errorf("DataHub rejected the request (HTTP %d): the calling principal needs the %s privilege", res.StatusCode, spec.privilege)
		}
		return nil, fmt.Errorf("DataHub rejected the request (HTTP %d) writing %s", res.StatusCode, spec.urn)
	case res.StatusCode == http.StatusPreconditionFailed,
		res.StatusCode == http.StatusInternalServerError && looksLikeVersionConflict(string(respBody)):
		// The server answers 412 whenever a precondition failure is among
		// the validation failures, not only when it is the sole one. Only
		// a body that names the versions is a conflict worth retrying;
		// anything else is surfaced as it is, so a real validation error
		// riding along with the precondition is not retried into silence.
		// Parse the whole message: the server dumps the rejected record
		// before the verdict, so the versions sit past any display limit.
		full := conflictMessage(respBody)
		expected, actual, aspect := parseVersionConflict(full, declared)
		if expected == "" || actual == "" {
			break
		}
		message := trimBody([]byte(full))
		if spec.createOnly {
			return nil, &EntityExistsError{URN: spec.urn, What: spec.what, Message: message}
		}
		if aspect == "" && len(declared) == 1 {
			aspect = declared[0]
		}
		return nil, &VersionConflictError{URN: spec.urn, Aspect: aspect, Expected: expected, Actual: actual, Message: message}
	case res.StatusCode == http.StatusTooManyRequests || res.StatusCode == http.StatusServiceUnavailable:
		return nil, &RetryAfterError{Status: res.StatusCode, After: parseRetryAfter(res.Header.Get("Retry-After")), Message: trimBody(respBody)}
	}
	text := string(respBody)
	if spec.explain != nil {
		text = spec.explain(text)
	}
	return nil, fmt.Errorf("unexpected HTTP %d from DataHub %s write API: %s", res.StatusCode, spec.what, text)
}

// conflictMessage pulls the server's message out of a 412 body, which is
// {"error":"Precondition Error","message":"..."}, falling back to the body.
// It is returned whole; callers trim what they show.
func conflictMessage(body []byte) string {
	var e struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &e) == nil && e.Message != "" {
		return strings.TrimSpace(e.Message)
	}
	return strings.TrimSpace(string(body))
}

// trimBody shortens a server message for display, keeping its start and its
// end: DataHub's validation messages put the verdict last, after a dump of
// the rejected record.
func trimBody(b []byte) string {
	s := strings.TrimSpace(string(b))
	const keep = 300
	if len(s) > 2*keep+5 {
		return s[:keep] + " ... " + s[len(s)-keep:]
	}
	return s
}
