// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

// Data contract management (the `dataContract` entity).
//
// A data contract binds a dataset to lists of existing assertion URNs, grouped
// into three fixed categories (freshness / schema / dataQuality), plus a
// lifecycle state (ACTIVE / PENDING). It does not create assertions -- it
// references assertions authored elsewhere (e.g. the typed assertion resources).
//
// The entity and its `upsertDataContract` mutation exist in open-source DataHub,
// so this is an OSS+Cloud surface. Writes go through GraphQL `upsertDataContract`
// (there is no OpenAPI write path that carries the contract lists); Read uses the
// strongly-consistent OpenAPI v3 entity endpoint; Delete uses the OpenAPI v3
// entity DELETE (no `deleteDataContract` mutation exists), verified to be an
// effective hard delete with no lingering soft-deleted entity.
//
// The URN is `urn:li:dataContract:<id>`. Because the resolver otherwise mints a
// random UUID (and its find-existing path is the eventually-consistent
// ContractFor graph edge), the provider always passes a deterministic id
// (DataContractID = datahub_guid over the dataset URN, matching the Python SDK).

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

// DataContractURNPrefix is the URN namespace for data contracts.
const DataContractURNPrefix = "urn:li:dataContract:"

// DataContractInput is the desired state for an upsert.
type DataContractInput struct {
	ID                       string // URN key; derived from EntityURN when unset
	EntityURN                string
	State                    string // ACTIVE / PENDING; omitted -> server default ACTIVE
	FreshnessAssertionURNs   []string
	SchemaAssertionURNs      []string
	DataQualityAssertionURNs []string
}

// DataContract is the read-shape returned by GetDataContractByURN.
type DataContract struct {
	URN                      string
	ID                       string
	EntityURN                string
	State                    string
	FreshnessAssertionURNs   []string
	SchemaAssertionURNs      []string
	DataQualityAssertionURNs []string
}

// dataContractEntity is the OpenAPI v3 response for
// GET /openapi/v3/entity/datacontract/{urn}. On read each contract element is
// `{"assertion": "<urn>"}` (note: the write input key is `assertionUrn`).
type dataContractEntity struct {
	URN string `json:"urn"`
	Key *struct {
		Value struct {
			ID string `json:"id"`
		} `json:"value"`
	} `json:"dataContractKey,omitempty"`
	Props *struct {
		Value struct {
			Entity    string            `json:"entity"`
			Freshness []dataContractRef `json:"freshness"`
			Schema    []dataContractRef `json:"schema"`
			DataQual  []dataContractRef `json:"dataQuality"`
		} `json:"value"`
	} `json:"dataContractProperties,omitempty"`
	Status *struct {
		Value struct {
			State string `json:"state"`
		} `json:"value"`
	} `json:"dataContractStatus,omitempty"`
}

type dataContractRef struct {
	Assertion string `json:"assertion"`
}

func refsToURNs(refs []dataContractRef) []string {
	if len(refs) == 0 {
		return nil
	}
	out := make([]string, 0, len(refs))
	for _, r := range refs {
		if r.Assertion != "" {
			out = append(out, r.Assertion)
		}
	}
	return out
}

// urnsToContractInput converts a URN list to the GraphQL `[{assertionUrn}]` shape.
func urnsToContractInput(urns []string) []map[string]any {
	out := make([]map[string]any, 0, len(urns))
	for _, u := range urns {
		out = append(out, map[string]any{"assertionUrn": u})
	}
	return out
}

