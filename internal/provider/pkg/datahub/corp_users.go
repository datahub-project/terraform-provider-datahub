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

// CorpUser is the read-shape returned by GetUserByURN. It carries the catalog
// metadata for a DataHub user. Login credentials are never exposed by the read
// path.
type CorpUser struct {
	URN          string
	Username     string
	FullName     string
	DisplayName  string
	Email        string
	Title        string
	InfoTitle    string // raw corpUserInfo.title, before corpUserEditableInfo shadows it
	Active       bool
	Status       string   // from corpUserStatus.status; empty when the aspect is absent
	NativeGroups []string // group URNs from nativeGroupMembership
	SubTypes     []string // typeNames from the subTypes aspect (e.g. "SERVICE_ACCOUNT")
}

// UpsertCorpUserInput carries the fields for creating or updating a corpUser
// catalog record via the OpenAPI v3 entity endpoint.
type UpsertCorpUserInput struct {
	Username    string
	FullName    string
	DisplayName string
	Email       string
	Title       string
	// SubTypes, when non-empty, writes the subTypes aspect (typeNames). Used to
	// mark a corpUser as a service account ("SERVICE_ACCOUNT"). Leave nil for
	// ordinary human users so their subTypes aspect is untouched.
	SubTypes []string
}

// corpUserEntity is the OpenAPI v3 response shape for
// GET /openapi/v3/entity/corpuser/{urn}. Aspects are optional: a freshly
// provisioned or system user may omit corpUserStatus or nativeGroupMembership.
type corpUserEntity struct {
	URN     string `json:"urn"`
	KeyData *struct {
		Value struct {
			Username string `json:"username"`
		} `json:"value"`
	} `json:"corpUserKey,omitempty"`
	Info *struct {
		Value struct {
			FullName    string `json:"fullName"`
			DisplayName string `json:"displayName"`
			Email       string `json:"email"`
			Title       string `json:"title"`
			Active      bool   `json:"active"`
		} `json:"value"`
	} `json:"corpUserInfo,omitempty"`
	EditableInfo *struct {
		Value struct {
			Email string `json:"email"`
			Title string `json:"title"`
		} `json:"value"`
	} `json:"corpUserEditableInfo,omitempty"`
	Status *struct {
		Value struct {
			Status string `json:"status"`
		} `json:"value"`
	} `json:"corpUserStatus,omitempty"`
	NativeGroupMembership *struct {
		Value struct {
			NativeGroups []string `json:"nativeGroups"`
		} `json:"value"`
	} `json:"nativeGroupMembership,omitempty"`
	SubTypes *struct {
		Value struct {
			TypeNames []string `json:"typeNames"`
		} `json:"value"`
	} `json:"subTypes,omitempty"`
}

// GetUserByURN fetches a DataHub user directly by URN via the OpenAPI v3 entity
// endpoint, which reads from the primary datastore (MySQL) and is strongly
// consistent. Returns nil (no error) on HTTP 404.
//
// editableInfo (UI-edited) takes precedence over corpUserInfo for email and
// title, matching how the DataHub UI resolves these fields.
func (c *Client) GetUserByURN(ctx context.Context, urn string) (*CorpUser, error) {
	if c == nil {
		return nil, errors.New("client is nil")
	}
	urn = strings.TrimSpace(urn)
	if urn == "" {
		return nil, errors.New("URN is required")
	}

	path := fmt.Sprintf("/openapi/v3/entity/corpuser/%s", urn)
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
		return nil, fmt.Errorf("DataHub rejected the request (HTTP %d): the calling principal needs the MANAGE_USERS_AND_GROUPS privilege", res.StatusCode)
	}
	if res.StatusCode >= http.StatusBadRequest {
		respBody, _ := io.ReadAll(res.Body)
		return nil, fmt.Errorf("unexpected HTTP %d from DataHub corpuser API: %s", res.StatusCode, respBody)
	}

	var entity corpUserEntity
	if err := json.NewDecoder(res.Body).Decode(&entity); err != nil {
		return nil, fmt.Errorf("parsing corpuser entity response: %w", err)
	}

	// A user with no recognizable aspects is treated as not found: the URN may
	// exist as a dangling key with no catalog data.
	if entity.Info == nil && entity.KeyData == nil {
		return nil, nil
	}

	user := &CorpUser{URN: entity.URN}

	if entity.KeyData != nil {
		user.Username = entity.KeyData.Value.Username
	}
	if user.Username == "" {
		user.Username = strings.TrimPrefix(entity.URN, "urn:li:corpuser:")
	}

	if entity.Info != nil {
		user.FullName = entity.Info.Value.FullName
		user.DisplayName = entity.Info.Value.DisplayName
		user.Email = entity.Info.Value.Email
		user.Title = entity.Info.Value.Title
		user.InfoTitle = entity.Info.Value.Title
		user.Active = entity.Info.Value.Active
	}
	// editableInfo (UI edits) wins for email and title when populated.
	if entity.EditableInfo != nil {
		if entity.EditableInfo.Value.Email != "" {
			user.Email = entity.EditableInfo.Value.Email
		}
		if entity.EditableInfo.Value.Title != "" {
			user.Title = entity.EditableInfo.Value.Title
		}
	}
	if entity.Status != nil {
		user.Status = entity.Status.Value.Status
	}
	if entity.NativeGroupMembership != nil {
		user.NativeGroups = entity.NativeGroupMembership.Value.NativeGroups
	}
	if entity.SubTypes != nil {
		user.SubTypes = entity.SubTypes.Value.TypeNames
	}

	return user, nil
}

