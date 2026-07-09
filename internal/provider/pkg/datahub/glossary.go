// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// GlossaryNode is the read-shape returned by GetGlossaryNodeByURN.
type GlossaryNode struct {
	URN              string
	ID               string
	Name             string
	Definition       string // mapped to "description" in the Terraform schema
	ParentNode       string // full glossaryNode URN, or ""
	Domain           string // full domain URN, or ""
	CustomProperties map[string]string
}

// GlossaryTerm is the read-shape returned by GetGlossaryTermByURN.
type GlossaryTerm struct {
	URN              string
	ID               string
	Name             string
	Definition       string // mapped to "description" in the Terraform schema
	ParentNode       string // full glossaryNode URN, or ""
	Domain           string // full domain URN, or ""
	CustomProperties map[string]string
}

// CreateGlossaryEntityInput groups the inputs for creating a DataHub glossary
// node or term. It maps to the CreateGlossaryEntityInput GraphQL type, which is
// shared by both createGlossaryNode and createGlossaryTerm mutations.
type CreateGlossaryEntityInput struct {
	// ID becomes the URN suffix. Always supply an explicit value; omitting it
	// causes the DataHub server to generate a random UUID, making the URN
	// non-deterministic and unmanageable by Terraform.
	ID         string
	Name       string
	Definition string // optional; sent as "description" in the GraphQL input
	ParentNode string // optional full glossaryNode URN; omitted when empty
}

// domainsAspect is the shared OpenAPI v3 shape for the "domains" aspect,
// present on any entity that has been associated with a DataHub domain.
type domainsAspect struct {
	Value struct {
		Domains []string `json:"domains"`
	} `json:"value"`
}

// firstDomain returns the first domain URN from the aspect, or "" if absent.
func (d *domainsAspect) firstDomain() string {
	if d == nil || len(d.Value.Domains) == 0 {
		return ""
	}
	return d.Value.Domains[0]
}

// glossaryNodeEntity is the OpenAPI v3 response shape for
// GET /openapi/v3/entity/glossarynode/{urn}.
type glossaryNodeEntity struct {
	URN     string `json:"urn"`
	KeyData *struct {
		Value struct {
			// "name" in the key aspect is the id/URN-suffix, not the display name.
			Name string `json:"name"`
		} `json:"value"`
	} `json:"glossaryNodeKey,omitempty"`
	Info *struct {
		Value struct {
			Name             string            `json:"name"`
			Definition       string            `json:"definition"`
			ParentNode       string            `json:"parentNode"`
			CustomProperties map[string]string `json:"customProperties"`
		} `json:"value"`
	} `json:"glossaryNodeInfo,omitempty"`
	Domains *domainsAspect `json:"domains,omitempty"`
}

// glossaryTermEntity is the OpenAPI v3 response shape for
// GET /openapi/v3/entity/glossaryterm/{urn}.
type glossaryTermEntity struct {
	URN     string `json:"urn"`
	KeyData *struct {
		Value struct {
			// "name" in the key aspect is the id/URN-suffix, not the display name.
			Name string `json:"name"`
		} `json:"value"`
	} `json:"glossaryTermKey,omitempty"`
	Info *struct {
		Value struct {
			Name             string            `json:"name"`
			Definition       string            `json:"definition"`
			ParentNode       string            `json:"parentNode"`
			CustomProperties map[string]string `json:"customProperties"`
		} `json:"value"`
	} `json:"glossaryTermInfo,omitempty"`
	Domains *domainsAspect `json:"domains,omitempty"`
}

