// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahub

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestPreservedAspectReadRequestsOnlyItsAspect checks the read asks for the
// one aspect it preserves. Without the filter the entity endpoint returns
// every aspect the entity has, all of which is discarded.
func TestPreservedAspectReadRequestsOnlyItsAspect(t *testing.T) {
	const urn = "urn:li:dataProduct:orders"

	var gotPath, gotAspects, gotMeta string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			gotPath = r.URL.Path
			gotAspects = r.URL.Query().Get("aspects")
			gotMeta = r.URL.Query().Get("systemMetadata")
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte("[]"))
	}))
	defer server.Close()
	c := newTestClient(t, server)

	if err := c.WriteDataProductProperties(t.Context(), urn, "Orders", "", "", nil, ""); err != nil {
		t.Fatalf("WriteDataProductProperties() error = %v", err)
	}
	if want := "/openapi/v3/entity/dataproduct/" + urn; gotPath != want {
		t.Errorf("GET path = %q, want %q", gotPath, want)
	}
	if gotAspects != "dataProductProperties" {
		t.Errorf("GET aspects = %q, want %q", gotAspects, "dataProductProperties")
	}
	if gotMeta != "true" {
		t.Errorf("GET systemMetadata = %q, want true: the version the write is conditioned on comes from it", gotMeta)
	}
}

// TestPreservedAspectReadKeepsIntegersExact guards the round trip of numbers
// the provider does not model. PDL longs (audit-stamp times, corpUserInfo's
// departmentId) are 64-bit, and decoding them into float64 rounds any value
// above 2^53, so the write-back would silently store a different number.
func TestPreservedAspectReadKeepsIntegersExact(t *testing.T) {
	const urn = "urn:li:dataProduct:orders"
	// 2^53 + 1: the smallest positive integer float64 cannot represent.
	const big = "9007199254740993"

	var posted string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"urn":"` + urn + `","dataProductProperties":{"value":{` +
				`"name":"Orders",` +
				`"assets":[{"destinationUrn":"urn:li:dataset:(urn:li:dataPlatform:hive,db.orders,PROD)",` +
				`"created":{"time":` + big + `,"actor":"urn:li:corpuser:datahub"}}]},"systemMetadata":{"version":"1"}}}`))
		case http.MethodPost:
			b, _ := io.ReadAll(r.Body)
			posted = string(b)
			_, _ = w.Write([]byte("[]"))
		}
	}))
	defer server.Close()
	c := newTestClient(t, server)

	if err := c.WriteDataProductProperties(t.Context(), urn, "Orders", "", "", nil, ""); err != nil {
		t.Fatalf("WriteDataProductProperties() error = %v", err)
	}
	if !strings.Contains(posted, `"time":`+big) {
		t.Errorf("POST body does not carry the stored time %s unchanged:\n%s", big, posted)
	}
}
