// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahubtesting

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
)

const mockFormURNPrefix = "urn:li:form:"

// The mock enforces the SDL and resolver contracts, not the provider client's
// behaviour: CreateFormInput requires name (String!), FormType and
// FormPromptType are closed enums, CreatePromptInput requires title and type,
// and CreateFormResolver rejects structured-property prompts without
// structuredPropertyParams. CreateDynamicFormAssignmentInput requires a
// non-empty orFilters and FormService requires the form to exist.

type mockFormPrompt struct {
	ID                    string
	Title                 string
	Description           string
	Type                  string
	StructuredPropertyURN string
	Required              bool
}

type mockFormActors struct {
	Owners bool
	Users  []string
	Groups []string
}

type mockFilterCriterion struct {
	Field     string
	Values    []string
	Condition string
	Negated   bool
}

type mockForm struct {
	ID          string
	Name        string
	Description string
	Type        string
	Prompts     []mockFormPrompt
	Actors      *mockFormActors
	// Assignment holds the dynamicFormAssignment filter as OR groups of AND
	// criteria; nil when the aspect is absent.
	Assignment [][]mockFilterCriterion
}

func graphQLError(w http.ResponseWriter, msg string) {
	_ = json.NewEncoder(w).Encode(map[string]any{
		"errors": []map[string]any{{"message": msg}},
	})
}

func stringSlice(v any) []string {
	list, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, e := range list {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// handleCreateForm mimics the createForm mutation: a full replace of the
// formInfo aspect at urn:li:form:<id> with no existence check, matching
// FormService.createForm. Enum and requiredness violations return GraphQL
// errors the way the server's schema layer would.
func (s *mockServer) handleCreateForm(w http.ResponseWriter, variables map[string]any) {
	input, _ := variables["input"].(map[string]any)
	name, _ := input["name"].(string)
	if name == "" {
		graphQLError(w, "Validation error: name must not be null")
		return
	}
	id, _ := input["id"].(string)
	if id == "" {
		// The real server mints a UUID; the provider always supplies an id, so
		// an empty one here is a client bug worth failing loudly on.
		graphQLError(w, "mock requires an explicit form id")
		return
	}

	f := mockForm{ID: id, Name: name, Type: "COMPLETION"}
	if desc, ok := input["description"].(string); ok {
		f.Description = desc
	}
	if ft, ok := input["type"].(string); ok && ft != "" {
		if ft != "COMPLETION" && ft != "VERIFICATION" {
			graphQLError(w, fmt.Sprintf("Validation error: invalid FormType %q", ft))
			return
		}
		f.Type = ft
	}
	if rawPrompts, ok := input["prompts"].([]any); ok {
		for _, rp := range rawPrompts {
			pm, _ := rp.(map[string]any)
			title, _ := pm["title"].(string)
			ptype, _ := pm["type"].(string)
			if title == "" || ptype == "" {
				graphQLError(w, "Validation error: prompt title and type must not be null")
				return
			}
			if ptype != "STRUCTURED_PROPERTY" && ptype != "FIELDS_STRUCTURED_PROPERTY" {
				graphQLError(w, fmt.Sprintf("Validation error: invalid FormPromptType %q", ptype))
				return
			}
			prompt := mockFormPrompt{Title: title, Type: ptype}
			prompt.ID, _ = pm["id"].(string)
			prompt.Description, _ = pm["description"].(string)
			prompt.Required, _ = pm["required"].(bool)
			if params, ok := pm["structuredPropertyParams"].(map[string]any); ok {
				prompt.StructuredPropertyURN, _ = params["urn"].(string)
			}
			if prompt.StructuredPropertyURN == "" {
				// CreateFormResolver.validatePrompts: both prompt types require params.
				graphQLError(w, "Provided prompt with type STRUCTURED_PROPERTY or FIELDS_STRUCTURED_PROPERTY and no structured property params")
				return
			}
			f.Prompts = append(f.Prompts, prompt)
		}
	}
	if rawActors, ok := input["actors"].(map[string]any); ok {
		actors := &mockFormActors{}
		actors.Owners, _ = rawActors["owners"].(bool)
		actors.Users = stringSlice(rawActors["users"])
		actors.Groups = stringSlice(rawActors["groups"])
		f.Actors = actors
	}

	s.mu.Lock()
	// Full formInfo replace; the dynamicFormAssignment aspect is untouched,
	// matching the server (separate aspects).
	if existing, ok := s.forms[id]; ok {
		f.Assignment = existing.Assignment
	}
	s.forms[id] = f
	s.mu.Unlock()

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{
			"createForm": map[string]any{"urn": mockFormURNPrefix + id},
		},
	})
}

