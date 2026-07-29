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

// GlobalSettingsURN is the hard-coded URN of the globalSettings singleton.
// DataHub keeps exactly one instance per deployment; the id is always "0".
const GlobalSettingsURN = "urn:li:globalSettings:0"

// globalSettingsEntityPath is the OpenAPI v3 path segment for the singleton.
// Note the all-lowercase form, as with every other entity type.
const globalSettingsEntityPath = "globalsettings"

// ErrOrganizationDisplayPreferencesCloudOnly is returned when the
// updateOrganizationDisplayPreferences mutation is absent from the target's
// schema, which is the case on OSS DataHub: both the mutation and the
// MANAGE_ORGANIZATION_DISPLAY_PREFERENCES privilege backing it exist only in
// DataHub Cloud.
var ErrOrganizationDisplayPreferencesCloudOnly = errors.New(
	"organization display preferences require DataHub Cloud; " +
		"the configured GMS instance does not expose organization display preference management",
)

// isOrganizationDisplayPreferencesCloudOnlyError reports whether a GraphQL
// error message indicates the mutation is missing from the schema (OSS).
//
// Two distinct graphql-java error codes appear in practice, and which one fires
// depends on the order validation rules run in:
//   - FieldUndefined: the mutation field itself is absent from the schema.
//   - UnknownType: the operation's variable declaration names an input type the
//     schema does not register, so variable validation rejects the document
//     before field resolution is ever reached. This is what OSS Quickstart
//     actually returns ("Unknown type
//     'UpdateOrganizationDisplayPreferencesInput'"), which is why matching
//     FieldUndefined alone is not enough. Same trap as isCloudOnlyError.
func isOrganizationDisplayPreferencesCloudOnlyError(msg string) bool {
	if strings.Contains(msg, "FieldUndefined") &&
		strings.Contains(msg, "in type 'Mutation'") {
		return true
	}
	return strings.Contains(msg, "UnknownType") &&
		strings.Contains(msg, "OrganizationDisplayPreferences")
}

// OrganizationDisplayPreferences is the org-wide branding stored at
// globalSettingsInfo.visual on the globalSettings singleton.
//
// An empty string means "not set": DataHub offers no way to remove either
// field once written (an explicit null in the mutation input is ignored), and
// the frontend's fallback chains treat an empty value as absent, so writing ""
// is how the provider resets a field to DataHub's default branding.
type OrganizationDisplayPreferences struct {
	OrgName string
	LogoURL string
}

// globalSettingsVisualEntity is the OpenAPI v3 response shape for the subset
// of globalSettingsInfo this client reads. Sibling sections of the aspect
// (docPropagation, homePage, integrations, notifications, views, and the rest
// of visual) are deliberately not modelled: the provider never writes the
// aspect directly, so it has no reason to round-trip them.
type globalSettingsVisualEntity struct {
	URN                string `json:"urn"`
	GlobalSettingsInfo *struct {
		Value struct {
			Visual *struct {
				CustomOrgName string `json:"customOrgName"`
				CustomLogoURL string `json:"customLogoUrl"`
			} `json:"visual"`
		} `json:"value"`
	} `json:"globalSettingsInfo,omitempty"`
}

// GetOrganizationDisplayPreferences reads the org display preferences from the
// globalSettings singleton via the OpenAPI v3 entity endpoint (MySQL, strongly
// consistent). The singleton always exists on a live instance; a 404 is
// reported as found=false so callers can surface it rather than masking it.
//
// Absent fields and empty strings are both returned as "" - see
// OrganizationDisplayPreferences.
func (c *Client) GetOrganizationDisplayPreferences(ctx context.Context) (*OrganizationDisplayPreferences, bool, error) {
	if c == nil {
		return nil, false, errors.New("client is nil")
	}

	path := fmt.Sprintf("/openapi/v3/entity/%s/%s", globalSettingsEntityPath, GlobalSettingsURN)
	req, err := c.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, false, err
	}

	res, err := c.Do(req)
	if err != nil {
		return nil, false, err
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusNotFound {
		return nil, false, nil
	}
	if res.StatusCode >= http.StatusBadRequest {
		respBody, _ := io.ReadAll(res.Body)
		return nil, false, fmt.Errorf(
			"unexpected HTTP %d reading global settings: %s", res.StatusCode, respBody)
	}

	var entity globalSettingsVisualEntity
	if err := json.NewDecoder(res.Body).Decode(&entity); err != nil {
		return nil, false, fmt.Errorf("parsing global settings response: %w", err)
	}

	out := &OrganizationDisplayPreferences{}
	if entity.GlobalSettingsInfo != nil && entity.GlobalSettingsInfo.Value.Visual != nil {
		out.OrgName = entity.GlobalSettingsInfo.Value.Visual.CustomOrgName
		out.LogoURL = entity.GlobalSettingsInfo.Value.Visual.CustomLogoURL
	}
	return out, true, nil
}

// SetOrganizationDisplayPreferences writes both org display preference fields
// via the GraphQL updateOrganizationDisplayPreferences mutation, then verifies
// the result with a read-back.
//
// The provider owns both fields, so both are always sent: an empty value in
// want resets that field to DataHub's default branding. Sending an explicit
// null instead would be a silent no-op (verified against DataHub Cloud).
//
// The mutation is a per-field read-modify-write of globalSettingsInfo.visual -
// verified live: writing only one field leaves its sibling intact, and the
// aspect's other sections (helpLink, sampleDataSettings, and the unrelated
// docPropagation/homePage/integrations/notifications/views) are untouched. So
// this call cannot clobber settings the provider does not manage.
func (c *Client) SetOrganizationDisplayPreferences(ctx context.Context, want OrganizationDisplayPreferences) error {
	if c == nil {
		return errors.New("client is nil")
	}

	const q = `mutation updateOrganizationDisplayPreferences($input: UpdateOrganizationDisplayPreferencesInput!) {
  updateOrganizationDisplayPreferences(input: $input)
}`

	body := map[string]any{
		"query": q,
		"variables": map[string]any{
			"input": map[string]any{
				"customOrgName": want.OrgName,
				"customLogoUrl": want.LogoURL,
			},
		},
	}

	var raw struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := c.doGraphQL(ctx, body, &raw); err != nil {
		return err
	}
	if len(raw.Errors) > 0 {
		msg := raw.Errors[0].Message
		if isOrganizationDisplayPreferencesCloudOnlyError(msg) {
			return ErrOrganizationDisplayPreferencesCloudOnly
		}
		return fmt.Errorf("DataHub API error: %s", msg)
	}

	got, found, err := c.GetOrganizationDisplayPreferences(ctx)
	if err != nil {
		return fmt.Errorf("verifying organization display preferences write: %w", err)
	}
	if !found {
		return errors.New(
			"verifying organization display preferences write: global settings not found on read-back")
	}
	if *got != want {
		return fmt.Errorf(
			"DataHub accepted the organization display preferences write but the values did not persist "+
				"(got org_name=%q logo_url=%q, want org_name=%q logo_url=%q)",
			got.OrgName, got.LogoURL, want.OrgName, want.LogoURL)
	}
	return nil
}