// UpsertCorpUser creates or updates a corpUser catalog record via the OpenAPI
// v3 entity endpoint. This is an upsert: it works for new entities and
// pre-existing ones (e.g. created by signUp or SSO JIT).
func (c *Client) UpsertCorpUser(ctx context.Context, in UpsertCorpUserInput) (string, error) {
	if c == nil {
		return "", errors.New("client is nil")
	}
	in.Username = strings.TrimSpace(in.Username)
	if in.Username == "" {
		return "", errors.New("username is required")
	}

	urn := "urn:li:corpuser:" + in.Username

	infoValue := map[string]any{
		"active": true,
	}
	if in.DisplayName != "" {
		infoValue["displayName"] = in.DisplayName
	}
	if in.FullName != "" {
		infoValue["fullName"] = in.FullName
	}
	if in.Email != "" {
		infoValue["email"] = in.Email
	}
	if in.Title != "" {
		infoValue["title"] = in.Title
	}

	aspects := map[string]any{
		"urn": urn,
		"corpUserKey": map[string]any{
			"value": map[string]any{
				"username": in.Username,
			},
		},
		"corpUserInfo": map[string]any{
			"value": infoValue,
		},
	}
	// Only write the subTypes aspect when requested (service accounts). Omitting
	// it leaves an ordinary user's subTypes untouched.
	if len(in.SubTypes) > 0 {
		aspects["subTypes"] = map[string]any{
			"value": map[string]any{
				"typeNames": in.SubTypes,
			},
		}
	}

	entity := []map[string]any{aspects}

	req, err := c.NewRequest(ctx, http.MethodPost, "/openapi/v3/entity/corpuser?async=false", entity)
	if err != nil {
		return "", fmt.Errorf("building corpuser upsert request: %w", err)
	}

	res, err := c.Do(req)
	if err != nil {
		return "", fmt.Errorf("corpuser upsert request failed: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
		return "", fmt.Errorf("DataHub rejected the request (HTTP %d): the calling principal needs the MANAGE_USERS_AND_GROUPS privilege", res.StatusCode)
	}
	if res.StatusCode >= http.StatusBadRequest {
		respBody, _ := io.ReadAll(res.Body)
		return "", fmt.Errorf("unexpected HTTP %d from DataHub corpuser upsert API: %s", res.StatusCode, respBody)
	}

	return urn, nil
}

// DeleteUser hard-deletes a DataHub user by URN via the removeUser GraphQL
// mutation. This removes the entity and all its aspects (including credentials
// and references). Returns nil if the user is already gone.
func (c *Client) DeleteUser(ctx context.Context, urn string) error {
	if c == nil {
		return errors.New("client is nil")
	}
	urn = strings.TrimSpace(urn)
	if urn == "" {
		return errors.New("URN is required")
	}

	const q = `
mutation removeUser($urn: String!) {
  removeUser(urn: $urn)
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
