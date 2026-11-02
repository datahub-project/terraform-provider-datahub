// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahub

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// These tests drive every writer with a preserved aspect into the conflict
// paths of writeAspects and check the contract: a version mismatch surfaces
// as a typed error, a retry re-reads and re-merges rather than replaying the
// stale write, retries are bounded, a batch retries whole, and server
// back-pressure is never mistaken for a conflict.

// fastRetry shortens the backoff for the duration of a test.
func fastRetry(t *testing.T) {
	t.Helper()
	saved := aspectWriteRetry
	aspectWriteRetry.baseDelay = time.Millisecond
	aspectWriteRetry.maxDelay = 2 * time.Millisecond
	t.Cleanup(func() { aspectWriteRetry = saved })
}

// preservedWriters returns the writers that read at least one aspect, with
// the preserved aspects of each in sorted order.
func preservedWriters() map[string][]string {
	out := map[string][]string{}
	for _, name := range sortedWriterNames() {
		var aspects []string
		for aspect, rule := range aspectWriters[name].aspects {
			if rule.ownership == preserveUnmanaged {
				aspects = append(aspects, aspect)
			}
		}
		if len(aspects) > 0 {
			sortStrings(aspects)
			out[name] = aspects
		}
	}
	return out
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// postedAspect returns the first of aspects the guard saw a POST for. A
// writer that writes several entities in turn stops at the first failure,
// so under a forced status only its first aspect is ever sent.
func (g *aspectGuard) postedAspect(t *testing.T, aspects []string) string {
	t.Helper()
	for _, a := range aspects {
		if len(g.postsOf(a)) > 0 {
			return a
		}
	}
	t.Fatalf("none of %v was ever POSTed", aspects)
	return ""
}

func (g *aspectGuard) postsOf(aspect string) []recordedPost {
	g.mu.Lock()
	defer g.mu.Unlock()
	var out []recordedPost
	for _, p := range g.posts {
		if p.Aspect == aspect {
			out = append(out, p)
		}
	}
	return out
}

func TestAspectWriteConflictSurfacesTypedError(t *testing.T) {
	fastRetry(t)
	for name, aspects := range preservedWriters() {
		t.Run(name, func(t *testing.T) {
			g := newAspectGuard(t)
			g.always = http.StatusPreconditionFailed
			err := aspectWriters[name].write(t.Context(), newTestClient(t, g.Server))
			var conflict *VersionConflictError
			if !errors.As(err, &conflict) {
				t.Fatalf("want *VersionConflictError, got %T: %v", err, err)
			}
			if !errors.Is(err, ErrVersionConflict) {
				t.Errorf("errors.Is(err, ErrVersionConflict) is false")
			}
			if conflict.Attempts != aspectWriteRetry.maxAttempts {
				t.Errorf("Attempts = %d, want %d", conflict.Attempts, aspectWriteRetry.maxAttempts)
			}
			for _, want := range []string{"changed while Terraform was writing it", "safe to run terraform apply again"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error lacks %q: %v", want, err)
				}
			}
			aspect := g.postedAspect(t, aspects)
			if got := len(g.postsOf(aspect)); got != aspectWriteRetry.maxAttempts {
				t.Errorf("%s was POSTed %d times, want %d", aspect, got, aspectWriteRetry.maxAttempts)
			}
			reads := 0
			for _, get := range g.readsOf(aspect) {
				if get.SystemMetadata {
					reads++
				}
			}
			if reads != aspectWriteRetry.maxAttempts {
				t.Errorf("%s was read %d times with systemMetadata, want one read per attempt (%d)", aspect, reads, aspectWriteRetry.maxAttempts)
			}
		})
	}
}

func TestAspectWriteConflictRetriesWithFreshRead(t *testing.T) {
	fastRetry(t)
	for name, aspects := range preservedWriters() {
		aspect := aspects[0]
		t.Run(name+"/"+aspect, func(t *testing.T) {
			g := newAspectGuard(t)
			g.mu.Lock()
			old := strconv.Itoa(g.seededVersion(aspect))
			g.mu.Unlock()
			g.bumpOnGet[aspect] = 1
			if err := aspectWriters[name].write(t.Context(), newTestClient(t, g.Server)); err != nil {
				t.Fatalf("writer failed: %v", err)
			}
			posts := g.postsOf(aspect)
			if len(posts) != 2 {
				t.Fatalf("%s was POSTed %d times, want 2 (one conflict, one retry)", aspect, len(posts))
			}
			if posts[0].Status != http.StatusPreconditionFailed || posts[0].Headers["If-Version-Match"] != old {
				t.Errorf("first POST: status %d, If-Version-Match %q; want 412 with the version first read (%s)", posts[0].Status, posts[0].Headers["If-Version-Match"], old)
			}
			if posts[1].Status != http.StatusOK {
				t.Errorf("retry POST status %d, want 200", posts[1].Status)
			}
			if got := posts[1].Headers["If-Version-Match"]; got == old {
				t.Errorf("retry re-sent the stale version %q instead of the one re-read", got)
			}
			if posts[1].Value[unmanagedConcurrentEdit] != "theirs" {
				t.Errorf("retry did not carry the concurrent edit: the merge was not redone from the fresh read; sent %s", posts[1].Raw)
			}
			if posts[1].Value[unmanagedSentinel] != "keep-me" {
				t.Errorf("retry dropped the unmanaged field: sent %s", posts[1].Raw)
			}
		})
	}
}

