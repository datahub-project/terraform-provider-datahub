// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

// Form management (the `form` entity).
//
// A form is a metadata-collection questionnaire (COMPLETION) or compliance
// check (VERIFICATION) that platform teams assign to assets. The entity owns
// two aspects here: `formInfo` (name, description, type, prompts, actors) and
// `dynamicFormAssignment` (a search filter that assigns the form to every
// matching entity). Both exist in open-source DataHub, so this is an OSS+Cloud
// surface.
//
// The URN is `urn:li:form:<id>`. The provider always passes a deterministic id
// to createForm (the server would otherwise mint a random UUID), matching the
// Python SDK convention (`urn:li:form:{id}` with a user-supplied id).
//
// Writes go through GraphQL. createForm is used for BOTH create and update:
// FormService.createForm performs an unconditional ingest of the full formInfo
// aspect at the URN derived from the id -- no existence check -- so calling it
// again with the same id is a full-aspect replace. That gives clean full-state
// ownership of the prompts and actors lists; the alternative updateForm
// mutation is patch-shaped (promptsToAdd/promptsToRemove, usersToAdd/...)
// and cannot express "this is the complete desired state". The Python SDK
// behaves the same way (it emits the whole FormInfo aspect on every run).
//
// The dynamic assignment is upserted via createDynamicFormAssignment and
// cleared via the OpenAPI v3 aspect DELETE
// (DELETE /openapi/v3/entity/form/{urn}/dynamicformassignment) -- no GraphQL
// mutation exists to remove the aspect. Reads use the strongly-consistent
// OpenAPI v3 entity endpoint. Delete uses deleteForm, which is a hard delete
// plus an asynchronous cleanup of references to the form on other entities.

package datahub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// FormURNPrefix is the URN namespace for forms.
const FormURNPrefix = "urn:li:form:"

// FormPrompt is one question on a form. Both prompt types collect a structured
// property value, so StructuredPropertyURN is always required.
type FormPrompt struct {
	ID                    string
	Title                 string
	Description           string
	Type                  string // STRUCTURED_PROPERTY / FIELDS_STRUCTURED_PROPERTY
	StructuredPropertyURN string
	Required              bool
}

// FormActors describes who is asked to complete the form.
type FormActors struct {
	Owners bool // assign to owners of matched assets
	Users  []string
	Groups []string
}

// FormInput is the desired state for an upsert of the formInfo aspect.
type FormInput struct {
	ID          string // URN key; the resource derives this deterministically
	Name        string
	Description string
	Type        string // COMPLETION / VERIFICATION; empty -> server default COMPLETION
	Prompts     []FormPrompt
	Actors      *FormActors // nil -> server default {owners: true}
}

// Form is the read shape returned by GetFormByURN. OrFilters carries the
// dynamicFormAssignment aspect and is nil when the aspect is absent.
type Form struct {
	URN         string
	ID          string
	Name        string
	Description string
	Type        string
	Prompts     []FormPrompt
	Actors      *FormActors
	OrFilters   []AndFilter
}

// formEntity is the OpenAPI v3 response for GET /openapi/v3/entity/form/{urn}.
type formEntity struct {
	URN string `json:"urn"`
	Key *struct {
		Value struct {
			ID string `json:"id"`
		} `json:"value"`
	} `json:"formKey,omitempty"`
	Info *struct {
		Value struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Type        string `json:"type"`
			Prompts     []struct {
				ID                       string `json:"id"`
				Title                    string `json:"title"`
				Description              string `json:"description"`
				Type                     string `json:"type"`
				StructuredPropertyParams *struct {
					URN string `json:"urn"`
				} `json:"structuredPropertyParams,omitempty"`
				Required bool `json:"required"`
			} `json:"prompts"`
			Actors *struct {
				Owners bool     `json:"owners"`
				Users  []string `json:"users"`
				Groups []string `json:"groups"`
			} `json:"actors,omitempty"`
		} `json:"value"`
	} `json:"formInfo,omitempty"`
	Assignment *struct {
		Value struct {
			Filter struct {
				Or []struct {
					And []struct {
						Field     string   `json:"field"`
						Values    []string `json:"values"`
						Condition string   `json:"condition"`
						Negated   bool     `json:"negated"`
					} `json:"and"`
				} `json:"or"`
			} `json:"filter"`
		} `json:"value"`
	} `json:"dynamicFormAssignment,omitempty"`
}