type createGlossaryEntityResponse struct {
	Data   map[string]string `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// createGlossaryEntity is the shared implementation for CreateGlossaryNode and
// CreateGlossaryTerm. mutationName is either "createGlossaryNode" or
// "createGlossaryTerm". urnPrefix is the expected URN prefix used as a
// fallback if the server returns an empty URN. entityPath and infoAspectName
// identify the OpenAPI v3 read used for husk detection.
//
// The repairedHusk return is true when the create initially failed with
// "already exists", the existing entity turned out to be an empty husk left
// by DataHub's structured-property cleanup writing to a hard-deleted entity
// (CAT-2583), and the husk was deleted and the create retried successfully.
func (c *Client) createGlossaryEntity(ctx context.Context, mutationName, urnPrefix, entityPath, infoAspectName string, in CreateGlossaryEntityInput) (urn string, repairedHusk bool, err error) {
	if c == nil {
		return "", false, errors.New("client is nil")
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.ID == "" {
		return "", false, errors.New("id is required")
	}
	if in.Name == "" {
		return "", false, errors.New("name is required")
	}

	q := `
mutation ` + mutationName + `($input: CreateGlossaryEntityInput!) {
  ` + mutationName + `(input: $input)
}`

	input := map[string]any{
		"id":   in.ID,
		"name": in.Name,
	}
	if in.Definition != "" {
		input["description"] = in.Definition
	}
	if in.ParentNode != "" {
		input["parentNode"] = in.ParentNode
	}

	body := map[string]any{
		"query":     q,
		"variables": map[string]any{"input": input},
	}

	var gqlResp createGlossaryEntityResponse
	if err := c.doGraphQL(ctx, body, &gqlResp); err != nil {
		return "", false, err
	}
	if len(gqlResp.Errors) > 0 {
		msg := gqlResp.Errors[0].Message
		if !strings.Contains(strings.ToLower(msg), "already exists") {
			return "", false, fmt.Errorf("DataHub API error: %s", msg)
		}
		// CAT-2583 husk repair: if the blocking entity is an empty husk left
		// by the structured-property cleanup cascade patching a hard-deleted
		// entity, remove it and retry the create once. Anything that is not
		// provably a husk surfaces the original error untouched.
		huskURN := urnPrefix + in.ID
		husk, herr := c.isGlossaryHusk(ctx, entityPath, infoAspectName, huskURN)
		if herr != nil || !husk {
			return "", false, fmt.Errorf("DataHub API error: %s", msg)
		}
		if derr := c.DeleteGlossaryEntity(ctx, huskURN); derr != nil {
			return "", false, fmt.Errorf("DataHub API error: %s (husk repair failed: %w)", msg, derr)
		}
		tflog.Warn(ctx, "removed orphaned glossary husk before create (CAT-2583)",
			map[string]any{"urn": huskURN})
		gqlResp = createGlossaryEntityResponse{}
		if err := c.doGraphQL(ctx, body, &gqlResp); err != nil {
			return "", false, err
		}
		if len(gqlResp.Errors) > 0 {
			return "", false, fmt.Errorf("DataHub API error: %s", gqlResp.Errors[0].Message)
		}
		repairedHusk = true
	}

	urn = gqlResp.Data[mutationName]
	if urn == "" {
		urn = urnPrefix + in.ID
	}
	return urn, repairedHusk, nil
}

// isGlossaryHusk reports whether the entity at urn is a resurrection husk:
// it exists, has no info aspect, and carries nothing beyond its key aspect
// and an empty structuredProperties aspect. This is exactly the shape
// DataHub's PropertyDefinitionDeleteSideEffect leaves behind when its
// cleanup patch lands on a hard-deleted entity (CAT-2583). Any other shape
// -- an info aspect, values still assigned, or any unexpected aspect --
// disqualifies the entity so a real pre-existing entity is never touched.
func (c *Client) isGlossaryHusk(ctx context.Context, entityPath, infoAspectName, urn string) (bool, error) {
	path := fmt.Sprintf("/openapi/v3/entity/%s/%s", entityPath, urn)
	req, err := c.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return false, err
	}
	res, err := c.Do(req)
	if err != nil {
		return false, err
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if res.StatusCode >= http.StatusBadRequest {
		return false, fmt.Errorf("unexpected HTTP %d from DataHub entity API", res.StatusCode)
	}

	var aspects map[string]json.RawMessage
	if err := json.NewDecoder(res.Body).Decode(&aspects); err != nil {
		return false, fmt.Errorf("parsing entity response: %w", err)
	}

	if _, hasInfo := aspects[infoAspectName]; hasInfo {
		return false, nil
	}
	for name, raw := range aspects {
		switch name {
		case "urn", "glossaryNodeKey", "glossaryTermKey":
			// expected on any entity
		case "structuredProperties":
			var sp struct {
				Value struct {
					Properties []json.RawMessage `json:"properties"`
				} `json:"value"`
			}
			if err := json.Unmarshal(raw, &sp); err != nil || len(sp.Value.Properties) > 0 {
				return false, nil
			}
		default:
			return false, nil
		}
	}
	return true, nil
}

// CreateGlossaryNode creates a DataHub glossary node (Term Group) and returns
// its URN. Always supply a non-empty ID to produce a deterministic URN;
// omitting it causes the server to generate a random UUID. The bool return
// is true when an orphaned husk (CAT-2583) was removed to make way for the
// create; callers should surface that as a warning.
func (c *Client) CreateGlossaryNode(ctx context.Context, in CreateGlossaryEntityInput) (string, bool, error) {
	return c.createGlossaryEntity(ctx, "createGlossaryNode", "urn:li:glossaryNode:", "glossarynode", "glossaryNodeInfo", in)
}

// CreateGlossaryTerm creates a DataHub glossary term (Term) and returns its
// URN. Always supply a non-empty ID to produce a deterministic URN; omitting
// it causes the server to generate a random UUID. The bool return is true
// when an orphaned husk (CAT-2583) was removed to make way for the create;
// callers should surface that as a warning.
func (c *Client) CreateGlossaryTerm(ctx context.Context, in CreateGlossaryEntityInput) (string, bool, error) {
	return c.createGlossaryEntity(ctx, "createGlossaryTerm", "urn:li:glossaryTerm:", "glossaryterm", "glossaryTermInfo", in)
}

// setGlossaryProperties writes the glossaryNodeInfo/glossaryTermInfo aspect for a
// glossary entity via the OpenAPI v3 entity endpoint. This is how
// customProperties reaches DataHub: the GraphQL createGlossaryNode/
// createGlossaryTerm mutations do not carry customProperties.
//
// The write replaces the whole info aspect, so every field the aspect owns must
// be passed through or it is clobbered. name and definition are always sent
// (definition is a required field of both aspects). parentNode is sent when
// non-empty. extra carries aspect-specific required fields (glossaryTermInfo has
// a required termSource with no GraphQL-create analog, supplied as "INTERNAL").
// The domains aspect is separate (setDomain), so it is not touched here.
func (c *Client) setGlossaryProperties(ctx context.Context, entityPath, aspectName, urn, name, definition, parentNode string, extra map[string]any, customProperties map[string]string) error {
	if c == nil {
		return errors.New("client is nil")
	}
	urn = strings.TrimSpace(urn)
	name = strings.TrimSpace(name)
	if urn == "" {
		return errors.New("URN is required")
	}
	if name == "" {
		return errors.New("name is required")
	}

	infoValue := map[string]any{
		"name": name,
		// definition is a required field of the info aspect; always send it (even
		// empty) so the aspect write validates.
		"definition": definition,
	}
	if parentNode != "" {
		infoValue["parentNode"] = parentNode
	}
	for k, v := range extra {
		infoValue[k] = v
	}
	// Always include customProperties (even empty) so that clearing the map
	// overwrites a previously-set value rather than leaving it in place.
	if customProperties == nil {
		customProperties = map[string]string{}
	}
	infoValue["customProperties"] = customProperties

	entity := map[string]any{
		"urn":      urn,
		aspectName: map[string]any{"value": infoValue},
	}
	payload := []map[string]any{entity}

	path := fmt.Sprintf("/openapi/v3/entity/%s?async=false", entityPath)
	req, err := c.NewRequest(ctx, http.MethodPost, path, payload)
	if err != nil {
		return fmt.Errorf("building glossary properties write request: %w", err)
	}

	res, err := c.Do(req)
	if err != nil {
		return fmt.Errorf("glossary properties write request failed: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
		return fmt.Errorf("DataHub rejected the request (HTTP %d): the calling principal needs the MANAGE_GLOSSARIES privilege", res.StatusCode)
	}
	if res.StatusCode >= http.StatusBadRequest {
		respBody, _ := io.ReadAll(res.Body)
		return fmt.Errorf("unexpected HTTP %d from DataHub glossary write API: %s", res.StatusCode, respBody)
	}
	return nil
}

// SetGlossaryNodeProperties writes the glossaryNodeInfo aspect (carrying
// customProperties) for a glossary node. name/definition/parentNode are passed
// through to avoid clobbering the values the GraphQL mutations set.
func (c *Client) SetGlossaryNodeProperties(ctx context.Context, urn, name, definition, parentNode string, customProperties map[string]string) error {
	return c.setGlossaryProperties(ctx, "glossarynode", "glossaryNodeInfo", urn, name, definition, parentNode, nil, customProperties)
}

// SetGlossaryTermProperties writes the glossaryTermInfo aspect (carrying
// customProperties) for a glossary term. glossaryTermInfo has a required
// termSource field with no GraphQL-create analog (the server defaults it to
// INTERNAL), so it is supplied explicitly here to satisfy the full-aspect write.
func (c *Client) SetGlossaryTermProperties(ctx context.Context, urn, name, definition, parentNode string, customProperties map[string]string) error {
	return c.setGlossaryProperties(ctx, "glossaryterm", "glossaryTermInfo", urn, name, definition, parentNode, map[string]any{"termSource": "INTERNAL"}, customProperties)
}

// GetGlossaryNodeByURN fetches a DataHub glossary node directly by URN via the
// OpenAPI v3 entity endpoint (MySQL, strongly consistent). Returns nil (no
// error) on 404.
func (c *Client) GetGlossaryNodeByURN(ctx context.Context, urn string) (*GlossaryNode, error) {
	if c == nil {
		return nil, errors.New("client is nil")
	}
	urn = strings.TrimSpace(urn)
	if urn == "" {
		return nil, errors.New("URN is required")
	}

	path := fmt.Sprintf("/openapi/v3/entity/glossarynode/%s", urn)
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
	if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("DataHub rejected the request (HTTP %d): the calling principal needs the MANAGE_GLOSSARIES privilege", res.StatusCode)
	}
	if res.StatusCode >= http.StatusBadRequest {
		respBody, _ := io.ReadAll(res.Body)
		return nil, fmt.Errorf("unexpected HTTP %d from DataHub glossary node API: %s", res.StatusCode, respBody)
	}

	var entity glossaryNodeEntity
	if err := json.NewDecoder(res.Body).Decode(&entity); err != nil {
		return nil, fmt.Errorf("parsing glossary node entity response: %w", err)
	}

	if entity.KeyData == nil && entity.Info == nil {
		return nil, nil
	}

	id := ""
	if entity.KeyData != nil {
		id = entity.KeyData.Value.Name
	}
	if id == "" {
		id = strings.TrimPrefix(entity.URN, "urn:li:glossaryNode:")
	}

	node := &GlossaryNode{URN: entity.URN, ID: id, Domain: entity.Domains.firstDomain()}
	if entity.Info != nil {
		node.Name = entity.Info.Value.Name
		node.Definition = entity.Info.Value.Definition
		node.ParentNode = entity.Info.Value.ParentNode
		if len(entity.Info.Value.CustomProperties) > 0 {
			node.CustomProperties = entity.Info.Value.CustomProperties
		}
	}
	return node, nil
}

// GetGlossaryTermByURN fetches a DataHub glossary term directly by URN via the
// OpenAPI v3 entity endpoint (MySQL, strongly consistent). Returns nil (no
// error) on 404.
func (c *Client) GetGlossaryTermByURN(ctx context.Context, urn string) (*GlossaryTerm, error) {
	if c == nil {
		return nil, errors.New("client is nil")
	}
	urn = strings.TrimSpace(urn)
	if urn == "" {
		return nil, errors.New("URN is required")
	}

	path := fmt.Sprintf("/openapi/v3/entity/glossaryterm/%s", urn)
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
	if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("DataHub rejected the request (HTTP %d): the calling principal needs the MANAGE_GLOSSARIES privilege", res.StatusCode)
	}
	if res.StatusCode >= http.StatusBadRequest {
		respBody, _ := io.ReadAll(res.Body)
		return nil, fmt.Errorf("unexpected HTTP %d from DataHub glossary term API: %s", res.StatusCode, respBody)
	}

	var entity glossaryTermEntity
	if err := json.NewDecoder(res.Body).Decode(&entity); err != nil {
		return nil, fmt.Errorf("parsing glossary term entity response: %w", err)
	}

	if entity.KeyData == nil && entity.Info == nil {
		return nil, nil
	}

	id := ""
	if entity.KeyData != nil {
		id = entity.KeyData.Value.Name
	}
	if id == "" {
		id = strings.TrimPrefix(entity.URN, "urn:li:glossaryTerm:")
	}

	term := &GlossaryTerm{URN: entity.URN, ID: id, Domain: entity.Domains.firstDomain()}
	if entity.Info != nil {
		term.Name = entity.Info.Value.Name
		term.Definition = entity.Info.Value.Definition
		term.ParentNode = entity.Info.Value.ParentNode
		if len(entity.Info.Value.CustomProperties) > 0 {
			term.CustomProperties = entity.Info.Value.CustomProperties
		}
	}
	return term, nil
}

// MoveGlossaryEntity reparents a glossary node or term via the
// updateParentNode mutation. Pass "" for newParent to detach from any parent
// (promotes to root level). The parentNode field must be JSON null (not
// omitted) to remove an existing parent.
func (c *Client) MoveGlossaryEntity(ctx context.Context, urn, newParent string) error {
	if c == nil {
		return errors.New("client is nil")
	}
	const q = `
mutation updateParentNode($input: UpdateParentNodeInput!) {
  updateParentNode(input: $input)
}`
	// parentNode must be JSON null (not omitted) to remove an existing parent.
	var parentVal any
	if newParent != "" {
		parentVal = newParent
	}
	body := map[string]any{
		"query": q,
		"variables": map[string]any{
			"input": map[string]any{
				"resourceUrn": urn,
				"parentNode":  parentVal,
			},
		},
	}
	var gqlResp genericGraphQLErrors
	if err := c.doGraphQL(ctx, body, &gqlResp); err != nil {
		return err
	}
	if len(gqlResp.Errors) > 0 {
		return fmt.Errorf("DataHub API error: %s", gqlResp.Errors[0].Message)
	}
	return nil
}

// DeleteGlossaryEntity hard-deletes a DataHub glossary node or term by URN
// via the deleteGlossaryEntity GraphQL mutation. The mutation also
// asynchronously removes all references to the deleted entity. Unlike
// deleteDomain, there is no server-side child guard -- the server will succeed
// even if children exist, potentially leaving them parentless.
func (c *Client) DeleteGlossaryEntity(ctx context.Context, urn string) error {
	if c == nil {
		return errors.New("client is nil")
	}
	urn = strings.TrimSpace(urn)
	if urn == "" {
		return errors.New("URN is required")
	}

	const q = `
mutation deleteGlossaryEntity($urn: String!) {
  deleteGlossaryEntity(urn: $urn)
}`
	body := map[string]any{
		"query":     q,
		"variables": map[string]any{"urn": urn},
	}
	var gqlResp genericGraphQLErrors
	if err := c.doGraphQL(ctx, body, &gqlResp); err != nil {
		return err
	}
	if len(gqlResp.Errors) > 0 {
		return fmt.Errorf("DataHub API error: %s", gqlResp.Errors[0].Message)
	}
	return nil
}
