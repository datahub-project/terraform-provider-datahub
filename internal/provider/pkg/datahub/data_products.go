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

// DataProduct is the read-shape returned by GetDataProductByURN.
type DataProduct struct {
	URN              string
	ID               string
	Name             string
	Description      string
	ExternalURL      string
	CustomProperties map[string]string
	Domain           string // first URN from the domains aspect
}

// dataProductEntity is the OpenAPI v3 response shape for
// GET /openapi/v3/entity/dataproduct/{urn}.
type dataProductEntity struct {
	URN     string `json:"urn"`
	KeyData *struct {
		Value struct {
			ID string `json:"id"`
		} `json:"value"`
	} `json:"dataProductKey,omitempty"`
	Props *struct {
		Value struct {
			Name             string            `json:"name"`
			Description      string            `json:"description"`
			ExternalURL      string            `json:"externalUrl"`
			CustomProperties map[string]string `json:"customProperties"`
		} `json:"value"`
	} `json:"dataProductProperties,omitempty"`
	Domains *struct {
		Value struct {
			Domains []string `json:"domains"`
		} `json:"value"`
	} `json:"domains,omitempty"`
}

type deleteDataProductResponse struct {
	Data struct {
		DeleteDataProduct bool `json:"deleteDataProduct"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// WriteDataProductProperties creates or updates the dataProductProperties
// (and, when domain is non-empty, the domains) aspect of a DataHub data
// product via the OpenAPI v3 entity collection endpoint.
//
// This is the correct write path for this entity type. The GraphQL
// createDataProduct mutation accepts only name/description/domainUrn/id and
// updateDataProduct accepts only name/description -- neither can set
// externalUrl or customProperties. Writing the aspects directly with a
// user-supplied URN matches the DataHub Python SDK convention
// (make_data_product_urn) and produces stable, importable URNs.
func (c *Client) WriteDataProductProperties(
	ctx context.Context,
	urn, name, description, externalURL string,
	customProperties map[string]string,
	domain string,
) error {
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
	if externalURL != "" {
		propsValue["externalUrl"] = externalURL
	}
	if len(customProperties) > 0 {
		propsValue["customProperties"] = customProperties
	}

	// Always include the domains aspect so that clearing domain = "" overwrites
	// a previously-set domain rather than leaving the old value in place.
	var domainList []string
	if domain != "" {
		domainList = []string{domain}
	} else {
		domainList = []string{}
	}

	entity := map[string]any{
		"urn": urn,
		"dataProductProperties": map[string]any{
			"value": propsValue,
		},
		"domains": map[string]any{
			"value": map[string]any{
				"domains": domainList,
			},
		},
	}

	payload := []map[string]any{entity}

	req, err := c.NewRequest(ctx, http.MethodPost, "/openapi/v3/entity/dataproduct?async=false", payload)
	if err != nil {
		return fmt.Errorf("building data product write request: %w", err)
	}

	res, err := c.Do(req)
	if err != nil {
		return fmt.Errorf("data product write request failed: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
		return fmt.Errorf("DataHub rejected the request (HTTP %d): the calling principal needs the MANAGE_DATA_PRODUCTS privilege", res.StatusCode)
	}
	if res.StatusCode >= http.StatusBadRequest {
		respBody, _ := io.ReadAll(res.Body)
		return fmt.Errorf("unexpected HTTP %d from DataHub data product write API: %s", res.StatusCode, respBody)
	}
	return nil
}

// GetDataProductByURN fetches a DataHub data product directly by URN via
// the OpenAPI v3 entity endpoint (MySQL, strongly consistent). Returns nil
// (no error) on 404.
func (c *Client) GetDataProductByURN(ctx context.Context, urn string) (*DataProduct, error) {
	if c == nil {
		return nil, errors.New("client is nil")
	}
	urn = strings.TrimSpace(urn)
	if urn == "" {
		return nil, errors.New("URN is required")
	}

	path := fmt.Sprintf("/openapi/v3/entity/dataproduct/%s", urn)
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
		return nil, fmt.Errorf("DataHub rejected the request (HTTP %d): the calling principal needs the MANAGE_DATA_PRODUCTS privilege", res.StatusCode)
	}
	if res.StatusCode >= http.StatusBadRequest {
		respBody, _ := io.ReadAll(res.Body)
		return nil, fmt.Errorf("unexpected HTTP %d from DataHub data product API: %s", res.StatusCode, respBody)
	}

	var entity dataProductEntity
	if err := json.NewDecoder(res.Body).Decode(&entity); err != nil {
		return nil, fmt.Errorf("parsing data product entity response: %w", err)
	}

	if entity.KeyData == nil && entity.Props == nil {
		return nil, nil
	}

	id := ""
	if entity.KeyData != nil {
		id = entity.KeyData.Value.ID
	}
	if id == "" {
		id = strings.TrimPrefix(entity.URN, "urn:li:dataProduct:")
	}

	dp := &DataProduct{URN: entity.URN, ID: id}
	if entity.Props != nil {
		dp.Name = entity.Props.Value.Name
		dp.Description = entity.Props.Value.Description
		dp.ExternalURL = entity.Props.Value.ExternalURL
		if len(entity.Props.Value.CustomProperties) > 0 {
			dp.CustomProperties = entity.Props.Value.CustomProperties
		}
	}
	if entity.Domains != nil && len(entity.Domains.Value.Domains) > 0 {
		dp.Domain = entity.Domains.Value.Domains[0]
	}
	return dp, nil
}

// DeleteDataProduct hard-deletes a DataHub data product by URN via the
// deleteDataProduct GraphQL mutation.
func (c *Client) DeleteDataProduct(ctx context.Context, urn string) error {
	if c == nil {
		return errors.New("client is nil")
	}
	urn = strings.TrimSpace(urn)
	if urn == "" {
		return errors.New("URN is required")
	}

	const q = `
mutation deleteDataProduct($urn: String!) {
  deleteDataProduct(urn: $urn)
}`
	body := map[string]any{
		"query":     q,
		"variables": map[string]any{"urn": urn},
	}
	var gqlResp deleteDataProductResponse
	if err := c.doGraphQL(ctx, body, &gqlResp); err != nil {
		return err
	}
	if len(gqlResp.Errors) > 0 {
		return fmt.Errorf("DataHub API error: %s", gqlResp.Errors[0].Message)
	}
	return nil
}
