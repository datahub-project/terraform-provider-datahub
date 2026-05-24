// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

// Remote Executor Pool management for DataHub Cloud.
//
// Pools are a DataHub Cloud-only concept. On OSS DataHub the GraphQL mutations
// used here do not exist; callers will receive an ErrExecutorPoolCloudOnly
// sentinel and should surface it as a clean Terraform diagnostic rather than a
// raw API error.
//
// API shape (DataHub Cloud GraphQL, remote_executor.saas.graphql):
//   - Create: createRemoteExecutorPool(input) - returns URN string
//   - Read:   getRemoteExecutorPool(urn) - returns RemoteExecutorPool or null
//   - Update description: updateRemoteExecutorPool(input)
//   - Set default: updateDefaultRemoteExecutorPool(urn)
//   - Delete: DELETE /openapi/v3/entity/datahubremoteexecutorpool/{urn}
//
// Read uses GraphQL (getRemoteExecutorPool) because this entity type is
// not registered for the OSS OpenAPI v3 entity endpoint. The GraphQL read
// goes through EntityClient batch loading which is strongly consistent.

package datahub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// ErrExecutorPoolCloudOnly is returned when the DataHub instance does not
// expose the Remote Executor Pool GraphQL operations (OSS DataHub, or an
// older Cloud build that pre-dates the feature).
var ErrExecutorPoolCloudOnly = errors.New(
	"datahub_remote_executor_pool requires DataHub Cloud; " +
		"the configured GMS instance does not expose remote executor pool management",
)

// poolIDPattern is the valid character set for a remote executor pool ID.
// Mirrors the server-side validation in CreateRemoteExecutorPoolResolver.java.
var poolIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_.-]+$`)

// RemoteExecutorPool is the read-shape returned by getRemoteExecutorPool.
type RemoteExecutorPool struct {
	URN         string
	PoolID      string
	Description string
	IsDefault   bool
	IsEmbedded  bool
	CreatedAt   int64
	StateStatus string
	StateMsg    string
	Channel     string
}

// CreateRemoteExecutorPoolInput groups the fields accepted by createRemoteExecutorPool.
type CreateRemoteExecutorPoolInput struct {
	PoolID      string
	Description string
	IsDefault   bool
}

// UpdateRemoteExecutorPoolInput groups the updatable fields for updateRemoteExecutorPool.
// Description is a pointer so callers can distinguish "clear description" (pointer to "")
// from "leave description unchanged" (nil pointer).
type UpdateRemoteExecutorPoolInput struct {
	URN         string
	Description *string
}

// gql response envelopes

type createRemoteExecutorPoolResponse struct {
	Data struct {
		CreateRemoteExecutorPool string `json:"createRemoteExecutorPool"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

