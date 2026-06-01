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

	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// SignUpInput carries the fields for the POST /signUp endpoint.
type SignUpInput struct {
	UserURN     string
	FullName    string
	Email       string
	Password    string
	Title       string
	InviteToken string
}

// GetInviteToken retrieves the current org-wide invite token via the
// getInviteToken GraphQL query. This does NOT create a new token; use
// CreateInviteToken to regenerate.
func (c *Client) GetInviteToken(ctx context.Context) (string, error) {
	if c == nil {
		return "", errors.New("client is nil")
	}

	const q = `query getInviteToken($input: GetInviteTokenInput!) {
  getInviteToken(input: $input) { inviteToken }
}`
	body := map[string]any{
		"query": q,
		"variables": map[string]any{
			"input": map[string]any{},
		},
	}

	var gqlResp struct {
		Data struct {
			GetInviteToken struct {
				InviteToken string `json:"inviteToken"`
			} `json:"getInviteToken"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := c.doGraphQL(ctx, body, &gqlResp); err != nil {
		return "", err
	}
	if len(gqlResp.Errors) > 0 {
		return "", fmt.Errorf("DataHub API error: %s", gqlResp.Errors[0].Message)
	}
	token := gqlResp.Data.GetInviteToken.InviteToken
	if token == "" {
		return "", errors.New("getInviteToken returned an empty token")
	}
	return token, nil
}

// CreateInviteToken regenerates the org-wide invite token via the
// createInviteToken GraphQL mutation. This invalidates the previous token.
func (c *Client) CreateInviteToken(ctx context.Context) (string, error) {
	if c == nil {
		return "", errors.New("client is nil")
	}

	const q = `mutation createInviteToken($input: CreateInviteTokenInput!) {
  createInviteToken(input: $input) { inviteToken }
}`
	body := map[string]any{
		"query": q,
		"variables": map[string]any{
			"input": map[string]any{},
		},
	}

	var gqlResp struct {
		Data struct {
			CreateInviteToken struct {
				InviteToken string `json:"inviteToken"`
			} `json:"createInviteToken"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := c.doGraphQL(ctx, body, &gqlResp); err != nil {
		return "", err
	}
	if len(gqlResp.Errors) > 0 {
		return "", fmt.Errorf("DataHub API error: %s", gqlResp.Errors[0].Message)
	}
	return gqlResp.Data.CreateInviteToken.InviteToken, nil
}

// SignUp calls the DataHub frontend /signUp endpoint to create a native login
// user. The endpoint is on the frontend URL, not the GMS URL.
//
// Returns the HTTP response body (for diagnostics) and an error. The error
// contains "already exists" if the user entity exists and already has
// credentials (Cloud) or if the entity exists at all (OSS).
func (c *Client) SignUp(ctx context.Context, in SignUpInput) (string, error) {
	if c == nil {
		return "", errors.New("client is nil")
	}
	signUpURL := c.baseURL + "/auth/signUp"
	payload := map[string]string{
		"userUrn":     in.UserURN,
		"fullName":    in.FullName,
		"email":       in.Email,
		"password":    in.Password,
		"title":       in.Title,
		"inviteToken": in.InviteToken,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshaling signUp payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, signUpURL, strings.NewReader(string(payloadBytes)))
	if err != nil {
		return "", fmt.Errorf("building signUp request: %w", err)
	}
	req.Header.Set("Authorization", c.authHeader)
	req.Header.Set("Content-Type", "application/json")

	tflog.Debug(ctx, "DataHub signUp request", map[string]any{
		"url":     signUpURL,
		"userUrn": in.UserURN,
	})

	// Use a client that does not follow redirects. The frontend may redirect
	// unauthenticated/misconfigured requests to the login page, and a
	// followed redirect returning 200 HTML would silently mask a failure.
	noRedirectClient := &http.Client{
		Timeout:   c.httpClient.Timeout,
		Transport: c.httpClient.Transport,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	res, err := noRedirectClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("signUp request to %s failed: %w", signUpURL, err)
	}
	defer res.Body.Close()

	respBody, _ := io.ReadAll(res.Body)
	bodyStr := strings.TrimSpace(string(respBody))

	tflog.Debug(ctx, "DataHub signUp response", map[string]any{
		"status":       res.StatusCode,
		"content_type": res.Header.Get("Content-Type"),
		"body_len":     len(bodyStr),
	})

	if res.StatusCode >= 300 && res.StatusCode < 400 {
		return bodyStr, fmt.Errorf("signUp endpoint at %s returned redirect (HTTP %d) to %s; "+
			"this usually means the frontend_url is wrong or the frontend's auth middleware "+
			"is rejecting the request. Set the frontend_url provider attribute or "+
			"DATAHUB_FRONTEND_URL environment variable explicitly",
			signUpURL, res.StatusCode, res.Header.Get("Location"))
	}

	if res.StatusCode >= http.StatusBadRequest {
		if strings.Contains(bodyStr, "already exists") {
			return bodyStr, fmt.Errorf("signUp rejected: this user entity already exists. " +
				"On OSS DataHub, native credentials can only be added to a brand-new user. " +
				"On DataHub Cloud, this operation succeeds for users without existing credentials. " +
				"Workaround: create the datahub_local_user_login resource first, then add " +
				"datahub_corp_user referencing it via the username attribute")
		}
		return bodyStr, fmt.Errorf("unexpected HTTP %d from signUp endpoint at %s: %s", res.StatusCode, signUpURL, bodyStr)
	}

	ct := res.Header.Get("Content-Type")
	if strings.Contains(ct, "text/html") {
		return bodyStr, fmt.Errorf("signUp endpoint at %s returned HTML instead of JSON (HTTP %d); "+
			"this usually means the frontend_url is wrong (pointing at a static page rather "+
			"than the DataHub frontend API). Set the frontend_url provider attribute or "+
			"DATAHUB_FRONTEND_URL environment variable explicitly",
			signUpURL, res.StatusCode)
	}

	return bodyStr, nil
}

// CreateNativeUserResetToken generates a per-user password reset token via
// the createNativeUserResetToken GraphQL mutation. The token has a 24h TTL
// (default, configurable server-side) and is single-use.
func (c *Client) CreateNativeUserResetToken(ctx context.Context, userURN string) (string, error) {
	if c == nil {
		return "", errors.New("client is nil")
	}

	const q = `
mutation createNativeUserResetToken($input: CreateNativeUserResetTokenInput!) {
  createNativeUserResetToken(input: $input) {
    resetToken
  }
}`
	body := map[string]any{
		"query": q,
		"variables": map[string]any{
			"input": map[string]any{
				"userUrn": userURN,
			},
		},
	}

	var gqlResp struct {
		Data struct {
			CreateNativeUserResetToken struct {
				ResetToken string `json:"resetToken"`
			} `json:"createNativeUserResetToken"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := c.doGraphQL(ctx, body, &gqlResp); err != nil {
		return "", err
	}
	if len(gqlResp.Errors) > 0 {
		return "", fmt.Errorf("DataHub API error: %s", gqlResp.Errors[0].Message)
	}
	token := gqlResp.Data.CreateNativeUserResetToken.ResetToken
	if token == "" {
		return "", errors.New("createNativeUserResetToken returned an empty token")
	}
	return token, nil
}
