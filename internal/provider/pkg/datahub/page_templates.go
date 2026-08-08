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

// PageTemplateURNPrefix is the URN namespace for dataHubPageTemplate entities.
const PageTemplateURNPrefix = "urn:li:dataHubPageTemplate:"

// PageTemplateURN builds the deterministic URN for a page-template id.
//
// As with modules, the provider must ALWAYS send a URN: PageTemplateService
// mints a random UUID when the input URN is null, which is the path the DataHub
// UI takes. Supplying it is what makes a template survive a destroy and
// re-apply with the same URN, so a default-template pointer keeps resolving.
func PageTemplateURN(id string) string {
	return PageTemplateURNPrefix + id
}

// PageTemplate is the read shape returned by GetPageTemplateByURN.
//
// Rows hold module URNs, not hydrated modules. That matches the write shape --
// PageTemplateRowInput.modules is [String!]! -- so the round-trip is symmetric.
// (The GraphQL read hydrates each URN into a full module object, but the
// provider reads through the OpenAPI v3 aspect, which stores URNs.)
type PageTemplate struct {
	URN         string
	ID          string
	Scope       string
	SurfaceType string
	Rows        [][]string
}

// UpsertPageTemplateInput groups the inputs for creating or updating a template.
type UpsertPageTemplateInput struct {
	ID          string
	Scope       string
	SurfaceType string
	Rows        [][]string
}

type pageTemplateEntity struct {
	URN     string `json:"urn"`
	KeyData *struct {
		Value struct {
			ID string `json:"id"`
		} `json:"value"`
	} `json:"dataHubPageTemplateKey"`
	Props *struct {
		Value struct {
			Rows []struct {
				Modules []string `json:"modules"`
			} `json:"rows"`
			Surface struct {
				SurfaceType string `json:"surfaceType"`
			} `json:"surface"`
			Visibility struct {
				Scope string `json:"scope"`
			} `json:"visibility"`
		} `json:"value"`
	} `json:"dataHubPageTemplateProperties"`
}

