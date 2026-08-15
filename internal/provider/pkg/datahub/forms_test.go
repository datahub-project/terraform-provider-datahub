// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahub

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestGetFormByURN verifies the OpenAPI v3 read parse: key, formInfo (prompts,
// actors) and the dynamicFormAssignment filter, all from one entity response.
func TestGetFormByURN(t *testing.T) {
	const id = "pii-audit"
	body := `{
	  "urn": "urn:li:form:` + id + `",
	  "formKey": { "value": { "id": "` + id + `" } },
	  "formInfo": { "value": {
	    "name": "PII Audit",
	    "description": "Quarterly PII review",
	    "type": "VERIFICATION",
	    "prompts": [ {
	      "id": "pii-audit-classification",
	      "title": "Classify this dataset",
	      "type": "STRUCTURED_PROPERTY",
	      "structuredPropertyParams": { "urn": "urn:li:structuredProperty:classification" },
	      "required": true
	    } ],
	    "actors": { "owners": false, "users": ["urn:li:corpuser:jdoe"] }
	  } },
	  "dynamicFormAssignment": { "value": { "filter": { "or": [ { "and": [ {
	    "field": "platform.keyword",
	    "values": ["urn:li:dataPlatform:snowflake"],
	    "condition": "EQUAL",
	    "negated": false
	  } ] } ] } } }
	}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/openapi/v3/entity/form/") {
			_, _ = w.Write([]byte(body))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	form, err := newTestClient(t, server).GetFormByURN(t.Context(), "urn:li:form:"+id)
	if err != nil {
		t.Fatalf("GetFormByURN() error = %v", err)
	}
	if form == nil {
		t.Fatal("GetFormByURN() = nil")
	}
	if form.ID != id || form.Name != "PII Audit" || form.Type != "VERIFICATION" {
		t.Errorf("id/name/type = %q/%q/%q", form.ID, form.Name, form.Type)
	}
	if len(form.Prompts) != 1 {
		t.Fatalf("prompts = %d, want 1", len(form.Prompts))
	}
	p := form.Prompts[0]
	if p.ID != "pii-audit-classification" || !p.Required ||
		p.StructuredPropertyURN != "urn:li:structuredProperty:classification" {
		t.Errorf("prompt = %+v", p)
	}
	if form.Actors == nil || form.Actors.Owners || len(form.Actors.Users) != 1 {
		t.Errorf("actors = %+v", form.Actors)
	}
	if len(form.OrFilters) != 1 || len(form.OrFilters[0].And) != 1 {
		t.Fatalf("orFilters = %+v", form.OrFilters)
	}
	f := form.OrFilters[0].And[0]
	if f.Field != "platform.keyword" || f.Condition != "EQUAL" ||
		len(f.Values) != 1 || f.Values[0] != "urn:li:dataPlatform:snowflake" {
		t.Errorf("filter = %+v", f)
	}
}

// TestGetFormByURN_NotFound verifies a 404 returns (nil, nil).
func TestGetFormByURN_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.NotFound(w, nil)
	}))
	defer server.Close()

	form, err := newTestClient(t, server).GetFormByURN(t.Context(), "urn:li:form:missing")
	if err != nil {
		t.Fatalf("GetFormByURN() error = %v", err)
	}
	if form != nil {
		t.Errorf("GetFormByURN() = %+v, want nil for 404", form)
	}
}

// TestUpsertForm_InputShape verifies the createForm variables: the id is always
// sent (deterministic URN), prompts always present (an empty list clears them,
// since the aspect write is a full replace), params nested under
// structuredPropertyParams, and actors omitted when nil so the server default
// {owners: true} applies.
func TestUpsertForm_InputShape(t *testing.T) {
	var gotInput map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		raw, _ := io.ReadAll(r.Body)
		var req struct {
			Variables struct {
				Input map[string]any `json:"input"`
			} `json:"variables"`
		}
		_ = json.Unmarshal(raw, &req)
		gotInput = req.Variables.Input
		_, _ = w.Write([]byte(`{"data":{"createForm":{"urn":"urn:li:form:pii-audit"}}}`))
	}))
	defer server.Close()

	urn, err := newTestClient(t, server).UpsertForm(t.Context(), FormInput{
		ID:   "pii-audit",
		Name: "PII Audit",
		Type: "VERIFICATION",
		Prompts: []FormPrompt{{
			ID:                    "p1",
			Title:                 "Classify",
			Type:                  "STRUCTURED_PROPERTY",
			StructuredPropertyURN: "urn:li:structuredProperty:classification",
			Required:              true,
		}},
	})
	if err != nil {
		t.Fatalf("UpsertForm() error = %v", err)
	}
	if urn != "urn:li:form:pii-audit" {
		t.Errorf("urn = %q", urn)
	}
	if gotInput["id"] != "pii-audit" || gotInput["type"] != "VERIFICATION" {
		t.Errorf("input id/type = %v/%v", gotInput["id"], gotInput["type"])
	}
	if _, hasActors := gotInput["actors"]; hasActors {
		t.Error("actors sent despite nil input; server default would be overridden")
	}
	prompts, ok := gotInput["prompts"].([]any)
	if !ok || len(prompts) != 1 {
		t.Fatalf("prompts = %v", gotInput["prompts"])
	}
	pm, _ := prompts[0].(map[string]any)
	params, _ := pm["structuredPropertyParams"].(map[string]any)
	if params["urn"] != "urn:li:structuredProperty:classification" {
		t.Errorf("structuredPropertyParams = %v", pm["structuredPropertyParams"])
	}
	if pm["required"] != true {
		t.Errorf("required = %v", pm["required"])
	}
}

// TestUpsertForm_EmptyPromptsClears verifies prompts is present-and-empty when
// the input has none, so an upsert clears prompts rather than leaving them.
func TestUpsertForm_EmptyPromptsClears(t *testing.T) {
	var gotInput map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		raw, _ := io.ReadAll(r.Body)
		var req struct {
			Variables struct {
				Input map[string]any `json:"input"`
			} `json:"variables"`
		}
		_ = json.Unmarshal(raw, &req)
		gotInput = req.Variables.Input
		_, _ = w.Write([]byte(`{"data":{"createForm":{"urn":"urn:li:form:x"}}}`))
	}))
	defer server.Close()

	if _, err := newTestClient(t, server).UpsertForm(t.Context(), FormInput{ID: "x", Name: "X"}); err != nil {
		t.Fatalf("UpsertForm() error = %v", err)
	}
	prompts, present := gotInput["prompts"]
	if !present {
		t.Fatal("prompts key absent; an upsert without it cannot clear existing prompts")
	}
	if list, ok := prompts.([]any); !ok || len(list) != 0 {
		t.Errorf("prompts = %v, want empty list", prompts)
	}
}

// graphQLErrorServer returns a server that answers every request with an HTTP
// 200 carrying a GraphQL errors array -- the shape DataHub uses for privilege
// and validation failures.
func graphQLErrorServer(t *testing.T, msg string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"errors":[{"message":"` + msg + `"}]}`))
	}))
	t.Cleanup(server.Close)
	return server
}

