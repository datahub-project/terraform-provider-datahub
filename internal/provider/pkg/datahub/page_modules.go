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

// PageModuleURNPrefix is the URN namespace for dataHubPageModule entities.
const PageModuleURNPrefix = "urn:li:dataHubPageModule:"

// PageModuleURN builds the deterministic URN for a page-module id.
//
// The provider must ALWAYS send a URN to upsertPageModule. PageModuleService
// mints a random UUID when the input URN is null -- the path the DataHub UI
// takes -- so omitting it would make every apply produce a new entity and
// leave the previous one orphaned. See docs/design/provider-home-page-layout.md.
func PageModuleURN(id string) string {
	return PageModuleURNPrefix + id
}

// PageModule is the read shape returned by GetPageModuleByURN.
type PageModule struct {
	URN    string
	ID     string
	Name   string
	Type   string
	Scope  string
	Params PageModuleParams
}

// PageModuleParams carries the type-specific configuration for a module. At
// most one field is populated; the majority of module types take no parameters
// at all, in which case every field is nil.
type PageModuleParams struct {
	Link            *LinkModuleParams
	RichText        *RichTextModuleParams
	AssetCollection *AssetCollectionModuleParams
	HierarchyView   *HierarchyViewModuleParams
	AgentCard       *AgentCardModuleParams
}

// LinkModuleParams configures a LINK module.
type LinkModuleParams struct {
	LinkURL     string `json:"linkUrl,omitempty"`
	ImageURL    string `json:"imageUrl,omitempty"`
	Description string `json:"description,omitempty"`
}

// RichTextModuleParams configures a RICH_TEXT module.
type RichTextModuleParams struct {
	Content string `json:"content,omitempty"`
}

// AssetCollectionModuleParams configures an ASSET_COLLECTION module.
type AssetCollectionModuleParams struct {
	AssetURNs         []string `json:"assetUrns,omitempty"`
	DynamicFilterJSON string   `json:"dynamicFilterJson,omitempty"`
}

// HierarchyViewModuleParams configures a HIERARCHY module.
type HierarchyViewModuleParams struct {
	AssetURNs                 []string `json:"assetUrns,omitempty"`
	ShowRelatedEntities       *bool    `json:"showRelatedEntities,omitempty"`
	RelatedEntitiesFilterJSON string   `json:"relatedEntitiesFilterJson,omitempty"`
}

// AgentCardModuleParams configures an AGENT_CARD module.
type AgentCardModuleParams struct {
	AgentURN    string `json:"agentUrn,omitempty"`
	DisplayMode string `json:"displayMode,omitempty"`
}

// UpsertPageModuleInput groups the inputs for creating or updating a module.
//
// Type is deliberately a plain string rather than a Go enum. The server's
// DataHubPageModuleType enum grew from 22 values to 30 in the single Cloud
// release v2.0.3 -> v2.1.0, so a compiled-in list would make every new module
// type unusable until the provider cut a release. The server validates the
// value and its rejection is translated for the user instead.
type UpsertPageModuleInput struct {
	ID     string
	Name   string
	Type   string
	Scope  string
	Params PageModuleParams
}

// pageModuleEntity is the OpenAPI v3 entity envelope for a page module.
type pageModuleEntity struct {
	URN     string `json:"urn"`
	KeyData *struct {
		Value struct {
			ID string `json:"id"`
		} `json:"value"`
	} `json:"dataHubPageModuleKey"`
	Props *struct {
		Value struct {
			Name       string `json:"name"`
			Type       string `json:"type"`
			Visibility struct {
				Scope string `json:"scope"`
			} `json:"visibility"`
			Params struct {
				LinkParams            *LinkModuleParams            `json:"linkParams"`
				RichTextParams        *RichTextModuleParams        `json:"richTextParams"`
				AssetCollectionParams *AssetCollectionModuleParams `json:"assetCollectionParams"`
				HierarchyViewParams   *HierarchyViewModuleParams   `json:"hierarchyViewParams"`
				AgentCardParams       *AgentCardModuleParams       `json:"agentCardParams"`
			} `json:"params"`
		} `json:"value"`
	} `json:"dataHubPageModuleProperties"`
}

