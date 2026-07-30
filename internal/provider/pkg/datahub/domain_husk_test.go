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

// domainHuskTestServer simulates the CAT-2583 husk-repair flow for
// createDomain: the create mutation fails with "already exists" while
// entityJSON is non-nil; the OpenAPI read returns entityJSON; a deleteDomain
// clears entityJSON so the retried create succeeds. The failure knobs let
// tests exercise each repair error path.
type domainHuskTestServer struct {
	mu           sync.Mutex
	entityJSON   map[string]any // nil => entity absent (404 on read, create succeeds)
	createErrMsg string         // create error while entity exists; default "This Domain already exists!"
	createAlways bool           // return createErrMsg even when entityJSON is nil (name-conflict case)
	get404       bool           // OpenAPI read 404s even while entityJSON is set
	deleteFails  bool           // deleteDomain returns a GraphQL error and does not clear entityJSON
	createCalls  int
	getCalls     int
	deleteCalls  int
}

func (s *domainHuskTestServer) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/openapi/v3/entity/domain/") {
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
		case strings.Contains(req.Query, "createDomain"):
			s.mu.Lock()
			exists := s.entityJSON != nil || s.createAlways
			errMsg := s.createErrMsg
			if errMsg == "" {
				errMsg = "This Domain already exists!"
			}
			s.createCalls++
			s.mu.Unlock()
			if exists {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"errors": []map[string]any{{"message": errMsg}},
				})
				return
			}
			_, _ = w.Write([]byte(`{"data":{"createDomain":"urn:li:domain:tf-example-finance"}}`))
		case strings.Contains(req.Query, "deleteDomain"):
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
			_, _ = w.Write([]byte(`{"data":{"deleteDomain":true}}`))
		default:
			http.Error(w, `{"errors":[{"message":"unexpected query"}]}`, http.StatusBadRequest)
		}
	}
}

func domainHuskEntityJSON() map[string]any {
	return map[string]any{
		"urn": "urn:li:domain:tf-example-finance",
		"domainKey": map[string]any{
			"value": map[string]any{"id": "tf-example-finance"},
		},
		"structuredProperties": map[string]any{
			"value": map[string]any{"properties": []any{}},
		},
	}
}

func newDomainHuskInput() CreateDomainInput {
	return CreateDomainInput{
		ID:   "tf-example-finance",
		Name: "TF Example - Finance",
	}
}

// TestCreateDomain_RepairsHusk covers the CAT-2583 self-heal: a create blocked
// by a key-plus-empty-structuredProperties husk deletes the husk and retries,
// reporting the repair to the caller.
func TestCreateDomain_RepairsHusk(t *testing.T) {
	ts := &domainHuskTestServer{entityJSON: domainHuskEntityJSON()}
	srv := httptest.NewServer(ts.handler())
	defer srv.Close()

	c := newTestClient(t, srv)
	urn, repaired, err := c.CreateDomain(context.Background(), newDomainHuskInput())
	if err != nil {
		t.Fatalf("CreateDomain: %v", err)
	}
	if !repaired {
		t.Error("expected repairedHusk=true")
	}
	if urn != "urn:li:domain:tf-example-finance" {
		t.Errorf("unexpected urn %q", urn)
	}

	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.createCalls != 2 || ts.deleteCalls != 1 || ts.getCalls != 1 {
		t.Errorf("expected create/get/delete = 2/1/1, got %d/%d/%d", ts.createCalls, ts.getCalls, ts.deleteCalls)
	}
}

// TestCreateDomain_RealDomainNotTouched verifies a genuine pre-existing domain
// (domainProperties aspect present) is never deleted: the original error
// surfaces unchanged.
func TestCreateDomain_RealDomainNotTouched(t *testing.T) {
	entity := domainHuskEntityJSON()
	entity["domainProperties"] = map[string]any{
		"value": map[string]any{"name": "Real Domain", "description": "hands off"},
	}
	ts := &domainHuskTestServer{entityJSON: entity}
	srv := httptest.NewServer(ts.handler())
	defer srv.Close()

	c := newTestClient(t, srv)
	_, repaired, err := c.CreateDomain(context.Background(), newDomainHuskInput())
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected already-exists error, got %v", err)
	}
	if err.Error() != "DataHub API error: This Domain already exists!" {
		t.Errorf("expected the original error untouched, got %q", err.Error())
	}
	if repaired {
		t.Error("expected repairedHusk=false")
	}

	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.deleteCalls != 0 {
		t.Errorf("a real domain must never be deleted, got %d delete calls", ts.deleteCalls)
	}
	if ts.createCalls != 1 {
		t.Errorf("expected no create retry against a real domain, got %d create calls", ts.createCalls)
	}
}

// TestCreateDomain_NonEmptyPropertiesNotTouched verifies a domain with real
// structured property values does not qualify as a husk.
func TestCreateDomain_NonEmptyPropertiesNotTouched(t *testing.T) {
	entity := domainHuskEntityJSON()
	entity["structuredProperties"] = map[string]any{
		"value": map[string]any{"properties": []any{
			map[string]any{"propertyUrn": "urn:li:structuredProperty:x", "values": []any{map[string]any{"string": "v"}}},
		}},
	}
	ts := &domainHuskTestServer{entityJSON: entity}
	srv := httptest.NewServer(ts.handler())
	defer srv.Close()

	c := newTestClient(t, srv)
	_, repaired, err := c.CreateDomain(context.Background(), newDomainHuskInput())
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected already-exists error, got %v", err)
	}
	if repaired {
		t.Error("expected repairedHusk=false")
	}

	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.deleteCalls != 0 {
		t.Errorf("a domain with assigned values must never be deleted, got %d delete calls", ts.deleteCalls)
	}
}

