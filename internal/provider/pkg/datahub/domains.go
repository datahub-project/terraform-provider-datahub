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

// Domain is the read-shape returned by GetDomainByURN.
type Domain struct {
	URN          string
	ID           string
	Name         string
	Description  string
	ParentDomain string // full URN or ""
}

// CreateDomainInput groups the inputs for creating a DataHub domain.
type CreateDomainInput struct {
	// ID becomes the URN suffix: urn:li:domain:<ID>. Required. Always supply an
	// explicit value; omitting it causes the DataHub server to generate a random
	// UUID, making the URN non-deterministic and unmanageable by Terraform.
	ID           string
	Name         string
	Description  string // optional; omitted when empty
	ParentDomain string // optional full URN; omitted when empty
}

// domainEntity is the OpenAPI v3 response shape for
// GET /openapi/v3/entity/domain/{urn}.
type domainEntity struct {
	URN     string `json:"urn"`
	KeyData *struct {
		Value struct {
			ID string `json:"id"`
		} `json:"value"`
	} `json:"domainKey,omitempty"`
	Props *struct {
		Value struct {
			Name         string `json:"name"`
			Description  string `json:"description"`
			ParentDomain string `json:"parentDomain"`
		} `json:"value"`
	} `json:"domainProperties,omitempty"`
}

type createDomainResponse struct {
	Data struct {
		CreateDomain string `json:"createDomain"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// CreateDomain creates a DataHub domain via the GraphQL API and returns its
// URN. Always supply a non-empty ID to produce a deterministic URN; omitting
// it causes the server to generate a random UUID.
func (c *Client) CreateDomain(ctx context.Context, in CreateDomainInput) (string, error) {
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

	const q = `
mutation createDomain($input: CreateDomainInput!) {
  createDomain(input: $input)
}`

	input := map[string]any{
		"id":   in.ID,
		"name": in.Name,
	}
	if in.Description != "" {
		input["description"] = in.Description
	}
	if in.ParentDomain != "" {
		input["parentDomain"] = in.ParentDomain
	}

	body := map[string]any{
		"query":     q,
		"variables": map[string]any{"input": input},
	}

	var gqlResp createDomainResponse
	if err := c.doGraphQL(ctx, body, &gqlResp); err != nil {
		return "", err
	}
	if len(gqlResp.Errors) > 0 {
		return "", fmt.Errorf("DataHub API error: %s", gqlResp.Errors[0].Message)
	}

	urn := gqlResp.Data.CreateDomain
	if urn == "" {
		urn = "urn:li:domain:" + in.ID
	}
	return urn, nil
}

// GetDomainByURN fetches a DataHub domain directly by URN via the OpenAPI v3
// entity endpoint (MySQL, strongly consistent). Returns nil (no error) on 404.
func (c *Client) GetDomainByURN(ctx context.Context, urn string) (*Domain, error) {
	if c == nil {
		return nil, errors.New("client is nil")
	}
	urn = strings.TrimSpace(urn)
	if urn == "" {
		return nil, errors.New("URN is required")
	}

	path := fmt.Sprintf("/openapi/v3/entity/domain/%s", urn)
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
		return nil, fmt.Errorf("DataHub rejected the request (HTTP %d): the calling principal needs the MANAGE_DOMAINS privilege", res.StatusCode)
	}
	if res.StatusCode >= http.StatusBadRequest {
		respBody, _ := io.ReadAll(res.Body)
		return nil, fmt.Errorf("unexpected HTTP %d from DataHub domain API: %s", res.StatusCode, respBody)
	}

	var entity domainEntity
	if err := json.NewDecoder(res.Body).Decode(&entity); err != nil {
		return nil, fmt.Errorf("parsing domain entity response: %w", err)
	}

	if entity.KeyData == nil && entity.Props == nil {
		return nil, nil
	}

	id := ""
	if entity.KeyData != nil {
		id = entity.KeyData.Value.ID
	}
	if id == "" {
		id = strings.TrimPrefix(entity.URN, "urn:li:domain:")
	}

	domain := &Domain{URN: entity.URN, ID: id}
	if entity.Props != nil {
		domain.Name = entity.Props.Value.Name
		domain.Description = entity.Props.Value.Description
		domain.ParentDomain = entity.Props.Value.ParentDomain
	}
	return domain, nil
}

// UpdateDomainName updates a domain's display name via the generic updateName
// mutation, which writes domainProperties.name.
func (c *Client) UpdateDomainName(ctx context.Context, urn, name string) error {
	if c == nil {
		return errors.New("client is nil")
	}
	const q = `
mutation updateName($input: UpdateNameInput!) {
  updateName(input: $input)
}`
	body := map[string]any{
		"query": q,
		"variables": map[string]any{
			"input": map[string]any{
				"urn":  urn,
				"name": strings.TrimSpace(name),
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

// UpdateDomainDescription updates a domain's description via the generic
// updateDescription mutation. Pass "" to clear the description.
// The mutation input uses resourceUrn (not urn) for the target entity.
func (c *Client) UpdateDomainDescription(ctx context.Context, urn, description string) error {
	if c == nil {
		return errors.New("client is nil")
	}
	const q = `
mutation updateDescription($input: DescriptionUpdateInput!) {
  updateDescription(input: $input)
}`
	body := map[string]any{
		"query": q,
		"variables": map[string]any{
			"input": map[string]any{
				"resourceUrn": urn,
				"description": description,
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

// MoveDomain reparents a domain via the moveDomain mutation. Pass "" for
// newParent to promote the domain to root (removes any existing parent).
func (c *Client) MoveDomain(ctx context.Context, urn, newParent string) error {
	if c == nil {
		return errors.New("client is nil")
	}
	const q = `
mutation moveDomain($input: MoveDomainInput!) {
  moveDomain(input: $input)
}`
	// parentDomain must be JSON null (not omitted) to remove an existing parent.
	var parentVal any
	if newParent != "" {
		parentVal = newParent
	}
	body := map[string]any{
		"query": q,
		"variables": map[string]any{
			"input": map[string]any{
				"resourceUrn":  urn,
				"parentDomain": parentVal,
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

// DeleteDomain hard-deletes a DataHub domain by URN via the deleteDomain
// GraphQL mutation. The server rejects deletion if the domain has child domains.
func (c *Client) DeleteDomain(ctx context.Context, urn string) error {
	if c == nil {
		return errors.New("client is nil")
	}
	urn = strings.TrimSpace(urn)
	if urn == "" {
		return errors.New("URN is required")
	}

	const q = `
mutation deleteDomain($urn: String!) {
  deleteDomain(urn: $urn)
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
