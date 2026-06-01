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
// Returns an error containing "already exists" if the user entity exists and
// already has credentials (Cloud) or if the entity exists at all (OSS).
func (c *Client) SignUp(ctx context.Context, in SignUpInput) error {
	if c == nil {
		return errors.New("client is nil")
	}
	frontendURL := c.FrontendURL()
	if frontendURL == "" {
		return errors.New("frontend URL is not configured and could not be derived from the GMS URL")
	}

	signUpURL := frontendURL + "/signUp"
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
		return fmt.Errorf("marshaling signUp payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, signUpURL, strings.NewReader(string(payloadBytes)))
	if err != nil {
		return fmt.Errorf("building signUp request: %w", err)
	}
	req.Header.Set("Authorization", c.authHeader)
	req.Header.Set("Content-Type", "application/json")

	res, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("signUp request failed: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode >= http.StatusBadRequest {
		respBody, _ := io.ReadAll(res.Body)
		bodyStr := strings.TrimSpace(string(respBody))
		if strings.Contains(bodyStr, "already exists") {
			return fmt.Errorf("signUp rejected: this user entity already exists. " +
				"On OSS DataHub, native credentials can only be added to a brand-new user. " +
				"On DataHub Cloud, this operation succeeds for users without existing credentials. " +
				"Workaround: create the datahub_local_user_login resource first, then add " +
				"datahub_corp_user referencing it via the username attribute")
		}
		return fmt.Errorf("unexpected HTTP %d from signUp endpoint: %s", res.StatusCode, bodyStr)
	}

	return nil
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
