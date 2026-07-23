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
	"strconv"
	"strings"
)

// assignmentTargets is the set of entity types the provider supports as targets
// for a structured-property value assignment. It is deliberately limited to
// platform-governance entities; ingested assets (dataset, chart, dashboard, ...)
// are out of scope for the provider (per-asset enrichment is business-user /
// ingestion territory).
//
// The two names differ by case: pathSegment is the lowercase OpenAPI v3 entity
// path, while entityType matches the short name used in a structured property
// definition's entityTypes (as returned by shortEntityType), e.g. "glossaryNode"
// vs the path "glossarynode".
var assignmentTargets = []struct {
	urnPrefix   string
	pathSegment string
	entityType  string
}{
	{"urn:li:domain:", "domain", "domain"},
	{"urn:li:glossaryNode:", "glossarynode", "glossaryNode"},
	{"urn:li:glossaryTerm:", "glossaryterm", "glossaryTerm"},
	{"urn:li:dataProduct:", "dataproduct", "dataProduct"},
	// The corpuser prefix also matches service-account URNs
	// (urn:li:corpuser:service_<id>) - intended: they are corpuser entities.
	// Note the registry short name is all-lowercase "corpuser" (unlike
	// "corpGroup"/"dataContract").
	{"urn:li:corpuser:", "corpuser", "corpuser"},
	{"urn:li:corpGroup:", "corpgroup", "corpGroup"},
	{"urn:li:dataContract:", "datacontract", "dataContract"},
}

// SupportedAssignmentEntityTypes returns the short entity-type names the provider
// allows as assignment targets (for validator messages).
func SupportedAssignmentEntityTypes() []string {
	out := make([]string, len(assignmentTargets))
	for i, t := range assignmentTargets {
		out[i] = t.entityType
	}
	return out
}

// AssignmentTargetType resolves a target entity URN to its OpenAPI v3 path
// segment and its structured-property entityType short name. It errors for any
// URN whose entity type the provider does not support as an assignment target.
func AssignmentTargetType(entityURN string) (pathSegment, entityType string, err error) {
	for _, t := range assignmentTargets {
		if strings.HasPrefix(entityURN, t.urnPrefix) {
			return t.pathSegment, t.entityType, nil
		}
	}
	return "", "", fmt.Errorf(
		"entity URN %q is not a supported structured-property assignment target; supported types: %s",
		entityURN, strings.Join(SupportedAssignmentEntityTypes(), ", "))
}

// structuredPropertyValueParams maps assignment values to the GraphQL
// PropertyValueInput shape, routing by the property definition's value type: a
// "number" property takes numberValue (Float), everything else (string, date,
// urn, rich_text) takes stringValue.
func structuredPropertyValueParams(valueType string, values []string) ([]map[string]any, error) {
	out := make([]map[string]any, 0, len(values))
	for _, v := range values {
		if valueType == "number" {
			f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
			if err != nil {
				return nil, fmt.Errorf("value %q is not a valid number for a number-typed structured property", v)
			}
			out = append(out, map[string]any{"numberValue": f})
		} else {
			out = append(out, map[string]any{"stringValue": v})
		}
	}
	return out, nil
}

// lockEntityStructuredProps serializes structuredProperties-aspect writes to a
// single entity within this provider process, returning an unlock function
// (unlock := c.lockEntityStructuredProps(urn); defer unlock()).
//
// CAT-2568: upsertStructuredProperties / removeStructuredProperties perform a
// non-atomic read-modify-write of the entity's single structuredProperties
// aspect server-side, so concurrent writes to the SAME entity -- even for
// different properties -- silently lose updates (last-writer-wins, HTTP 200, no
// error). The provider models each assignment as its own resource, so Terraform
// fires these mutations in parallel at its default parallelism; without
// serialization, multi-property assignments to one entity drop values.
// Serializing per entity URN removes the race while leaving writes to different
// entities fully parallel. Remove this workaround once CAT-2568 is fixed
// server-side (atomic merge or optimistic-concurrency conflict).
func (c *Client) lockEntityStructuredProps(entityURN string) func() {
	return c.structuredPropLocks.lock(entityURN)
}

