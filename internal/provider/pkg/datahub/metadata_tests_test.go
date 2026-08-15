// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahub

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestGetMetadataTestByID verifies the OpenAPI v3 read parse, including
// tolerance of the Cloud-only server-managed fields (definition.md5, audit
// stamps) that appear alongside the fields the provider manages.
func TestGetMetadataTestByID(t *testing.T) {
	const id = "tf-test-ownership"
	body := `{
	  "urn": "urn:li:test:` + id + `",
	  "testKey": { "value": { "id": "` + id + `" } },
	  "testInfo": { "value": {
	    "name": "Dataset Ownership",
	    "category": "Governance",
	    "description": "Every dataset must have an owner.",
	    "definition": {
	      "type": "JSON",
	      "json": "{\"on\":{\"types\":[\"dataset\"]},\"rules\":{}}",
	      "md5": "0123456789abcdef"
	    },
	    "lastUpdatedTimestamp": 1755200000000,
	    "created": { "time": 1755200000000, "actor": "urn:li:corpuser:someone" }
	  } }
	}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/openapi/v3/entity/test/") {
			_, _ = w.Write([]byte(body))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	info, err := newTestClient(t, server).GetMetadataTestByID(t.Context(), id)
	if err != nil {
		t.Fatalf("GetMetadataTestByID() error = %v", err)
	}
	if info == nil {
		t.Fatal("GetMetadataTestByID() = nil")
	}
	if info.ID != id || info.Name != "Dataset Ownership" || info.Category != "Governance" {
		t.Errorf("id/name/category = %q/%q/%q", info.ID, info.Name, info.Category)
	}
	if info.Description != "Every dataset must have an owner." {
		t.Errorf("description = %q", info.Description)
	}
	if info.DefinitionJSON != `{"on":{"types":["dataset"]},"rules":{}}` {
		t.Errorf("definition = %q", info.DefinitionJSON)
	}
}

// TestGetMetadataTestByID_NotFound verifies a 404 returns (nil, nil).
func TestGetMetadataTestByID_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.NotFound(w, nil)
	}))
	defer server.Close()

	info, err := newTestClient(t, server).GetMetadataTestByID(t.Context(), "missing")
	if err != nil {
		t.Fatalf("GetMetadataTestByID() error = %v", err)
	}
	if info != nil {
		t.Errorf("GetMetadataTestByID() = %+v, want nil for 404", info)
	}
}

// TestGetMetadataTestByID_Husk verifies an entity response with neither
// testKey nor testInfo aspects (a bare URN echo) reads as absent.
func TestGetMetadataTestByID_Husk(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"urn":"urn:li:test:husk"}`))
	}))
	defer server.Close()

	info, err := newTestClient(t, server).GetMetadataTestByID(t.Context(), "husk")
	if err != nil {
		t.Fatalf("GetMetadataTestByID() error = %v", err)
	}
	if info != nil {
		t.Errorf("GetMetadataTestByID() = %+v, want nil for aspect-less entity", info)
	}
}

// TestCreateMetadataTest verifies the createTest mutation payload: the id is
// carried in the input (URN determinism), description is omitted when empty,
// and the definition rides in definition.json.
func TestCreateMetadataTest(t *testing.T) {
	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		_ = json.Unmarshal(body, &req)
		if !strings.Contains(req.Query, "createTest(") {
			t.Errorf("unexpected query: %s", req.Query)
		}
		captured, _ = req.Variables["input"].(map[string]any)
		_, _ = w.Write([]byte(`{"data":{"createTest":"urn:li:test:tf-test-1"}}`))
	}))
	defer server.Close()

	urn, err := newTestClient(t, server).CreateMetadataTest(t.Context(), MetadataTestInput{
		TestID:         "tf-test-1",
		Name:           "T1",
		Category:       "Governance",
		DefinitionJSON: `{"on":{},"rules":{}}`,
	})
	if err != nil {
		t.Fatalf("CreateMetadataTest() error = %v", err)
	}
	if urn != "urn:li:test:tf-test-1" {
		t.Errorf("urn = %q", urn)
	}
	if captured["id"] != "tf-test-1" || captured["name"] != "T1" || captured["category"] != "Governance" {
		t.Errorf("input = %v", captured)
	}
	if _, hasDesc := captured["description"]; hasDesc {
		t.Errorf("empty description should be omitted, input = %v", captured)
	}
	def, _ := captured["definition"].(map[string]any)
	if def["json"] != `{"on":{},"rules":{}}` {
		t.Errorf("definition = %v", def)
	}
}

