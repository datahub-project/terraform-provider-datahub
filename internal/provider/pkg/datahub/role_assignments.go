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

// AssignRole assigns a built-in DataHub role to an actor (user or group) via the
// batchAssignRole mutation. DataHub enforces one role per actor, so assigning a
// new role replaces any existing one.
func (c *Client) AssignRole(ctx context.Context, roleURN, actorURN string) error {
	if c == nil {
		return errors.New("client is nil")
	}
	if roleURN == "" || actorURN == "" {
		return errors.New("roleURN and actorURN are required")
	}
	return c.batchAssignRole(ctx, &roleURN, actorURN)
}

// UnassignRole removes any role from an actor via batchAssignRole with a null
// roleUrn, which sets the actor's roleMembership.roles to an empty array.
func (c *Client) UnassignRole(ctx context.Context, actorURN string) error {
	if c == nil {
		return errors.New("client is nil")
	}
	if actorURN == "" {
		return errors.New("actorURN is required")
	}
	return c.batchAssignRole(ctx, nil, actorURN)
}

// batchAssignRole calls the batchAssignRole mutation. A nil roleURN removes the
// actor's role.
func (c *Client) batchAssignRole(ctx context.Context, roleURN *string, actorURN string) error {
	const q = `
mutation batchAssignRole($input: BatchAssignRoleInput!) {
  batchAssignRole(input: $input)
}`
	input := map[string]any{
		"actors": []string{actorURN},
	}
	if roleURN != nil {
		input["roleUrn"] = *roleURN
	}
	body := map[string]any{
		"query":     q,
		"variables": map[string]any{"input": input},
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

type roleMembershipEntity struct {
	RoleMembership *struct {
		Value struct {
			Roles []string `json:"roles"`
		} `json:"value"`
	} `json:"roleMembership,omitempty"`
}

// GetActorRole returns the role URN currently assigned to an actor (user or
// group), reading the strongly-consistent roleMembership aspect via OpenAPI v3.
// found is false when the actor has no role (empty or absent aspect) or does not
// exist. DataHub stores at most one role per actor.
func (c *Client) GetActorRole(ctx context.Context, actorURN string) (string, bool, error) {
	if c == nil {
		return "", false, errors.New("client is nil")
	}
	entityType, err := actorEntityType(actorURN)
	if err != nil {
		return "", false, err
	}

	path := fmt.Sprintf("/openapi/v3/entity/%s/%s", entityType, actorURN)
	req, err := c.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return "", false, err
	}

	res, err := c.Do(req)
	if err != nil {
		return "", false, err
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusNotFound {
		return "", false, nil
	}
	if res.StatusCode >= http.StatusBadRequest {
		respBody, _ := io.ReadAll(res.Body)
		return "", false, fmt.Errorf("unexpected HTTP %d reading roleMembership for %s: %s", res.StatusCode, actorURN, respBody)
	}

	var entity roleMembershipEntity
	if err := json.NewDecoder(res.Body).Decode(&entity); err != nil {
		return "", false, fmt.Errorf("parsing roleMembership response: %w", err)
	}
	if entity.RoleMembership == nil || len(entity.RoleMembership.Value.Roles) == 0 {
		return "", false, nil
	}
	return entity.RoleMembership.Value.Roles[0], true, nil
}

// actorEntityType maps an actor URN to its OpenAPI v3 entity-type path segment.
func actorEntityType(actorURN string) (string, error) {
	switch {
	case strings.HasPrefix(actorURN, "urn:li:corpuser:"):
		return "corpuser", nil
	case strings.HasPrefix(actorURN, "urn:li:corpGroup:"):
		return "corpgroup", nil
	default:
		return "", fmt.Errorf("actor URN %q must be a corpuser or corpGroup URN", actorURN)
	}
}
