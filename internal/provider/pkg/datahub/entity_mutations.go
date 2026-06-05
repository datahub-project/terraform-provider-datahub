// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahub

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// UpdateEntityName updates the display name of any DataHub entity via the
// generic updateName GraphQL mutation, which writes the entity's *Properties
// aspect name field.
//
// This mutation is entity-agnostic and is shared across all entity types that
// support the updateName operation (domains, glossary nodes, glossary terms,
// corp groups, etc.).
func (c *Client) UpdateEntityName(ctx context.Context, urn, name string) error {
	if c == nil {
		return errors.New("client is nil")
	}
	const q = `
mutation updateName($input: UpdateNameInput!) {
  updateName(input: $input)
}`
	body := map[string]any{
		"query": q,
		"variables": map[string]any{
			"input": map[string]any{
				"urn":  urn,
				"name": strings.TrimSpace(name),
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

// UpdateEntityDescription updates the description of any DataHub entity via
// the generic updateDescription GraphQL mutation. Pass "" to clear the
// description. The mutation input uses resourceUrn (not urn) for the target
// entity.
//
// This mutation is entity-agnostic and is shared across all entity types that
// support the updateDescription operation (domains, glossary nodes, glossary
// terms, etc.).
func (c *Client) UpdateEntityDescription(ctx context.Context, urn, description string) error {
	if c == nil {
		return errors.New("client is nil")
	}
	const q = `
mutation updateDescription($input: DescriptionUpdateInput!) {
  updateDescription(input: $input)
}`
	body := map[string]any{
		"query": q,
		"variables": map[string]any{
			"input": map[string]any{
				"resourceUrn": urn,
				"description": description,
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
