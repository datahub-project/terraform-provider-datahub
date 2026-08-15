// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahub

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestUpsertOAuthAuthorizationServerVariables_EveryFieldAlwaysPresent asserts
// the load-bearing property of the upsert input: every field of the mutation
// input appears in the variables on every call. The resolver's per-field null
// semantics are non-uniform (preserve / clear / server-default), and several
// input fields carry SDL literal defaults injected on omission, so an omitted
// key silently means something the caller did not choose.
func TestUpsertOAuthAuthorizationServerVariables_EveryFieldAlwaysPresent(t *testing.T) {
	t.Parallel()

	vars := UpsertOAuthAuthorizationServerInput{
		ID:          "svr",
		DisplayName: "Server",
	}.upsertVariables()

	expected := []string{
		"id", "displayName", "description", "clientId", "clientSecret",
		"authorizationUrl", "tokenUrl", "scopes", "tokenAuthMethod",
		"authLocation", "authHeaderName", "authScheme", "authQueryParam",
		"additionalTokenParams", "additionalAuthParams",
	}
	for _, key := range expected {
		if _, ok := vars[key]; !ok {
			t.Errorf("upsert variables missing key %q; an omitted key falls to a preserve branch or an SDL literal default", key)
		}
	}
	if len(vars) != len(expected) {
		t.Errorf("upsert variables carry %d keys, expected %d; an extra key is a field the schema may reject", len(vars), len(expected))
	}
}

// TestUpsertOAuthAuthorizationServerVariables_NullSemantics pins the values a
// minimal input marshals: explicit nulls for the clear-on-null fields, "" for
// the preserve-on-null string fields (where "" is the only way to clear), and
// [] for scopes (null would preserve).
func TestUpsertOAuthAuthorizationServerVariables_NullSemantics(t *testing.T) {
	t.Parallel()

	vars := UpsertOAuthAuthorizationServerInput{
		ID:              "svr",
		DisplayName:     "Server",
		TokenAuthMethod: "POST_BODY",
		AuthLocation:    "HEADER",
		AuthHeaderName:  "Authorization",
	}.upsertVariables()

	for _, key := range []string{"description", "clientSecret", "authScheme", "authQueryParam", "additionalTokenParams", "additionalAuthParams"} {
		if vars[key] != nil {
			t.Errorf("%s should marshal as explicit null when unset, got %#v", key, vars[key])
		}
	}
	for _, key := range []string{"clientId", "authorizationUrl", "tokenUrl"} {
		if got, ok := vars[key].(string); !ok || got != "" {
			t.Errorf("%s should marshal as \"\" when unset (null would PRESERVE the stored value), got %#v", key, vars[key])
		}
	}
	scopes, ok := vars["scopes"].([]string)
	if !ok || scopes == nil || len(scopes) != 0 {
		t.Errorf("scopes should marshal as an empty array when unset (null would PRESERVE), got %#v", vars["scopes"])
	}
}

// TestUpsertOAuthAuthorizationServerVariables_SecretActions pins the three-way
// clientSecret contract: null preserves, "" clears, non-empty sets.
func TestUpsertOAuthAuthorizationServerVariables_SecretActions(t *testing.T) {
	t.Parallel()

	base := UpsertOAuthAuthorizationServerInput{ID: "svr", DisplayName: "Server"}

	preserve := base
	preserve.ClientSecretAction = ClientSecretPreserve
	if got := preserve.upsertVariables()["clientSecret"]; got != nil {
		t.Errorf("preserve should send null, got %#v", got)
	}

	clr := base
	clr.ClientSecretAction = ClientSecretClear
	if got := clr.upsertVariables()["clientSecret"]; got != "" {
		t.Errorf("clear should send the empty string, got %#v", got)
	}

	set := base
	set.ClientSecretAction = ClientSecretSet
	set.ClientSecretValue = "s3cret"
	if got := set.upsertVariables()["clientSecret"]; got != "s3cret" {
		t.Errorf("set should send the value, got %#v", got)
	}
}