// UpsertDataContract creates or updates the data contract for a dataset at the
// deterministic URN urn:li:dataContract:<in.ID>, and returns that URN. The
// properties aspect is fully replaced on every upsert, so omitting a category
// clears it -- the caller owns the complete lists.
func (c *Client) UpsertDataContract(ctx context.Context, in DataContractInput) (string, error) {
	if c == nil {
		return "", errors.New("client is nil")
	}
	in.EntityURN = strings.TrimSpace(in.EntityURN)
	if in.EntityURN == "" {
		return "", errors.New("dataset URN is required")
	}
	in.ID = strings.TrimSpace(in.ID)
	if in.ID == "" {
		id, err := DataContractID(in.EntityURN)
		if err != nil {
			return "", err
		}
		in.ID = id
	}

	const q = `
mutation upsertDataContract($input: UpsertDataContractInput!) {
  upsertDataContract(input: $input) { urn }
}`

	input := map[string]any{
		"entityUrn": in.EntityURN,
		"id":        in.ID,
	}
	if in.State != "" {
		input["state"] = in.State
	}
	if len(in.FreshnessAssertionURNs) > 0 {
		input["freshness"] = urnsToContractInput(in.FreshnessAssertionURNs)
	}
	if len(in.SchemaAssertionURNs) > 0 {
		input["schema"] = urnsToContractInput(in.SchemaAssertionURNs)
	}
	if len(in.DataQualityAssertionURNs) > 0 {
		input["dataQuality"] = urnsToContractInput(in.DataQualityAssertionURNs)
	}

	urn := DataContractURNPrefix + in.ID
	body := map[string]any{"query": q, "variables": map[string]any{"input": input}}

	var raw genericGraphQLErrors
	if err := c.doGraphQL(ctx, body, &raw); err != nil {
		return urn, err
	}
	if len(raw.Errors) > 0 {
		return urn, fmt.Errorf("DataHub API error: %s", raw.Errors[0].Message)
	}
	return urn, nil
}

// GetDataContractByURN reads a data contract from the OpenAPI v3 entity endpoint
// (MySQL, strongly consistent). Returns nil (no error) on 404.
func (c *Client) GetDataContractByURN(ctx context.Context, urn string) (*DataContract, error) {
	if c == nil {
		return nil, errors.New("client is nil")
	}
	urn = strings.TrimSpace(urn)
	if urn == "" {
		return nil, errors.New("URN is required")
	}

	path := fmt.Sprintf("/openapi/v3/entity/datacontract/%s", urn)
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
	if res.StatusCode >= http.StatusBadRequest {
		respBody, _ := io.ReadAll(res.Body)
		return nil, fmt.Errorf("unexpected HTTP %d from DataHub data contract API: %s", res.StatusCode, respBody)
	}

	var entity dataContractEntity
	if err := json.NewDecoder(res.Body).Decode(&entity); err != nil {
		return nil, fmt.Errorf("parsing data contract entity response: %w", err)
	}
	if entity.Key == nil && entity.Props == nil {
		return nil, nil
	}

	dc := &DataContract{URN: entity.URN}
	if entity.Key != nil {
		dc.ID = entity.Key.Value.ID
	}
	if dc.ID == "" {
		dc.ID = strings.TrimPrefix(entity.URN, DataContractURNPrefix)
	}
	if entity.Props != nil {
		dc.EntityURN = entity.Props.Value.Entity
		dc.FreshnessAssertionURNs = refsToURNs(entity.Props.Value.Freshness)
		dc.SchemaAssertionURNs = refsToURNs(entity.Props.Value.Schema)
		dc.DataQualityAssertionURNs = refsToURNs(entity.Props.Value.DataQual)
	}
	if entity.Status != nil {
		dc.State = entity.Status.Value.State
	}
	return dc, nil
}

// DeleteDataContract removes a data contract via the OpenAPI v3 entity DELETE
// (there is no deleteDataContract GraphQL mutation). Verified to be an effective
// hard delete: a subsequent read returns 404 with no lingering soft-deleted
// entity. A 404 is treated as success (idempotent).
func (c *Client) DeleteDataContract(ctx context.Context, urn string) error {
	if c == nil {
		return errors.New("client is nil")
	}
	urn = strings.TrimSpace(urn)
	if urn == "" {
		return errors.New("URN is required")
	}

	path := fmt.Sprintf("/openapi/v3/entity/datacontract/%s", urn)
	req, err := c.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return err
	}
	res, err := c.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusNotFound {
		return nil
	}
	if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
		return fmt.Errorf("DataHub rejected the request (HTTP %d): the calling principal needs the EDIT_ENTITY privilege on the dataset", res.StatusCode)
	}
	if res.StatusCode >= http.StatusBadRequest {
		respBody, _ := io.ReadAll(res.Body)
		return fmt.Errorf("unexpected HTTP %d from DataHub data contract API: %s", res.StatusCode, respBody)
	}
	return nil
}

// ListDataContractURNs returns the URNs of all data contracts visible to the
// authenticated principal, via searchAcrossEntities (entity type DATA_CONTRACT).
// Backed by OpenSearch (eventually consistent) -- for enumeration/import, not
// authoritative reads.
func (c *Client) ListDataContractURNs(ctx context.Context) ([]string, error) {
	return listURNsByEntityType(ctx, c, "DATA_CONTRACT")
}
