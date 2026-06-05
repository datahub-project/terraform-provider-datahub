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
)

// GlossaryNode is the read-shape returned by GetGlossaryNodeByURN.
type GlossaryNode struct {
	URN        string
	ID         string
	Name       string
	Definition string // mapped to "description" in the Terraform schema
	ParentNode string // full glossaryNode URN, or ""
}

// GlossaryTerm is the read-shape returned by GetGlossaryTermByURN.
type GlossaryTerm struct {
	URN        string
	ID         string
	Name       string
	Definition string // mapped to "description" in the Terraform schema
	ParentNode string // full glossaryNode URN, or ""
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
			Name       string `json:"name"`
			Definition string `json:"definition"`
			ParentNode string `json:"parentNode"`
		} `json:"value"`
	} `json:"glossaryNodeInfo,omitempty"`
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
			Name       string `json:"name"`
			Definition string `json:"definition"`
			ParentNode string `json:"parentNode"`
		} `json:"value"`
	} `json:"glossaryTermInfo,omitempty"`
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
// fallback if the server returns an empty URN.
func (c *Client) createGlossaryEntity(ctx context.Context, mutationName, urnPrefix string, in CreateGlossaryEntityInput) (string, error) {
	if c == nil {
		return "", errors.New("client is nil")
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.ID == "" {
		return "", errors.New("id is required")
	}
	if in.Name == "" {
		return "", errors.New("name is required")
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
		return "", err
	}
	if len(gqlResp.Errors) > 0 {
		return "", fmt.Errorf("DataHub API error: %s", gqlResp.Errors[0].Message)
	}

	urn := gqlResp.Data[mutationName]
	if urn == "" {
		urn = urnPrefix + in.ID
	}
	return urn, nil
}

// CreateGlossaryNode creates a DataHub glossary node (Term Group) and returns
// its URN. Always supply a non-empty ID to produce a deterministic URN;
// omitting it causes the server to generate a random UUID.
func (c *Client) CreateGlossaryNode(ctx context.Context, in CreateGlossaryEntityInput) (string, error) {
	return c.createGlossaryEntity(ctx, "createGlossaryNode", "urn:li:glossaryNode:", in)
}

// CreateGlossaryTerm creates a DataHub glossary term (Term) and returns its
// URN. Always supply a non-empty ID to produce a deterministic URN; omitting
// it causes the server to generate a random UUID.
func (c *Client) CreateGlossaryTerm(ctx context.Context, in CreateGlossaryEntityInput) (string, error) {
	return c.createGlossaryEntity(ctx, "createGlossaryTerm", "urn:li:glossaryTerm:", in)
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

	node := &GlossaryNode{URN: entity.URN, ID: id}
	if entity.Info != nil {
		node.Name = entity.Info.Value.Name
		node.Definition = entity.Info.Value.Definition
		node.ParentNode = entity.Info.Value.ParentNode
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

	term := &GlossaryTerm{URN: entity.URN, ID: id}
	if entity.Info != nil {
		term.Name = entity.Info.Value.Name
		term.Definition = entity.Info.Value.Definition
		term.ParentNode = entity.Info.Value.ParentNode
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