// TestCreateMetadataTest_Duplicate verifies the server's duplicate-id error
// surfaces to the caller.
func TestCreateMetadataTest_Duplicate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"errors":[{"message":"This Test already exists!"}]}`))
	}))
	defer server.Close()

	_, err := newTestClient(t, server).CreateMetadataTest(t.Context(), MetadataTestInput{
		TestID: "dup", Name: "n", Category: "c", DefinitionJSON: "{}",
	})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected duplicate error, got %v", err)
	}
}

// TestUpdateMetadataTest verifies the updateTest mutation targets the
// deterministic URN and carries the full replacement input.
func TestUpdateMetadataTest(t *testing.T) {
	var capturedURN string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		_ = json.Unmarshal(body, &req)
		capturedURN, _ = req.Variables["urn"].(string)
		_, _ = w.Write([]byte(`{"data":{"updateTest":"urn:li:test:tf-test-1"}}`))
	}))
	defer server.Close()

	err := newTestClient(t, server).UpdateMetadataTest(t.Context(), "tf-test-1", MetadataTestInput{
		Name: "T1", Category: "Governance", DefinitionJSON: "{}",
	})
	if err != nil {
		t.Fatalf("UpdateMetadataTest() error = %v", err)
	}
	if capturedURN != "urn:li:test:tf-test-1" {
		t.Errorf("urn = %q", capturedURN)
	}
}

// TestListMetadataTestURNs verifies pagination over listTests.
func TestListMetadataTestURNs(t *testing.T) {
	call := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		call++
		if call == 1 {
			page := make([]string, 0, 100)
			for range 100 {
				page = append(page, `{"urn":"urn:li:test:a"}`)
			}
			_, _ = w.Write([]byte(`{"data":{"listTests":{"total":101,"tests":[` + strings.Join(page, ",") + `]}}}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":{"listTests":{"total":101,"tests":[{"urn":"urn:li:test:b"}]}}}`))
	}))
	defer server.Close()

	urns, err := newTestClient(t, server).ListMetadataTestURNs(t.Context())
	if err != nil {
		t.Fatalf("ListMetadataTestURNs() error = %v", err)
	}
	if len(urns) != 101 || urns[100] != "urn:li:test:b" {
		t.Errorf("urns len=%d last=%q", len(urns), urns[len(urns)-1])
	}
	if call != 2 {
		t.Errorf("expected 2 pages, got %d", call)
	}
}

// TestValidateMetadataTestDefinition covers the three validateTest outcomes:
// valid, invalid-with-messages, and the OSS FieldUndefined error mapping to
// ErrMetadataTestValidationCloudOnly.
func TestValidateMetadataTestDefinition(t *testing.T) {
	cases := []struct {
		name      string
		response  string
		wantValid bool
		wantMsgs  int
		wantErr   error
	}{
		{
			name:      "valid",
			response:  `{"data":{"validateTest":{"isValid":true}}}`,
			wantValid: true,
		},
		{
			name:     "invalid with messages",
			response: `{"data":{"validateTest":{"isValid":false,"messages":["Unknown operator 'exits'"]}}}`,
			wantMsgs: 1,
		},
		{
			name: "oss field undefined",
			response: `{"errors":[{"message":"Validation error (FieldUndefined@[validateTest]) : ` +
				`Field 'validateTest' in type 'Query' is undefined"}]}`,
			wantErr: ErrMetadataTestValidationCloudOnly,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.response))
			}))
			defer server.Close()

			valid, msgs, err := newTestClient(t, server).ValidateMetadataTestDefinition(t.Context(), "{}")
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("error = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ValidateMetadataTestDefinition() error = %v", err)
			}
			if valid != tc.wantValid {
				t.Errorf("valid = %v, want %v", valid, tc.wantValid)
			}
			if len(msgs) != tc.wantMsgs {
				t.Errorf("messages = %v, want %d", msgs, tc.wantMsgs)
			}
		})
	}
}
