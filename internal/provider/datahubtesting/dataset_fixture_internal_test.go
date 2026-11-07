// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahubtesting

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"runtime"
	"strings"
	"sync"
	"testing"
)

const fixtureDatasetURN = "urn:li:dataset:(urn:li:dataPlatform:sqlite,tf_fixture_unit.orders,PROD)"

// fakeGMS records every request the dataset fixtures send and answers with
// the configured status codes. postStatus defaults to 200, the code GMS returns
// for a synchronous write; getStatus is what the read-back sees.
type fakeGMS struct {
	mu         sync.Mutex
	requests   []*http.Request
	postStatus int
	getStatus  int
}

func (f *fakeGMS) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.requests = append(f.requests, r.Clone(r.Context()))
	postStatus, getStatus := f.postStatus, f.getStatus
	f.mu.Unlock()

	switch r.Method {
	case http.MethodPost:
		if postStatus == 0 {
			postStatus = http.StatusOK
		}
		w.WriteHeader(postStatus)
		_, _ = w.Write([]byte(`[]`))
	case http.MethodGet:
		if getStatus == 0 {
			getStatus = http.StatusOK
		}
		w.WriteHeader(getStatus)
		_, _ = w.Write([]byte(`{}`))
	default:
		w.WriteHeader(http.StatusOK)
	}
}

func (f *fakeGMS) byMethod(method string) []*http.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*http.Request
	for _, r := range f.requests {
		if r.Method == method {
			out = append(out, r)
		}
	}
	return out
}

func startFakeGMS(t *testing.T) *fakeGMS {
	t.Helper()
	f := &fakeGMS{}
	srv := httptest.NewServer(f)
	// Registered before the fixture runs, so LIFO cleanup closes the server
	// only after the fixture's own DELETE cleanup has gone out.
	t.Cleanup(srv.Close)
	t.Setenv("DATAHUB_GMS_URL", srv.URL)
	t.Setenv("DATAHUB_GMS_TOKEN", "unit-test-token")
	return f
}

// The data contract upsert (and the assertion monitor upserts) refuse a
// dataset that does not exist yet. A collection POST without async=false is
// queued to Kafka and answered with 202 before the entity is persisted, so a
// fixture that writes that way races the very call it exists to satisfy.
// Every dataset fixture must therefore write synchronously.
func TestDatasetFixtures_WriteSynchronously(t *testing.T) {
	live := &Target{Kind: TargetLive}
	fixtures := map[string]func(t *testing.T){
		"EnsureDatasetEntity": func(t *testing.T) {
			live.EnsureDatasetEntity(t, fixtureDatasetURN)
		},
		"EnsureDatasetEntityWithSchema": func(t *testing.T) {
			live.EnsureDatasetEntityWithSchema(t, fixtureDatasetURN)
		},
		"EnsureDatasetEntityInDomain": func(t *testing.T) {
			live.EnsureDatasetEntityInDomain(t, fixtureDatasetURN, "urn:li:domain:tf-fixture-unit")
		},
	}
	for name, ensure := range fixtures {
		t.Run(name, func(t *testing.T) {
			gms := startFakeGMS(t)
			ensure(t)

			posts := gms.byMethod(http.MethodPost)
			if len(posts) != 1 {
				t.Fatalf("expected exactly 1 POST, got %d", len(posts))
			}
			post := posts[0]
			if post.URL.Path != "/openapi/v3/entity/dataset" {
				t.Errorf("POST path = %q, want /openapi/v3/entity/dataset", post.URL.Path)
			}
			if got := post.URL.Query().Get("async"); got != "false" {
				t.Errorf("POST %s carries async=%q, want async=false: without it GMS queues the write "+
					"and returns 202 before the dataset exists", post.URL.String(), got)
			}
		})
	}
}

// Every request the fixtures send must authenticate; a request missing the
// bearer token is rejected on a live instance and would surface as a
// confusing 401 rather than the fixture's own error.
func TestDatasetFixtures_Authenticate(t *testing.T) {
	gms := startFakeGMS(t)
	(&Target{Kind: TargetLive}).EnsureDatasetEntity(t, fixtureDatasetURN)
	for _, r := range append(gms.byMethod(http.MethodPost), gms.byMethod(http.MethodGet)...) {
		if got := r.Header.Get("Authorization"); got != "Bearer unit-test-token" {
			t.Errorf("%s %s Authorization = %q", r.Method, r.URL.Path, got)
		}
	}
}