func TestAspectWriteConflictRetriesAreBounded(t *testing.T) {
	fastRetry(t)
	for name, aspects := range preservedWriters() {
		aspect := aspects[0]
		t.Run(name, func(t *testing.T) {
			g := newAspectGuard(t)
			g.bumpOnGet[aspect] = 1 << 30
			err := aspectWriters[name].write(t.Context(), newTestClient(t, g.Server))
			var conflict *VersionConflictError
			if !errors.As(err, &conflict) {
				t.Fatalf("want *VersionConflictError after a perpetual conflict, got %v", err)
			}
			posts := g.postsOf(aspect)
			if len(posts) != aspectWriteRetry.maxAttempts {
				t.Errorf("%d POSTs, want exactly maxAttempts (%d)", len(posts), aspectWriteRetry.maxAttempts)
			}
			seen := map[string]bool{}
			for _, p := range posts {
				v := p.Headers["If-Version-Match"]
				if seen[v] {
					t.Errorf("version %q was sent twice: a retry replayed a version the server had already rejected", v)
				}
				seen[v] = true
			}
		})
	}
}

// TestAspectWriteBatchConflictResendsWholeBatch uses the one writer that
// sends two preserved aspects in one POST. A conflict on either must retry
// the whole batch with both versions re-read.
func TestAspectWriteBatchConflictResendsWholeBatch(t *testing.T) {
	fastRetry(t)
	g := newAspectGuard(t)
	g.bumpOnGet["structuredPropertySettings"] = 1
	if err := aspectWriters["writeStructuredProperty"].write(t.Context(), newTestClient(t, g.Server)); err != nil {
		t.Fatalf("writer failed: %v", err)
	}
	for _, aspect := range []string{"propertyDefinition", "structuredPropertySettings"} {
		posts := g.postsOf(aspect)
		if len(posts) != 2 {
			t.Fatalf("%s was POSTed %d times, want 2: the batch must retry whole", aspect, len(posts))
		}
		if posts[0].Status != http.StatusPreconditionFailed || posts[1].Status != http.StatusOK {
			t.Errorf("%s statuses %d then %d, want 412 then 200", aspect, posts[0].Status, posts[1].Status)
		}
		g.mu.Lock()
		want := strconv.Itoa(g.versions[aspect] - 1)
		g.mu.Unlock()
		if got := posts[1].Headers["If-Version-Match"]; got != want {
			t.Errorf("%s retry sent %q, want the re-read version %q", aspect, got, want)
		}
	}
	reads := g.readsOf("propertyDefinition")
	if len(reads) != 2 {
		t.Errorf("%d reads of propertyDefinition, want 2 (one per attempt)", len(reads))
	}
	for _, r := range reads {
		if len(r.Aspects) != 2 {
			t.Errorf("read asked for %v; both preserved aspects should be read in one GET", r.Aspects)
		}
	}
}

func TestAspectWriteBackPressureIsNotAConflict(t *testing.T) {
	fastRetry(t)
	for name, aspects := range preservedWriters() {
		t.Run(name, func(t *testing.T) {
			g := newAspectGuard(t)
			g.always = http.StatusServiceUnavailable
			err := aspectWriters[name].write(t.Context(), newTestClient(t, g.Server))
			var ra *RetryAfterError
			if !errors.As(err, &ra) {
				t.Fatalf("want *RetryAfterError, got %T: %v", err, err)
			}
			if errors.Is(err, ErrVersionConflict) {
				t.Errorf("a 503 was reported as a version conflict")
			}
			if ra.Waits != aspectWriteRetry.maxRetryAfterWaits {
				t.Errorf("Waits = %d, want %d", ra.Waits, aspectWriteRetry.maxRetryAfterWaits)
			}
			posts := g.postsOf(g.postedAspect(t, aspects))
			if len(posts) != aspectWriteRetry.maxRetryAfterWaits+1 {
				t.Errorf("%d POSTs, want %d (one plus the waits)", len(posts), aspectWriteRetry.maxRetryAfterWaits+1)
			}
			for _, p := range posts {
				if _, ok := p.Headers["If-Version-Match"]; !ok {
					t.Errorf("a retry after back-pressure dropped If-Version-Match")
				}
			}
		})
	}
}

