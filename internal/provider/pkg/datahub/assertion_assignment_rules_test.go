// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahub

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestGetAssertionAssignmentRuleByURN verifies the OpenAPI v3 read parse: the
// structured filter, the free-text query, and an enabled category config with
// its source type and incident actions.
func TestGetAssertionAssignmentRuleByURN(t *testing.T) {
	const urn = "urn:li:assertionAssignmentRule:tf-rule"
	body := `{
	  "urn": "` + urn + `",
	  "assertionAssignmentRuleKey": { "value": { "id": "tf-rule" } },
	  "assertionAssignmentRuleInfo": { "value": {
	    "mode": "ENABLED",
	    "name": "TF Rule",
	    "entityFilter": {
	      "json": "*",
	      "filter": { "or": [ { "and": [
	        { "field": "platform", "values": ["urn:li:dataPlatform:postgres"], "condition": "EQUAL", "negated": false }
	      ] } ] }
	    },
	    "freshnessConfig": {
	      "enabled": true,
	      "preferredEvaluationParameters": { "sourceType": "INFORMATION_SCHEMA" },
	      "onFailure": [ { "type": "RAISE_INCIDENT" } ]
	    },
	    "volumeConfig": { "enabled": false }
	  } }
	}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/openapi/v3/entity/assertionassignmentrule/") {
			_, _ = w.Write([]byte(body))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	info, err := newTestClient(t, server).GetAssertionAssignmentRuleByURN(t.Context(), urn)
	if err != nil {
		t.Fatalf("GetAssertionAssignmentRuleByURN() error = %v", err)
	}
	if info == nil {
		t.Fatal("GetAssertionAssignmentRuleByURN() = nil")
	}
	if info.ID != "tf-rule" || info.Name != "TF Rule" || info.Mode != "ENABLED" || info.Query != "*" {
		t.Errorf("id/name/mode/query = %q/%q/%q/%q", info.ID, info.Name, info.Mode, info.Query)
	}
	if len(info.OrFilters) != 1 || len(info.OrFilters[0].And) != 1 {
		t.Fatalf("or_filters shape = %+v", info.OrFilters)
	}
	f := info.OrFilters[0].And[0]
	if f.Field != "platform" || f.Condition != "EQUAL" || len(f.Values) != 1 {
		t.Errorf("facet = %+v", f)
	}
	if info.Freshness == nil || info.Freshness.SourceType != "INFORMATION_SCHEMA" ||
		len(info.Freshness.OnFailureActions) != 1 || info.Freshness.OnFailureActions[0] != "RAISE_INCIDENT" {
		t.Errorf("freshness = %+v", info.Freshness)
	}
	// volumeConfig.enabled = false must read back as an absent (nil) config.
	if info.Volume != nil {
		t.Errorf("volume = %+v, want nil for disabled config", info.Volume)
	}
}

// TestGetAssertionAssignmentRuleByURN_NotFound verifies a 404 returns (nil, nil).
func TestGetAssertionAssignmentRuleByURN_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.NotFound(w, nil)
	}))
	defer server.Close()

	info, err := newTestClient(t, server).GetAssertionAssignmentRuleByURN(t.Context(), "urn:li:assertionAssignmentRule:missing")
	if err != nil {
		t.Fatalf("GetAssertionAssignmentRuleByURN() error = %v", err)
	}
	if info != nil {
		t.Errorf("GetAssertionAssignmentRuleByURN() = %+v, want nil for 404", info)
	}
}

// TestListAssertionAssignmentRuleURNs verifies enumeration via the
// listAssertionAssignmentRules query.
func TestListAssertionAssignmentRuleURNs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"listAssertionAssignmentRules":{"total":2,"rules":[
		  {"urn":"urn:li:assertionAssignmentRule:a"},{"urn":"urn:li:assertionAssignmentRule:b"}]}}}`))
	}))
	defer server.Close()

	urns, err := newTestClient(t, server).ListAssertionAssignmentRuleURNs(t.Context())
	if err != nil {
		t.Fatalf("ListAssertionAssignmentRuleURNs() error = %v", err)
	}
	if len(urns) != 2 || urns[0] != "urn:li:assertionAssignmentRule:a" || urns[1] != "urn:li:assertionAssignmentRule:b" {
		t.Errorf("urns = %v", urns)
	}
}

// TestListAssertionAssignmentRuleURNs_CloudOnly verifies that a
// FieldUndefined-on-Query GraphQL error (the OSS shape) is mapped to the
// sentinel ErrAssertionAssignmentRuleCloudOnly.
func TestListAssertionAssignmentRuleURNs_CloudOnly(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"errors":[{"message":"Validation error (FieldUndefined@[listAssertionAssignmentRules]) : Field 'listAssertionAssignmentRules' in type 'Query' is undefined"}]}`))
	}))
	defer server.Close()

	_, err := newTestClient(t, server).ListAssertionAssignmentRuleURNs(t.Context())
	if !errors.Is(err, ErrAssertionAssignmentRuleCloudOnly) {
		t.Errorf("error = %v, want ErrAssertionAssignmentRuleCloudOnly", err)
	}
}
