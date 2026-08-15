// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

// OAuth Authorization Server management for DataHub Cloud.
//
// An oauthAuthorizationServer entity is an outbound OAuth client
// configuration: DataHub acts as the OAuth client, obtaining tokens from an
// external identity provider to call an external API. Today its only consumer
// is the AI plugin registry (AiPluginConfig.oauthConfig.serverUrn).
//
// API shape (DataHub Cloud GraphQL, service.graphql):
//   - Create/Update: upsertOAuthAuthorizationServer(input) - full upsert with
//     per-field null semantics (see UpsertOAuthAuthorizationServerInput)
//   - Delete: deleteOAuthAuthorizationServer(urn) - hard delete with cascades
//   - List: listOAuthAuthorizationServers(input) - OpenSearch-backed
//
// Read uses the OpenAPI v3 entity endpoint (MySQL, strongly consistent), per
// the project rule for Read/ImportState paths.
//
// The entity type is fork-only: it is absent from the OSS entity registry, so
// every operation returns ErrOAuthAuthorizationServerCloudOnly on OSS DataHub.
//
// SECRET HANDLING: the server never stores the client secret on the entity.
// upsertOAuthAuthorizationServer encrypts it via SecretService and writes it as
// a new dataHubSecret entity (id "<serverId>_clientSecret_<uuid>"), storing
// only that secret's URN on the properties aspect. Every non-empty write mints
// a NEW secret entity and orphans the previous one; nothing in the API cleans
// up the trail. Deletion removes only the CURRENT secret.

package datahub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
)

// OAuthAuthorizationServerURNPrefix is the URN prefix for the
// oauthAuthorizationServer entity type.
const OAuthAuthorizationServerURNPrefix = "urn:li:oauthAuthorizationServer:"

// oauthAuthorizationServerEntityPath is the OpenAPI v3 path segment for the
// entity type. Note the all-lowercase form, as with every other entity type.
const oauthAuthorizationServerEntityPath = "oauthauthorizationserver"

// ErrOAuthAuthorizationServerCloudOnly is returned when the OAuth
// authorization server GraphQL operations are absent from the target's schema,
// which is the case on OSS DataHub: the entity type and its mutations exist
// only in DataHub Cloud.
var ErrOAuthAuthorizationServerCloudOnly = errors.New(
	"datahub_oauth_authorization_server requires DataHub Cloud; " +
		"the configured GMS instance does not expose OAuth authorization server management",
)

// isOAuthAuthorizationServerCloudOnlyError reports whether a GraphQL error
// message indicates the operation is missing from the schema (OSS).
//
// Two distinct graphql-java error codes appear in practice, and which one
// fires depends on the order validation rules run in:
//   - FieldUndefined: the mutation/query field itself is absent from the
//     schema. Restricted to "in type 'Mutation'"/"in type 'Query'" to avoid
//     false positives from FieldUndefined on sub-fields of result types.
//   - UnknownType: the operation's variable declaration names an input type
//     the schema does not register, so variable validation rejects the
//     document before field resolution is reached. Matching FieldUndefined
//     alone is not enough - the same trap as isCloudOnlyError.
func isOAuthAuthorizationServerCloudOnlyError(msg string) bool {
	if strings.Contains(msg, "FieldUndefined") &&
		(strings.Contains(msg, "in type 'Mutation'") || strings.Contains(msg, "in type 'Query'")) {
		return true
	}
	return strings.Contains(msg, "UnknownType") &&
		strings.Contains(msg, "OAuthAuthorizationServer")
}

// OAuthAuthorizationServer is the read-shape assembled from the OpenAPI v3
// entity endpoint.
//
// Pointer-typed fields distinguish "absent from the aspect" (nil) from an
// explicitly stored empty string. That distinction is meaningful for
// AuthScheme, where an explicit "" means "inject the raw token with no scheme
// prefix" while absence falls back to the server default ("Bearer").
type OAuthAuthorizationServer struct {
	URN         string
	ID          string
	DisplayName string
	Description *string
	// ClientID is empty when not set (the provider writes "" to clear it, so
	// "" and absent are equivalent).
	ClientID string
	// ClientSecretURN is the URN of the dataHubSecret holding the encrypted
	// client secret. Empty when no secret is configured. The plaintext is
	// never returned by any read path.
	ClientSecretURN       string
	AuthorizationURL      string
	TokenURL              string
	Scopes                []string
	TokenAuthMethod       string
	AdditionalTokenParams map[string]string
	AdditionalAuthParams  map[string]string
	AuthLocation          string
	AuthHeaderName        string
	AuthScheme            *string
	AuthQueryParam        *string
}