// TestCreateDomain_UnexpectedAspectNotTouched verifies any aspect outside the
// husk allowlist disqualifies the domain.
func TestCreateDomain_UnexpectedAspectNotTouched(t *testing.T) {
	entity := domainHuskEntityJSON()
	entity["ownership"] = map[string]any{
		"value": map[string]any{"owners": []any{}},
	}
	ts := &domainHuskTestServer{entityJSON: entity}
	srv := httptest.NewServer(ts.handler())
	defer srv.Close()

	c := newTestClient(t, srv)
	_, repaired, err := c.CreateDomain(context.Background(), newDomainHuskInput())
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected already-exists error, got %v", err)
	}
	if repaired {
		t.Error("expected repairedHusk=false")
	}

	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.deleteCalls != 0 {
		t.Errorf("a domain with unexpected aspects must never be deleted, got %d delete calls", ts.deleteCalls)
	}
}

// TestCreateDomain_NameConflictNotTouched covers DataHub's other "already
// exists" failure: a sibling domain under the same parent already uses the
// name. That leaves this URN absent, so the husk read 404s, nothing is
// deleted, and the name-conflict message reaches the user verbatim.
func TestCreateDomain_NameConflictNotTouched(t *testing.T) {
	ts := &domainHuskTestServer{
		createAlways: true,
		createErrMsg: `"TF Example - Finance" already exists in this domain. Please pick a unique name.`,
	}
	srv := httptest.NewServer(ts.handler())
	defer srv.Close()

	c := newTestClient(t, srv)
	_, repaired, err := c.CreateDomain(context.Background(), newDomainHuskInput())
	if err == nil || !strings.Contains(err.Error(), "Please pick a unique name") {
		t.Fatalf("expected the name-conflict error, got %v", err)
	}
	if repaired {
		t.Error("expected repairedHusk=false")
	}

	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.deleteCalls != 0 {
		t.Errorf("a name conflict must never delete anything, got %d delete calls", ts.deleteCalls)
	}
	if ts.createCalls != 1 {
		t.Errorf("expected no create retry on a name conflict, got %d create calls", ts.createCalls)
	}
}

// TestCreateDomain_OtherErrorNoRepair verifies a create failure that is not
// "already exists" surfaces immediately without any husk inspection.
func TestCreateDomain_OtherErrorNoRepair(t *testing.T) {
	ts := &domainHuskTestServer{
		entityJSON:   domainHuskEntityJSON(),
		createErrMsg: "Unauthorized to perform this action.",
	}
	srv := httptest.NewServer(ts.handler())
	defer srv.Close()

	c := newTestClient(t, srv)
	_, repaired, err := c.CreateDomain(context.Background(), newDomainHuskInput())
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

// TestCreateDomain_HuskGoneBeforeCheck verifies the race where the blocking
// entity disappears between the failed create and the husk read: the original
// error surfaces and nothing is deleted.
func TestCreateDomain_HuskGoneBeforeCheck(t *testing.T) {
	ts := &domainHuskTestServer{
		entityJSON: domainHuskEntityJSON(),
		get404:     true,
	}
	srv := httptest.NewServer(ts.handler())
	defer srv.Close()

	c := newTestClient(t, srv)
	_, repaired, err := c.CreateDomain(context.Background(), newDomainHuskInput())
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

// TestCreateDomain_HuskDeleteFails verifies a failed husk removal surfaces
// both the original error and the repair failure, and does not retry the
// create.
func TestCreateDomain_HuskDeleteFails(t *testing.T) {
	ts := &domainHuskTestServer{
		entityJSON:  domainHuskEntityJSON(),
		deleteFails: true,
	}
	srv := httptest.NewServer(ts.handler())
	defer srv.Close()

	c := newTestClient(t, srv)
	_, repaired, err := c.CreateDomain(context.Background(), newDomainHuskInput())
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

// TestCreateDomain_InputValidationAndFields verifies the client-side guards
// and that optional fields ride along on the mutation input.
func TestCreateDomain_InputValidationAndFields(t *testing.T) {
	ts := &domainHuskTestServer{}
	srv := httptest.NewServer(ts.handler())
	defer srv.Close()

	c := newTestClient(t, srv)

	if _, _, err := c.CreateDomain(context.Background(), CreateDomainInput{Name: "x"}); err == nil {
		t.Error("expected error for missing id")
	}
	if _, _, err := c.CreateDomain(context.Background(), CreateDomainInput{ID: "x"}); err == nil {
		t.Error("expected error for missing name")
	}

	urn, repaired, err := c.CreateDomain(context.Background(), CreateDomainInput{
		ID:           "tf-example-finance",
		Name:         "TF Example - Finance",
		Description:  "with a description",
		ParentDomain: "urn:li:domain:parent",
	})
	if err != nil {
		t.Fatalf("CreateDomain: %v", err)
	}
	if repaired {
		t.Error("expected repairedHusk=false on a clean create")
	}
	if urn != "urn:li:domain:tf-example-finance" {
		t.Errorf("unexpected urn %q", urn)
	}
}
