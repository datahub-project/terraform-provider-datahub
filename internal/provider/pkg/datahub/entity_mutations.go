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

// SetEntityDomain associates any DataHub entity with a domain via the generic
// setDomain GraphQL mutation. Pass a full domain URN (e.g.
// "urn:li:domain:finance"). This replaces any previously set domain.
//
// This mutation is entity-agnostic and works for glossary nodes, glossary
// terms, datasets, and any other entity that supports the domains aspect.
func (c *Client) SetEntityDomain(ctx context.Context, entityURN, domainURN string) error {
	if c == nil {
		return errors.New("client is nil")
	}
	const q = `
mutation setDomain($entityUrn: String!, $domainUrn: String!) {
  setDomain(entityUrn: $entityUrn, domainUrn: $domainUrn)
}`
	body := map[string]any{
		"query": q,
		"variables": map[string]any{
			"entityUrn": entityURN,
			"domainUrn": domainURN,
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

// UnsetEntityDomain removes any domain association from a DataHub entity via
// the generic unsetDomain GraphQL mutation.
func (c *Client) UnsetEntityDomain(ctx context.Context, entityURN string) error {
	if c == nil {
		return errors.New("client is nil")
	}
	const q = `
mutation unsetDomain($entityUrn: String!) {
  unsetDomain(entityUrn: $entityUrn)
}`
	body := map[string]any{
		"query": q,
		"variables": map[string]any{
			"entityUrn": entityURN,
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
