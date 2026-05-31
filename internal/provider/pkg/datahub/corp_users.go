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
	DisplayName  string
	Email        string
	Title        string
	Active       bool
	Status       string   // from corpUserStatus.status; empty when the aspect is absent
	NativeGroups []string // group URNs from nativeGroupMembership
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
		user.DisplayName = entity.Info.Value.DisplayName
		user.Email = entity.Info.Value.Email
		user.Title = entity.Info.Value.Title
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

	return user, nil
}
