// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

// Metadata test management for DataHub.
//
// A metadata test is the `test` entity: a declarative governance rule (a JSON
// definition with `on` scoping and `rules` conditions) evaluated against
// entities in the catalog, e.g. "every PROD dataset must have an owner".
//
// The entity type and the createTest/updateTest/deleteTest mutations plus the
// test(urn)/listTests queries exist in OSS DataHub (`category: core` in the
// entity registry) and in DataHub Cloud. The evaluation engine that runs the
// tests, validates definitions at write time and records results is DataHub
// Cloud only -- on OSS the API stores the definition verbatim and nothing
// evaluates it.
//
// Writes go through the GraphQL mutations (on Cloud these validate the
// definition, stamp an md5 and audit timestamps, and invalidate the engine
// cache -- all bypassed by a raw OpenAPI aspect write); reads use the
// strongly-consistent OpenAPI v3 entity endpoint
// (GET /openapi/v3/entity/test/{urn}).
//
// The URN is `urn:li:test:<id>`. The createTest resolver mints a random UUID
// when no id is supplied, so callers must always pass an explicit id to keep
// the URN deterministic. This matches the Python SDK's TestUrn, which is the
// caller-supplied id verbatim.
//
// The validateTest query (definition validation without persisting anything)
// exists only in DataHub Cloud.

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

// ErrMetadataTestValidationCloudOnly is returned when the validateTest query
// is attempted against an OSS DataHub instance, whose GraphQL schema does not
// define the field.
var ErrMetadataTestValidationCloudOnly = errors.New(
	"metadata test definition validation (validateTest) requires DataHub Cloud; " +
		"the configured GMS instance does not expose it",
)

// MetadataTestURNPrefix is the URN namespace for metadata tests.
const MetadataTestURNPrefix = "urn:li:test:"

// isMetadataTestValidationCloudOnlyError returns true when the GraphQL error
// indicates the validateTest query field is absent from the schema (OSS).
func isMetadataTestValidationCloudOnlyError(msg string) bool {
	return strings.Contains(msg, "FieldUndefined") && strings.Contains(msg, "validateTest")
}

// MetadataTestInput groups the inputs for createTest / updateTest.
type MetadataTestInput struct {
	TestID         string // URN key; the resource always supplies this
	Name           string
	Category       string
	Description    string // optional; omitted when empty
	DefinitionJSON string // the JSON rule document stored in definition.json
}

// MetadataTestInfo is the read shape from the OpenAPI v3 entity endpoint.
type MetadataTestInfo struct {
	ID             string
	Name           string
	Category       string
	Description    string
	DefinitionJSON string
	// Removed is status.removed: the test is soft-deleted. DataHub Cloud's
	// deleteTest leaves a test in this state, and DataHub Cloud then neither
	// lists nor runs it, but it still occupies its id: createTest of the
	// same id is rejected as already existing.
	Removed bool
}

// metadataTestEntity is the OpenAPI v3 response shape for
// GET /openapi/v3/entity/test/{urn}. DataHub Cloud adds server-managed fields
// to testInfo (definition.md5, created/lastUpdated stamps, status, schedule);
// they are ignored here.
type metadataTestEntity struct {
	URN string `json:"urn"`
	Key *struct {
		Value struct {
			ID string `json:"id"`
		} `json:"value"`
	} `json:"testKey,omitempty"`
	Info *struct {
		Value struct {
			Name        string `json:"name"`
			Category    string `json:"category"`
			Description string `json:"description"`
			Definition  struct {
				JSON string `json:"json"`
			} `json:"definition"`
		} `json:"value"`
	} `json:"testInfo,omitempty"`
	Status *struct {
		Value struct {
			Removed bool `json:"removed"`
		} `json:"value"`
	} `json:"status,omitempty"`
}