// formPromptsToGraphQL converts prompts to the GraphQL [CreatePromptInput!] shape.
func formPromptsToGraphQL(prompts []FormPrompt) []map[string]any {
	out := make([]map[string]any, 0, len(prompts))
	for _, p := range prompts {
		m := map[string]any{
			"id":       p.ID,
			"title":    p.Title,
			"type":     p.Type,
			"required": p.Required,
		}
		if p.Description != "" {
			m["description"] = p.Description
		}
		if p.StructuredPropertyURN != "" {
			m["structuredPropertyParams"] = map[string]any{"urn": p.StructuredPropertyURN}
		}
		out = append(out, m)
	}
	return out
}

// UpsertForm writes the full formInfo aspect at the deterministic URN
// urn:li:form:<in.ID> via the createForm mutation, and returns that URN.
// createForm performs no existence check server-side, so calling it for an
// existing form is a full-aspect replace -- the caller owns the complete
// prompts and actors lists, and anything added outside Terraform is removed.
// Requires the MANAGE_FORMS ("Manage Metadata Forms") platform privilege.
func (c *Client) UpsertForm(ctx context.Context, in FormInput) (string, error) {
	if c == nil {
		return "", errors.New("client is nil")
	}
	in.ID = strings.TrimSpace(in.ID)
	if in.ID == "" {
		return "", errors.New("form ID is required")
	}
	if strings.TrimSpace(in.Name) == "" {
		return "", errors.New("form name is required")
	}

	const q = `
mutation createForm($input: CreateFormInput!) {
  createForm(input: $input) { urn }
}`

	input := map[string]any{
		"id":   in.ID,
		"name": in.Name,
	}
	if in.Description != "" {
		input["description"] = in.Description
	}
	if in.Type != "" {
		input["type"] = in.Type
	}
	// Always send prompts (an empty list clears them): the aspect write is a
	// full replace, and omitting the key would leave the server default rather
	// than the declared state.
	input["prompts"] = formPromptsToGraphQL(in.Prompts)
	if in.Actors != nil {
		actors := map[string]any{"owners": in.Actors.Owners}
		if len(in.Actors.Users) > 0 {
			actors["users"] = in.Actors.Users
		}
		if len(in.Actors.Groups) > 0 {
			actors["groups"] = in.Actors.Groups
		}
		input["actors"] = actors
	}

	urn := FormURNPrefix + in.ID
	body := map[string]any{"query": q, "variables": map[string]any{"input": input}}

	var raw genericGraphQLErrors
	if err := c.doGraphQL(ctx, body, &raw); err != nil {
		return urn, err
	}
	if len(raw.Errors) > 0 {
		return urn, fmt.Errorf("DataHub API error: %s", raw.Errors[0].Message)
	}
	return urn, nil
}

// UpsertDynamicFormAssignment replaces the dynamicFormAssignment aspect on a
// form via the createDynamicFormAssignment mutation. The server requires the
// form to exist. Assignment to matching entities happens asynchronously via a
// server-side hook reacting to the aspect write.
func (c *Client) UpsertDynamicFormAssignment(ctx context.Context, formURN string, orFilters []AndFilter) error {
	if c == nil {
		return errors.New("client is nil")
	}
	formURN = strings.TrimSpace(formURN)
	if formURN == "" {
		return errors.New("form URN is required")
	}
	if len(orFilters) == 0 {
		return errors.New("at least one filter group is required")
	}

	const q = `
mutation createDynamicFormAssignment($input: CreateDynamicFormAssignmentInput!) {
  createDynamicFormAssignment(input: $input)
}`

	input := map[string]any{
		"formUrn":   formURN,
		"orFilters": orFiltersToGraphQL(orFilters),
	}
	body := map[string]any{"query": q, "variables": map[string]any{"input": input}}

	var raw genericGraphQLErrors
	if err := c.doGraphQL(ctx, body, &raw); err != nil {
		return err
	}
	if len(raw.Errors) > 0 {
		return fmt.Errorf("DataHub API error: %s", raw.Errors[0].Message)
	}
	return nil
}

// ClearDynamicFormAssignment removes the dynamicFormAssignment aspect via the
// OpenAPI v3 aspect DELETE (no GraphQL mutation exists for removal). A 404 --
// aspect or entity already absent -- is treated as success. Note the server
// does not retract the form from entities it already assigned; only future
// assignment stops.
func (c *Client) ClearDynamicFormAssignment(ctx context.Context, formURN string) error {
	if c == nil {
		return errors.New("client is nil")
	}
	formURN = strings.TrimSpace(formURN)
	if formURN == "" {
		return errors.New("form URN is required")
	}

	path := fmt.Sprintf("/openapi/v3/entity/form/%s/dynamicformassignment", formURN)
	req, err := c.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return err
	}
	res, err := c.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusNotFound {
		return nil
	}
	if res.StatusCode >= http.StatusBadRequest {
		respBody, _ := io.ReadAll(res.Body)
		return fmt.Errorf("unexpected HTTP %d from DataHub form API: %s", res.StatusCode, respBody)
	}
	return nil
}

