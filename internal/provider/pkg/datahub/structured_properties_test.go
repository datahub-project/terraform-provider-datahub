// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahub

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// structuredPropertyMockServer stores the propertyDefinition value from the
// last POST and serves it on GET. A nil stored value means the entity does not
// exist.
func structuredPropertyMockServer(t *testing.T, urn string, stored *map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			var payload []struct {
				Def struct {
					Value map[string]any `json:"value"`
				} `json:"propertyDefinition"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Errorf("decoding POST payload: %v", err)
			}
			*stored = payload[0].Def.Value
			_, _ = w.Write([]byte("[]"))
		case http.MethodGet:
			if *stored == nil {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"urn":                urn,
				"propertyDefinition": map[string]any{"value": *stored},
			})
		}
	}))
}

func TestUpdateStructuredPropertyPreservesUnmanagedFields(t *testing.T) {
	const urn = "urn:li:structuredProperty:io.acme.retention"
	allowedPlatforms := []any{"urn:li:dataPlatform:snowflake"}
	searchConfiguration := map[string]any{"searchable": true}
	created := map[string]any{"time": float64(1700000000000), "actor": "urn:li:corpuser:admin"}

	stored := map[string]any{
		"qualifiedName":       "io.acme.retention",
		"valueType":           "urn:li:dataType:datahub.urn",
		"entityTypes":         []any{"urn:li:entityType:datahub.dataset"},
		"cardinality":         "SINGLE",
		"immutable":           false,
		"version":             "20240610120000",
		"displayName":         "Retention",
		"description":         "old",
		"allowedValues":       []any{map[string]any{"value": map[string]any{"string": "a"}}},
		"typeQualifier":       map[string]any{"allowedTypes": []any{"urn:li:entityType:datahub.corpuser"}},
		"allowedPlatforms":    allowedPlatforms,
		"searchConfiguration": searchConfiguration,
		"created":             created,
		"lastModified":        created,
	}
	server := structuredPropertyMockServer(t, urn, &stored)
	defer server.Close()
	c := newTestClient(t, server)

	in := CreateStructuredPropertyInput{
		ID:          "io.acme.retention",
		ValueType:   "urn",
		Cardinality: "MULTIPLE",
		EntityTypes: []string{"dataset", "chart"},
	}
	if err := c.UpdateStructuredProperty(t.Context(), urn, in); err != nil {
		t.Fatalf("UpdateStructuredProperty() error = %v", err)
	}

	if stored["cardinality"] != "MULTIPLE" {
		t.Errorf("cardinality = %v, want MULTIPLE", stored["cardinality"])
	}
	if got, want := mustJSON(t, stored["entityTypes"]), mustJSON(t, []string{"urn:li:entityType:datahub.dataset", "urn:li:entityType:datahub.chart"}); got != want {
		t.Errorf("entityTypes = %s, want %s", got, want)
	}
	for _, k := range []string{"version", "displayName", "description", "allowedValues", "typeQualifier", "lastModified"} {
		if _, ok := stored[k]; ok {
			t.Errorf("%s = %v, want it cleared", k, stored[k])
		}
	}
	for k, want := range map[string]any{
		"allowedPlatforms":    allowedPlatforms,
		"searchConfiguration": searchConfiguration,
		"created":             created,
	} {
		if got := mustJSON(t, stored[k]); got != mustJSON(t, want) {
			t.Errorf("%s = %s, want it preserved as %s", k, got, mustJSON(t, want))
		}
	}
}

func TestCreateStructuredPropertyWhenAbsent(t *testing.T) {
	const urn = "urn:li:structuredProperty:io.acme.retention"
	var stored map[string]any
	server := structuredPropertyMockServer(t, urn, &stored)
	defer server.Close()
	c := newTestClient(t, server)

	got, err := c.CreateStructuredProperty(t.Context(), CreateStructuredPropertyInput{
		ID:          "io.acme.retention",
		ValueType:   "number",
		EntityTypes: []string{"dataset"},
		Description: "desc",
	})
	if err != nil {
		t.Fatalf("CreateStructuredProperty() error = %v", err)
	}
	if got != urn {
		t.Errorf("urn = %q, want %q", got, urn)
	}
	if stored["qualifiedName"] != "io.acme.retention" || stored["description"] != "desc" {
		t.Errorf("stored = %v, want qualifiedName and description written", stored)
	}
	if _, ok := stored["allowedPlatforms"]; ok {
		t.Errorf("allowedPlatforms = %v, want absent on create", stored["allowedPlatforms"])
	}
}