func (s *mockServer) handleDeleteForm(w http.ResponseWriter, variables map[string]any) {
	input, _ := variables["input"].(map[string]any)
	urn, _ := input["urn"].(string)
	id := strings.TrimPrefix(urn, mockFormURNPrefix)

	s.mu.Lock()
	delete(s.forms, id)
	s.mu.Unlock()

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{"deleteForm": true},
	})
}

// handleCreateDynamicFormAssignment mimics createDynamicFormAssignment: the
// form must exist (FormService errors otherwise) and orFilters is non-null.
func (s *mockServer) handleCreateDynamicFormAssignment(w http.ResponseWriter, variables map[string]any) {
	input, _ := variables["input"].(map[string]any)
	formURN, _ := input["formUrn"].(string)
	id := strings.TrimPrefix(formURN, mockFormURNPrefix)

	orFilters, ok := input["orFilters"].([]any)
	if !ok || len(orFilters) == 0 {
		graphQLError(w, "Validation error: orFilters must not be null")
		return
	}

	var assignment [][]mockFilterCriterion
	for _, rawGroup := range orFilters {
		gm, _ := rawGroup.(map[string]any)
		ands, _ := gm["and"].([]any)
		var group []mockFilterCriterion
		for _, rawCrit := range ands {
			cm, _ := rawCrit.(map[string]any)
			crit := mockFilterCriterion{Condition: "EQUAL"}
			crit.Field, _ = cm["field"].(string)
			if crit.Field == "" {
				graphQLError(w, "Validation error: facet filter field must not be null")
				return
			}
			crit.Values = stringSlice(cm["values"])
			if cond, ok := cm["condition"].(string); ok && cond != "" {
				crit.Condition = cond
			}
			crit.Negated, _ = cm["negated"].(bool)
			group = append(group, crit)
		}
		assignment = append(assignment, group)
	}

	s.mu.Lock()
	f, exists := s.forms[id]
	if exists {
		f.Assignment = assignment
		s.forms[id] = f
	}
	s.mu.Unlock()

	if !exists {
		graphQLError(w, fmt.Sprintf("Form %s does not exist. Skipping dynamic form assignment", formURN))
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{"createDynamicFormAssignment": true},
	})
}