// SetStructuredPropertyValues assigns values for one structured property on one
// entity via the upsertStructuredProperties mutation. This is a per-property
// MERGE: values for other properties already on the entity are left intact, and
// this property's values are replaced with the supplied list. valueType is the
// property definition's short value type (drives string vs number routing).
//
// Server-side validation (allowedValues, cardinality, value type) is performed
// by DataHub's StructuredPropertiesValidator; its error is surfaced verbatim.
func (c *Client) SetStructuredPropertyValues(ctx context.Context, entityURN, propertyURN, valueType string, values []string) error {
	if c == nil {
		return errors.New("client is nil")
	}
	if entityURN == "" || propertyURN == "" {
		return errors.New("entityURN and propertyURN are required")
	}
	if _, _, err := AssignmentTargetType(entityURN); err != nil {
		return err
	}

	// CAT-2568: serialize writes to this entity's structuredProperties aspect.
	unlock := c.lockEntityStructuredProps(entityURN)
	defer unlock()

	valueParams, err := structuredPropertyValueParams(valueType, values)
	if err != nil {
		return err
	}

	const q = `
mutation upsertStructuredProperties($input: UpsertStructuredPropertiesInput!) {
  upsertStructuredProperties(input: $input) {
    properties { structuredProperty { urn } }
  }
}`
	input := map[string]any{
		"assetUrn": entityURN,
		"structuredPropertyInputParams": []map[string]any{
			{
				"structuredPropertyUrn": propertyURN,
				"values":                valueParams,
			},
		},
	}
	body := map[string]any{"query": q, "variables": map[string]any{"input": input}}

	var gqlResp genericGraphQLErrors
	if err := c.doGraphQL(ctx, body, &gqlResp); err != nil {
		return err
	}
	if len(gqlResp.Errors) > 0 {
		return fmt.Errorf("DataHub API error: %s", gqlResp.Errors[0].Message)
	}
	return nil
}

// RemoveStructuredProperty clears one structured property's values from one
// entity via the removeStructuredProperties mutation (per-property; other
// properties on the entity are left intact). Idempotent when already absent.
func (c *Client) RemoveStructuredProperty(ctx context.Context, entityURN, propertyURN string) error {
	if c == nil {
		return errors.New("client is nil")
	}
	if entityURN == "" || propertyURN == "" {
		return errors.New("entityURN and propertyURN are required")
	}

	// CAT-2568: serialize writes to this entity's structuredProperties aspect.
	unlock := c.lockEntityStructuredProps(entityURN)
	defer unlock()

	const q = `
mutation removeStructuredProperties($input: RemoveStructuredPropertiesInput!) {
  removeStructuredProperties(input: $input) {
    properties { structuredProperty { urn } }
  }
}`
	input := map[string]any{
		"assetUrn":               entityURN,
		"structuredPropertyUrns": []string{propertyURN},
	}
	body := map[string]any{"query": q, "variables": map[string]any{"input": input}}

	var gqlResp genericGraphQLErrors
	if err := c.doGraphQL(ctx, body, &gqlResp); err != nil {
		return err
	}
	if len(gqlResp.Errors) > 0 {
		return fmt.Errorf("DataHub API error: %s", gqlResp.Errors[0].Message)
	}
	return nil
}

// structuredPropertiesAspectEntity is the OpenAPI v3 read shape for the
// structuredProperties aspect on any target entity.
type structuredPropertiesAspectEntity struct {
	URN                  string `json:"urn"`
	StructuredProperties *struct {
		Value struct {
			Properties []struct {
				PropertyURN string `json:"propertyUrn"`
				Values      []struct {
					String *string  `json:"string,omitempty"`
					Double *float64 `json:"double,omitempty"`
				} `json:"values"`
			} `json:"properties"`
		} `json:"value"`
	} `json:"structuredProperties,omitempty"`
}

// GetStructuredPropertyValues reads the values assigned for one structured
// property on one entity via the strongly-consistent OpenAPI v3 entity endpoint.
// found is false when the entity has no assignment for that property, or the
// entity does not exist. Number values are formatted minimally (e.g. "30", not
// "30.000000") so they round-trip against list(string) config.
func (c *Client) GetStructuredPropertyValues(ctx context.Context, entityURN, propertyURN string) ([]string, bool, error) {
	if c == nil {
		return nil, false, errors.New("client is nil")
	}
	pathSegment, _, err := AssignmentTargetType(entityURN)
	if err != nil {
		return nil, false, err
	}

	path := fmt.Sprintf("/openapi/v3/entity/%s/%s", pathSegment, entityURN)
	req, err := c.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, false, err
	}

	res, err := c.Do(req)
	if err != nil {
		return nil, false, err
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusNotFound {
		return nil, false, nil
	}
	if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
		return nil, false, fmt.Errorf("DataHub rejected the request (HTTP %d): the calling principal needs the Edit Properties privilege on %s", res.StatusCode, entityURN)
	}
	if res.StatusCode >= http.StatusBadRequest {
		respBody, _ := io.ReadAll(res.Body)
		return nil, false, fmt.Errorf("unexpected HTTP %d reading structuredProperties for %s: %s", res.StatusCode, entityURN, respBody)
	}

	var entity structuredPropertiesAspectEntity
	if err := json.NewDecoder(res.Body).Decode(&entity); err != nil {
		return nil, false, fmt.Errorf("parsing structuredProperties response: %w", err)
	}
	if entity.StructuredProperties == nil {
		return nil, false, nil
	}
	for _, p := range entity.StructuredProperties.Value.Properties {
		if p.PropertyURN != propertyURN {
			continue
		}
		values := make([]string, 0, len(p.Values))
		for _, v := range p.Values {
			switch {
			case v.String != nil:
				values = append(values, *v.String)
			case v.Double != nil:
				values = append(values, strconv.FormatFloat(*v.Double, 'f', -1, 64))
			}
		}
		return values, true, nil
	}
	return nil, false, nil
}