// metadataTestGraphQLInput builds the shared name/category/description/definition
// portion of CreateTestInput and UpdateTestInput.
func metadataTestGraphQLInput(in MetadataTestInput) map[string]any {
	input := map[string]any{
		"name":       in.Name,
		"category":   in.Category,
		"definition": map[string]any{"json": in.DefinitionJSON},
	}
	if in.Description != "" {
		input["description"] = in.Description
	}
	return input
}

// CreateMetadataTest creates a metadata test at the deterministic URN
// urn:li:test:<TestID> via the createTest GraphQL mutation and returns that
// URN. The server rejects an id that is already taken ("This Test already
// exists!"). On DataHub Cloud the mutation also validates the definition and
// fails with the validation messages; OSS stores it unvalidated.
func (c *Client) CreateMetadataTest(ctx context.Context, in MetadataTestInput) (string, error) {
	if c == nil {
		return "", errors.New("client is nil")
	}
	in.TestID = strings.TrimSpace(in.TestID)
	if in.TestID == "" {
		return "", errors.New("test ID is required")
	}
	if strings.TrimSpace(in.Name) == "" {
		return "", errors.New("name is required")
	}
	if strings.TrimSpace(in.Category) == "" {
		return "", errors.New("category is required")
	}
	if strings.TrimSpace(in.DefinitionJSON) == "" {
		return "", errors.New("definition JSON is required")
	}

	const q = `
mutation createTest($input: CreateTestInput!) {
  createTest(input: $input)
}`

	input := metadataTestGraphQLInput(in)
	input["id"] = in.TestID

	body := map[string]any{
		"query":     q,
		"variables": map[string]any{"input": input},
	}

	var gqlResp struct {
		Data struct {
			CreateTest string `json:"createTest"`
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

	urn := gqlResp.Data.CreateTest
	if urn == "" {
		urn = MetadataTestURNPrefix + in.TestID
	}
	return urn, nil
}

// UpdateMetadataTest replaces a metadata test's testInfo aspect via the
// updateTest GraphQL mutation. The mutation rebuilds the whole aspect from the
// input, so omitted optional fields (description) are cleared rather than
// preserved, and Cloud-managed fields not expressible here (run schedule,
// status) are reset to their defaults.
func (c *Client) UpdateMetadataTest(ctx context.Context, testID string, in MetadataTestInput) error {
	if c == nil {
		return errors.New("client is nil")
	}
	testID = strings.TrimSpace(testID)
	if testID == "" {
		return errors.New("test ID is required")
	}

	const q = `
mutation updateTest($urn: String!, $input: UpdateTestInput!) {
  updateTest(urn: $urn, input: $input)
}`

	body := map[string]any{
		"query": q,
		"variables": map[string]any{
			"urn":   MetadataTestURNPrefix + testID,
			"input": metadataTestGraphQLInput(in),
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

// GetMetadataTestByID reads a metadata test from the OpenAPI v3 entity
// endpoint (MySQL, strongly consistent). Returns nil (no error) on 404.
func (c *Client) GetMetadataTestByID(ctx context.Context, testID string) (*MetadataTestInfo, error) {
	if c == nil {
		return nil, errors.New("client is nil")
	}
	testID = strings.TrimSpace(testID)
	if testID == "" {
		return nil, errors.New("test ID is required")
	}

	urn := MetadataTestURNPrefix + testID
	path := fmt.Sprintf("/openapi/v3/entity/test/%s", urn)
	req, err := c.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	res, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("DataHub rejected the request (HTTP %d): the calling principal needs the MANAGE_TESTS privilege", res.StatusCode)
	}
	if res.StatusCode >= http.StatusBadRequest {
		respBody, _ := io.ReadAll(res.Body)
		return nil, fmt.Errorf("unexpected HTTP %d from DataHub test API: %s", res.StatusCode, respBody)
	}

	var entity metadataTestEntity
	if err := json.NewDecoder(res.Body).Decode(&entity); err != nil {
		return nil, fmt.Errorf("parsing test entity response: %w", err)
	}
	if entity.Key == nil && entity.Info == nil {
		return nil, nil
	}

	out := &MetadataTestInfo{ID: testID}
	if entity.Status != nil {
		out.Removed = entity.Status.Value.Removed
	}
	if entity.Key != nil && entity.Key.Value.ID != "" {
		out.ID = entity.Key.Value.ID
	}
	if entity.Info != nil {
		v := entity.Info.Value
		out.Name = v.Name
		out.Category = v.Category
		out.Description = v.Description
		out.DefinitionJSON = v.Definition.JSON
	}
	return out, nil
}

// DeleteMetadataTest removes a metadata test entirely, in two steps.
//
// First the deleteTest GraphQL mutation, which carries the service-layer side
// effects: on DataHub Cloud it invalidates the test engine's cache and clears
// the test's results from the entities it was evaluated against. What it does
// to the test itself differs by server. OSS DataHub hard-deletes it. DataHub
// Cloud only soft-deletes it (status.removed = true): the entity keeps its
// testKey and testInfo, still reads back, and still occupies the id, so a
// later createTest of the same id is rejected as already existing.
//
// Second a hard delete through the OpenAPI v3 entity endpoint, which removes
// whatever the mutation left behind, so terraform destroy means the same
// thing on both servers and the id can be reused. On OSS there is nothing
// left and the endpoint answers success regardless.
//
// Finally a read confirms the test is gone, so a delete the server reports as
// successful but does not perform fails here rather than surfacing later as a
// test that cannot be re-created.
//
// A not-found result from the mutation is treated as success (the entity is
// already gone, or was never created) and the hard delete still runs, which
// also clears a test that an earlier destroy, or the DataHub Cloud UI, left
// soft-deleted.
func (c *Client) DeleteMetadataTest(ctx context.Context, testID string) error {
	if c == nil {
		return errors.New("client is nil")
	}
	testID = strings.TrimSpace(testID)
	if testID == "" {
		return errors.New("test ID is required")
	}
	urn := MetadataTestURNPrefix + testID

	const q = `
mutation deleteTest($urn: String!) {
  deleteTest(urn: $urn)
}`
	body := map[string]any{
		"query":     q,
		"variables": map[string]any{"urn": urn},
	}
	var gqlResp genericGraphQLErrors
	if err := c.doGraphQL(ctx, body, &gqlResp); err != nil {
		return err
	}
	if len(gqlResp.Errors) > 0 {
		msg := gqlResp.Errors[0].Message
		if !strings.Contains(msg, "not found") && !strings.Contains(msg, "does not exist") {
			return fmt.Errorf("DataHub API error: %s", msg)
		}
	}

	if err := c.hardDeleteMetadataTest(ctx, urn); err != nil {
		return err
	}

	info, err := c.GetMetadataTestByID(ctx, testID)
	if err != nil {
		return fmt.Errorf("confirming metadata test %s was deleted: %w", urn, err)
	}
	if info != nil {
		return fmt.Errorf("metadata test %s still exists after DataHub accepted both the deleteTest mutation "+
			"and the hard delete (soft-deleted: %t); it has not been removed from Terraform state, and running "+
			"terraform destroy again retries the delete", urn, info.Removed)
	}
	return nil
}

// hardDeleteMetadataTest removes every aspect of a test via the OpenAPI v3
// entity endpoint. The endpoint answers success for an absent entity; a 404
// is tolerated too.
func (c *Client) hardDeleteMetadataTest(ctx context.Context, urn string) error {
	path := fmt.Sprintf("/openapi/v3/entity/test/%s", urn)
	req, err := c.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return err
	}
	res, err := c.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusNotFound || res.StatusCode < http.StatusBadRequest {
		return nil
	}
	respBody, _ := io.ReadAll(res.Body)
	if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
		return fmt.Errorf("DataHub rejected the hard delete of metadata test %s (HTTP %d): the calling principal "+
			"needs permission to delete the entity through the OpenAPI entity endpoint as well as MANAGE_TESTS. "+
			"On DataHub Cloud the test is left soft-deleted, which stops it running; running terraform destroy "+
			"again retries the hard delete. DataHub said: %s", urn, res.StatusCode, respBody)
	}
	return fmt.Errorf("unexpected HTTP %d hard-deleting metadata test %s: %s", res.StatusCode, urn, respBody)
}

// ListMetadataTestURNs returns the URNs of all metadata tests visible to the
// authenticated principal, via the listTests GraphQL query. Backed by search
// (eventually consistent) -- for enumeration/import, not authoritative reads.
func (c *Client) ListMetadataTestURNs(ctx context.Context) ([]string, error) {
	if c == nil {
		return nil, errors.New("client is nil")
	}

	const q = `
query listTests($input: ListTestsInput!) {
  listTests(input: $input) {
    total
    tests { urn }
  }
}`

	const pageSize = 100
	var urns []string
	start := 0
	for {
		body := map[string]any{
			"query": q,
			"variables": map[string]any{
				"input": map[string]any{"start": start, "count": pageSize},
			},
		}
		var raw struct {
			Data struct {
				ListTests struct {
					Total int `json:"total"`
					Tests []struct {
						URN string `json:"urn"`
					} `json:"tests"`
				} `json:"listTests"`
			} `json:"data"`
			Errors []struct {
				Message string `json:"message"`
			} `json:"errors"`
		}
		if err := c.doGraphQL(ctx, body, &raw); err != nil {
			return nil, err
		}
		if len(raw.Errors) > 0 {
			return nil, fmt.Errorf("DataHub API error: %s", raw.Errors[0].Message)
		}
		page := raw.Data.ListTests.Tests
		for _, p := range page {
			if p.URN != "" {
				urns = append(urns, p.URN)
			}
		}
		start += len(page)
		if start >= raw.Data.ListTests.Total || len(page) == 0 {
			break
		}
	}
	return urns, nil
}

// ValidateMetadataTestDefinition validates a metadata test definition against
// the server's test engine via the validateTest GraphQL query, without
// persisting anything. Returns whether the definition is valid plus the
// server's messages explaining why not. DataHub Cloud only; returns
// ErrMetadataTestValidationCloudOnly on OSS.
func (c *Client) ValidateMetadataTestDefinition(ctx context.Context, definitionJSON string) (bool, []string, error) {
	if c == nil {
		return false, nil, errors.New("client is nil")
	}
	if strings.TrimSpace(definitionJSON) == "" {
		return false, nil, errors.New("definition JSON is required")
	}

	const q = `
query validateTest($input: TestDefinitionInput!) {
  validateTest(input: $input) {
    isValid
    messages
  }
}`
	body := map[string]any{
		"query": q,
		"variables": map[string]any{
			"input": map[string]any{"json": definitionJSON},
		},
	}

	var raw struct {
		Data struct {
			ValidateTest *struct {
				IsValid  *bool    `json:"isValid"`
				Messages []string `json:"messages"`
			} `json:"validateTest"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := c.doGraphQL(ctx, body, &raw); err != nil {
		return false, nil, err
	}
	if len(raw.Errors) > 0 {
		msg := raw.Errors[0].Message
		if isMetadataTestValidationCloudOnlyError(msg) {
			return false, nil, ErrMetadataTestValidationCloudOnly
		}
		return false, nil, fmt.Errorf("DataHub API error: %s", msg)
	}
	if raw.Data.ValidateTest == nil {
		return false, nil, errors.New("DataHub returned no validateTest result")
	}
	valid := raw.Data.ValidateTest.IsValid != nil && *raw.Data.ValidateTest.IsValid
	return valid, raw.Data.ValidateTest.Messages, nil
}