// HasClientSecret reports whether a client secret is configured.
func (s *OAuthAuthorizationServer) HasClientSecret() bool {
	return s != nil && s.ClientSecretURN != ""
}

// ClientSecretAction expresses the caller's intent for the clientSecret field
// on upsert. The server's contract is: null preserves the stored secret, empty
// string clears it, and a non-empty string encrypts and stores a NEW secret
// (orphaning the previous one).
type ClientSecretAction int

const (
	// ClientSecretPreserve sends an explicit null: the stored secret (if any)
	// is kept. This is the default for updates that do not touch the secret,
	// and it is what makes every unrelated update NOT mint an orphan secret.
	ClientSecretPreserve ClientSecretAction = iota
	// ClientSecretClear sends an empty string: the stored secret reference is
	// dropped (the old dataHubSecret entity itself is orphaned, not deleted).
	ClientSecretClear
	// ClientSecretSet sends the provided value: a new encrypted dataHubSecret
	// is created and referenced.
	ClientSecretSet
)

// UpsertOAuthAuthorizationServerInput groups the inputs for
// upsertOAuthAuthorizationServer.
//
// The resolver rebuilds the properties aspect on every call with NON-UNIFORM
// per-field null semantics (read from the Cloud resolver source):
//
//   - clientId, authorizationUrl, tokenUrl, scopes: null PRESERVES the stored
//     value ("" / [] clears)
//   - clientSecret: null preserves, "" clears, non-empty writes a new secret
//   - description, additionalTokenParams, additionalAuthParams,
//     authQueryParam: null CLEARS (no fallback branch)
//   - tokenAuthMethod, authLocation, authHeaderName: null falls back to a
//     hard-coded server default (POST_BODY, HEADER, "Authorization")
//   - authScheme: null clears at the resolver, BUT the GraphQL input type
//     declares a literal default ("Bearer") that applies when the field is
//     OMITTED from the variables - so omission and explicit null differ
//
// The client therefore includes EVERY field in the variables on every call,
// so that no value is ever left to a preserve branch or an input-literal
// default the caller did not choose. Omitting a preserve-on-null field would
// silently keep a stale value; omitting a clear-on-null field would silently
// wipe it; omitting a defaulted field would silently write the default.
type UpsertOAuthAuthorizationServerInput struct {
	// ID is the server id (URN suffix). Always sent: omitting it makes the
	// server mint a random UUID, which the provider's URN-determinism rule
	// forbids.
	ID          string
	DisplayName string
	// Description is cleared when nil (explicit null in the input).
	Description *string
	// ClientID is sent verbatim; use "" to clear (null would preserve).
	ClientID string
	// ClientSecretAction selects the null/""/value intent for clientSecret;
	// ClientSecretValue is used only with ClientSecretSet.
	ClientSecretAction ClientSecretAction
	ClientSecretValue  string
	// AuthorizationURL and TokenURL are sent verbatim; "" clears.
	AuthorizationURL string
	TokenURL         string
	// Scopes is always sent as a (possibly empty) array; an empty array
	// clears the stored scopes.
	Scopes []string
	// TokenAuthMethod, AuthLocation and AuthHeaderName are always sent; the
	// caller supplies the effective value (the provider's schema defaults
	// mirror the server's).
	TokenAuthMethod string
	AuthLocation    string
	AuthHeaderName  string
	// AuthScheme: nil sends an explicit null (clears); "" is a meaningful
	// value (raw token, no scheme prefix).
	AuthScheme *string
	// AuthQueryParam: nil sends an explicit null (clears).
	AuthQueryParam *string
	// AdditionalTokenParams / AdditionalAuthParams: nil sends an explicit
	// null (clears); otherwise the full map is sent.
	AdditionalTokenParams map[string]string
	AdditionalAuthParams  map[string]string
}

// stringMapEntries converts a map to the GraphQL [StringMapEntryInput!] shape,
// sorted by key so request bodies are deterministic (and testable).
func stringMapEntries(m map[string]string) []map[string]string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	entries := make([]map[string]string, 0, len(keys))
	for _, k := range keys {
		entries = append(entries, map[string]string{"key": k, "value": m[k]})
	}
	return entries
}