// TestUpsertForm_GraphQLErrorSurfaced pins the nastiest GraphQL failure shape:
// HTTP 200 with an errors array (e.g. the caller lacks MANAGE_FORMS). If the
// errors check were lost, the create would report success while writing
// nothing, and the next Read would remove the resource from state.
func TestUpsertForm_GraphQLErrorSurfaced(t *testing.T) {
	server := graphQLErrorServer(t, "Unauthorized to perform this action")

	_, err := newTestClient(t, server).UpsertForm(t.Context(), FormInput{ID: "x", Name: "X"})
	if err == nil {
		t.Fatal("UpsertForm() = nil error on a GraphQL errors response; a failed write would look like success")
	}
	if !strings.Contains(err.Error(), "Unauthorized to perform this action") {
		t.Errorf("error %q does not carry the server message", err)
	}
}

// TestUpsertDynamicFormAssignment_GraphQLErrorSurfaced covers the same silent
// success class for the assignment mutation.
func TestUpsertDynamicFormAssignment_GraphQLErrorSurfaced(t *testing.T) {
	server := graphQLErrorServer(t, "Form urn:li:form:x does not exist")

	err := newTestClient(t, server).UpsertDynamicFormAssignment(t.Context(), "urn:li:form:x",
		[]AndFilter{{And: []FacetFilter{{Field: "platform.keyword", Values: []string{"v"}}}}})
	if err == nil {
		t.Fatal("UpsertDynamicFormAssignment() = nil error on a GraphQL errors response")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("error %q does not carry the server message", err)
	}
}

// TestDeleteForm_NotFoundTolerated verifies destroying an already-deleted form
// succeeds. Without the tolerance a user whose form was removed out-of-band
// could never complete a destroy.
func TestDeleteForm_NotFoundTolerated(t *testing.T) {
	for _, msg := range []string{
		"Failed to delete: entity not found",
		"Form urn:li:form:x does not exist",
	} {
		server := graphQLErrorServer(t, msg)
		if err := newTestClient(t, server).DeleteForm(t.Context(), "urn:li:form:x"); err != nil {
			t.Errorf("DeleteForm() with %q = %v, want nil (already gone is success)", msg, err)
		}
	}
}