// recordingTB stands in for the test handed to ensureDataset so its failure
// paths can be observed without failing this test. Fatalf ends the goroutine
// exactly as testing.T does; cleanups are collected and run on demand.
type recordingTB struct {
	testing.TB
	mu       sync.Mutex
	fatals   []string
	errors   []string
	cleanups []func()
}

func (r *recordingTB) Helper() {}

func (r *recordingTB) Fatalf(format string, args ...any) {
	r.mu.Lock()
	r.fatals = append(r.fatals, fmt.Sprintf(format, args...))
	r.mu.Unlock()
	runtime.Goexit()
}

func (r *recordingTB) Errorf(format string, args ...any) {
	r.mu.Lock()
	r.errors = append(r.errors, fmt.Sprintf(format, args...))
	r.mu.Unlock()
}

func (r *recordingTB) Cleanup(f func()) {
	r.mu.Lock()
	r.cleanups = append(r.cleanups, f)
	r.mu.Unlock()
}

func (r *recordingTB) runCleanups() {
	for i := len(r.cleanups) - 1; i >= 0; i-- {
		r.cleanups[i]()
	}
}

// runEnsure calls ensureDataset under a recordingTB on its own goroutine, so
// a Fatalf (runtime.Goexit) ends that goroutine rather than this test.
func runEnsure(t *testing.T) *recordingTB {
	t.Helper()
	rec := &recordingTB{TB: t}
	done := make(chan struct{})
	go func() {
		defer close(done)
		ensureDataset(rec, "ensureDataset", fixtureDatasetURN, map[string]any{
			"datasetProperties": map[string]any{"value": map[string]any{"name": "unit"}},
		})
	}()
	<-done
	return rec
}

// The fixture must fail at the fixture, not three calls later inside the
// provider with a misleading "does not exist" from an unrelated mutation.
func TestEnsureDataset_FailsLoudlyWhenTheWriteDidNotLand(t *testing.T) {
	cases := []struct {
		name       string
		postStatus int
		getStatus  int
		wantFatal  string
		wantDelete bool
	}{
		{name: "queued asynchronously", postStatus: http.StatusAccepted, wantFatal: "HTTP 202", wantDelete: true},
		{name: "rejected", postStatus: http.StatusInternalServerError, wantFatal: "HTTP 500", wantDelete: false},
		{name: "read-back absent", postStatus: http.StatusOK, getStatus: http.StatusNotFound, wantFatal: "read-back returned HTTP 404", wantDelete: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gms := startFakeGMS(t)
			gms.postStatus, gms.getStatus = tc.postStatus, tc.getStatus

			rec := runEnsure(t)
			if len(rec.fatals) != 1 || !strings.Contains(rec.fatals[0], tc.wantFatal) {
				t.Fatalf("fatals = %q, want one containing %q", rec.fatals, tc.wantFatal)
			}

			rec.runCleanups()
			deletes := gms.byMethod(http.MethodDelete)
			if tc.wantDelete && len(deletes) != 1 {
				t.Errorf("expected the accepted write to be cleaned up with 1 DELETE, got %d", len(deletes))
			}
			if !tc.wantDelete && len(deletes) != 0 {
				t.Errorf("expected no DELETE for a rejected write, got %d", len(deletes))
			}
		})
	}
}

func TestEnsureDataset_SucceedsAndCleansUp(t *testing.T) {
	gms := startFakeGMS(t)
	rec := runEnsure(t)
	if len(rec.fatals) != 0 {
		t.Fatalf("unexpected fatals: %q", rec.fatals)
	}

	gets := gms.byMethod(http.MethodGet)
	wantPath := "/openapi/v3/entity/dataset/" + url.PathEscape(fixtureDatasetURN)
	if len(gets) != 1 || gets[0].URL.EscapedPath() != wantPath {
		t.Errorf("expected one read-back GET of %s, got %d", wantPath, len(gets))
	}

	rec.runCleanups()
	if n := len(gms.byMethod(http.MethodDelete)); n != 1 {
		t.Errorf("expected 1 cleanup DELETE, got %d", n)
	}
	if len(rec.errors) != 0 {
		t.Errorf("unexpected cleanup errors: %q", rec.errors)
	}
}
