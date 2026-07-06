// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
)

// Service accounts are DataHub corpUser entities distinguished by a subTypes
// aspect containing "SERVICE_ACCOUNT", under a "service_" username/URN prefix.
// The provider writes those aspects directly via the OpenAPI v3 corpuser
// endpoint with a user-supplied deterministic id, rather than calling the
// UUID-minting createServiceAccount GraphQL mutation. See
// docs/design/datahub-model-and-resource-design.md and the roadmap.
const (
	serviceAccountUsernamePrefix = "service_"
	serviceAccountSubType        = "SERVICE_ACCOUNT"
	corpUserURNPrefixSA          = "urn:li:corpuser:"
)

// ErrServiceAccountsUnsupported is returned when the configured GMS instance
// does not expose service-account support (open-source DataHub older than Core
// v1.4.0). Callers should surface it as a clean Terraform diagnostic rather than
// a raw API error.
var ErrServiceAccountsUnsupported = errors.New(
	"service accounts require DataHub Core >= 1.4.0 or DataHub Cloud; " +
		"the configured GMS instance does not expose service account support",
)

// ServiceAccountURN builds the deterministic URN for a bare service-account id
// (i.e. the id without the "service_" prefix).
func ServiceAccountURN(id string) string {
	return corpUserURNPrefixSA + serviceAccountUsernamePrefix + id
}

// ServiceAccountIDFromURN returns the bare id (no "service_" prefix) for a
// service-account corpuser URN. Inputs without the prefixes are returned with
// whatever prefixes are present stripped.
func ServiceAccountIDFromURN(urn string) string {
	username := strings.TrimPrefix(urn, corpUserURNPrefixSA)
	return strings.TrimPrefix(username, serviceAccountUsernamePrefix)
}

// corpUserIsServiceAccount reports whether the corpUser carries the
// SERVICE_ACCOUNT subtype.
func corpUserIsServiceAccount(u *CorpUser) bool {
	return u != nil && slices.Contains(u.SubTypes, serviceAccountSubType)
}

// UpsertServiceAccount creates or updates a service account by writing the
// corpUser key, info (active, displayName, title=description), and subTypes
// ([SERVICE_ACCOUNT]) aspects via OpenAPI v3. id is the bare id; the "service_"
// prefix is applied here. Returns the deterministic URN.
func (c *Client) UpsertServiceAccount(ctx context.Context, id, displayName, description string) (string, error) {
	urn, err := c.UpsertCorpUser(ctx, UpsertCorpUserInput{
		Username:    serviceAccountUsernamePrefix + id,
		DisplayName: displayName,
		Title:       description,
		SubTypes:    []string{serviceAccountSubType},
	})
	if err != nil {
		if isServiceAccountsUnsupportedError(err.Error()) {
			return "", ErrServiceAccountsUnsupported
		}
		return "", err
	}
	return urn, nil
}

// GetServiceAccountByURN fetches a service account by URN via the strongly
// consistent OpenAPI v3 corpuser endpoint. It returns nil (no error) when the
// entity does not exist OR exists but is not a service account (missing the
// SERVICE_ACCOUNT subtype) -- both mean "not managed by this resource".
func (c *Client) GetServiceAccountByURN(ctx context.Context, urn string) (*CorpUser, error) {
	user, err := c.GetUserByURN(ctx, urn)
	if err != nil {
		return nil, err
	}
	if !corpUserIsServiceAccount(user) {
		return nil, nil
	}
	return user, nil
}

type listServiceAccountsPageResponse struct {
	Data struct {
		ListServiceAccounts struct {
			Total           int `json:"total"`
			ServiceAccounts []struct {
				URN string `json:"urn"`
			} `json:"serviceAccounts"`
		} `json:"listServiceAccounts"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// ListServiceAccountURNs returns the URNs of all service accounts.
//
// The underlying listServiceAccounts GraphQL query is backed by OpenSearch and
// is eventually consistent (and filters server-side to the SERVICE_ACCOUNT
// subtype). Entities created within the last few seconds may not appear. Use
// this for enumeration (import tooling, inventory data source), not for
// authoritative reads -- use GetServiceAccountByURN for those.
func (c *Client) ListServiceAccountURNs(ctx context.Context) ([]string, error) {
	if c == nil {
		return nil, errors.New("client is nil")
	}

	const q = `
query listServiceAccounts($input: ListServiceAccountsInput!) {
  listServiceAccounts(input: $input) {
    total
    serviceAccounts {
      urn
    }
  }
}`

	const pageSize = 100
	var urns []string
	start := 0

	for {
		body := map[string]any{
			"query": q,
			"variables": map[string]any{
				"input": map[string]any{
					"start": start,
					"count": pageSize,
				},
			},
		}

		req, err := c.NewRequest(ctx, http.MethodPost, "/api/graphql", body)
		if err != nil {
			return nil, err
		}

		res, err := c.Do(req)
		if err != nil {
			return nil, err
		}

		if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
			res.Body.Close()
			return nil, fmt.Errorf("DataHub rejected the request (HTTP %d): the calling principal needs the MANAGE_SERVICE_ACCOUNTS privilege", res.StatusCode)
		}
		if res.StatusCode >= http.StatusBadRequest {
			res.Body.Close()
			return nil, fmt.Errorf("unexpected HTTP %d from DataHub listServiceAccounts", res.StatusCode)
		}

		var gqlResp listServiceAccountsPageResponse
		decodeErr := json.NewDecoder(res.Body).Decode(&gqlResp)
		res.Body.Close()
		if decodeErr != nil {
			return nil, fmt.Errorf("parsing listServiceAccounts response: %w", decodeErr)
		}
		if len(gqlResp.Errors) > 0 {
			msg := gqlResp.Errors[0].Message
			if isServiceAccountsUnsupportedError(msg) {
				return nil, ErrServiceAccountsUnsupported
			}
			return nil, fmt.Errorf("DataHub API error: %s", msg)
		}

		page := gqlResp.Data.ListServiceAccounts.ServiceAccounts
		for _, sa := range page {
			urns = append(urns, sa.URN)
		}

		start += len(page)
		if start >= gqlResp.Data.ListServiceAccounts.Total || len(page) == 0 {
			break
		}
	}

	return urns, nil
}

// isServiceAccountsUnsupportedError reports whether a GraphQL/REST error
// indicates the service-account feature is absent from this GMS instance (OSS
// older than Core v1.4.0): the listServiceAccounts query field is undefined, or
// the subTypes aspect is not registered for the corpUser entity.
func isServiceAccountsUnsupportedError(msg string) bool {
	if strings.Contains(msg, "listServiceAccounts") && strings.Contains(msg, "undefined") {
		return true
	}
	if strings.Contains(msg, "FieldUndefined") &&
		strings.Contains(msg, "in type 'Query'") &&
		strings.Contains(msg, "ServiceAccount") {
		return true
	}
	// subTypes aspect not registered for corpUser on older servers.
	if strings.Contains(msg, "subTypes") &&
		(strings.Contains(msg, "not registered") ||
			strings.Contains(msg, "Unknown aspect") ||
			strings.Contains(msg, "unknown aspect")) {
		return true
	}
	return false
}
