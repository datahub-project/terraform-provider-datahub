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

// TestGetDataContractByURN verifies the OpenAPI v3 read parse: the dataset
// entity, the state, and a dataQuality reference (read key is `assertion`).
func TestGetDataContractByURN(t *testing.T) {
	const id = "b28e16460efef1059ed3749e0de03755"
	body := `{
	  "urn": "urn:li:dataContract:` + id + `",
	  "dataContractKey": { "value": { "id": "` + id + `" } },
	  "dataContractStatus": { "value": { "state": "ACTIVE" } },
	  "dataContractProperties": { "value": {
	    "entity": "urn:li:dataset:(urn:li:dataPlatform:postgres,db.orders,PROD)",
	    "dataQuality": [ { "assertion": "urn:li:assertion:abc" } ]
	  } }
	}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/openapi/v3/entity/datacontract/") {
			_, _ = w.Write([]byte(body))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	dc, err := newTestClient(t, server).GetDataContractByURN(t.Context(), "urn:li:dataContract:"+id)
	if err != nil {
		t.Fatalf("GetDataContractByURN() error = %v", err)
	}
	if dc == nil {
		t.Fatal("GetDataContractByURN() = nil")
	}
	if dc.ID != id || dc.State != "ACTIVE" {
		t.Errorf("id/state = %q/%q", dc.ID, dc.State)
	}
	if dc.EntityURN != "urn:li:dataset:(urn:li:dataPlatform:postgres,db.orders,PROD)" {
		t.Errorf("entity = %q", dc.EntityURN)
	}
	if len(dc.DataQualityAssertionURNs) != 1 || dc.DataQualityAssertionURNs[0] != "urn:li:assertion:abc" {
		t.Errorf("dataQuality = %v", dc.DataQualityAssertionURNs)
	}
	if len(dc.FreshnessAssertionURNs) != 0 || len(dc.SchemaAssertionURNs) != 0 {
		t.Errorf("expected empty freshness/schema, got %v / %v", dc.FreshnessAssertionURNs, dc.SchemaAssertionURNs)
	}
}

// TestGetDataContractByURN_NotFound verifies a 404 returns (nil, nil).
func TestGetDataContractByURN_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.NotFound(w, nil)
	}))
	defer server.Close()

	dc, err := newTestClient(t, server).GetDataContractByURN(t.Context(), "urn:li:dataContract:missing")
	if err != nil {
		t.Fatalf("GetDataContractByURN() error = %v", err)
	}
	if dc != nil {
		t.Errorf("GetDataContractByURN() = %+v, want nil for 404", dc)
	}
}

// TestUpsertDataContract_DerivesID verifies the client derives the deterministic
// SDK-compatible id from the dataset URN when none is supplied, and returns that URN.
func TestUpsertDataContract_DerivesID(t *testing.T) {
	const ds = "urn:li:dataset:(urn:li:dataPlatform:postgres,mydb.public.orders,PROD)"
	const wantURN = "urn:li:dataContract:de9ff15b4d1545e318da79d38ae05d10"

	var gotID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		raw, _ := io.ReadAll(r.Body)
		// crude check that the derived id was sent
		if strings.Contains(string(raw), "de9ff15b4d1545e318da79d38ae05d10") {
			gotID = "de9ff15b4d1545e318da79d38ae05d10"
		}
		_, _ = w.Write([]byte(`{"data":{"upsertDataContract":{"urn":"` + wantURN + `"}}}`))
	}))
	defer server.Close()

	urn, err := newTestClient(t, server).UpsertDataContract(t.Context(), DataContractInput{EntityURN: ds, State: "ACTIVE"})
	if err != nil {
		t.Fatalf("UpsertDataContract() error = %v", err)
	}
	if urn != wantURN {
		t.Errorf("urn = %q, want %q", urn, wantURN)
	}
	if gotID == "" {
		t.Error("derived id was not sent in the mutation input")
	}
}

// TestUpsertDataContract_APIError verifies a GraphQL error is surfaced.
func TestUpsertDataContract_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"errors":[{"message":"Provided assertion with urn urn:li:assertion:x does not exist!"}]}`))
	}))
	defer server.Close()

	_, err := newTestClient(t, server).UpsertDataContract(t.Context(), DataContractInput{
		EntityURN: "urn:li:dataset:(urn:li:dataPlatform:postgres,db.t,PROD)",
		ID:        "fixed-id",
	})
	if err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("error = %v, want assertion-does-not-exist", err)
	}
}