// upsertVariables builds the full GraphQL input map. Every input field is
// present in the map on every call - see UpsertOAuthAuthorizationServerInput.
func (in UpsertOAuthAuthorizationServerInput) upsertVariables() map[string]any {
	vars := map[string]any{
		"id":               in.ID,
		"displayName":      in.DisplayName,
		"description":      nil,
		"clientId":         in.ClientID,
		"clientSecret":     nil,
		"authorizationUrl": in.AuthorizationURL,
		"tokenUrl":         in.TokenURL,
		"tokenAuthMethod":  in.TokenAuthMethod,
		"authLocation":     in.AuthLocation,
		"authHeaderName":   in.AuthHeaderName,
		"authScheme":       nil,
		"authQueryParam":   nil,
	}
	if in.Description != nil {
		vars["description"] = *in.Description
	}
	switch in.ClientSecretAction {
	case ClientSecretPreserve:
		// leave the explicit null
	case ClientSecretClear:
		vars["clientSecret"] = ""
	case ClientSecretSet:
		vars["clientSecret"] = in.ClientSecretValue
	}
	scopes := in.Scopes
	if scopes == nil {
		scopes = []string{}
	}
	vars["scopes"] = scopes
	if in.AuthScheme != nil {
		vars["authScheme"] = *in.AuthScheme
	}
	if in.AuthQueryParam != nil {
		vars["authQueryParam"] = *in.AuthQueryParam
	}
	if in.AdditionalTokenParams != nil {
		vars["additionalTokenParams"] = stringMapEntries(in.AdditionalTokenParams)
	} else {
		vars["additionalTokenParams"] = nil
	}
	if in.AdditionalAuthParams != nil {
		vars["additionalAuthParams"] = stringMapEntries(in.AdditionalAuthParams)
	} else {
		vars["additionalAuthParams"] = nil
	}
	return vars
}

