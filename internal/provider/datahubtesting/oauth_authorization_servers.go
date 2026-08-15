// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahubtesting

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
)

// mockOAuthServer stores the oauthAuthorizationServerProperties aspect for one
// server. Pointer fields distinguish "absent from the aspect" from an
// explicitly stored empty string, mirroring the PDL's optional fields.
type mockOAuthServer struct {
	URN                   string
	ID                    string
	DisplayName           string
	Description           *string
	ClientID              *string
	ClientSecretURN       *string
	AuthorizationURL      *string
	TokenURL              *string
	Scopes                []string
	TokenAuthMethod       string
	AdditionalTokenParams map[string]string
	AdditionalAuthParams  map[string]string
	AuthLocation          string
	AuthHeaderName        string
	AuthScheme            *string
	AuthQueryParam        *string
}

// handleUpsertOAuthAuthorizationServer models the Cloud resolver's per-field
// semantics faithfully - written from the resolver source, not from what the
// provider client happens to send (a mock written from client behaviour agrees
// with any client bug):
//
//   - clientSecret: null preserves the stored secret URN, "" clears it, and a
//     non-empty value mints a NEW dataHubSecret URN every time (the rotation
//     orphan trail).
//   - clientId / authorizationUrl / tokenUrl / scopes: null preserves, a
//     present value (including "" / []) overwrites.
//   - description / additionalTokenParams / additionalAuthParams /
//     authQueryParam: set only when non-null, i.e. null clears.
//   - tokenAuthMethod / authLocation / authHeaderName: fall back to the server
//     defaults (POST_BODY, HEADER, "Authorization") when null.
//   - authScheme: set only when non-null - AND, per the SDL, the input field
//     carries a literal default of "Bearer" that graphql-java injects when the
//     field is OMITTED from the variables (an explicit null suppresses it).
//     The same applies to the three defaulted fields above. The mock
//     distinguishes omitted from explicit-null for exactly that reason.
//   - id: omitted means the server mints a UUID; the mock mints a
//     deterministic stand-in so a client that forgets the id fails loudly in
//     URN assertions.
func (s *mockServer) handleUpsertOAuthAuthorizationServer(w http.ResponseWriter, variables map[string]any) {
	input, _ := variables["input"].(map[string]any)

	getStr := func(key string) (val string, present bool) {
		raw, ok := input[key]
		if !ok || raw == nil {
			return "", ok && raw != nil
		}
		v, _ := raw.(string)
		return v, true
	}

	id, idPresent := getStr("id")
	if !idPresent || id == "" {
		id = fmt.Sprintf("mock-uuid-%d", len(s.oauthServers)+1)
	}
	urn := "urn:li:oauthAuthorizationServer:" + id

	s.mu.Lock()
	defer s.mu.Unlock()

	existing, hadExisting := s.oauthServers[id]

	srv := mockOAuthServer{URN: urn, ID: id}

	displayName, _ := getStr("displayName")
	srv.DisplayName = displayName

	if v, present := getStr("description"); present {
		srv.Description = &v
	}

	// clientSecret: null preserves, "" clears, non-empty mints a new secret.
	if raw, ok := input["clientSecret"]; !ok || raw == nil {
		if hadExisting {
			srv.ClientSecretURN = existing.ClientSecretURN
		}
	} else if v, _ := raw.(string); v != "" {
		s.oauthSecretCounter++
		secretURN := fmt.Sprintf("urn:li:dataHubSecret:%s_clientSecret_%d", id, s.oauthSecretCounter)
		srv.ClientSecretURN = &secretURN
	}
	// "" falls through with ClientSecretURN nil (cleared).

	// Preserve-on-null fields.
	preserveString := func(key string, existingVal *string) *string {
		if v, present := getStr(key); present {
			return &v
		}
		if hadExisting {
			return existingVal
		}
		return nil
	}
	var existingClientID, existingAuthURL, existingTokenURL *string
	var existingScopes []string
	if hadExisting {
		existingClientID = existing.ClientID
		existingAuthURL = existing.AuthorizationURL
		existingTokenURL = existing.TokenURL
		existingScopes = existing.Scopes
	}
	srv.ClientID = preserveString("clientId", existingClientID)
	srv.AuthorizationURL = preserveString("authorizationUrl", existingAuthURL)
	srv.TokenURL = preserveString("tokenUrl", existingTokenURL)

	if raw, ok := input["scopes"]; ok && raw != nil {
		list, _ := raw.([]any)
		scopes := make([]string, 0, len(list))
		for _, e := range list {
			if sv, ok := e.(string); ok {
				scopes = append(scopes, sv)
			}
		}
		srv.Scopes = scopes
	} else {
		srv.Scopes = existingScopes
	}

	// Defaulted fields. The SDL gives these input-literal defaults, injected
	// when the key is ABSENT; an explicit null reaches the resolver, whose own
	// fallback then applies (same value for these three).
	applyDefault := func(key, def string) string {
		if raw, ok := input[key]; ok && raw != nil {
			v, _ := raw.(string)
			return v
		}
		return def
	}
	srv.TokenAuthMethod = applyDefault("tokenAuthMethod", "POST_BODY")
	srv.AuthLocation = applyDefault("authLocation", "HEADER")
	srv.AuthHeaderName = applyDefault("authHeaderName", "Authorization")

	// authScheme: input-literal default "Bearer" when the key is OMITTED; an
	// explicit null clears (the resolver has no fallback branch).
	if raw, ok := input["authScheme"]; !ok {
		def := "Bearer"
		srv.AuthScheme = &def
	} else if raw != nil {
		v, _ := raw.(string)
		srv.AuthScheme = &v
	}

	if v, present := getStr("authQueryParam"); present {
		srv.AuthQueryParam = &v
	}

	entriesToMap := func(key string) map[string]string {
		raw, ok := input[key]
		if !ok || raw == nil {
			return nil
		}
		list, _ := raw.([]any)
		m := make(map[string]string, len(list))
		for _, e := range list {
			entry, _ := e.(map[string]any)
			k, _ := entry["key"].(string)
			v, _ := entry["value"].(string)
			m[k] = v
		}
		return m
	}
	srv.AdditionalTokenParams = entriesToMap("additionalTokenParams")
	srv.AdditionalAuthParams = entriesToMap("additionalAuthParams")

	s.oauthServers[id] = srv

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{
			"upsertOAuthAuthorizationServer": map[string]any{
				"urn": urn,
			},
		},
	})
}

