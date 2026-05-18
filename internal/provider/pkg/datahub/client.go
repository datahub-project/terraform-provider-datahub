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
	baseURL    string
	authHeader string
	httpClient *http.Client
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

	// Normalize base URL (no trailing slash).
	baseURL := strings.TrimRight(parsed.String(), "/")

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