// TestDeleteForm_OtherErrorSurfaced is the boundary of that tolerance: any
// other failure must surface, or a destroy would report success while the form
// keeps assigning itself to entities.
func TestDeleteForm_OtherErrorSurfaced(t *testing.T) {
	server := graphQLErrorServer(t, "Unauthorized to perform this action")

	err := newTestClient(t, server).DeleteForm(t.Context(), "urn:li:form:x")
	if err == nil {
		t.Fatal("DeleteForm() = nil error; a failed delete would look like success")
	}
	if !strings.Contains(err.Error(), "Unauthorized") {
		t.Errorf("error %q does not carry the server message", err)
	}
}

// TestGetFormByURN_ServerErrorIsNotAbsent pins that a 5xx is an error, not
// (nil, nil): treating a transient failure as "gone" would make Read remove
// the resource from state and the next apply recreate it.
func TestGetFormByURN_ServerErrorIsNotAbsent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "opensearch unavailable", http.StatusInternalServerError)
	}))
	defer server.Close()

	form, err := newTestClient(t, server).GetFormByURN(t.Context(), "urn:li:form:x")
	if err == nil {
		t.Fatal("GetFormByURN() = nil error on HTTP 500; the resource would be dropped from state")
	}
	if form != nil {
		t.Errorf("form = %+v, want nil alongside the error", form)
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error %q does not carry the status code", err)
	}
}

// TestGetFormByURN_HuskIsAbsent verifies an entity response carrying neither
// owned aspect (the CAT-2583 husk shape: a URN with only status/system
// aspects) is treated as absent. Parsing it as a form would clobber state with
// empty name and prompts instead of flagging the entity for recreation.
func TestGetFormByURN_HuskIsAbsent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"urn":"urn:li:form:husk"}`))
	}))
	defer server.Close()

	form, err := newTestClient(t, server).GetFormByURN(t.Context(), "urn:li:form:husk")
	if err != nil {
		t.Fatalf("GetFormByURN() error = %v", err)
	}
	if form != nil {
		t.Errorf("GetFormByURN(husk) = %+v, want nil", form)
	}
}

// TestGetFormByURN_MissingKeyDerivesIDFromURN verifies the id falls back to
// the URN suffix when the response carries formInfo but no formKey, which the
// entity endpoint is free to do (the key aspect is reconstructible).
func TestGetFormByURN_MissingKeyDerivesIDFromURN(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"urn":"urn:li:form:keyless","formInfo":{"value":{"name":"Keyless"}}}`))
	}))
	defer server.Close()

	form, err := newTestClient(t, server).GetFormByURN(t.Context(), "urn:li:form:keyless")
	if err != nil {
		t.Fatalf("GetFormByURN() error = %v", err)
	}
	if form == nil {
		t.Fatal("GetFormByURN() = nil")
	}
	if form.ID != "keyless" {
		t.Errorf("ID = %q, want %q (derived from the URN)", form.ID, "keyless")
	}
}

// TestClearDynamicFormAssignment_ServerErrorSurfaced is the boundary of the
// 404-is-success tolerance: a 5xx must fail, or "remove the assignment" would
// silently leave the form assigning itself to every matching entity.
func TestClearDynamicFormAssignment_ServerErrorSurfaced(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "aspect delete failed", http.StatusInternalServerError)
	}))
	defer server.Close()

	err := newTestClient(t, server).ClearDynamicFormAssignment(t.Context(), "urn:li:form:x")
	if err == nil {
		t.Fatal("ClearDynamicFormAssignment() = nil error on HTTP 500; the assignment would silently persist")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error %q does not carry the status code", err)
	}
}

// TestClearDynamicFormAssignment_NotFoundOK verifies a 404 on the aspect
// DELETE is success (aspect already absent).
func TestClearDynamicFormAssignment_NotFoundOK(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		http.NotFound(w, r)
	}))
	defer server.Close()

	if err := newTestClient(t, server).ClearDynamicFormAssignment(t.Context(), "urn:li:form:x"); err != nil {
		t.Fatalf("ClearDynamicFormAssignment() error = %v", err)
	}
	if gotPath != "/openapi/v3/entity/form/urn:li:form:x/dynamicformassignment" {
		t.Errorf("path = %q", gotPath)
	}
}