// handleDeleteOAuthAuthorizationServer deletes the server. The real resolver's
// cascade (rewriting referencing AI plugins, deleting the current secret) has
// no observable surface in the mock yet; deleting a nonexistent URN succeeds,
// matching the server's best-effort behaviour.
func (s *mockServer) handleDeleteOAuthAuthorizationServer(w http.ResponseWriter, variables map[string]any) {
	urn, _ := variables["urn"].(string)
	id := strings.TrimPrefix(urn, "urn:li:oauthAuthorizationServer:")

	s.mu.Lock()
	delete(s.oauthServers, id)
	s.mu.Unlock()

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{
			"deleteOAuthAuthorizationServer": true,
		},
	})
}

// handleListOAuthAuthorizationServers serves the paginated list query.
func (s *mockServer) handleListOAuthAuthorizationServers(w http.ResponseWriter, variables map[string]any) {
	input, _ := variables["input"].(map[string]any)
	start := 0
	count := 20
	if v, ok := input["start"].(float64); ok {
		start = int(v)
	}
	if v, ok := input["count"].(float64); ok {
		count = int(v)
	}

	s.mu.Lock()
	ids := make([]string, 0, len(s.oauthServers))
	for id := range s.oauthServers {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	total := len(ids)
	var page []map[string]any
	for i := start; i < total && i < start+count; i++ {
		page = append(page, map[string]any{"urn": s.oauthServers[ids[i]].URN})
	}
	s.mu.Unlock()

	if page == nil {
		page = []map[string]any{}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{
			"listOAuthAuthorizationServers": map[string]any{
				"start":                start,
				"count":                len(page),
				"total":                total,
				"authorizationServers": page,
			},
		},
	})
}

// handleOAuthAuthorizationServerItem serves GET on
// /openapi/v3/entity/oauthauthorizationserver/{urn}, rendering the stored
// aspect the way the v3 endpoint does: optional fields are omitted when unset,
// not serialised as null.
func (s *mockServer) handleOAuthAuthorizationServerItem(w http.ResponseWriter, r *http.Request) {
	urn := strings.TrimPrefix(r.URL.Path, "/openapi/v3/entity/oauthauthorizationserver/")
	id := strings.TrimPrefix(urn, "urn:li:oauthAuthorizationServer:")

	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.mu.Lock()
	srv, ok := s.oauthServers[id]
	s.mu.Unlock()

	if !ok {
		http.NotFound(w, r)
		return
	}

	props := map[string]any{
		"displayName":     srv.DisplayName,
		"tokenAuthMethod": srv.TokenAuthMethod,
		"authLocation":    srv.AuthLocation,
		"authHeaderName":  srv.AuthHeaderName,
	}
	if srv.Description != nil {
		props["description"] = *srv.Description
	}
	if srv.ClientID != nil {
		props["clientId"] = *srv.ClientID
	}
	if srv.ClientSecretURN != nil {
		props["clientSecretUrn"] = *srv.ClientSecretURN
	}
	if srv.AuthorizationURL != nil {
		props["authorizationUrl"] = *srv.AuthorizationURL
	}
	if srv.TokenURL != nil {
		props["tokenUrl"] = *srv.TokenURL
	}
	if srv.Scopes != nil {
		props["scopes"] = srv.Scopes
	}
	if srv.AdditionalTokenParams != nil {
		props["additionalTokenParams"] = srv.AdditionalTokenParams
	}
	if srv.AdditionalAuthParams != nil {
		props["additionalAuthParams"] = srv.AdditionalAuthParams
	}
	if srv.AuthScheme != nil {
		props["authScheme"] = *srv.AuthScheme
	}
	if srv.AuthQueryParam != nil {
		props["authQueryParam"] = *srv.AuthQueryParam
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"urn": srv.URN,
		"oauthAuthorizationServerKey": map[string]any{
			"value": map[string]any{"id": srv.ID},
		},
		"oauthAuthorizationServerProperties": map[string]any{
			"value": props,
		},
	})
}
