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
// The failure knobs let tests exercise each repair error path.
type huskTestServer struct {
	mu           sync.Mutex
	entityJSON   map[string]any // nil => entity absent (404 on read, create succeeds)
	createErrMsg string         // create error while entity exists; default "This Glossary Node already exists!"
	get404       bool           // OpenAPI read 404s even while entityJSON is set
	deleteFails  bool           // deleteGlossaryEntity returns a GraphQL error and does not clear entityJSON
	createCalls  int
	getCalls     int
	deleteCalls  int
}

func (s *huskTestServer) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/openapi/v3/entity/glossarynode/") {
			s.mu.Lock()
			s.getCalls++
			entity := s.entityJSON
			gone := s.get404
			s.mu.Unlock()
			if entity == nil || gone {
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
			errMsg := s.createErrMsg
			if errMsg == "" {
				errMsg = "This Glossary Node already exists!"
			}
			s.createCalls++
			s.mu.Unlock()
			if exists {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"errors": []map[string]any{{"message": errMsg}},
				})
				return
			}
			_, _ = w.Write([]byte(`{"data":{"createGlossaryNode":"urn:li:glossaryNode:tf-example-governance"}}`))
		case strings.Contains(req.Query, "deleteGlossaryEntity"):
			s.mu.Lock()
			s.deleteCalls++
			fails := s.deleteFails
			if !fails {
				s.entityJSON = nil
			}
			s.mu.Unlock()
			if fails {
				_, _ = w.Write([]byte(`{"errors":[{"message":"delete rejected"}]}`))
				return
			}
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

// TestCreateGlossaryNode_OtherErrorNoRepair verifies a create failure that is
// not "already exists" surfaces immediately without any husk inspection.
func TestCreateGlossaryNode_OtherErrorNoRepair(t *testing.T) {
	ts := &huskTestServer{
		entityJSON:   huskEntityJSON(),
		createErrMsg: "Unauthorized to perform this action.",
	}
	srv := httptest.NewServer(ts.handler())
	defer srv.Close()

	c := newTestClient(t, srv)
	_, repaired, err := c.CreateGlossaryNode(context.Background(), CreateGlossaryEntityInput{
		ID:   "tf-example-governance",
		Name: "TF Example - Governance",
	})
	if err == nil || !strings.Contains(err.Error(), "Unauthorized") {
		t.Fatalf("expected the original error, got %v", err)
	}
	if repaired {
		t.Error("expected repairedHusk=false")
	}

	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.getCalls != 0 || ts.deleteCalls != 0 {
		t.Errorf("expected no husk inspection for non-already-exists errors, got get=%d delete=%d", ts.getCalls, ts.deleteCalls)
	}
}

// TestCreateGlossaryNode_HuskGoneBeforeCheck verifies the race where the
// blocking entity disappears between the failed create and the husk read:
// the original error surfaces and nothing is deleted.
func TestCreateGlossaryNode_HuskGoneBeforeCheck(t *testing.T) {
	ts := &huskTestServer{
		entityJSON: huskEntityJSON(),
		get404:     true,
	}
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
		t.Errorf("expected no delete when the entity is already gone, got %d", ts.deleteCalls)
	}
}

// TestCreateGlossaryNode_HuskDeleteFails verifies a failed husk removal
// surfaces both the original error and the repair failure, and does not
// retry the create.
func TestCreateGlossaryNode_HuskDeleteFails(t *testing.T) {
	ts := &huskTestServer{
		entityJSON:  huskEntityJSON(),
		deleteFails: true,
	}
	srv := httptest.NewServer(ts.handler())
	defer srv.Close()

	c := newTestClient(t, srv)
	_, repaired, err := c.CreateGlossaryNode(context.Background(), CreateGlossaryEntityInput{
		ID:   "tf-example-governance",
		Name: "TF Example - Governance",
	})
	if err == nil || !strings.Contains(err.Error(), "already exists") || !strings.Contains(err.Error(), "husk repair failed") {
		t.Fatalf("expected combined already-exists + repair-failure error, got %v", err)
	}
	if repaired {
		t.Error("expected repairedHusk=false")
	}

	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.createCalls != 1 {
		t.Errorf("expected no create retry after a failed husk delete, got %d create calls", ts.createCalls)
	}
}

// TestCreateGlossaryNode_InputValidationAndFields verifies the client-side
// guards and that optional fields ride along on the mutation input.
func TestCreateGlossaryNode_InputValidationAndFields(t *testing.T) {
	ts := &huskTestServer{}
	srv := httptest.NewServer(ts.handler())
	defer srv.Close()

	c := newTestClient(t, srv)

	if _, _, err := c.CreateGlossaryNode(context.Background(), CreateGlossaryEntityInput{Name: "x"}); err == nil {
		t.Error("expected error for missing id")
	}
	if _, _, err := c.CreateGlossaryNode(context.Background(), CreateGlossaryEntityInput{ID: "x"}); err == nil {
		t.Error("expected error for missing name")
	}

	urn, repaired, err := c.CreateGlossaryNode(context.Background(), CreateGlossaryEntityInput{
		ID:         "tf-example-governance",
		Name:       "TF Example - Governance",
		Definition: "with a definition",
		ParentNode: "urn:li:glossaryNode:parent",
	})
	if err != nil {
		t.Fatalf("CreateGlossaryNode: %v", err)
	}
	if repaired {
		t.Error("expected repairedHusk=false on a clean create")
	}
	if urn != "urn:li:glossaryNode:tf-example-governance" {
		t.Errorf("unexpected urn %q", urn)
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