// handleFormItem serves the OpenAPI v3 form surface:
//
//	GET    /openapi/v3/entity/form/{urn}                        entity read
//	DELETE /openapi/v3/entity/form/{urn}                        entity delete (used by drift tests)
//	DELETE /openapi/v3/entity/form/{urn}/dynamicformassignment  aspect delete
func (s *mockServer) handleFormItem(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/openapi/v3/entity/form/")

	if r.Method == http.MethodDelete && strings.HasSuffix(rest, "/dynamicformassignment") {
		urn := strings.TrimSuffix(rest, "/dynamicformassignment")
		id := strings.TrimPrefix(urn, mockFormURNPrefix)
		s.mu.Lock()
		f, ok := s.forms[id]
		if ok {
			f.Assignment = nil
			s.forms[id] = f
		}
		s.mu.Unlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
		return
	}

	id := strings.TrimPrefix(rest, mockFormURNPrefix)

	switch r.Method {
	case http.MethodDelete:
		s.mu.Lock()
		delete(s.forms, id)
		s.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	case http.MethodGet:
		s.mu.Lock()
		f, ok := s.forms[id]
		s.mu.Unlock()
		if !ok {
			http.NotFound(w, r)
			return
		}

		prompts := make([]map[string]any, 0, len(f.Prompts))
		for _, p := range f.Prompts {
			pm := map[string]any{
				"id":       p.ID,
				"title":    p.Title,
				"type":     p.Type,
				"required": p.Required,
				"structuredPropertyParams": map[string]any{
					"urn": p.StructuredPropertyURN,
				},
			}
			if p.Description != "" {
				pm["description"] = p.Description
			}
			prompts = append(prompts, pm)
		}
		info := map[string]any{
			"name":    f.Name,
			"prompts": prompts,
		}
		// The stored aspect omits type when it was never set (FormInfo.type has
		// no PDL default): a form seeded via /test-control/seed-form reads back
		// without it, the way an SDK-written aspect does. Every mutation path
		// sets it, so provider-created forms always carry it.
		if f.Type != "" {
			info["type"] = f.Type
		}
		if f.Description != "" {
			info["description"] = f.Description
		}
		// The formInfo aspect always materialises actors (PDL default
		// {owners: true}), so the mock does too.
		actors := map[string]any{"owners": true}
		if f.Actors != nil {
			actors["owners"] = f.Actors.Owners
			if len(f.Actors.Users) > 0 {
				actors["users"] = f.Actors.Users
			}
			if len(f.Actors.Groups) > 0 {
				actors["groups"] = f.Actors.Groups
			}
		}
		info["actors"] = actors

		entity := map[string]any{
			"urn":      mockFormURNPrefix + id,
			"formKey":  map[string]any{"value": map[string]any{"id": id}},
			"formInfo": map[string]any{"value": info},
		}
		if f.Assignment != nil {
			orGroups := make([]map[string]any, 0, len(f.Assignment))
			for _, group := range f.Assignment {
				ands := make([]map[string]any, 0, len(group))
				for _, crit := range group {
					cm := map[string]any{
						"field":   crit.Field,
						"values":  crit.Values,
						"negated": crit.Negated,
					}
					// A serialiser that drops defaulted fields omits condition;
					// seeded criteria reproduce that, mutation-written ones
					// always carry it.
					if crit.Condition != "" {
						cm["condition"] = crit.Condition
					}
					ands = append(ands, cm)
				}
				orGroups = append(orGroups, map[string]any{"and": ands})
			}
			entity["dynamicFormAssignment"] = map[string]any{
				"value": map[string]any{
					"filter": map[string]any{"or": orGroups},
				},
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(entity)
	default:
		http.NotFound(w, r)
	}
}

// handleSeedForm injects a form into the mock store without going through the
// createForm mutation -- standing in for a form written by the DataHub Python
// SDK or UI, whose stored aspect can omit `type` and filter `condition`
// entirely. Import tests need this: importing a form the provider itself wrote
// cannot exercise the read-path defaults, because the provider's write path
// always sends those fields.
//
//	POST /test-control/seed-form
//	{"id":"...","name":"...","orFilters":[[{"field":"platform.keyword","values":["..."]}]]}
func (s *mockServer) handleSeedForm(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		Type      string `json:"type"`
		OrFilters [][]struct {
			Field     string   `json:"field"`
			Values    []string `json:"values"`
			Condition string   `json:"condition"`
			Negated   bool     `json:"negated"`
		} `json:"orFilters"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ID == "" || body.Name == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	f := mockForm{ID: body.ID, Name: body.Name, Type: body.Type}
	for _, group := range body.OrFilters {
		var g []mockFilterCriterion
		for _, c := range group {
			g = append(g, mockFilterCriterion{
				Field:     c.Field,
				Values:    c.Values,
				Condition: c.Condition,
				Negated:   c.Negated,
			})
		}
		f.Assignment = append(f.Assignment, g)
	}

	s.mu.Lock()
	s.forms[body.ID] = f
	s.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

// SeedRawForm injects a form into the mock store at baseURL via
// /test-control/seed-form, bypassing the provider's own write path. Stands in
// for a form authored by the Python SDK or the DataHub UI, whose stored aspect
// omits `type` and filter `condition`. Mock-only.
func SeedRawForm(baseURL, bodyJSON string) {
	resp, err := http.Post(baseURL+"/test-control/seed-form", "application/json", strings.NewReader(bodyJSON)) //nolint:noctx
	if err != nil {
		panic(fmt.Sprintf("SeedRawForm: %v", err))
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		panic(fmt.Sprintf("SeedRawForm: unexpected status %d", resp.StatusCode))
	}
}

// FormCheckDestroy verifies every datahub_form is removed.
func FormCheckDestroy(s *terraform.State) error {
	client, err := datahub.NewClient(os.Getenv("DATAHUB_GMS_URL"), os.Getenv("DATAHUB_GMS_TOKEN"))
	if err != nil {
		return fmt.Errorf("CheckDestroy: failed to build DataHub client: %w", err)
	}
	ctx := context.Background()
	for _, rs := range s.RootModule().Resources {
		if rs.Type != "datahub_form" {
			continue
		}
		urn := rs.Primary.Attributes["urn"]
		if urn == "" {
			urn = datahub.FormURNPrefix + rs.Primary.Attributes["id"]
		}
		form, getErr := client.GetFormByURN(ctx, urn)
		if getErr != nil {
			return fmt.Errorf("CheckDestroy: unexpected error checking form %q: %w", urn, getErr)
		}
		if form != nil {
			return stillExistsAfterDestroyError(ctx, client, "datahub_form", urn)
		}
	}
	return nil
}
