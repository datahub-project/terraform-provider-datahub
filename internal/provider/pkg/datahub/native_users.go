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

// SignUp creates a native login user via the DataHub signUp endpoint.
//
// The signUp endpoint differs significantly between OSS and Cloud:
//
//   - OSS: POST <gms_url>/auth/signUp (Spring MVC on metadata-service).
//     Accepts Bearer token auth (but doesn't enforce it). Uses userUrn from
//     the request body to set the corpUser URN.
//
//   - Cloud: POST <base_url>/signUp (Play Framework on the frontend proxy;
//     /auth/signUp returns 404). MUST NOT send an Authorization header (the
//     Play auth middleware rejects it with 500). The invite token is the sole
//     auth. Ignores userUrn and derives the URN from the email field; on
//     Cloud, non-SSO usernames are always the email address.
//
// The provider tries /auth/signUp first (OSS), falls back to /signUp on 404
// (Cloud). The /signUp path omits the Authorization header.
//
// Returns the HTTP response body (for diagnostics) and an error. The error
// contains "already exists" if the user entity exists and already has
// credentials (Cloud) or if the entity exists at all (OSS).
func (c *Client) SignUp(ctx context.Context, in SignUpInput) (string, error) {
	if c == nil {
		return "", errors.New("client is nil")
	}
	payload := map[string]any{
		"fullName":    in.FullName,
		"email":       in.Email,
		"password":    in.Password,
		"inviteToken": in.InviteToken,
	}
	if in.UserURN != "" {
		payload["userUrn"] = in.UserURN
	}
	if in.Title != "" {
		payload["title"] = in.Title
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshaling signUp payload: %w", err)
	}

	// Try /auth/signUp (OSS GMS path) first. If 404, fall back to /signUp
	// (Cloud frontend path, no auth header).
	signUpPaths := []struct {
		url      string
		sendAuth bool
	}{
		{c.baseURL + "/auth/signUp", true},
		{c.baseURL + "/signUp", false},
	}

	var bodyStr string
	var lastErr error
	for _, p := range signUpPaths {
		authHeader := ""
		if p.sendAuth {
			authHeader = c.authHeader
		}
		bodyStr, lastErr = c.doSignUp(ctx, p.url, payloadBytes, authHeader)
		if lastErr == nil {
			return bodyStr, nil
		}
		if !strings.Contains(lastErr.Error(), "HTTP 404") {
			return bodyStr, lastErr
		}
		tflog.Debug(ctx, "signUp path returned 404, trying next", map[string]any{
			"url": p.url,
		})
	}
	return bodyStr, lastErr
}

func (c *Client) doSignUp(ctx context.Context, signUpURL string, payloadBytes []byte, authHeader string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, signUpURL, strings.NewReader(string(payloadBytes)))
	if err != nil {
		return "", fmt.Errorf("building signUp request: %w", err)
	}
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	req.Header.Set("Content-Type", "application/json")

	tflog.Debug(ctx, "DataHub signUp request", map[string]any{
		"url": signUpURL,
	})

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
		return bodyStr, fmt.Errorf("signUp endpoint at %s returned redirect (HTTP %d) to %s",
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
		return bodyStr, fmt.Errorf("signUp endpoint at %s returned HTML instead of JSON (HTTP %d)",
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