// GetFormByURN reads a form from the OpenAPI v3 entity endpoint (MySQL,
// strongly consistent). Both owned aspects come back in one call. Returns nil
// (no error) on 404 or when the entity carries neither owned aspect.
func (c *Client) GetFormByURN(ctx context.Context, urn string) (*Form, error) {
	if c == nil {
		return nil, errors.New("client is nil")
	}
	urn = strings.TrimSpace(urn)
	if urn == "" {
		return nil, errors.New("URN is required")
	}

	path := fmt.Sprintf("/openapi/v3/entity/form/%s", urn)
	req, err := c.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	res, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if res.StatusCode >= http.StatusBadRequest {
		respBody, _ := io.ReadAll(res.Body)
		return nil, fmt.Errorf("unexpected HTTP %d from DataHub form API: %s", res.StatusCode, respBody)
	}

	var entity formEntity
	if err := json.NewDecoder(res.Body).Decode(&entity); err != nil {
		return nil, fmt.Errorf("parsing form entity response: %w", err)
	}
	if entity.Key == nil && entity.Info == nil {
		return nil, nil
	}

	f := &Form{URN: entity.URN}
	if entity.Key != nil {
		f.ID = entity.Key.Value.ID
	}
	if f.ID == "" {
		f.ID = strings.TrimPrefix(entity.URN, FormURNPrefix)
	}
	if entity.Info != nil {
		v := entity.Info.Value
		f.Name = v.Name
		f.Description = v.Description
		f.Type = v.Type
		for _, p := range v.Prompts {
			prompt := FormPrompt{
				ID:          p.ID,
				Title:       p.Title,
				Description: p.Description,
				Type:        p.Type,
				Required:    p.Required,
			}
			if p.StructuredPropertyParams != nil {
				prompt.StructuredPropertyURN = p.StructuredPropertyParams.URN
			}
			f.Prompts = append(f.Prompts, prompt)
		}
		if v.Actors != nil {
			f.Actors = &FormActors{
				Owners: v.Actors.Owners,
				Users:  v.Actors.Users,
				Groups: v.Actors.Groups,
			}
		}
	}
	if entity.Assignment != nil {
		for _, group := range entity.Assignment.Value.Filter.Or {
			ag := AndFilter{}
			for _, cr := range group.And {
				ag.And = append(ag.And, FacetFilter{
					Field:     cr.Field,
					Values:    cr.Values,
					Condition: cr.Condition,
					Negated:   cr.Negated,
				})
			}
			f.OrFilters = append(f.OrFilters, ag)
		}
	}
	return f, nil
}

// DeleteForm hard-deletes a form via the deleteForm mutation. The server also
// asynchronously deletes references to the form on other entities (assignments
// and completion records); structured-property values collected through the
// form's prompts are ordinary metadata on the target entities and survive. A
// not-found result is treated as success (the entity is already gone).
func (c *Client) DeleteForm(ctx context.Context, urn string) error {
	if c == nil {
		return errors.New("client is nil")
	}
	urn = strings.TrimSpace(urn)
	if urn == "" {
		return errors.New("form URN is required")
	}

	const q = `
mutation deleteForm($input: DeleteFormInput!) {
  deleteForm(input: $input)
}`
	body := map[string]any{"query": q, "variables": map[string]any{"input": map[string]any{"urn": urn}}}

	var raw genericGraphQLErrors
	if err := c.doGraphQL(ctx, body, &raw); err != nil {
		return err
	}
	if len(raw.Errors) > 0 {
		msg := raw.Errors[0].Message
		if strings.Contains(msg, "not found") || strings.Contains(msg, "does not exist") {
			return nil
		}
		return fmt.Errorf("DataHub API error: %s", msg)
	}
	return nil
}

// ListFormURNs returns the URNs of all forms visible to the authenticated
// principal, via searchAcrossEntities (entity type FORM). Backed by OpenSearch
// (eventually consistent) -- for enumeration/import, not authoritative reads.
func (c *Client) ListFormURNs(ctx context.Context) ([]string, error) {
	return listURNsByEntityType(ctx, c, "FORM")
}
