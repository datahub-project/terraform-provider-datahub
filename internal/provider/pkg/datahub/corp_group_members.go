// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahub

import (
	"context"
	"errors"
	"fmt"
)

// AddGroupMember adds a user to a native group via the addGroupMembers mutation.
// Membership is stored as the nativeGroupMembership aspect on the user.
func (c *Client) AddGroupMember(ctx context.Context, groupURN, userURN string) error {
	if c == nil {
		return errors.New("client is nil")
	}
	if groupURN == "" || userURN == "" {
		return errors.New("groupURN and userURN are required")
	}

	const q = `
mutation addGroupMembers($input: AddGroupMembersInput!) {
  addGroupMembers(input: $input)
}`
	body := map[string]any{
		"query": q,
		"variables": map[string]any{
			"input": map[string]any{
				"groupUrn": groupURN,
				"userUrns": []string{userURN},
			},
		},
	}
	var gqlResp genericGraphQLErrors
	if err := c.doGraphQL(ctx, body, &gqlResp); err != nil {
		return err
	}
	if len(gqlResp.Errors) > 0 {
		return fmt.Errorf("DataHub API error: %s", gqlResp.Errors[0].Message)
	}
	return nil
}

// RemoveGroupMember removes a user from a group via the removeGroupMembers
// mutation. Returns nil if the membership is already gone (idempotent).
func (c *Client) RemoveGroupMember(ctx context.Context, groupURN, userURN string) error {
	if c == nil {
		return errors.New("client is nil")
	}
	if groupURN == "" || userURN == "" {
		return errors.New("groupURN and userURN are required")
	}

	const q = `
mutation removeGroupMembers($input: RemoveGroupMembersInput!) {
  removeGroupMembers(input: $input)
}`
	body := map[string]any{
		"query": q,
		"variables": map[string]any{
			"input": map[string]any{
				"groupUrn": groupURN,
				"userUrns": []string{userURN},
			},
		},
	}
	var gqlResp genericGraphQLErrors
	if err := c.doGraphQL(ctx, body, &gqlResp); err != nil {
		return err
	}
	if len(gqlResp.Errors) > 0 {
		return fmt.Errorf("DataHub API error: %s", gqlResp.Errors[0].Message)
	}
	return nil
}

// GroupMemberExists reports whether userURN is a member of groupURN. It reads
// the user's nativeGroupMembership aspect via the strongly-consistent OpenAPI v3
// entity endpoint (not the group's relationships, which are OpenSearch-backed
// and eventually consistent). A missing user is treated as "not a member".
func (c *Client) GroupMemberExists(ctx context.Context, groupURN, userURN string) (bool, error) {
	if c == nil {
		return false, errors.New("client is nil")
	}
	user, err := c.GetUserByURN(ctx, userURN)
	if err != nil {
		return false, err
	}
	if user == nil {
		return false, nil
	}
	for _, g := range user.NativeGroups {
		if g == groupURN {
			return true, nil
		}
	}
	return false, nil
}