// TestUpsertOAuthAuthorizationServerVariables_MapsAreSortedEntries asserts map
// attributes marshal to the GraphQL [StringMapEntryInput!] shape with
// deterministic key order.
func TestUpsertOAuthAuthorizationServerVariables_MapsAreSortedEntries(t *testing.T) {
	t.Parallel()

	in := UpsertOAuthAuthorizationServerInput{
		ID:          "svr",
		DisplayName: "Server",
		AdditionalTokenParams: map[string]string{
			"resource": "https://example.com",
			"audience": "api",
		},
	}
	entries, ok := in.upsertVariables()["additionalTokenParams"].([]map[string]string)
	if !ok {
		t.Fatalf("additionalTokenParams should marshal to entry maps, got %#v", in.upsertVariables()["additionalTokenParams"])
	}
	if len(entries) != 2 || entries[0]["key"] != "audience" || entries[1]["key"] != "resource" {
		t.Errorf("entries should be sorted by key, got %#v", entries)
	}
}

func TestIsOAuthAuthorizationServerCloudOnlyError(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		msg  string
		want bool
	}{
		{
			name: "mutation absent",
			msg:  "Validation error (FieldUndefined@[upsertOAuthAuthorizationServer]) : Field 'upsertOAuthAuthorizationServer' in type 'Mutation' is undefined",
			want: true,
		},
		{
			name: "query absent",
			msg:  "Validation error (FieldUndefined@[listOAuthAuthorizationServers]) : Field 'listOAuthAuthorizationServers' in type 'Query' is undefined",
			want: true,
		},
		{
			name: "input type unknown (what OSS actually returns)",
			msg:  "Validation error (UnknownType) : Unknown type 'UpsertOAuthAuthorizationServerInput'",
			want: true,
		},
		{
			name: "subfield undefined on a Cloud build (schema version skew, NOT cloud-only)",
			msg:  "Validation error (FieldUndefined@[upsertOAuthAuthorizationServer/channel]) : Field 'channel' in type 'OAuthAuthorizationServer' is undefined",
			want: false,
		},
		{
			name: "unrelated error",
			msg:  "Unauthorized to create/update OAuth authorization servers.",
			want: false,
		},
	}
	for _, tc := range cases {
		if got := isOAuthAuthorizationServerCloudOnlyError(tc.msg); got != tc.want {
			t.Errorf("%s: isOAuthAuthorizationServerCloudOnlyError(%q) = %v, want %v", tc.name, tc.msg, got, tc.want)
		}
	}
}

func TestUpsertOAuthAuthorizationServer_CloudOnlySentinel(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(ossGraphQLHandler("upsertOAuthAuthorizationServer"))
	defer srv.Close()

	c, err := NewClient(srv.URL, "token")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	_, err = c.UpsertOAuthAuthorizationServer(context.Background(), UpsertOAuthAuthorizationServerInput{
		ID:          "svr",
		DisplayName: "Server",
	})
	if !errors.Is(err, ErrOAuthAuthorizationServerCloudOnly) {
		t.Errorf("expected ErrOAuthAuthorizationServerCloudOnly, got %v", err)
	}
}