type updateRemoteExecutorPoolResponse struct {
	Data struct {
		UpdateRemoteExecutorPool bool `json:"updateRemoteExecutorPool"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

type updateDefaultRemoteExecutorPoolResponse struct {
	Data struct {
		UpdateDefaultRemoteExecutorPool bool `json:"updateDefaultRemoteExecutorPool"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

type getRemoteExecutorPoolResponse struct {
	Data struct {
		GetRemoteExecutorPool *remoteExecutorPoolGQL `json:"getRemoteExecutorPool"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

type remoteExecutorPoolGQL struct {
	URN            string `json:"urn"`
	ExecutorPoolID string `json:"executorPoolId"`
	Description    string `json:"description"`
	IsDefault      bool   `json:"isDefault"`
	IsEmbedded     bool   `json:"isEmbedded"`
	CreatedAt      int64  `json:"createdAt"`
	State          *struct {
		Status  string `json:"status"`
		Message string `json:"message"`
	} `json:"state"`
	Channel string `json:"channel"`
}

func toRemoteExecutorPool(g *remoteExecutorPoolGQL) *RemoteExecutorPool {
	p := &RemoteExecutorPool{
		URN:        g.URN,
		PoolID:     g.ExecutorPoolID,
		IsDefault:  g.IsDefault,
		IsEmbedded: g.IsEmbedded,
		CreatedAt:  g.CreatedAt,
		Channel:    g.Channel,
	}
	if g.Description != "" {
		p.Description = g.Description
	}
	if g.State != nil {
		p.StateStatus = g.State.Status
		p.StateMsg = g.State.Message
	}
	return p
}

// isCloudOnlyError returns true when the GraphQL error message indicates the
// mutation or query is not defined on this GMS instance (OSS DataHub).
func isCloudOnlyError(msg string) bool {
	return strings.Contains(msg, "FieldUndefined") ||
		(strings.Contains(msg, "is undefined") && strings.Contains(msg, "RemoteExecutorPool"))
}

// ValidatePoolID returns an error if poolID fails the server-side format rules.
// Call this before Create to surface validation errors as Terraform diagnostics
// rather than raw API errors.
func ValidatePoolID(poolID string) error {
	if poolID == "" {
		return errors.New("pool_id must not be empty")
	}
	if !poolIDPattern.MatchString(poolID) {
		return errors.New("pool_id must contain only alphanumeric characters, _, ., or -")
	}
	switch poolID {
	case "default":
		// The "default" pool is auto-provisioned on Cloud tenant onboarding. Import it rather than create.
		return fmt.Errorf(
			"pool_id %q is reserved by DataHub Cloud; use the data source to reference it: "+
				"data \"datahub_remote_executor_pool\" \"default\" { pool_id = \"default\" }",
			poolID,
		)
	case "embedded":
		// "embedded" is a reserved name in Cloud's validation; no pool entity exists at this ID.
		return fmt.Errorf(
			"pool_id %q is reserved by DataHub Cloud and does not correspond to an active pool entity; "+
				"choose a different pool_id",
			poolID,
		)
	}
	return nil
}

const getRemoteExecutorPoolQuery = `
query getRemoteExecutorPool($urn: String!) {
  getRemoteExecutorPool(urn: $urn) {
    urn
    executorPoolId
    description
    isDefault
    isEmbedded
    createdAt
    state {
      status
      message
    }
    channel
  }
}`

// CreateRemoteExecutorPool creates a pool and returns its URN.
func (c *Client) CreateRemoteExecutorPool(ctx context.Context, in CreateRemoteExecutorPoolInput) (string, error) {
	if c == nil {
		return "", errors.New("client is nil")
	}

	const q = `
mutation createRemoteExecutorPool($input: CreateRemoteExecutorPoolInput!) {
  createRemoteExecutorPool(input: $input)
}`

	body := map[string]any{
		"query": q,
		"variables": map[string]any{
			"input": map[string]any{
				"executorPoolId": in.PoolID,
				"description":    in.Description,
				"isDefault":      in.IsDefault,
			},
		},
	}

	req, err := c.NewRequest(ctx, http.MethodPost, "/api/graphql", body)
	if err != nil {
		return "", err
	}

	res, err := c.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
		return "", fmt.Errorf("DataHub rejected the request (HTTP %d): the calling principal needs the MANAGE_INGESTION privilege", res.StatusCode)
	}
	if res.StatusCode >= http.StatusBadRequest {
		return "", fmt.Errorf("unexpected HTTP %d from DataHub executor pool API", res.StatusCode)
	}

	var gqlResp createRemoteExecutorPoolResponse
	if err := json.NewDecoder(res.Body).Decode(&gqlResp); err != nil {
		return "", fmt.Errorf("parsing createRemoteExecutorPool response: %w", err)
	}

	if len(gqlResp.Errors) > 0 {
		msg := gqlResp.Errors[0].Message
		if isCloudOnlyError(msg) {
			return "", ErrExecutorPoolCloudOnly
		}
		if strings.Contains(msg, "already exists") {
			return "", fmt.Errorf(
				"executor pool %q already exists in DataHub; import it with "+
					"`terraform import datahub_remote_executor_pool.<label> %s`",
				in.PoolID,
				fmt.Sprintf("urn:li:dataHubRemoteExecutorPool:%s", in.PoolID),
			)
		}
		return "", fmt.Errorf("DataHub API error: %s", msg)
	}

	urn := gqlResp.Data.CreateRemoteExecutorPool
	if urn == "" {
		urn = fmt.Sprintf("urn:li:dataHubRemoteExecutorPool:%s", in.PoolID)
	}
	return urn, nil
}

// GetRemoteExecutorPoolByURN fetches a pool by URN via GraphQL.
// Returns nil (no error) when the URN does not exist or the pool is not found.
func (c *Client) GetRemoteExecutorPoolByURN(ctx context.Context, urn string) (*RemoteExecutorPool, error) {
	if c == nil {
		return nil, errors.New("client is nil")
	}
	urn = strings.TrimSpace(urn)
	if urn == "" {
		return nil, errors.New("URN is required")
	}

	body := map[string]any{
		"query":     getRemoteExecutorPoolQuery,
		"variables": map[string]any{"urn": urn},
	}

	req, err := c.NewRequest(ctx, http.MethodPost, "/api/graphql", body)
	if err != nil {
		return nil, err
	}

	res, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("DataHub rejected the request (HTTP %d): the calling principal needs the MANAGE_INGESTION privilege", res.StatusCode)
	}
	if res.StatusCode >= http.StatusBadRequest {
		respBody, _ := io.ReadAll(res.Body)
		return nil, fmt.Errorf("unexpected HTTP %d from DataHub executor pool API: %s", res.StatusCode, respBody)
	}

	var gqlResp getRemoteExecutorPoolResponse
	if err := json.NewDecoder(res.Body).Decode(&gqlResp); err != nil {
		return nil, fmt.Errorf("parsing getRemoteExecutorPool response: %w", err)
	}

	if len(gqlResp.Errors) > 0 {
		msg := gqlResp.Errors[0].Message
		if isCloudOnlyError(msg) {
			return nil, ErrExecutorPoolCloudOnly
		}
		return nil, fmt.Errorf("DataHub API error: %s", msg)
	}

	if gqlResp.Data.GetRemoteExecutorPool == nil {
		return nil, nil
	}
	return toRemoteExecutorPool(gqlResp.Data.GetRemoteExecutorPool), nil
}

// GetRemoteExecutorPoolByID is a convenience wrapper that constructs the URN
// from a pool ID before fetching.
func (c *Client) GetRemoteExecutorPoolByID(ctx context.Context, poolID string) (*RemoteExecutorPool, error) {
	urn := fmt.Sprintf("urn:li:dataHubRemoteExecutorPool:%s", poolID)
	return c.GetRemoteExecutorPoolByURN(ctx, urn)
}

// UpdateRemoteExecutorPool updates the mutable fields of an existing pool.
func (c *Client) UpdateRemoteExecutorPool(ctx context.Context, in UpdateRemoteExecutorPoolInput) error {
	if c == nil {
		return errors.New("client is nil")
	}
	in.URN = strings.TrimSpace(in.URN)
	if in.URN == "" {
		return errors.New("URN is required")
	}

	const q = `
mutation updateRemoteExecutorPool($input: UpdateRemoteExecutorPoolInput!) {
  updateRemoteExecutorPool(input: $input)
}`

	inputVars := map[string]any{"urn": in.URN}
	if in.Description != nil {
		inputVars["description"] = *in.Description
	}

	body := map[string]any{
		"query":     q,
		"variables": map[string]any{"input": inputVars},
	}

	req, err := c.NewRequest(ctx, http.MethodPost, "/api/graphql", body)
	if err != nil {
		return err
	}

	res, err := c.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
		return fmt.Errorf("DataHub rejected the request (HTTP %d): the calling principal needs the MANAGE_INGESTION privilege", res.StatusCode)
	}
	if res.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf("unexpected HTTP %d from DataHub executor pool API", res.StatusCode)
	}

	var gqlResp updateRemoteExecutorPoolResponse
	if err := json.NewDecoder(res.Body).Decode(&gqlResp); err != nil {
		return fmt.Errorf("parsing updateRemoteExecutorPool response: %w", err)
	}

	if len(gqlResp.Errors) > 0 {
		msg := gqlResp.Errors[0].Message
		if isCloudOnlyError(msg) {
			return ErrExecutorPoolCloudOnly
		}
		return fmt.Errorf("DataHub API error: %s", msg)
	}
	return nil
}

// SetDefaultRemoteExecutorPool promotes the given pool to the global default.
// Demotes the previously-default pool atomically on the server side.
func (c *Client) SetDefaultRemoteExecutorPool(ctx context.Context, urn string) error {
	if c == nil {
		return errors.New("client is nil")
	}
	urn = strings.TrimSpace(urn)
	if urn == "" {
		return errors.New("URN is required")
	}

	const q = `
mutation updateDefaultRemoteExecutorPool($urn: String!) {
  updateDefaultRemoteExecutorPool(urn: $urn)
}`

	body := map[string]any{
		"query":     q,
		"variables": map[string]any{"urn": urn},
	}

	req, err := c.NewRequest(ctx, http.MethodPost, "/api/graphql", body)
	if err != nil {
		return err
	}

	res, err := c.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
		return fmt.Errorf("DataHub rejected the request (HTTP %d): the calling principal needs the MANAGE_INGESTION privilege", res.StatusCode)
	}
	if res.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf("unexpected HTTP %d from DataHub executor pool API", res.StatusCode)
	}

	var gqlResp updateDefaultRemoteExecutorPoolResponse
	if err := json.NewDecoder(res.Body).Decode(&gqlResp); err != nil {
		return fmt.Errorf("parsing updateDefaultRemoteExecutorPool response: %w", err)
	}

	if len(gqlResp.Errors) > 0 {
		msg := gqlResp.Errors[0].Message
		if isCloudOnlyError(msg) {
			return ErrExecutorPoolCloudOnly
		}
		return fmt.Errorf("DataHub API error: %s", msg)
	}
	return nil
}

// DeleteRemoteExecutorPool deletes a pool by URN via the OpenAPI v3 entity endpoint.
// Returns nil when the pool is already gone (idempotent on 404).
//
// There is no GraphQL deleteRemoteExecutorPool mutation; deletion goes through
// the generic OpenAPI entity DELETE path.
func (c *Client) DeleteRemoteExecutorPool(ctx context.Context, urn string) error {
	if c == nil {
		return errors.New("client is nil")
	}
	urn = strings.TrimSpace(urn)
	if urn == "" {
		return errors.New("URN is required")
	}

	path := fmt.Sprintf("/openapi/v3/entity/datahubremoteexecutorpool/%s", urn)
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
		return fmt.Errorf("DataHub rejected the request (HTTP %d): the calling principal needs the MANAGE_INGESTION privilege", res.StatusCode)
	}
	if res.StatusCode >= http.StatusBadRequest {
		respBody, _ := io.ReadAll(res.Body)
		return fmt.Errorf("unexpected HTTP %d from DataHub executor pool API: %s", res.StatusCode, respBody)
	}
	return nil
}

// WaitForRemoteExecutorPoolReady polls getRemoteExecutorPool until the pool
// reaches READY or PROVISIONING_FAILED, then returns the pool. This is needed
// for SQS-channel pools, which start at PROVISIONING_PENDING and transition
// asynchronously. KAFKA-channel pools start READY immediately so the first poll
// returns without waiting.
//
// If maxWait is zero, a 5-minute default is used. The poll interval is 5 s.
// Returns an error if provisioning fails or the timeout is exceeded.
func (c *Client) WaitForRemoteExecutorPoolReady(ctx context.Context, urn string, maxWait time.Duration) (*RemoteExecutorPool, error) {
	if maxWait == 0 {
		maxWait = 5 * time.Minute
	}
	deadline := time.Now().Add(maxWait)
	for {
		pool, err := c.GetRemoteExecutorPoolByURN(ctx, urn)
		if err != nil {
			return nil, err
		}
		if pool == nil {
			return nil, fmt.Errorf("pool %s disappeared while waiting for READY state", urn)
		}
		switch pool.StateStatus {
		case "READY", "":
			return pool, nil
		case "PROVISIONING_FAILED":
			msg := pool.StateMsg
			if msg == "" {
				msg = "provisioning failed (no detail available)"
			}
			return nil, fmt.Errorf("executor pool provisioning failed: %s", msg)
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf(
				"executor pool %s did not reach READY within %s (current state: %s)",
				urn, maxWait, pool.StateStatus,
			)
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("context cancelled while waiting for pool READY: %w", ctx.Err())
		case <-time.After(5 * time.Second):
		}
	}
}
