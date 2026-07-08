// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahub

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// huskTestServer simulates the CAT-2583 husk-repair flow for
// createGlossaryNode: the create mutation fails with "already exists" while
// entityJSON is non-nil; the OpenAPI read returns entityJSON; a
// deleteGlossaryEntity clears entityJSON so the retried create succeeds.
type huskTestServer struct {
	mu          sync.Mutex
	entityJSON  map[string]any // nil => entity absent (404 on read, create succeeds)
	createCalls int
	getCalls    int
	deleteCalls int
}

func (s *huskTestServer) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/openapi/v3/entity/glossarynode/") {
			s.mu.Lock()
			s.getCalls++
			entity := s.entityJSON
			s.mu.Unlock()
			if entity == nil {
				http.NotFound(w, r)
				return
			}
			_ = json.NewEncoder(w).Encode(entity)
			return
		}

		body, _ := io.ReadAll(r.Body)
		var req struct {
			Query string `json:"query"`
		}
		_ = json.Unmarshal(body, &req)

		switch {
		case strings.Contains(req.Query, "createGlossaryNode"):
			s.mu.Lock()
			exists := s.entityJSON != nil
			s.createCalls++
			s.mu.Unlock()
			if exists {
				_, _ = w.Write([]byte(`{"errors":[{"message":"This Glossary Node already exists!"}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"data":{"createGlossaryNode":"urn:li:glossaryNode:tf-example-governance"}}`))
		case strings.Contains(req.Query, "deleteGlossaryEntity"):
			s.mu.Lock()
			s.deleteCalls++
			s.entityJSON = nil
			s.mu.Unlock()
			_, _ = w.Write([]byte(`{"data":{"deleteGlossaryEntity":true}}`))
		default:
			http.Error(w, `{"errors":[{"message":"unexpected query"}]}`, http.StatusBadRequest)
		}
	}
}

func huskEntityJSON() map[string]any {
	return map[string]any{
		"urn": "urn:li:glossaryNode:tf-example-governance",
		"glossaryNodeKey": map[string]any{
			"value": map[string]any{"name": "tf-example-governance"},
		},
		"structuredProperties": map[string]any{
			"value": map[string]any{"properties": []any{}},
		},
	}
}

// TestCreateGlossaryNode_RepairsHusk covers the CAT-2583 self-heal: a create
// blocked by a key-plus-empty-structuredProperties husk deletes the husk and
// retries, reporting the repair to the caller.
func TestCreateGlossaryNode_RepairsHusk(t *testing.T) {
	ts := &huskTestServer{entityJSON: huskEntityJSON()}
	srv := httptest.NewServer(ts.handler())
	defer srv.Close()

	c := newTestClient(t, srv)
	urn, repaired, err := c.CreateGlossaryNode(context.Background(), CreateGlossaryEntityInput{
		ID:   "tf-example-governance",
		Name: "TF Example - Governance",
	})
	if err != nil {
		t.Fatalf("CreateGlossaryNode: %v", err)
	}
	if !repaired {
		t.Error("expected repairedHusk=true")
	}
	if urn != "urn:li:glossaryNode:tf-example-governance" {
		t.Errorf("unexpected urn %q", urn)
	}

	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.createCalls != 2 || ts.deleteCalls != 1 || ts.getCalls != 1 {
		t.Errorf("expected create/get/delete = 2/1/1, got %d/%d/%d", ts.createCalls, ts.getCalls, ts.deleteCalls)
	}
}

// TestCreateGlossaryNode_RealEntityNotTouched verifies a genuine pre-existing
// entity (info aspect present) is never deleted: the original error surfaces.
func TestCreateGlossaryNode_RealEntityNotTouched(t *testing.T) {
	entity := huskEntityJSON()
	entity["glossaryNodeInfo"] = map[string]any{
		"value": map[string]any{"name": "Real Node", "definition": "hands off"},
	}
	ts := &huskTestServer{entityJSON: entity}
	srv := httptest.NewServer(ts.handler())
	defer srv.Close()

	c := newTestClient(t, srv)
	_, repaired, err := c.CreateGlossaryNode(context.Background(), CreateGlossaryEntityInput{
		ID:   "tf-example-governance",
		Name: "TF Example - Governance",
	})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected already-exists error, got %v", err)
	}
	if repaired {
		t.Error("expected repairedHusk=false")
	}

	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.deleteCalls != 0 {
		t.Errorf("a real entity must never be deleted, got %d delete calls", ts.deleteCalls)
	}
}

// TestCreateGlossaryNode_NonEmptyPropertiesNotTouched verifies an entity with
// real structured property values does not qualify as a husk.
func TestCreateGlossaryNode_NonEmptyPropertiesNotTouched(t *testing.T) {
	entity := huskEntityJSON()
	entity["structuredProperties"] = map[string]any{
		"value": map[string]any{"properties": []any{
			map[string]any{"propertyUrn": "urn:li:structuredProperty:x", "values": []any{map[string]any{"string": "v"}}},
		}},
	}
	ts := &huskTestServer{entityJSON: entity}
	srv := httptest.NewServer(ts.handler())
	defer srv.Close()

	c := newTestClient(t, srv)
	_, repaired, err := c.CreateGlossaryNode(context.Background(), CreateGlossaryEntityInput{
		ID:   "tf-example-governance",
		Name: "TF Example - Governance",
	})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected already-exists error, got %v", err)
	}
	if repaired {
		t.Error("expected repairedHusk=false")
	}

	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.deleteCalls != 0 {
		t.Errorf("an entity with assigned values must never be deleted, got %d delete calls", ts.deleteCalls)
	}
}

// TestCreateGlossaryNode_UnexpectedAspectNotTouched verifies any aspect
// outside the husk allowlist disqualifies the entity.
func TestCreateGlossaryNode_UnexpectedAspectNotTouched(t *testing.T) {
	entity := huskEntityJSON()
	entity["ownership"] = map[string]any{
		"value": map[string]any{"owners": []any{}},
	}
	ts := &huskTestServer{entityJSON: entity}
	srv := httptest.NewServer(ts.handler())
	defer srv.Close()

	c := newTestClient(t, srv)
	_, repaired, err := c.CreateGlossaryNode(context.Background(), CreateGlossaryEntityInput{
		ID:   "tf-example-governance",
		Name: "TF Example - Governance",
	})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected already-exists error, got %v", err)
	}
	if repaired {
		t.Error("expected repairedHusk=false")
	}

	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.deleteCalls != 0 {
		t.Errorf("an entity with unexpected aspects must never be deleted, got %d delete calls", ts.deleteCalls)
	}
}
