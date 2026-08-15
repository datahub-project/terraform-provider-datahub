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

// TestIsMetadataTestValidationCloudOnlyError pins the OSS detector against
// realistic graphql-java message shapes. The matcher gates whether an OSS user
// sees "requires DataHub Cloud" or a raw API error, and it was written from
// graphql-java's documented format rather than observed live -- so both match
// directions need locking: the true cases catch a tightening that misses the
// real message, and the near-misses catch a loosening that would misreport an
// unrelated failure (bad definition, missing privilege) as "you need Cloud",
// hiding the actual problem from a Cloud user.
func TestIsMetadataTestValidationCloudOnlyError(t *testing.T) {
	cases := []struct {
		name string
		msg  string
		want bool
	}{
		{
			// graphql-java >= 17 (current OSS DataHub) validation format.
			name: "modern graphql-java FieldUndefined",
			msg:  "Validation error (FieldUndefined@[validateTest]) : Field 'validateTest' in type 'Query' is undefined",
			want: true,
		},
		{
			// graphql-java < 17 format, in case the target runs an older server.
			name: "legacy graphql-java FieldUndefined",
			msg:  "Validation error of type FieldUndefined: Field 'validateTest' in type 'Query' is undefined @ 'validateTest'",
			want: true,
		},
		{
			// FieldUndefined for some other query must not read as Cloud-only.
			name: "FieldUndefined for unrelated field",
			msg:  "Validation error (FieldUndefined@[globalSettings]) : Field 'globalSettings' in type 'Query' is undefined",
			want: false,
		},
		{
			// A resolver failure mentioning the field name is not a schema gap.
			name: "resolver error naming validateTest",
			msg:  "An unknown error occurred while resolving field validateTest",
			want: false,
		},
		{
			// The most common real-world failure on Cloud: missing privilege.
			name: "authorization error",
			msg:  "Unauthorized to perform this action. Please contact your DataHub administrator.",
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isMetadataTestValidationCloudOnlyError(tc.msg); got != tc.want {
				t.Errorf("isMetadataTestValidationCloudOnlyError(%q) = %v, want %v", tc.msg, got, tc.want)
			}
		})
	}
}

// TestCreateMetadataTest_URNFallback verifies that when the createTest
// response carries an empty URN, the client falls back to constructing the
// deterministic urn:li:test:<id>. Without the fallback an empty URN would be
// written to state silently, breaking every downstream reference to .urn.
func TestCreateMetadataTest_URNFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"createTest":""}}`))
	}))
	defer server.Close()

	urn, err := newTestClient(t, server).CreateMetadataTest(t.Context(), MetadataTestInput{
		TestID: "fallback", Name: "n", Category: "c", DefinitionJSON: "{}",
	})
	if err != nil {
		t.Fatalf("CreateMetadataTest() error = %v", err)
	}
	if urn != "urn:li:test:fallback" {
		t.Errorf("urn = %q, want constructed fallback urn:li:test:fallback", urn)
	}
}

// TestDeleteMetadataTest_Tolerance verifies both directions of the
// already-absent tolerance: a not-found style error is treated as success (so
// destroying a test deleted out-of-band cannot wedge terraform destroy), and
// any other error still surfaces (so the tolerance cannot swallow a real
// failure, e.g. missing MANAGE_TESTS, and drop state while the entity lives).
func TestDeleteMetadataTest_Tolerance(t *testing.T) {
	cases := []struct {
		name    string
		message string
		wantErr bool
	}{
		{name: "not found tolerated", message: "Failed to perform delete against Test with urn urn:li:test:x: not found", wantErr: false},
		{name: "does not exist tolerated", message: "Entity urn:li:test:x does not exist", wantErr: false},
		{name: "authorization error surfaces", message: "Unauthorized to perform this action. Please contact your DataHub administrator.", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				resp, _ := json.Marshal(map[string]any{
					"errors": []map[string]string{{"message": tc.message}},
				})
				_, _ = w.Write(resp)
			}))
			defer server.Close()

			err := newTestClient(t, server).DeleteMetadataTest(t.Context(), "x")
			if tc.wantErr && (err == nil || !strings.Contains(err.Error(), tc.message)) {
				t.Errorf("expected error containing %q, got %v", tc.message, err)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("expected tolerated delete, got error %v", err)
			}
		})
	}
}

// TestGetMetadataTestByID_HTTPErrors verifies the diagnostics on the two
// error classes a user actually hits on the read path: a 403 must name the
// MANAGE_TESTS privilege (the fix), and any other failure must carry the
// response body rather than discarding it.
func TestGetMetadataTestByID_HTTPErrors(t *testing.T) {
	cases := []struct {
		name       string
		status     int
		body       string
		wantSubstr string
	}{
		{name: "forbidden names privilege", status: http.StatusForbidden, body: `{}`, wantSubstr: "MANAGE_TESTS"},
		{name: "server error carries body", status: http.StatusInternalServerError, body: `upstream exploded`, wantSubstr: "upstream exploded"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()

			_, err := newTestClient(t, server).GetMetadataTestByID(t.Context(), "x")
			if err == nil || !strings.Contains(err.Error(), tc.wantSubstr) {
				t.Errorf("error = %v, want it to contain %q", err, tc.wantSubstr)
			}
		})
	}
}

// TestValidateMetadataTestDefinition covers the three validateTest outcomes:
// valid, invalid-with-messages, and the OSS FieldUndefined error mapping to
// ErrMetadataTestValidationCloudOnly.
func TestValidateMetadataTestDefinition(t *testing.T) {
	cases := []struct {
		name          string
		response      string
		wantValid     bool
		wantMsgs      int
		wantErr       error
		wantErrSubstr string
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
		{
			// An unrelated GraphQL error (here: missing privilege) must surface
			// as a plain API error, not be misread as "requires DataHub Cloud".
			name:          "unrelated graphql error not mapped to cloud-only",
			response:      `{"errors":[{"message":"Unauthorized to perform this action. Please contact your DataHub administrator."}]}`,
			wantErrSubstr: "Unauthorized to perform this action",
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
			if tc.wantErrSubstr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErrSubstr) {
					t.Fatalf("error = %v, want it to contain %q", err, tc.wantErrSubstr)
				}
				if errors.Is(err, ErrMetadataTestValidationCloudOnly) {
					t.Fatalf("error = %v was wrongly mapped to ErrMetadataTestValidationCloudOnly", err)
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
