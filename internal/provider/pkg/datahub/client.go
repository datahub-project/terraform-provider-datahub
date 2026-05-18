// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahub

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// Client is a minimal HTTP client for the Datahub API.
//
// It stores the API host (base URL) and attaches the configured Bearer token
// to every request.
type Client struct {
	baseURL        string
	authHeader     string
	httpClient     *http.Client
	cachedIdentity *MeIdentity
}

// NewClient creates a new Datahub API client.
//
// host must be a valid URL (e.g. https://datahub.example.com).
// token is the GMS token and will be sent as an Authorization Bearer token.
func NewClient(host, token string) (*Client, error) {
	host = strings.TrimSpace(host)
	if host == "" {
		return nil, errors.New("host is required")
	}

	// Accept hosts without an explicit scheme (default to https).
	parsed, err := url.Parse(host)
	if err != nil || parsed.Scheme == "" {
		parsed, err = url.Parse("https://" + strings.TrimPrefix(host, "//"))
		if err != nil {
			return nil, errors.New("invalid host URL")
		}
	}

	if parsed.Host == "" {
		return nil, errors.New("invalid host URL: missing host")
	}

	// Normalize base URL: no trailing slash, and strip any /gms path suffix.
	// Some DataHub CLI configs and older tooling append /gms to the host URL,
	// but all API paths (e.g. /api/graphql, /openapi/v3/...) are rooted at /.
	baseURL := strings.TrimRight(parsed.String(), "/")
	baseURL = strings.TrimSuffix(baseURL, "/gms")

	token = strings.TrimSpace(token)
	if token == "" {
		return nil, errors.New("token is required")
	}

	authHeader := token
	if !strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
		authHeader = "Bearer " + authHeader
	}

	return &Client{
		baseURL:    baseURL,
		authHeader: authHeader,
		httpClient: &http.Client{
			Timeout:   30 * time.Second,
			Transport: &loggingTransport{inner: http.DefaultTransport},
		},
	}, nil
}

// loggingTransport wraps http.DefaultTransport to emit tflog debug entries for
// every outbound request and its response. Wrapping http.DefaultTransport
// (rather than constructing a bare &http.Transport{}) preserves proxy support
// via http.ProxyFromEnvironment.
type loggingTransport struct {
	inner http.RoundTripper
}

func (t *loggingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	fields := map[string]any{
		"method": req.Method,
		"url":    req.URL.String(),
	}
	if proxyURL, err := http.ProxyFromEnvironment(req); err == nil && proxyURL != nil {
		fields["proxy"] = proxyURL.Host
	}
	tflog.Debug(req.Context(), "DataHub API request", fields)
	resp, err := t.inner.RoundTrip(req)
	if err != nil {
		fields["error"] = err.Error()
		tflog.Debug(req.Context(), "DataHub API request failed", fields)
		return nil, err
	}
	fields["status"] = resp.StatusCode
	tflog.Debug(req.Context(), "DataHub API response", fields)
	return resp, nil
}

// BaseURL returns the normalized API base URL.
func (c *Client) BaseURL() string {
	if c == nil {
		return ""
	}
	return c.baseURL
}

// HTTPClient exposes the underlying http.Client (primarily for tests).
func (c *Client) HTTPClient() *http.Client {
	return c.httpClient
}

// MeIdentity contains the identity of the authenticated DataHub user.
type MeIdentity struct {
	Urn         string
	Username    string
	Type        string
	DisplayName string // empty when the user has no display name set
	Email       string // empty when the user has no email set
}

// meGraphQLResponse is the JSON response shape from POST /api/graphql with the me query.
type meGraphQLResponse struct {
	Data struct {
		Me struct {
			CorpUser struct {
				Urn      string `json:"urn"`
				Username string `json:"username"`
				Type     string `json:"type"`
				Info     *struct {
					DisplayName string `json:"displayName"`
					Email       string `json:"email"`
				} `json:"info"`
			} `json:"corpUser"`
		} `json:"me"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// Me calls the DataHub GraphQL API and returns the authenticated user's identity.
// Used both for credential verification in provider Configure and by the datahub_me data source.
// The result is cached on the Client after the first successful call.
func (c *Client) Me(ctx context.Context) (*MeIdentity, error) {
	if c == nil {
		return nil, errors.New("client is nil")
	}
	if c.cachedIdentity != nil {
		return c.cachedIdentity, nil
	}

	const query = `{ me { corpUser { urn username type info { displayName email } } } }`
	req, err := c.NewRequest(ctx, http.MethodPost, "/api/graphql", map[string]string{"query": query})
	if err != nil {
		return nil, fmt.Errorf("building credential-check request: %w", err)
	}

	resp, err := c.Do(req)
	if err != nil {
		return nil, fmt.Errorf("credential-check request to %s failed: %w", c.BaseURL(), err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("DataHub rejected the token (HTTP %d): check DATAHUB_GMS_TOKEN", resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("unexpected HTTP %d from %s/api/graphql", resp.StatusCode, c.BaseURL())
	}

	var gqlResp meGraphQLResponse
	if err := json.NewDecoder(resp.Body).Decode(&gqlResp); err != nil {
		return nil, fmt.Errorf("parsing credential-check response from %s: %w", c.BaseURL(), err)
	}

	if len(gqlResp.Errors) > 0 {
		return nil, fmt.Errorf("DataHub API error: %s", gqlResp.Errors[0].Message)
	}

	cu := gqlResp.Data.Me.CorpUser
	id := &MeIdentity{
		Urn:      cu.Urn,
		Username: cu.Username,
		Type:     cu.Type,
	}
	if cu.Info != nil {
		id.DisplayName = cu.Info.DisplayName
		id.Email = cu.Info.Email
	}

	if id.Urn == "" {
		return nil, fmt.Errorf("credential check succeeded but returned no user URN from %s", c.BaseURL())
	}

	c.cachedIdentity = id
	return id, nil
}

// NewRequest builds an HTTP request against the API base URL.
//
// path is appended to the base URL (e.g. "/api/v2/..."), and the Bearer token
// is attached automatically.
func (c *Client) NewRequest(ctx context.Context, method, path string, body any) (*http.Request, error) {
	if c == nil {
		return nil, errors.New("client is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if method == "" {
		return nil, errors.New("method is required")
	}

	fullURL := c.baseURL + "/" + strings.TrimLeft(path, "/")

	var reader io.Reader
	var contentType string
	if body != nil {
		switch v := body.(type) {
		case io.Reader:
			reader = v
		case []byte:
			reader = bytes.NewReader(v)
			contentType = "application/json"
		case string:
			reader = strings.NewReader(v)
			contentType = "application/json"
		default:
			b, err := json.Marshal(v)
			if err != nil {
				return nil, fmt.Errorf("marshaling request body: %w", err)
			}
			reader = bytes.NewReader(b)
			contentType = "application/json"
		}
	}

	req, err := http.NewRequestWithContext(ctx, method, fullURL, reader)
	if err != nil {
		return nil, fmt.Errorf("building HTTP request: %w", err)
	}

	req.Header.Set("Authorization", c.authHeader)
	req.Header.Set("Accept", "application/json")
	if contentType != "" && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", contentType)
	}
	return req, nil
}

// Do executes the request using the underlying http.Client.
func (c *Client) Do(req *http.Request) (*http.Response, error) {
	if c == nil {
		return nil, errors.New("client is nil")
	}
	if req == nil {
		return nil, errors.New("request is nil")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing HTTP request: %w", err)
	}
	return resp, nil
}
