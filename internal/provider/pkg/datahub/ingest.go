// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahub

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// IngestionSource is the OpenAPI entity payload used by DataHub for ingestion sources.
// It is used for both POST (create/update) and GET (read) operations.
type IngestionSource struct {
	Urn                        string                    `json:"urn"`
	DataHubIngestionSourceKey  ingestionSourceKeyAspect  `json:"dataHubIngestionSourceKey,omitempty"`
	DataHubIngestionSourceInfo ingestionSourceInfoAspect `json:"dataHubIngestionSourceInfo,omitempty"`
}
type ingestionSourceKeyAspect struct {
	Value ingestionSourceKey `json:"value"`
}

type ingestionSourceKey struct {
	ID string `json:"id"`
}

type ingestionSourceInfoAspect struct {
	Value ingestionSourceInfo `json:"value"`
}

type ingestionSourceInfo struct {
	Name     string                   `json:"name"`
	Type     string                   `json:"type"`
	Schedule *ingestionSourceSchedule `json:"schedule,omitempty"`
	Config   ingestionSourceConfig    `json:"config"`
}

type ingestionSourceSchedule struct {
	Interval string `json:"interval"`
	Timezone string `json:"timezone"`
}

type ingestionSourceConfig struct {
	Recipe     string            `json:"recipe"`
	Version    string            `json:"version,omitempty"`
	ExecutorID string            `json:"executorId,omitempty"`
	ExtraArgs  map[string]string `json:"extraArgs,omitempty"`
	DebugMode  *bool             `json:"debugMode,omitempty"`
}

// DatasourceIngestionInput groups the inputs used to create or update a Datahub
// ingestion source via the OpenAPI endpoint.
type DatasourceIngestionInput struct {
	SourceID   string
	SourceName string
	SourceType string
	// ExtraArgs sets `config.extraArgs` when non-empty.
	ExtraArgs map[string]string
	// ExecutorID sets `config.executorId` when non-nil and non-empty.
	ExecutorID *string
	// CronInterval sets `schedule.interval` when non-nil and non-empty.
	CronInterval *string
	// Timezone sets `schedule.timezone` when non-nil and non-empty. If CronInterval is set but timezone is nil/empty, UTC is used.
	Timezone *string
	// CLIVersion sets `config.version` when non-nil and non-empty.
	CLIVersion *string
	// RecipeJSON is the ingestion source recipe JSON string stored in `config.recipe`.
	RecipeJSON *string
	Async      *bool // defaults to false if nil
}

// Datahub endpoint used: POST /openapi/v3/entity/datahubingestionsource.
func (c *Client) NewDatasourceIngestion(ctx context.Context, in DatasourceIngestionInput) ([]byte, error) {
	if c == nil {
		return nil, errors.New("client is nil")
	}

	in.SourceID = strings.TrimSpace(in.SourceID)
	if in.SourceID == "" {
		return nil, errors.New("sourceID is required")
	}
	in.SourceName = strings.TrimSpace(in.SourceName)
	if in.SourceName == "" {
		return nil, errors.New("sourceName is required")
	}

	in.SourceType = strings.TrimSpace(in.SourceType)
	if in.SourceType == "" {
		return nil, errors.New("sourceType is required")
	}

	if in.RecipeJSON == nil {
		return nil, errors.New("recipeJSON is required")
	}
	recipeJSON := strings.TrimSpace(*in.RecipeJSON)
	if recipeJSON == "" {
		return nil, errors.New("recipeJSON is empty")
	}

	async := false
	if in.Async != nil {
		async = *in.Async
	}

	payload := []IngestionSource{
		{
			Urn: fmt.Sprintf("urn:li:dataHubIngestionSource:%s", in.SourceID),
			DataHubIngestionSourceKey: ingestionSourceKeyAspect{
				Value: ingestionSourceKey{ID: in.SourceID},
			},
			DataHubIngestionSourceInfo: ingestionSourceInfoAspect{
				Value: ingestionSourceInfo{
					Name: in.SourceName,
					Type: in.SourceType,
					Config: ingestionSourceConfig{
						Recipe: recipeJSON,
					},
				},
			},
		},
	}

	if len(in.ExtraArgs) > 0 {
		payload[0].DataHubIngestionSourceInfo.Value.Config.ExtraArgs = in.ExtraArgs
	}

	if in.CronInterval != nil {
		cronInterval := strings.TrimSpace(*in.CronInterval)
		if cronInterval != "" {
			timezone := "UTC"
			if in.Timezone != nil {
				tz := strings.TrimSpace(*in.Timezone)
				if tz != "" {
					timezone = tz
				}
			}
			payload[0].DataHubIngestionSourceInfo.Value.Schedule = &ingestionSourceSchedule{
				Interval: cronInterval,
				Timezone: timezone,
			}
		}
	}

	if in.CLIVersion != nil {
		cliVersion := strings.TrimSpace(*in.CLIVersion)
		if cliVersion != "" {
			payload[0].DataHubIngestionSourceInfo.Value.Config.Version = cliVersion
		}
	}

	if in.ExecutorID != nil {
		exec := strings.TrimSpace(*in.ExecutorID)
		if exec != "" {
			payload[0].DataHubIngestionSourceInfo.Value.Config.ExecutorID = exec
		}
	}

	path := fmt.Sprintf("/openapi/v3/entity/datahubingestionsource?async=%t", async)
	req, err := c.NewRequest(ctx, http.MethodPost, path, payload)
	if err != nil {
		return nil, err
	}

	res, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	respBody, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}
	if res.StatusCode >= http.StatusBadRequest {
		return nil, fmt.Errorf("unexpected status %s: %s", res.Status, respBody)
	}

	return respBody, nil
}

// Datahub endpoint used: GET /openapi/v3/entity/datahubingestionsource/{urn}.
func (c *Client) GetIngestionSourceByID(ctx context.Context, sourceID string) ([]byte, error) {
	urn := fmt.Sprintf("urn:li:dataHubIngestionSource:%s", sourceID)
	path := fmt.Sprintf("/openapi/v3/entity/datahubingestionsource/%s", urn)
	req, err := c.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	res, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	respBody, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}
	if res.StatusCode >= http.StatusBadRequest {
		return nil, fmt.Errorf("unexpected status %s: %s", res.Status, respBody)
	}

	return respBody, nil
}

// Datahub endpoint used: DELETE /openapi/v3/entity/datahubingestionsource/{urn}.
func (c *Client) DeleteIngestionSourceByID(ctx context.Context, sourceID string) error {
	urn := fmt.Sprintf("urn:li:dataHubIngestionSource:%s", sourceID)
	path := fmt.Sprintf("/openapi/v3/entity/datahubingestionsource/%s", urn)
	req, err := c.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return err
	}

	res, err := c.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode >= http.StatusBadRequest {
		respBody, _ := io.ReadAll(res.Body)
		return fmt.Errorf("unexpected status %s: %s", res.Status, respBody)
	}

	return nil
}