type upsertPageTemplateResponse struct {
	Data struct {
		UpsertPageTemplate *struct {
			URN string `json:"urn"`
		} `json:"upsertPageTemplate"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// UpsertPageTemplate creates or overwrites a page template and returns its URN.
//
// The full row list is always sent. DataHub replaces the template's rows with
// whatever this call carries, so the resource owns the complete layout and a
// module added to the template outside Terraform is removed on the next apply.
func (c *Client) UpsertPageTemplate(ctx context.Context, in UpsertPageTemplateInput) (string, error) {
	if c == nil {
		return "", errors.New("client is nil")
	}
	id := strings.TrimSpace(in.ID)
	if id == "" {
		return "", errors.New("page template id is required")
	}
	scope := in.Scope
	if scope == "" {
		scope = "GLOBAL"
	}
	surface := in.SurfaceType
	if surface == "" {
		surface = "HOME_PAGE"
	}

	const q = `
mutation upsertPageTemplate($input: UpsertPageTemplateInput!) {
  upsertPageTemplate(input: $input) {
    urn
  }
}`

	// rows is [PageTemplateRowInput!]! -- non-null, so send an empty array
	// rather than omitting it when a template has no rows.
	rows := make([]map[string]any, 0, len(in.Rows))
	for _, modules := range in.Rows {
		if modules == nil {
			modules = []string{}
		}
		rows = append(rows, map[string]any{"modules": modules})
	}

	body := map[string]any{
		"query": q,
		"variables": map[string]any{
			"input": map[string]any{
				"urn":         PageTemplateURN(id),
				"rows":        rows,
				"scope":       scope,
				"surfaceType": surface,
			},
		},
	}

	var resp upsertPageTemplateResponse
	if err := c.doGraphQL(ctx, body, &resp); err != nil {
		return "", err
	}
	if len(resp.Errors) > 0 {
		return "", fmt.Errorf("DataHub API error: %s", resp.Errors[0].Message)
	}
	if resp.Data.UpsertPageTemplate == nil || resp.Data.UpsertPageTemplate.URN == "" {
		return "", errors.New("DataHub returned no URN from upsertPageTemplate")
	}
	return resp.Data.UpsertPageTemplate.URN, nil
}

// BackupPageTemplateURN names the entity holding the layout a template had
// before Terraform adopted it.
//
// The backup lives in DataHub rather than only in Terraform state so that it
// survives the state file being lost -- which is the case where it matters
// most. Aspect version history is NOT a substitute: the global retention
// default is maxVersions 20, and a Terraform-managed template writes a version
// on every change, so the pre-adoption version is evicted by ordinary use of
// this resource. See docs/design/provider-home-page-layout.md.
//
// Templates are not enumerable through DataHub's GraphQL API -- there is no
// listPageTemplates query -- so a backup is invisible in the product and can
// only be found by an operator deliberately reading the OpenAPI v3 collection.
func BackupPageTemplateURN(id string) string {
	return PageTemplateURNPrefix + "tfprovider-backup-" + id
}

// CaptureTemplateBackup stores rows as the pre-adoption backup for id, unless a
// backup already exists.
//
// Never overwriting is the whole point. If a second adoption could overwrite an
// existing backup, then adopting a template Terraform had already edited would
// record *our* layout as the original and lose the real one irrecoverably.
// First capture wins, permanently.
func (c *Client) CaptureTemplateBackup(ctx context.Context, id string, rows [][]string) error {
	backupURN := BackupPageTemplateURN(id)

	existing, err := c.GetPageTemplateByURN(ctx, backupURN)
	if err != nil {
		return fmt.Errorf("checking for an existing backup: %w", err)
	}
	if existing != nil {
		return nil
	}

	_, err = c.UpsertPageTemplate(ctx, UpsertPageTemplateInput{
		ID:          strings.TrimPrefix(backupURN, PageTemplateURNPrefix),
		Scope:       "GLOBAL",
		SurfaceType: "HOME_PAGE",
		Rows:        rows,
	})
	if err != nil {
		return fmt.Errorf("writing the backup template: %w", err)
	}
	return nil
}

// OldestTemplateRows reads the oldest aspect version DataHub still holds for a
// template, or nil when no history survives.
//
// Used only to enrich a diagnostic, never to drive a restore. The oldest
// surviving version is whatever the 20-version retention window happens to
// still contain, so there is no way to know it is the layout Terraform
// displaced -- it may be any arbitrary point in someone else's history.
// Restoring it would silently write a layout nobody chose. Reporting it lets an
// operator decide.
func (c *Client) OldestTemplateRows(ctx context.Context, urn string) [][]string {
	path := fmt.Sprintf("/openapi/v3/entity/datahubpagetemplate/%s/datahubpagetemplateproperties?version=1", urn)
	req, err := c.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil
	}
	res, err := c.Do(req)
	if err != nil {
		return nil
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil
	}

	var aspect struct {
		Value struct {
			Rows []struct {
				Modules []string `json:"modules"`
			} `json:"rows"`
		} `json:"value"`
	}
	if err := json.NewDecoder(res.Body).Decode(&aspect); err != nil {
		return nil
	}
	out := make([][]string, 0, len(aspect.Value.Rows))
	for _, r := range aspect.Value.Rows {
		out = append(out, r.Modules)
	}
	return out
}

// globalSettingsHomePageEntity decodes just the home-page pointer out of the
// settings singleton. Everything else in globalSettingsInfo is ignored rather
// than modelled: this is a read, so unmodelled sections are simply not looked
// at, and nothing here ever writes the aspect back.
type globalSettingsHomePageEntity struct {
	GlobalSettingsInfo *struct {
		Value struct {
			HomePage *struct {
				DefaultTemplate string `json:"defaultTemplate"`
			} `json:"homePage"`
		} `json:"value"`
	} `json:"globalSettingsInfo"`
}

// GetDefaultHomePageTemplateURN returns the template URN the instance currently
// renders as everyone's home page, or "" when the pointer is unset.
//
// Read-only, and used only to protect a destroy: DataHub offers no way to move
// this pointer, so a delete that removes the template it names leaves the
// instance with no home page at all. See docs/design/provider-home-page-layout.md.
func (c *Client) GetDefaultHomePageTemplateURN(ctx context.Context) (string, error) {
	if c == nil {
		return "", errors.New("client is nil")
	}

	path := fmt.Sprintf("/openapi/v3/entity/%s/%s", globalSettingsEntityPath, GlobalSettingsURN)
	req, err := c.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return "", err
	}

	res, err := c.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusNotFound {
		return "", nil
	}
	if res.StatusCode >= http.StatusBadRequest {
		respBody, _ := io.ReadAll(res.Body)
		return "", fmt.Errorf("unexpected HTTP %d reading global settings: %s", res.StatusCode, respBody)
	}

	var entity globalSettingsHomePageEntity
	if err := json.NewDecoder(res.Body).Decode(&entity); err != nil {
		return "", fmt.Errorf("parsing global settings response: %w", err)
	}
	if entity.GlobalSettingsInfo == nil || entity.GlobalSettingsInfo.Value.HomePage == nil {
		return "", nil
	}
	return entity.GlobalSettingsInfo.Value.HomePage.DefaultTemplate, nil
}

// GetPageTemplateByURN reads a page template through the OpenAPI v3 entity
// endpoint. Returns (nil, nil) when the template does not exist.
func (c *Client) GetPageTemplateByURN(ctx context.Context, urn string) (*PageTemplate, error) {
	if c == nil {
		return nil, errors.New("client is nil")
	}
	urn = strings.TrimSpace(urn)
	if urn == "" {
		return nil, errors.New("URN is required")
	}

	path := fmt.Sprintf("/openapi/v3/entity/datahubpagetemplate/%s", urn)
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
		return nil, fmt.Errorf("DataHub rejected the request (HTTP %d): the calling principal needs the MANAGE_HOME_PAGE_TEMPLATES privilege to manage global page templates", res.StatusCode)
	}
	if res.StatusCode >= http.StatusBadRequest {
		respBody, _ := io.ReadAll(res.Body)
		return nil, fmt.Errorf("unexpected HTTP %d from DataHub page template API: %s", res.StatusCode, respBody)
	}

	var entity pageTemplateEntity
	if err := json.NewDecoder(res.Body).Decode(&entity); err != nil {
		return nil, fmt.Errorf("parsing page template entity response: %w", err)
	}
	if entity.KeyData == nil && entity.Props == nil {
		return nil, nil
	}

	out := &PageTemplate{URN: entity.URN}
	if out.URN == "" {
		out.URN = urn
	}
	if entity.KeyData != nil {
		out.ID = entity.KeyData.Value.ID
	}
	if out.ID == "" {
		out.ID = strings.TrimPrefix(out.URN, PageTemplateURNPrefix)
	}
	if entity.Props != nil {
		v := entity.Props.Value
		out.Scope = v.Visibility.Scope
		out.SurfaceType = v.Surface.SurfaceType
		out.Rows = make([][]string, 0, len(v.Rows))
		for _, row := range v.Rows {
			modules := row.Modules
			if modules == nil {
				modules = []string{}
			}
			out.Rows = append(out.Rows, modules)
		}
	}
	return out, nil
}

// DeletePageTemplate removes a page template by URN.
//
// Note this does not delete the modules the template referenced: modules are
// independent entities and may be shared between templates. Terraform destroys
// each module resource separately.
func (c *Client) DeletePageTemplate(ctx context.Context, urn string) error {
	if c == nil {
		return errors.New("client is nil")
	}
	if strings.TrimSpace(urn) == "" {
		return errors.New("URN is required")
	}

	const q = `
mutation deletePageTemplate($input: DeletePageTemplateInput!) {
  deletePageTemplate(input: $input)
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