type upsertPageModuleResponse struct {
	Data struct {
		UpsertPageModule *struct {
			URN string `json:"urn"`
		} `json:"upsertPageModule"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// UpsertPageModule creates or overwrites a page module and returns its URN.
//
// Writes go through GraphQL per the provider's write convention; the URN is
// always supplied so the entity is addressable deterministically across
// applies.
func (c *Client) UpsertPageModule(ctx context.Context, in UpsertPageModuleInput) (string, error) {
	if c == nil {
		return "", errors.New("client is nil")
	}
	id := strings.TrimSpace(in.ID)
	if id == "" {
		return "", errors.New("page module id is required")
	}
	if strings.TrimSpace(in.Type) == "" {
		return "", errors.New("page module type is required")
	}

	const q = `
mutation upsertPageModule($input: UpsertPageModuleInput!) {
  upsertPageModule(input: $input) {
    urn
  }
}`

	input := map[string]any{
		"urn":   PageModuleURN(id),
		"name":  in.Name,
		"type":  in.Type,
		"scope": in.Scope,
	}
	if params := in.Params.toGraphQL(); params != nil {
		input["params"] = params
	}

	body := map[string]any{
		"query":     q,
		"variables": map[string]any{"input": input},
	}

	var resp upsertPageModuleResponse
	if err := c.doGraphQL(ctx, body, &resp); err != nil {
		return "", err
	}
	if len(resp.Errors) > 0 {
		return "", fmt.Errorf("DataHub API error: %s", resp.Errors[0].Message)
	}
	if resp.Data.UpsertPageModule == nil || resp.Data.UpsertPageModule.URN == "" {
		return "", errors.New("DataHub returned no URN from upsertPageModule")
	}
	return resp.Data.UpsertPageModule.URN, nil
}

// toGraphQL renders the params into the PageModuleParamsInput shape, returning
// nil when the module type takes no parameters.
func (p PageModuleParams) toGraphQL() map[string]any {
	out := map[string]any{}
	if p.Link != nil {
		out["linkParams"] = p.Link
	}
	if p.RichText != nil {
		out["richTextParams"] = p.RichText
	}
	if p.AssetCollection != nil {
		out["assetCollectionParams"] = p.AssetCollection
	}
	if p.HierarchyView != nil {
		out["hierarchyViewParams"] = p.HierarchyView
	}
	if p.AgentCard != nil {
		out["agentCardParams"] = p.AgentCard
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// GetPageModuleByURN reads a page module through the OpenAPI v3 entity
// endpoint, which is MySQL-backed and strongly consistent. Returns (nil, nil)
// when the module does not exist.
func (c *Client) GetPageModuleByURN(ctx context.Context, urn string) (*PageModule, error) {
	if c == nil {
		return nil, errors.New("client is nil")
	}
	urn = strings.TrimSpace(urn)
	if urn == "" {
		return nil, errors.New("URN is required")
	}

	path := fmt.Sprintf("/openapi/v3/entity/datahubpagemodule/%s", urn)
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
		return nil, fmt.Errorf("DataHub rejected the request (HTTP %d): the calling principal needs the MANAGE_HOME_PAGE_TEMPLATES privilege to manage global page modules", res.StatusCode)
	}
	if res.StatusCode >= http.StatusBadRequest {
		respBody, _ := io.ReadAll(res.Body)
		return nil, fmt.Errorf("unexpected HTTP %d from DataHub page module API: %s", res.StatusCode, respBody)
	}

	var entity pageModuleEntity
	if err := json.NewDecoder(res.Body).Decode(&entity); err != nil {
		return nil, fmt.Errorf("parsing page module entity response: %w", err)
	}
	if entity.KeyData == nil && entity.Props == nil {
		return nil, nil
	}

	out := &PageModule{URN: entity.URN}
	if out.URN == "" {
		out.URN = urn
	}
	if entity.KeyData != nil {
		out.ID = entity.KeyData.Value.ID
	}
	if out.ID == "" {
		out.ID = strings.TrimPrefix(out.URN, PageModuleURNPrefix)
	}
	if entity.Props != nil {
		v := entity.Props.Value
		out.Name = v.Name
		out.Type = v.Type
		out.Scope = v.Visibility.Scope
		out.Params = PageModuleParams{
			Link:            v.Params.LinkParams,
			RichText:        v.Params.RichTextParams,
			AssetCollection: v.Params.AssetCollectionParams,
			HierarchyView:   v.Params.HierarchyViewParams,
			AgentCard:       v.Params.AgentCardParams,
		}
	}
	return out, nil
}

// DeletePageModule removes a page module by URN.
func (c *Client) DeletePageModule(ctx context.Context, urn string) error {
	if c == nil {
		return errors.New("client is nil")
	}
	if strings.TrimSpace(urn) == "" {
		return errors.New("URN is required")
	}

	const q = `
mutation deletePageModule($input: DeletePageModuleInput!) {
  deletePageModule(input: $input)
}`
	body := map[string]any{
		"query":     q,
		"variables": map[string]any{"input": map[string]any{"urn": urn}},
	}

	var resp genericGraphQLErrors
	if err := c.doGraphQL(ctx, body, &resp); err != nil {
		return err
	}
	if len(resp.Errors) > 0 {
		return fmt.Errorf("DataHub API error: %s", resp.Errors[0].Message)
	}
	return nil
}