func TestGetOAuthAuthorizationServerByURN(t *testing.T) {
	t.Parallel()

	const urn = "urn:li:oauthAuthorizationServer:svr"
	authScheme := ""

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/openapi/v3/entity/oauthauthorizationserver/" + urn:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"urn": urn,
				"oauthAuthorizationServerKey": map[string]any{
					"value": map[string]any{"id": "svr"},
				},
				"oauthAuthorizationServerProperties": map[string]any{
					"value": map[string]any{
						"displayName":     "Server",
						"description":     "desc",
						"clientId":        "client-1",
						"clientSecretUrn": "urn:li:dataHubSecret:svr_clientSecret_abc",
						"tokenUrl":        "https://idp.example.com/token",
						"scopes":          []string{"read", "write"},
						"tokenAuthMethod": "BASIC",
						"authLocation":    "HEADER",
						"authHeaderName":  "X-API-Key",
						"authScheme":      authScheme,
						"additionalTokenParams": map[string]string{
							"audience": "api",
						},
					},
				},
			})
		case "/openapi/v3/entity/oauthauthorizationserver/urn:li:oauthAuthorizationServer:husk":
			// Entity present without its properties aspect: unmanageable.
			_ = json.NewEncoder(w).Encode(map[string]any{
				"urn": "urn:li:oauthAuthorizationServer:husk",
				"oauthAuthorizationServerKey": map[string]any{
					"value": map[string]any{"id": "husk"},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c, err := NewClient(srv.URL, "token")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	got, err := c.GetOAuthAuthorizationServerByURN(context.Background(), urn)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got == nil {
		t.Fatal("Get returned nil for an existing server")
	}
	if got.ID != "svr" || got.DisplayName != "Server" || got.ClientID != "client-1" {
		t.Errorf("unexpected decode: %+v", got)
	}
	if !got.HasClientSecret() {
		t.Error("HasClientSecret should be true when clientSecretUrn is set")
	}
	if got.AuthScheme == nil || *got.AuthScheme != "" {
		t.Errorf("authScheme \"\" must survive as an explicit empty string (raw-token injection), got %v", got.AuthScheme)
	}
	if len(got.Scopes) != 2 || got.AdditionalTokenParams["audience"] != "api" {
		t.Errorf("scopes/params decode wrong: %+v", got)
	}

	missing, err := c.GetOAuthAuthorizationServerByURN(context.Background(), "urn:li:oauthAuthorizationServer:nope")
	if err != nil || missing != nil {
		t.Errorf("404 should be (nil, nil), got (%v, %v)", missing, err)
	}

	husk, err := c.GetOAuthAuthorizationServerByURN(context.Background(), "urn:li:oauthAuthorizationServer:husk")
	if err != nil || husk != nil {
		t.Errorf("an entity without its properties aspect should read as gone, got (%v, %v)", husk, err)
	}
}

// TestDeleteOAuthAuthorizationServer_RealErrorIsNotSwallowed is the strict
// counterpart of the not-found tolerance below: a delete that fails for any
// other reason (here a privilege denial) MUST surface an error. If the
// tolerance ever broadened to swallow it, terraform destroy would report
// success and remove the resource from state while the entity survives
// server-side - a silently orphaned OAuth server nobody manages anymore.
func TestDeleteOAuthAuthorizationServer_RealErrorIsNotSwallowed(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"errors":[{"message":"Unauthorized to delete OAuth authorization servers."}]}`))
	}))
	defer srv.Close()

	c, err := NewClient(srv.URL, "token")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	err = c.DeleteOAuthAuthorizationServer(context.Background(), "urn:li:oauthAuthorizationServer:svr")
	if err == nil {
		t.Fatal("a non-not-found delete error was swallowed; destroy would orphan the entity while claiming success")
	}
	if !strings.Contains(err.Error(), "Unauthorized") {
		t.Errorf("delete error should carry the server message, got %v", err)
	}
}

// TestGetOAuthAuthorizationServerByURN_ForbiddenIsErrorNotAbsent pins that an
// HTTP 401/403 on the read path is an error, never (nil, nil). The nil return
// means "gone" to the resource's Read, which responds with RemoveResource - so
// misclassifying a privilege lapse as absence would silently drop the resource
// from state and make the next apply recreate it.
func TestGetOAuthAuthorizationServerByURN_ForbiddenIsErrorNotAbsent(t *testing.T) {
	t.Parallel()

	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "denied", status)
		}))

		c, err := NewClient(srv.URL, "token")
		if err != nil {
			srv.Close()
			t.Fatalf("NewClient: %v", err)
		}
		got, err := c.GetOAuthAuthorizationServerByURN(context.Background(), "urn:li:oauthAuthorizationServer:svr")
		srv.Close()
		if err == nil {
			t.Fatalf("HTTP %d must be an error, not absence (got=%v); Read would RemoveResource on a privilege lapse", status, got)
		}
		if got != nil {
			t.Errorf("HTTP %d returned a non-nil server: %+v", status, got)
		}
		if !strings.Contains(err.Error(), "MANAGE_CONNECTIONS") {
			t.Errorf("HTTP %d error should name the missing privilege, got %v", status, err)
		}
	}
}

// TestGetOAuthAuthorizationServerByURN_ServerErrorCarriesBody pins that a
// non-auth HTTP failure surfaces the response body. Cloud deployments sit
// behind gateways whose 5xx pages are the only clue to what broke; dropping
// the body (or feeding it to the JSON decoder) turns an outage into an
// undiagnosable "parsing response" error.
func TestGetOAuthAuthorizationServerByURN_ServerErrorCarriesBody(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "upstream connect error", http.StatusBadGateway)
	}))
	defer srv.Close()

	c, err := NewClient(srv.URL, "token")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	_, err = c.GetOAuthAuthorizationServerByURN(context.Background(), "urn:li:oauthAuthorizationServer:svr")
	if err == nil {
		t.Fatal("HTTP 502 must be an error")
	}
	if !strings.Contains(err.Error(), "502") || !strings.Contains(err.Error(), "upstream connect error") {
		t.Errorf("error should carry the HTTP status and body, got %v", err)
	}
}

// TestUpsertOAuthAuthorizationServer_HTTPErrors pins the GraphQL transport's
// HTTP error mapping: 401/403 name the MANAGE_CONNECTIONS privilege (the first
// thing a user with an under-privileged token hits), and other >=400 responses
// carry the body rather than being fed to the JSON decoder.
func TestUpsertOAuthAuthorizationServer_HTTPErrors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"forbidden names the privilege", http.StatusForbidden, "denied", "MANAGE_CONNECTIONS"},
		{"gateway error carries the body", http.StatusServiceUnavailable, "upstream unavailable", "upstream unavailable"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, tc.body, tc.status)
			}))
			defer srv.Close()

			c, err := NewClient(srv.URL, "token")
			if err != nil {
				t.Fatalf("NewClient: %v", err)
			}
			_, err = c.UpsertOAuthAuthorizationServer(context.Background(), UpsertOAuthAuthorizationServerInput{
				ID:          "svr",
				DisplayName: "Server",
			})
			if err == nil {
				t.Fatalf("HTTP %d must be an error", tc.status)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("HTTP %d error should contain %q, got %v", tc.status, tc.want, err)
			}
		})
	}
}

func TestDeleteOAuthAuthorizationServer_NotFoundIsSuccess(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"errors":[{"message":"Failed to delete: entity urn:li:oauthAuthorizationServer:svr not found"}]}`))
	}))
	defer srv.Close()

	c, err := NewClient(srv.URL, "token")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	// Delete must tolerate not-found: deleteAiPlugin's cascade may have
	// removed the server before Terraform's destroy reaches it.
	if err := c.DeleteOAuthAuthorizationServer(context.Background(), "urn:li:oauthAuthorizationServer:svr"); err != nil {
		t.Errorf("not-found delete should succeed, got %v", err)
	}
}

func TestListOAuthAuthorizationServerURNs_Paginates(t *testing.T) {
	t.Parallel()

	page := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var servers []map[string]string
		if page == 0 {
			for i := 0; i < 100; i++ {
				servers = append(servers, map[string]string{"urn": "urn:li:oauthAuthorizationServer:a"})
			}
		} else {
			servers = append(servers, map[string]string{"urn": "urn:li:oauthAuthorizationServer:last"})
		}
		start := page * 100
		page++
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"listOAuthAuthorizationServers": map[string]any{
					"start":                start,
					"count":                len(servers),
					"total":                101,
					"authorizationServers": servers,
				},
			},
		})
	}))
	defer srv.Close()

	c, err := NewClient(srv.URL, "token")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	urns, err := c.ListOAuthAuthorizationServerURNs(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(urns) != 101 {
		t.Errorf("expected 101 URNs across two pages, got %d", len(urns))
	}
	if urns[100] != "urn:li:oauthAuthorizationServer:last" {
		t.Errorf("second page missing: last URN = %s", urns[100])
	}
}