func TestAspectWriteCreateOnlyReportsExisting(t *testing.T) {
	fastRetry(t)
	g := newAspectGuard(t)
	c := newTestClient(t, g.Server)
	// The guard holds every aspect, so "-1" mismatches: the property exists.
	_, err := c.CreateStructuredProperty(t.Context(), CreateStructuredPropertyInput{
		ID: "io.guard", ValueType: "string", EntityTypes: []string{"dataset"},
	})
	var exists *EntityExistsError
	if !errors.As(err, &exists) {
		t.Fatalf("want *EntityExistsError, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "already exists") || !strings.Contains(err.Error(), "terraform import") {
		t.Errorf("error should say the property exists and how to adopt it: %v", err)
	}
	if posts := g.postsOf("propertyDefinition"); len(posts) != 1 || posts[0].Headers["If-Version-Match"] != "-1" {
		t.Errorf("create-only must POST exactly once with If-Version-Match -1 and never retry: %+v", posts)
	}
	if reads := g.readsOf("propertyDefinition"); len(reads) != 0 {
		t.Errorf("create-only must not read first; the precondition is the existence check: %d reads", len(reads))
	}
}

func TestAspectWriteCreateOnlySucceedsWhenAbsent(t *testing.T) {
	g := newAspectGuard(t)
	g.absent["propertyDefinition"] = true
	g.absent["structuredPropertySettings"] = true
	c := newTestClient(t, g.Server)
	urn, err := c.CreateStructuredProperty(t.Context(), CreateStructuredPropertyInput{
		ID: "io.fresh", ValueType: "string", EntityTypes: []string{"dataset"}, Settings: &StructuredPropertySettings{},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if urn != "urn:li:structuredProperty:io.fresh" {
		t.Errorf("urn = %q", urn)
	}
	for _, aspect := range []string{"propertyDefinition", "structuredPropertySettings"} {
		if posts := g.postsOf(aspect); len(posts) != 1 || posts[0].Headers["If-Version-Match"] != "-1" || posts[0].Status != http.StatusOK {
			t.Errorf("%s: want one successful POST with If-Version-Match -1, got %+v", aspect, posts)
		}
	}
}

func TestAspectWriteStopsOnCancelledContext(t *testing.T) {
	saved := aspectWriteRetry
	aspectWriteRetry.baseDelay = time.Hour
	aspectWriteRetry.maxDelay = time.Hour
	t.Cleanup(func() { aspectWriteRetry = saved })

	g := newAspectGuard(t)
	g.always = http.StatusPreconditionFailed
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := aspectWriters["SetDomainProperties"].write(ctx, newTestClient(t, g.Server))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want the context error, got %v", err)
	}
	if !errors.Is(err, ErrVersionConflict) {
		t.Errorf("the error should also carry the conflict it was retrying: %v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Errorf("cancellation did not interrupt the backoff")
	}
}

func TestAspectWriteRuntimeFenceRejectsUnversionedPreserved(t *testing.T) {
	g := newAspectGuard(t)
	c := newTestClient(t, g.Server)
	spec := aspectWriteSpec{entityPath: "domain", urn: "urn:li:domain:fence", what: "domain",
		preserved: []preservedSpec{{aspect: "domainProperties"}}}
	for name, merge := range map[string]mergeFunc{
		"owned_entry_for_preserved": func(prev preservedAspects) ([]aspectEntry, error) {
			return []aspectEntry{ownedEntry("domainProperties", prev.get("domainProperties").fields)}, nil
		},
		"preserved_omitted": func(preservedAspects) ([]aspectEntry, error) {
			return []aspectEntry{ownedEntry("ownership", map[string]any{})}, nil
		},
		"wrong_version": func(prev preservedAspects) ([]aspectEntry, error) {
			v := aspectVersion("999")
			return []aspectEntry{{name: "domainProperties", value: prev.get("domainProperties").fields, version: &v}}, nil
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := c.writeAspects(t.Context(), spec, merge)
			if err == nil || !strings.Contains(err.Error(), "provider bug") {
				t.Fatalf("want a provider-bug error, got %v", err)
			}
			if len(g.postsOf("domainProperties")) != 0 || len(g.postsOf("ownership")) != 0 {
				t.Errorf("the fence let a write through")
			}
		})
	}
}

func TestAspectWriteBodyShape(t *testing.T) {
	v := aspectVersion("3")
	body, err := aspectWriteBody("urn:li:tag:x", []aspectEntry{
		{name: "a", value: map[string]any{"k": 1}, version: &v},
		ownedEntry("b", map[string]any{"k": 2}),
	})
	if err != nil {
		t.Fatal(err)
	}
	entity := body[0]
	a, ok := entity["a"].(map[string]any)
	if !ok {
		t.Fatalf("aspect a = %T", entity["a"])
	}
	if hdr, _ := a["headers"].(map[string]string); hdr["If-Version-Match"] != "3" {
		t.Errorf("versioned aspect headers = %v", a["headers"])
	}
	b, ok := entity["b"].(map[string]any)
	if !ok {
		t.Fatalf("aspect b = %T", entity["b"])
	}
	if _, ok := b["headers"]; ok {
		t.Errorf("owned aspect carries headers: %v", b)
	}
	for _, aspect := range []map[string]any{a, b} {
		if _, ok := aspect["systemMetadata"]; ok {
			t.Errorf("body carries systemMetadata")
		}
	}
	if _, err := aspectWriteBody("urn:li:tag:x", []aspectEntry{ownedEntry("a", 1), ownedEntry("a", 2)}); err == nil {
		t.Errorf("duplicate aspect accepted")
	}
}

func TestParseVersionConflict(t *testing.T) {
	const server = `Failed to validate MCP due to: ValidationExceptionCollection{EntityAspect:(urn:li:ownershipType:x,ownershipTypeInfo) Exceptions: [AspectValidationException(item=ChangeItemImpl{changeType=UPSERT, systemMetadata={version=2}}, changeType=UPSERT, entityUrn=urn:li:ownershipType:x, aspectName=ownershipTypeInfo, subType=PRECONDITION, msg=Expected version 99, actual version 1)]}`
	expected, actual, aspect := parseVersionConflict(server, []string{"ownershipTypeInfo"})
	if expected != "99" || actual != "1" || aspect != "ownershipTypeInfo" {
		t.Errorf("got expected=%q actual=%q aspect=%q", expected, actual, aspect)
	}
	if e, a, as := parseVersionConflict("nothing useful", []string{"x"}); e != "" || a != "" || as != "" {
		t.Errorf("garbage parsed as %q %q %q", e, a, as)
	}
	if !looksLikeVersionConflict(server) || looksLikeVersionConflict(`{"error":"boom"}`) {
		t.Errorf("looksLikeVersionConflict misclassifies")
	}
}

func TestParseRetryAfter(t *testing.T) {
	if got := parseRetryAfter("7"); got != 7*time.Second {
		t.Errorf("seconds: %v", got)
	}
	if got := parseRetryAfter(time.Now().Add(10 * time.Second).UTC().Format(http.TimeFormat)); got <= 0 || got > 11*time.Second {
		t.Errorf("http date: %v", got)
	}
	if got := parseRetryAfter("garbage"); got != 0 {
		t.Errorf("garbage: %v", got)
	}
}

// TestVersionHeaderIsBuiltInOnePlace keeps the precondition in the shared
// builder. The server reads If-Version-Match only from the per-aspect body
// object, so setting it as an HTTP header would be a silent no-op.
func TestVersionHeaderIsBuiltInOnePlace(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		text := string(src)
		if strings.Contains(text, `"If-Version-Match"`) && name != "aspect_write.go" {
			t.Errorf("%s names If-Version-Match; only aspect_write.go builds the precondition", name)
		}
		if strings.Contains(text, `Header.Set("If-Version-Match"`) || strings.Contains(text, `Header.Add("If-Version-Match"`) {
			t.Errorf("%s sets If-Version-Match as an HTTP header, which DataHub ignores; it belongs in the aspect's body headers", name)
		}
	}
}

// TestAspectWriteSurvey reports, per writer and aspect, what the guard saw.
// It asserts nothing new; the guards above do. Run it with -v to read it.
func TestAspectWriteSurvey(t *testing.T) {
	t.Logf("%-28s %-28s %-10s %-13s %-9s %-7s", "writer", "aspect", "ownership", "precondition", "read+meta", "status")
	for _, name := range sortedWriterNames() {
		w := aspectWriters[name]
		g := newAspectGuard(t)
		if err := w.write(t.Context(), newTestClient(t, g.Server)); err != nil {
			t.Logf("%-28s writer failed: %v", name, err)
			continue
		}
		for _, aspect := range sortedAspectNames(w.aspects) {
			p, sent := g.sent()[aspect]
			ownership := "ownedWhole"
			if w.aspects[aspect].ownership == preserveUnmanaged {
				ownership = "preserved"
			}
			precond, status := "none", "-"
			if sent {
				status = strconv.Itoa(p.Status)
				if v, ok := p.Headers["If-Version-Match"]; ok {
					precond = v
				}
			}
			read := "no"
			for _, get := range g.readsOf(aspect) {
				if get.SystemMetadata {
					read = "yes"
				}
			}
			t.Logf("%-28s %-28s %-10s %-13s %-9s %-7s", name, aspect, ownership, precond, read, status)
		}
	}
}

func sortedAspectNames(m map[string]aspectRule) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sortStrings(out)
	return out
}
