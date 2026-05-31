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

// PolicyActors mirrors the DataHubActorFilter. There is no roles field in the
// OSS ActorFilterInput, so roles are not modeled here.
type PolicyActors struct {
	Users               []string
	Groups              []string
	AllUsers            bool
	AllGroups           bool
	ResourceOwners      bool
	ResourceOwnersTypes []string
}

// PolicyResources mirrors the (legacy) DataHubResourceFilter form: a resource
// type plus an explicit list of resource URNs, or all resources of the type.
type PolicyResources struct {
	Type         string
	Resources    []string
	AllResources bool
}

// PolicyInput is the write-shape for UpsertPolicy.
type PolicyInput struct {
	Type        string // PLATFORM | METADATA
	Name        string
	State       string // ACTIVE | INACTIVE
	Description string // required by the server; send "" when unset
	Privileges  []string
	Actors      PolicyActors
	Resources   *PolicyResources // nil for platform-wide policies
}

// Policy is the read-shape returned by GetPolicyByURN.
type Policy struct {
	URN         string
	ID          string
	Name        string
	Type        string
	State       string
	Description string
	Privileges  []string
	Actors      PolicyActors
	Resources   *PolicyResources
}

type upsertPolicyResponse struct {
	Data struct {
		UpdatePolicy string `json:"updatePolicy"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// UpsertPolicy creates or updates a policy at the given deterministic URN via the
// updatePolicy mutation. The updatePolicy resolver upserts at any URN supplied
// (it has no pre-existence check), so this is used for both create and update --
// keeping the policy URN deterministic (urn:li:dataHubPolicy:<id>) rather than a
// server-generated UUID. The full privilege/actor/resource state is sent every
// call (aspect-list ownership). Returns the policy URN.
func (c *Client) UpsertPolicy(ctx context.Context, urn string, in PolicyInput) (string, error) {
	if c == nil {
		return "", errors.New("client is nil")
	}
	urn = strings.TrimSpace(urn)
	if urn == "" {
		return "", errors.New("URN is required")
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return "", errors.New("name is required")
	}
	if len(in.Privileges) == 0 {
		return "", errors.New("at least one privilege is required")
	}

	actors := map[string]any{
		"allUsers":       in.Actors.AllUsers,
		"allGroups":      in.Actors.AllGroups,
		"resourceOwners": in.Actors.ResourceOwners,
	}
	if len(in.Actors.Users) > 0 {
		actors["users"] = in.Actors.Users
	}
	if len(in.Actors.Groups) > 0 {
		actors["groups"] = in.Actors.Groups
	}
	if len(in.Actors.ResourceOwnersTypes) > 0 {
		actors["resourceOwnersTypes"] = in.Actors.ResourceOwnersTypes
	}

	input := map[string]any{
		"type":        in.Type,
		"name":        in.Name,
		"state":       in.State,
		"description": in.Description, // must be non-null; "" is accepted
		"privileges":  in.Privileges,
		"actors":      actors,
	}
	if in.Resources != nil {
		res := map[string]any{
			"allResources": in.Resources.AllResources,
		}
		if in.Resources.Type != "" {
			res["type"] = in.Resources.Type
		}
		if len(in.Resources.Resources) > 0 {
			res["resources"] = in.Resources.Resources
		}
		input["resources"] = res
	}

	const q = `
mutation updatePolicy($urn: String!, $input: PolicyUpdateInput!) {
  updatePolicy(urn: $urn, input: $input)
}`
	body := map[string]any{
		"query": q,
		"variables": map[string]any{
			"urn":   urn,
			"input": input,
		},
	}

	var gqlResp upsertPolicyResponse
	if err := c.doGraphQL(ctx, body, &gqlResp); err != nil {
		return "", err
	}
	if len(gqlResp.Errors) > 0 {
		return "", fmt.Errorf("DataHub API error: %s", gqlResp.Errors[0].Message)
	}
	if gqlResp.Data.UpdatePolicy != "" {
		return gqlResp.Data.UpdatePolicy, nil
	}
	return urn, nil
}

type dataHubPolicyEntity struct {
	URN     string `json:"urn"`
	KeyData *struct {
		Value struct {
			ID string `json:"id"`
		} `json:"value"`
	} `json:"dataHubPolicyKey,omitempty"`
	Info *struct {
		Value struct {
			DisplayName string   `json:"displayName"`
			Description string   `json:"description"`
			Type        string   `json:"type"`
			State       string   `json:"state"`
			Privileges  []string `json:"privileges"`
			Actors      struct {
				Users               []string `json:"users"`
				Groups              []string `json:"groups"`
				AllUsers            bool     `json:"allUsers"`
				AllGroups           bool     `json:"allGroups"`
				ResourceOwners      bool     `json:"resourceOwners"`
				ResourceOwnersTypes []string `json:"resourceOwnersTypes"`
			} `json:"actors"`
			Resources *struct {
				Type         string   `json:"type"`
				Resources    []string `json:"resources"`
				AllResources bool     `json:"allResources"`
			} `json:"resources"`
		} `json:"value"`
	} `json:"dataHubPolicyInfo,omitempty"`
}

// GetPolicyByURN fetches a policy by URN via the OpenAPI v3 entity endpoint
// (MySQL, strongly consistent). Returns nil (no error) on 404.
func (c *Client) GetPolicyByURN(ctx context.Context, urn string) (*Policy, error) {
	if c == nil {
		return nil, errors.New("client is nil")
	}
	urn = strings.TrimSpace(urn)
	if urn == "" {
		return nil, errors.New("URN is required")
	}

	path := fmt.Sprintf("/openapi/v3/entity/datahubpolicy/%s", urn)
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
		return nil, fmt.Errorf("DataHub rejected the request (HTTP %d): the calling principal needs the MANAGE_POLICIES privilege", res.StatusCode)
	}
	if res.StatusCode >= http.StatusBadRequest {
		respBody, _ := io.ReadAll(res.Body)
		return nil, fmt.Errorf("unexpected HTTP %d from DataHub datahubpolicy API: %s", res.StatusCode, respBody)
	}

	var entity dataHubPolicyEntity
	if err := json.NewDecoder(res.Body).Decode(&entity); err != nil {
		return nil, fmt.Errorf("parsing datahubpolicy entity response: %w", err)
	}
	if entity.Info == nil {
		return nil, nil
	}

	id := ""
	if entity.KeyData != nil {
		id = entity.KeyData.Value.ID
	}
	if id == "" {
		id = strings.TrimPrefix(entity.URN, "urn:li:dataHubPolicy:")
	}

	v := entity.Info.Value
	p := &Policy{
		URN:         entity.URN,
		ID:          id,
		Name:        v.DisplayName,
		Type:        v.Type,
		State:       v.State,
		Description: v.Description,
		Privileges:  v.Privileges,
		Actors: PolicyActors{
			Users:               v.Actors.Users,
			Groups:              v.Actors.Groups,
			AllUsers:            v.Actors.AllUsers,
			AllGroups:           v.Actors.AllGroups,
			ResourceOwners:      v.Actors.ResourceOwners,
			ResourceOwnersTypes: v.Actors.ResourceOwnersTypes,
		},
	}
	if v.Resources != nil {
		p.Resources = &PolicyResources{
			Type:         v.Resources.Type,
			Resources:    v.Resources.Resources,
			AllResources: v.Resources.AllResources,
		}
	}
	return p, nil
}

// DeletePolicy deletes a policy by URN via the deletePolicy GraphQL mutation.
func (c *Client) DeletePolicy(ctx context.Context, urn string) error {
	if c == nil {
		return errors.New("client is nil")
	}
	urn = strings.TrimSpace(urn)
	if urn == "" {
		return errors.New("URN is required")
	}

	const q = `
mutation deletePolicy($urn: String!) {
  deletePolicy(urn: $urn)
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
		return fmt.Errorf("DataHub API error: %s", gqlResp.Errors[0].Message)
	}
	return nil
}