type upsertOAuthAuthorizationServerResponse struct {
	Data struct {
		UpsertOAuthAuthorizationServer *struct {
			URN string `json:"urn"`
		} `json:"upsertOAuthAuthorizationServer"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// UpsertOAuthAuthorizationServer creates or updates an OAuth authorization
// server and returns its URN. Callers should follow with a
// GetOAuthAuthorizationServerByURN read-back (strongly consistent) to verify
// the write and recover computed fields.
func (c *Client) UpsertOAuthAuthorizationServer(ctx context.Context, in UpsertOAuthAuthorizationServerInput) (string, error) {
	if c == nil {
		return "", errors.New("client is nil")
	}
	if strings.TrimSpace(in.ID) == "" {
		return "", errors.New("id is required")
	}
	if strings.TrimSpace(in.DisplayName) == "" {
		return "", errors.New("displayName is required")
	}

	const q = `
mutation upsertOAuthAuthorizationServer($input: UpsertOAuthAuthorizationServerInput!) {
  upsertOAuthAuthorizationServer(input: $input) {
    urn
  }
}`

	body := map[string]any{
		"query":     q,
		"variables": map[string]any{"input": in.upsertVariables()},
	}

	var gqlResp upsertOAuthAuthorizationServerResponse
	if err := c.doOAuthGraphQL(ctx, body, &gqlResp); err != nil {
		return "", err
	}
	if len(gqlResp.Errors) > 0 {
		msg := gqlResp.Errors[0].Message
		if isOAuthAuthorizationServerCloudOnlyError(msg) {
			return "", ErrOAuthAuthorizationServerCloudOnly
		}
		return "", fmt.Errorf("DataHub API error: %s", msg)
	}

	urn := ""
	if gqlResp.Data.UpsertOAuthAuthorizationServer != nil {
		urn = gqlResp.Data.UpsertOAuthAuthorizationServer.URN
	}
	if urn == "" {
		urn = OAuthAuthorizationServerURNPrefix + in.ID
	}
	return urn, nil
}

// oauthAuthorizationServerEntity is the OpenAPI v3 response shape for
// GET /openapi/v3/entity/oauthauthorizationserver/{urn}.
type oauthAuthorizationServerEntity struct {
	URN                         string `json:"urn"`
	OAuthAuthorizationServerKey *struct {
		Value struct {
			ID string `json:"id"`
		} `json:"value"`
	} `json:"oauthAuthorizationServerKey,omitempty"`
	OAuthAuthorizationServerProperties *struct {
		Value struct {
			DisplayName           string            `json:"displayName"`
			Description           *string           `json:"description"`
			ClientID              string            `json:"clientId"`
			ClientSecretURN       string            `json:"clientSecretUrn"`
			AuthorizationURL      string            `json:"authorizationUrl"`
			TokenURL              string            `json:"tokenUrl"`
			Scopes                []string          `json:"scopes"`
			TokenAuthMethod       string            `json:"tokenAuthMethod"`
			AdditionalTokenParams map[string]string `json:"additionalTokenParams"`
			AdditionalAuthParams  map[string]string `json:"additionalAuthParams"`
			AuthLocation          string            `json:"authLocation"`
			AuthHeaderName        string            `json:"authHeaderName"`
			AuthScheme            *string           `json:"authScheme"`
			AuthQueryParam        *string           `json:"authQueryParam"`
		} `json:"value"`
	} `json:"oauthAuthorizationServerProperties,omitempty"`
}

// GetOAuthAuthorizationServerByURN fetches an OAuth authorization server by
// URN via the OpenAPI v3 entity endpoint (MySQL, strongly consistent).
// Returns nil (no error) on HTTP 404, and also when the entity exists without
// its properties aspect - an aspect-less husk carries nothing the resource can
// manage, so callers treat it as gone.
func (c *Client) GetOAuthAuthorizationServerByURN(ctx context.Context, urn string) (*OAuthAuthorizationServer, error) {
	if c == nil {
		return nil, errors.New("client is nil")
	}
	urn = strings.TrimSpace(urn)
	if urn == "" {
		return nil, errors.New("URN is required")
	}

	path := fmt.Sprintf("/openapi/v3/entity/%s/%s", oauthAuthorizationServerEntityPath, urn)
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
		return nil, fmt.Errorf("DataHub rejected the request (HTTP %d): the calling principal needs the MANAGE_CONNECTIONS privilege", res.StatusCode)
	}
	if res.StatusCode >= http.StatusBadRequest {
		respBody, _ := io.ReadAll(res.Body)
		return nil, fmt.Errorf("unexpected HTTP %d from DataHub OAuth authorization server API: %s", res.StatusCode, respBody)
	}

	var entity oauthAuthorizationServerEntity
	if err := json.NewDecoder(res.Body).Decode(&entity); err != nil {
		return nil, fmt.Errorf("parsing OAuth authorization server entity response: %w", err)
	}

	if entity.OAuthAuthorizationServerProperties == nil {
		return nil, nil
	}

	id := ""
	if entity.OAuthAuthorizationServerKey != nil {
		id = entity.OAuthAuthorizationServerKey.Value.ID
	}
	if id == "" {
		id = strings.TrimPrefix(entity.URN, OAuthAuthorizationServerURNPrefix)
	}

	p := entity.OAuthAuthorizationServerProperties.Value
	return &OAuthAuthorizationServer{
		URN:                   entity.URN,
		ID:                    id,
		DisplayName:           p.DisplayName,
		Description:           p.Description,
		ClientID:              p.ClientID,
		ClientSecretURN:       p.ClientSecretURN,
		AuthorizationURL:      p.AuthorizationURL,
		TokenURL:              p.TokenURL,
		Scopes:                p.Scopes,
		TokenAuthMethod:       p.TokenAuthMethod,
		AdditionalTokenParams: p.AdditionalTokenParams,
		AdditionalAuthParams:  p.AdditionalAuthParams,
		AuthLocation:          p.AuthLocation,
		AuthHeaderName:        p.AuthHeaderName,
		AuthScheme:            p.AuthScheme,
		AuthQueryParam:        p.AuthQueryParam,
	}, nil
}

type deleteOAuthAuthorizationServerResponse struct {
	Data struct {
		DeleteOAuthAuthorizationServer bool `json:"deleteOAuthAuthorizationServer"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// DeleteOAuthAuthorizationServer hard-deletes an OAuth authorization server
// via the deleteOAuthAuthorizationServer mutation. Idempotent: a not-found
// error is treated as success (the entity may already have been removed by
// deleteAiPlugin's cascade, which deletes an unshared server when the last
// referencing plugin is deleted).
//
// The mutation's cascade rewrites globalSettingsInfo (flipping referencing
// plugins to authType NONE) via an unlocked server-side read-modify-write, so
// the call is serialised behind the client's globalSettings mutex to keep two
// resources in one apply from losing each other's singleton writes.
func (c *Client) DeleteOAuthAuthorizationServer(ctx context.Context, urn string) error {
	if c == nil {
		return errors.New("client is nil")
	}
	urn = strings.TrimSpace(urn)
	if urn == "" {
		return errors.New("URN is required")
	}

	unlock := c.globalSettingsLocks.lock(GlobalSettingsURN)
	defer unlock()

	const q = `
mutation deleteOAuthAuthorizationServer($urn: String!) {
  deleteOAuthAuthorizationServer(urn: $urn)
}`

	body := map[string]any{
		"query":     q,
		"variables": map[string]any{"urn": urn},
	}

	var gqlResp deleteOAuthAuthorizationServerResponse
	if err := c.doOAuthGraphQL(ctx, body, &gqlResp); err != nil {
		return err
	}
	if len(gqlResp.Errors) > 0 {
		msg := gqlResp.Errors[0].Message
		if isOAuthAuthorizationServerCloudOnlyError(msg) {
			return ErrOAuthAuthorizationServerCloudOnly
		}
		if strings.Contains(msg, "not found") || strings.Contains(msg, "does not exist") {
			return nil
		}
		return fmt.Errorf("DataHub API error: %s", msg)
	}
	return nil
}

type listOAuthAuthorizationServersResponse struct {
	Data struct {
		ListOAuthAuthorizationServers *struct {
			Start                int `json:"start"`
			Count                int `json:"count"`
			Total                int `json:"total"`
			AuthorizationServers []struct {
				URN string `json:"urn"`
			} `json:"authorizationServers"`
		} `json:"listOAuthAuthorizationServers"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// ListOAuthAuthorizationServerURNs enumerates every OAuth authorization server
// URN via the listOAuthAuthorizationServers GraphQL query.
//
// The list is OpenSearch-backed and eventually consistent: an entity created
// seconds ago may be missing. Suitable for bulk-import enumeration; never used
// on a Read or ImportState path.
func (c *Client) ListOAuthAuthorizationServerURNs(ctx context.Context) ([]string, error) {
	if c == nil {
		return nil, errors.New("client is nil")
	}

	const q = `
query listOAuthAuthorizationServers($input: ListOAuthAuthorizationServersInput!) {
  listOAuthAuthorizationServers(input: $input) {
    start
    count
    total
    authorizationServers {
      urn
    }
  }
}`

	const pageSize = 100
	var urns []string
	for start := 0; ; {
		body := map[string]any{
			"query": q,
			"variables": map[string]any{
				"input": map[string]any{"start": start, "count": pageSize},
			},
		}

		var gqlResp listOAuthAuthorizationServersResponse
		if err := c.doOAuthGraphQL(ctx, body, &gqlResp); err != nil {
			return nil, err
		}
		if len(gqlResp.Errors) > 0 {
			msg := gqlResp.Errors[0].Message
			if isOAuthAuthorizationServerCloudOnlyError(msg) {
				return nil, ErrOAuthAuthorizationServerCloudOnly
			}
			return nil, fmt.Errorf("DataHub API error: %s", msg)
		}

		result := gqlResp.Data.ListOAuthAuthorizationServers
		if result == nil || len(result.AuthorizationServers) == 0 {
			return urns, nil
		}
		for _, s := range result.AuthorizationServers {
			urns = append(urns, s.URN)
		}
		start += len(result.AuthorizationServers)
		if start >= result.Total {
			return urns, nil
		}
	}
}

// doOAuthGraphQL posts a GraphQL body and decodes the response,
// mapping HTTP 401/403 to the MANAGE_CONNECTIONS privilege message shared by
// the OAuth authorization server operations.
func (c *Client) doOAuthGraphQL(ctx context.Context, body, out any) error {
	req, err := c.NewRequest(ctx, http.MethodPost, "/api/graphql", body)
	if err != nil {
		return err
	}

	res, err := c.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
		return fmt.Errorf("DataHub rejected the request (HTTP %d): the calling principal needs the MANAGE_CONNECTIONS privilege", res.StatusCode)
	}
	if res.StatusCode >= http.StatusBadRequest {
		respBody, _ := io.ReadAll(res.Body)
		return fmt.Errorf("unexpected HTTP %d from DataHub OAuth authorization server API: %s", res.StatusCode, respBody)
	}

	if err := json.NewDecoder(res.Body).Decode(out); err != nil {
		return fmt.Errorf("parsing OAuth authorization server response: %w", err)
	}
	return nil
}
