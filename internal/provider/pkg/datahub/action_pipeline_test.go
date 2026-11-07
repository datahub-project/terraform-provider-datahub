// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahub

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestGetActionPipelineByID verifies the OpenAPI v3 read parse, including the
// nested config and a verbatim ${SECRET} recipe placeholder.
func TestGetActionPipelineByID(t *testing.T) {
	const id = "tf-example-dataplex-sync"
	body := `{
	  "urn": "urn:li:dataHubAction:` + id + `",
	  "dataHubActionKey": { "value": { "id": "` + id + `" } },
	  "dataHubActionInfo": { "value": {
	    "name": "TF Example",
	    "description": "sync glossary",
	    "type": "dataplex_metadata_sync",
	    "category": "Data Discovery",
	    "config": {
	      "recipe": "{\"action\":{\"type\":\"dataplex_metadata_sync\",\"config\":{\"token\":\"${SECRET_TOKEN}\"}}}",
	      "executorId": "default"
	    }
	  } }
	}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/openapi/v3/entity/datahubaction/") {
			_, _ = w.Write([]byte(body))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	info, err := newTestClient(t, server).GetActionPipelineByID(t.Context(), id)
	if err != nil {
		t.Fatalf("GetActionPipelineByID() error = %v", err)
	}
	if info == nil {
		t.Fatal("GetActionPipelineByID() = nil")
	}
	if info.ID != id || info.Name != "TF Example" || info.Type != "dataplex_metadata_sync" {
		t.Errorf("id/name/type = %q/%q/%q", info.ID, info.Name, info.Type)
	}
	if info.Category != "Data Discovery" || info.Description != "sync glossary" || info.ExecutorID != "default" {
		t.Errorf("category/description/executor = %q/%q/%q", info.Category, info.Description, info.ExecutorID)
	}
	if !strings.Contains(info.Recipe, "${SECRET_TOKEN}") {
		t.Errorf("recipe lost the ${SECRET_TOKEN} placeholder: %q", info.Recipe)
	}
}

// TestGetActionPipelineByID_NotFound verifies a 404 returns (nil, nil).
func TestGetActionPipelineByID_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.NotFound(w, nil)
	}))
	defer server.Close()

	info, err := newTestClient(t, server).GetActionPipelineByID(t.Context(), "missing")
	if err != nil {
		t.Fatalf("GetActionPipelineByID() error = %v", err)
	}
	if info != nil {
		t.Errorf("GetActionPipelineByID() = %+v, want nil for 404", info)
	}
}

// TestListActionPipelineURNs verifies enumeration via the listActionPipelines query.
func TestListActionPipelineURNs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"listActionPipelines":{"total":2,"actionPipelines":[
		  {"urn":"urn:li:dataHubAction:a"},{"urn":"urn:li:dataHubAction:b"}]}}}`))
	}))
	defer server.Close()

	urns, err := newTestClient(t, server).ListActionPipelineURNs(t.Context())
	if err != nil {
		t.Fatalf("ListActionPipelineURNs() error = %v", err)
	}
	if len(urns) != 2 || urns[0] != "urn:li:dataHubAction:a" || urns[1] != "urn:li:dataHubAction:b" {
		t.Errorf("urns = %v", urns)
	}
}

// TestUpsertActionPipeline_ExecutorID verifies config.executorId is always on
// the wire: upsertActionPipeline declares it optional but fails with a
// NullPointerException when it is omitted, so an empty ExecutorID is sent as
// the default executor and an explicit one is sent unchanged.
func TestUpsertActionPipeline_ExecutorID(t *testing.T) {
	for _, tc := range []struct {
		name, in, want string
	}{
		{name: "unset sends default", in: "", want: ActionPipelineDefaultExecutorID},
		{name: "explicit is kept", in: "remote-pool", want: "remote-pool"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var sent map[string]any
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					Variables struct {
						Input struct {
							Config map[string]any `json:"config"`
						} `json:"input"`
					} `json:"variables"`
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Errorf("decoding request: %v", err)
				}
				sent = req.Variables.Input.Config
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"data":{"upsertActionPipeline":"urn:li:dataHubAction:x"}}`))
			}))
			defer server.Close()

			_, err := newTestClient(t, server).UpsertActionPipeline(t.Context(), ActionPipelineInput{
				ActionID:   "x",
				Name:       "X",
				Type:       "dataplex_metadata_sync",
				Recipe:     "{}",
				ExecutorID: tc.in,
			})
			if err != nil {
				t.Fatalf("UpsertActionPipeline() error = %v", err)
			}
			if got, ok := sent["executorId"]; !ok || got != tc.want {
				t.Errorf("config.executorId = %v (present %t), want %q", got, ok, tc.want)
			}
		})
	}
}
