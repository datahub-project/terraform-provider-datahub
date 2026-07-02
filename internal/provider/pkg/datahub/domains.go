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
	"time"
)

// Domain is the read-shape returned by GetDomainByURN.
type Domain struct {
	URN              string
	ID               string
	Name             string
	Description      string
	ParentDomain     string // full URN or ""
	CustomProperties map[string]string
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
			Name             string            `json:"name"`
			Description      string            `json:"description"`
			ParentDomain     string            `json:"parentDomain"`
			CustomProperties map[string]string `json:"customProperties"`
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
		if len(entity.Props.Value.CustomProperties) > 0 {
			domain.CustomProperties = entity.Props.Value.CustomProperties
		}
	}
	return domain, nil
}

// SetDomainProperties writes the domainProperties aspect for a domain via the
// OpenAPI v3 entity endpoint. This is how customProperties reaches DataHub:
// the GraphQL createDomain/updateName/updateDescription/moveDomain mutations do
// not carry customProperties.
//
// The write replaces the whole domainProperties aspect, so name/description/
// parentDomain must be passed through alongside customProperties to preserve
// them (they are otherwise owned by the GraphQL mutations). Callers pass the
// domain's current name/description/parentDomain; parentDomain is carried at its
// existing value so the parent relationship, already established by createDomain
// or moveDomain, is not disturbed.
func (c *Client) SetDomainProperties(ctx context.Context, urn, name, description, parentDomain string, customProperties map[string]string) error {
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

	propsValue := map[string]any{
		"name": name,
	}
	if description != "" {
		propsValue["description"] = description
	}
	if parentDomain != "" {
		propsValue["parentDomain"] = parentDomain
	}
	// Always include customProperties (even empty) so that clearing the map
	// overwrites a previously-set value rather than leaving it in place.
	if customProperties == nil {
		customProperties = map[string]string{}
	}
	propsValue["customProperties"] = customProperties

	entity := map[string]any{
		"urn": urn,
		"domainProperties": map[string]any{
			"value": propsValue,
		},
	}
	payload := []map[string]any{entity}

	req, err := c.NewRequest(ctx, http.MethodPost, "/openapi/v3/entity/domain?async=false", payload)
	if err != nil {
		return fmt.Errorf("building domain properties write request: %w", err)
	}

	res, err := c.Do(req)
	if err != nil {
		return fmt.Errorf("domain properties write request failed: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
		return fmt.Errorf("DataHub rejected the request (HTTP %d): the calling principal needs the MANAGE_DOMAINS privilege", res.StatusCode)
	}
	if res.StatusCode >= http.StatusBadRequest {
		respBody, _ := io.ReadAll(res.Body)
		return fmt.Errorf("unexpected HTTP %d from DataHub domain write API: %s", res.StatusCode, respBody)
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
//
// DataHub's child-domain guard queries OpenSearch, which is eventually
// consistent. When a child domain is deleted and the parent delete follows
// immediately (e.g. terraform destroy), the guard can fire spuriously because
// OpenSearch has not yet indexed the child's removal. This function retries on
// that specific error with short exponential backoff to let the index catch up.
// See: https://github.com/datahub-project/datahub/pull/17732
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

	const maxRetries = 3
	const baseDelay = 2 * time.Second

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return fmt.Errorf("domain deletion cancelled: %w", ctx.Err())
			case <-time.After(time.Duration(attempt) * baseDelay):
			}
		}

		var gqlResp genericGraphQLErrors
		if err := c.doGraphQL(ctx, body, &gqlResp); err != nil {
			return err
		}
		if len(gqlResp.Errors) == 0 {
			return nil
		}
		msg := gqlResp.Errors[0].Message
		if !strings.Contains(msg, "which has child domains") || attempt == maxRetries {
			return fmt.Errorf("DataHub API error: %s", msg)
		}
	}
	return nil
}
