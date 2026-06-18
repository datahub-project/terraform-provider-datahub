// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

// Assertion management for DataHub.
//
// Four assertion types are supported:
//   - Custom assertions (OSS + Cloud): upsertCustomAssertion / deleteAssertion
//   - Freshness monitors (Cloud-only): upsertDatasetFreshnessAssertionMonitor
//   - Volume monitors (Cloud-only): upsertDatasetVolumeAssertionMonitor
//   - SQL monitors (Cloud-only): upsertDatasetSqlAssertionMonitor
//
// All assertion types share the same URN format (urn:li:assertion:<uuid>) and
// the same read path (GET /openapi/v3/entity/assertion/{urn}).
//
// The three monitor types require DataHub Cloud; they create a Monitor entity
// that does not exist in OSS DataHub. Callers receive ErrAssertionCloudOnly
// when the operation is attempted against an OSS instance.

package datahub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ErrAssertionCloudOnly is returned when a monitor mutation is attempted
// against an OSS DataHub instance that does not support the Monitor entity.
var ErrAssertionCloudOnly = errors.New(
	"this assertion type requires DataHub Cloud; " +
		"the configured GMS instance does not expose assertion monitor management",
)

// isAssertionCloudOnlyError returns true when the GraphQL error indicates the
// mutation or input type is absent from the OSS schema (cloud-only feature).
func isAssertionCloudOnlyError(msg string) bool {
	if strings.Contains(msg, "FieldUndefined") &&
		(strings.Contains(msg, "in type 'Mutation'") || strings.Contains(msg, "in type 'Query'")) {
		return true
	}
	if strings.Contains(msg, "UnknownType") &&
		(strings.Contains(msg, "Freshness") || strings.Contains(msg, "Volume") ||
			strings.Contains(msg, "SqlAssertion") || strings.Contains(msg, "MonitorMode") ||
			strings.Contains(msg, "Monitor")) {
		return true
	}
	return false
}

// AssertionInfo is the read-shape common to all assertion types.
type AssertionInfo struct {
	URN             string
	Type            string // FRESHNESS, VOLUME, SQL, CUSTOM
	Source          string // NATIVE, EXTERNAL, INFERRED (empty if absent)
	EntityURN       string
	Description     string // top-level assertionInfo.description (all monitor types)
	FilterSQL       string // row-level SQL filter (volume and freshness; empty if none)
	FailureSeverity string // defaultSeverity LOW/MEDIUM/HIGH (freshness and sql; field uses Field.FailureSeverity)
	// Type-specific sub-structs; only one is non-nil depending on Type.
	Freshness *FreshnessAssertionInfo
	Volume    *VolumeAssertionInfo
	SQL       *SQLAssertionInfo
	Field     *FieldAssertionInfo
	Schema    *SchemaAssertionInfo
	Custom    *CustomAssertionInfo
	// Actions
	OnSuccessActions []string
	OnFailureActions []string
}

type FreshnessAssertionInfo struct {
	ScheduleType          string // FIXED_INTERVAL or CRON
	FixedIntervalUnit     string // HOUR, DAY, WEEK, MONTH, YEAR
	FixedIntervalMultiple int64
	CronSchedule          string // cron expression for CRON type
	CronTimezone          string
}

type VolumeAssertionInfo struct {
	VolumeType string // ROW_COUNT_TOTAL, ROW_COUNT_CHANGE
	ChangeType string // ABSOLUTE or PERCENTAGE (ROW_COUNT_CHANGE only; empty otherwise)
	Operator   string // BETWEEN, GREATER_THAN, LESS_THAN, EQUAL_TO, etc.
	MinValue   string
	MaxValue   string
	Value      string // single value when not BETWEEN
}

type SQLAssertionInfo struct {
	SQLType     string // METRIC or METRIC_CHANGE
	ChangeType  string // ABSOLUTE or PERCENTAGE (METRIC_CHANGE only; empty otherwise)
	Statement   string
	Operator    string
	Value       string
	Description string
}

type FieldAssertionInfo struct {
	FieldType       string // FIELD_VALUES or FIELD_METRIC
	FieldPath       string // column path
	StdType         string // SchemaFieldSpec std type, e.g. NUMBER, STRING
	NativeType      string // platform-native column type
	Operator        string
	MinValue        string
	MaxValue        string
	Value           string
	FailureSeverity string // defaultSeverity LOW/MEDIUM/HIGH (empty if none)
	// FIELD_VALUES only
	TransformType  string // e.g. LENGTH (empty if no transform)
	FailThreshold  string // COUNT or PERCENTAGE (empty if unset)
	FailThresholdN int64  // threshold value
	ExcludeNulls   bool
	HasExcludeNull bool // whether excludeNulls was present in the read
	// FIELD_METRIC only
	Metric string // UNIQUE_COUNT, NULL_COUNT, MIN, MAX, ...
}

type SchemaAssertionField struct {
	Path       string
	StdType    string // NUMBER, STRING, BOOLEAN, ...
	NativeType string
}

type SchemaAssertionInfo struct {
	Compatibility string // EXACT_MATCH, SUPERSET, SUBSET
	Fields        []SchemaAssertionField
}

type CustomAssertionInfo struct {
	AssertionType string
	Description   string
	FieldPath     string
	PlatformURN   string
	ExternalURL   string
	Logic         string
}

// assertionEntity is the OpenAPI v3 response shape for
// GET /openapi/v3/entity/assertion/{urn}.
type assertionEntity struct {
	URN string `json:"urn"`
	Key *struct {
		Value struct {
			AssertionID string `json:"assertionId"`
		} `json:"value"`
	} `json:"assertionKey,omitempty"`
	Info *struct {
		Value assertionInfoValue `json:"value"`
	} `json:"assertionInfo,omitempty"`
	Actions *struct {
		Value struct {
			OnSuccess []struct {
				Type string `json:"type"`
			} `json:"onSuccess"`
			OnFailure []struct {
				Type string `json:"type"`
			} `json:"onFailure"`
		} `json:"value"`
	} `json:"assertionActions,omitempty"`
	// DataPlatformInstance is a separate aspect that carries the platform URN
	// for custom assertions (upsertCustomAssertion's platform input is stored here,
	// not inside assertionInfo.value.customAssertion).
	DataPlatformInstance *struct {
		Value struct {
			Platform string `json:"platform"`
		} `json:"value"`
	} `json:"dataPlatformInstance,omitempty"`
}

// assertionSeverityConfig is the read shape of AssertionFailureSeverityConfig.
// Only defaultSeverity is modeled; the conditional rules list is read but not
// surfaced. Nested inside the freshness and sql type objects.
type assertionSeverityConfig struct {
	DefaultSeverity string `json:"defaultSeverity"`
}

// assertionFilter is the read shape of a DatasetFilter: a row-level filter
// applied before the assertion evaluates. DatasetFilterType currently has only
// SQL, so only the sql clause is meaningful. Shared by volume and freshness.
type assertionFilter struct {
	Type string `json:"type"`
	SQL  string `json:"sql"`
}

// fieldSpec is the read shape of a SchemaFieldSpec as it appears inside a field
// assertion (fieldValuesAssertion.field / fieldMetricAssertion.field). Unlike the
// schema-assertion field list, here the std type round-trips as a plain string.
type fieldSpec struct {
	Path       string `json:"path"`
	Type       string `json:"type"`
	NativeType string `json:"nativeType"`
}

// assertionStdParameters is the read shape of AssertionStdParameters: a single
// value (most operators) or a min/max pair (BETWEEN). Shared by the volume
// rowCountTotal and rowCountChange variants.
type assertionStdParameters struct {
	Value *struct {
		Value string `json:"value"`
	} `json:"value,omitempty"`
	MinValue *struct {
		Value string `json:"value"`
	} `json:"minValue,omitempty"`
	MaxValue *struct {
		Value string `json:"value"`
	} `json:"maxValue,omitempty"`
}

type assertionInfoValue struct {
	Type   string `json:"type"`
	Source *struct {
		Type string `json:"type"`
	} `json:"source"`
	EntityURN string `json:"entityUrn"`
	// Description and ExternalURL are top-level fields in the real DataHub API response,
	// not nested inside customAssertion.
	Description string `json:"description"`
	ExternalURL string `json:"externalUrl"`
	// Freshness
	FreshnessAssertion *struct {
		Schedule *struct {
			Type          string `json:"type"`
			FixedInterval *struct {
				Unit     string `json:"unit"`
				Multiple int64  `json:"multiple"`
			} `json:"fixedInterval,omitempty"`
			Cron *struct {
				Cron     string `json:"cron"`
				Timezone string `json:"timezone"`
			} `json:"cron,omitempty"`
		} `json:"schedule,omitempty"`
		Filter                *assertionFilter         `json:"filter,omitempty"`
		FailureSeverityConfig *assertionSeverityConfig `json:"failureSeverityConfig,omitempty"`
	} `json:"freshnessAssertion,omitempty"`
	// Volume
	VolumeAssertion *struct {
		Type          string `json:"type"`
		RowCountTotal *struct {
			Operator   string                  `json:"operator"`
			Parameters *assertionStdParameters `json:"parameters,omitempty"`
		} `json:"rowCountTotal,omitempty"`
		RowCountChange *struct {
			Type       string                  `json:"type"` // ABSOLUTE or PERCENTAGE
			Operator   string                  `json:"operator"`
			Parameters *assertionStdParameters `json:"parameters,omitempty"`
		} `json:"rowCountChange,omitempty"`
		Filter *assertionFilter `json:"filter,omitempty"`
	} `json:"volumeAssertion,omitempty"`
	// SQL
	SQLAssertion *struct {
		Type        string `json:"type"`
		ChangeType  string `json:"changeType"` // ABSOLUTE or PERCENTAGE (METRIC_CHANGE only)
		Statement   string `json:"statement"`
		Operator    string `json:"operator"`
		Description string `json:"description"`
		Parameters  *struct {
			Value *struct {
				Value string `json:"value"`
			} `json:"value,omitempty"`
		} `json:"parameters,omitempty"`
		FailureSeverityConfig *assertionSeverityConfig `json:"failureSeverityConfig,omitempty"`
	} `json:"sqlAssertion,omitempty"`
	// Field (column) assertions
	FieldAssertion *struct {
		Type                 string `json:"type"` // FIELD_VALUES or FIELD_METRIC
		FieldValuesAssertion *struct {
			Field     *fieldSpec `json:"field"`
			Transform *struct {
				Type string `json:"type"`
			} `json:"transform,omitempty"`
			Operator      string                  `json:"operator"`
			Parameters    *assertionStdParameters `json:"parameters,omitempty"`
			FailThreshold *struct {
				Type  string `json:"type"`
				Value int64  `json:"value"`
			} `json:"failThreshold,omitempty"`
			ExcludeNulls          *bool                    `json:"excludeNulls,omitempty"`
			FailureSeverityConfig *assertionSeverityConfig `json:"failureSeverityConfig,omitempty"`
		} `json:"fieldValuesAssertion,omitempty"`
		FieldMetricAssertion *struct {
			Field                 *fieldSpec               `json:"field"`
			Metric                string                   `json:"metric"`
			Operator              string                   `json:"operator"`
			Parameters            *assertionStdParameters  `json:"parameters,omitempty"`
			FailureSeverityConfig *assertionSeverityConfig `json:"failureSeverityConfig,omitempty"`
		} `json:"fieldMetricAssertion,omitempty"`
	} `json:"fieldAssertion,omitempty"`
	// Schema assertions. On read the field std type comes back as a nested
	// SchemaFieldDataType class object (type.type.{"com.linkedin.schema.XType":{}}),
	// not the plain std string sent on write, so it needs class->std mapping.
	SchemaAssertion *struct {
		Compatibility string `json:"compatibility"`
		Schema        *struct {
			Fields []struct {
				FieldPath      string `json:"fieldPath"`
				NativeDataType string `json:"nativeDataType"`
				Type           struct {
					Type map[string]json.RawMessage `json:"type"`
				} `json:"type"`
			} `json:"fields"`
		} `json:"schema,omitempty"`
	} `json:"schemaAssertion,omitempty"`
	// Custom
	// Note: Description and ExternalURL are at the assertionInfoValue top level (above),
	// not inside customAssertion. Platform is in the dataPlatformInstance aspect.
	// FieldPath (fieldPath input) is stored as a full schema-field URN in customAssertion.field
	// and cannot be round-tripped safely to the simple field name the user supplies.
	CustomAssertion *struct {
		Type   string `json:"type"`
		Logic  string `json:"logic"`
		Entity string `json:"entity"` // OSS schema v3: entity URN stored here, not in top-level entityUrn
	} `json:"customAssertion,omitempty"`
}

// GetAssertionByURN fetches an assertion directly from the OpenAPI v3 entity
// endpoint (MySQL, strongly consistent). Returns nil (no error) on 404.
func (c *Client) GetAssertionByURN(ctx context.Context, urn string) (*AssertionInfo, error) {
	if c == nil {
		return nil, errors.New("client is nil")
	}
	urn = strings.TrimSpace(urn)
	if urn == "" {
		return nil, errors.New("URN is required")
	}

	path := fmt.Sprintf("/openapi/v3/entity/assertion/%s", urn)
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
		return nil, fmt.Errorf("DataHub rejected the request (HTTP %d): the calling principal needs the MANAGE_DATA_QUALITY privilege", res.StatusCode)
	}
	if res.StatusCode >= http.StatusBadRequest {
		body, _ := io.ReadAll(res.Body)
		return nil, fmt.Errorf("unexpected HTTP %d from DataHub assertion API: %s", res.StatusCode, body)
	}

	var entity assertionEntity
	if err := json.NewDecoder(res.Body).Decode(&entity); err != nil {
		return nil, fmt.Errorf("parsing assertion entity response: %w", err)
	}

	if entity.Key == nil && entity.Info == nil {
		return nil, nil
	}

	return toAssertionInfo(&entity), nil
}

func toAssertionInfo(e *assertionEntity) *AssertionInfo {
	ai := &AssertionInfo{URN: e.URN}

	if e.Info != nil {
		v := e.Info.Value
		ai.Type = v.Type
		if v.Source != nil {
			ai.Source = v.Source.Type
		}
		ai.EntityURN = v.EntityURN
		ai.Description = v.Description // top-level field, shared by all monitor types
		// OSS assertionInfo schema v3 stores the entity URN inside customAssertion.entity
		// rather than at the top-level entityUrn field. Fall back when entityUrn is absent.
		if ai.EntityURN == "" && v.CustomAssertion != nil {
			ai.EntityURN = v.CustomAssertion.Entity
		}

		if v.FreshnessAssertion != nil && v.FreshnessAssertion.Schedule != nil {
			fi := &FreshnessAssertionInfo{
				ScheduleType: v.FreshnessAssertion.Schedule.Type,
			}
			if v.FreshnessAssertion.Schedule.FixedInterval != nil {
				fi.FixedIntervalUnit = v.FreshnessAssertion.Schedule.FixedInterval.Unit
				fi.FixedIntervalMultiple = v.FreshnessAssertion.Schedule.FixedInterval.Multiple
			}
			if v.FreshnessAssertion.Schedule.Cron != nil {
				fi.CronSchedule = v.FreshnessAssertion.Schedule.Cron.Cron
				fi.CronTimezone = v.FreshnessAssertion.Schedule.Cron.Timezone
			}
			ai.Freshness = fi
			if v.FreshnessAssertion.Filter != nil {
				ai.FilterSQL = v.FreshnessAssertion.Filter.SQL
			}
			if v.FreshnessAssertion.FailureSeverityConfig != nil {
				ai.FailureSeverity = v.FreshnessAssertion.FailureSeverityConfig.DefaultSeverity
			}
		}

		if v.VolumeAssertion != nil {
			vi := &VolumeAssertionInfo{VolumeType: v.VolumeAssertion.Type}
			switch {
			case v.VolumeAssertion.RowCountTotal != nil:
				vi.Operator = v.VolumeAssertion.RowCountTotal.Operator
				applyStdParameters(vi, v.VolumeAssertion.RowCountTotal.Parameters)
			case v.VolumeAssertion.RowCountChange != nil:
				vi.ChangeType = v.VolumeAssertion.RowCountChange.Type
				vi.Operator = v.VolumeAssertion.RowCountChange.Operator
				applyStdParameters(vi, v.VolumeAssertion.RowCountChange.Parameters)
			}
			ai.Volume = vi
			if v.VolumeAssertion.Filter != nil {
				ai.FilterSQL = v.VolumeAssertion.Filter.SQL
			}
		}

		if v.SQLAssertion != nil {
			si := &SQLAssertionInfo{
				SQLType:     v.SQLAssertion.Type,
				ChangeType:  v.SQLAssertion.ChangeType,
				Statement:   v.SQLAssertion.Statement,
				Operator:    v.SQLAssertion.Operator,
				Description: v.Description, // top-level field in real API, same as custom assertions
			}
			if v.SQLAssertion.Parameters != nil && v.SQLAssertion.Parameters.Value != nil {
				si.Value = v.SQLAssertion.Parameters.Value.Value
			}
			if v.SQLAssertion.FailureSeverityConfig != nil {
				ai.FailureSeverity = v.SQLAssertion.FailureSeverityConfig.DefaultSeverity
			}
			ai.SQL = si
		}

		if v.FieldAssertion != nil {
			fi := &FieldAssertionInfo{FieldType: v.FieldAssertion.Type}
			if fv := v.FieldAssertion.FieldValuesAssertion; fv != nil {
				if fv.Field != nil {
					fi.FieldPath = fv.Field.Path
					fi.StdType = fv.Field.Type
					fi.NativeType = fv.Field.NativeType
				}
				fi.Operator = fv.Operator
				applyStdParametersField(fi, fv.Parameters)
				if fv.Transform != nil {
					fi.TransformType = fv.Transform.Type
				}
				if fv.FailThreshold != nil {
					fi.FailThreshold = fv.FailThreshold.Type
					fi.FailThresholdN = fv.FailThreshold.Value
				}
				if fv.ExcludeNulls != nil {
					fi.ExcludeNulls = *fv.ExcludeNulls
					fi.HasExcludeNull = true
				}
				if fv.FailureSeverityConfig != nil {
					fi.FailureSeverity = fv.FailureSeverityConfig.DefaultSeverity
				}
			}
			if fm := v.FieldAssertion.FieldMetricAssertion; fm != nil {
				if fm.Field != nil {
					fi.FieldPath = fm.Field.Path
					fi.StdType = fm.Field.Type
					fi.NativeType = fm.Field.NativeType
				}
				fi.Metric = fm.Metric
				fi.Operator = fm.Operator
				applyStdParametersField(fi, fm.Parameters)
				if fm.FailureSeverityConfig != nil {
					fi.FailureSeverity = fm.FailureSeverityConfig.DefaultSeverity
				}
			}
			ai.Field = fi
		}

		if v.SchemaAssertion != nil {
			si := &SchemaAssertionInfo{Compatibility: v.SchemaAssertion.Compatibility}
			if v.SchemaAssertion.Schema != nil {
				for _, f := range v.SchemaAssertion.Schema.Fields {
					si.Fields = append(si.Fields, SchemaAssertionField{
						Path:       f.FieldPath,
						StdType:    stdTypeFromSchemaClass(f.Type.Type),
						NativeType: f.NativeDataType,
					})
				}
			}
			ai.Schema = si
		}

		if v.CustomAssertion != nil {
			ci := &CustomAssertionInfo{
				AssertionType: v.CustomAssertion.Type,
				Description:   v.Description, // top-level field in real API
				ExternalURL:   v.ExternalURL, // top-level field in real API
				Logic:         v.CustomAssertion.Logic,
				// FieldPath: not read back -- API stores as full schema-field URN which
				// cannot be safely round-tripped to the simple field name in config.
				// The value is preserved from prior state by the resource's Read function.
			}
			if e.DataPlatformInstance != nil {
				ci.PlatformURN = e.DataPlatformInstance.Value.Platform
			}
			ai.Custom = ci
		}
	}

	if e.Actions != nil {
		for _, a := range e.Actions.Value.OnSuccess {
			ai.OnSuccessActions = append(ai.OnSuccessActions, a.Type)
		}
		for _, a := range e.Actions.Value.OnFailure {
			ai.OnFailureActions = append(ai.OnFailureActions, a.Type)
		}
	}

	return ai
}

// DeleteAssertion deletes a DataHub assertion by URN. Works on OSS and Cloud.
func (c *Client) DeleteAssertion(ctx context.Context, urn string) error {
	if c == nil {
		return errors.New("client is nil")
	}
	urn = strings.TrimSpace(urn)
	if urn == "" {
		return errors.New("URN is required")
	}

	const q = `
mutation deleteAssertion($urn: String!) {
  deleteAssertion(urn: $urn)
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
		// OSS DataHub rejects deleteAssertion for CUSTOM type. Fall back to the
		// OpenAPI v3 entity endpoint, which works on both OSS and Cloud.
		if strings.Contains(gqlResp.Errors[0].Message, "Unsupported Assertion Type") {
			return c.deleteAssertionEntity(ctx, urn)
		}
		return fmt.Errorf("DataHub API error: %s", gqlResp.Errors[0].Message)
	}
	return nil
}

// deleteAssertionEntity hard-deletes an assertion via the OpenAPI v3 entity
// endpoint. Used as an OSS fallback when the GraphQL deleteAssertion mutation
// rejects the assertion type (e.g. CUSTOM on OSS DataHub).
func (c *Client) deleteAssertionEntity(ctx context.Context, urn string) error {
	path := fmt.Sprintf("/openapi/v3/entity/assertion/%s", urn)
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
	if res.StatusCode >= http.StatusBadRequest {
		body, _ := io.ReadAll(res.Body)
		return fmt.Errorf("unexpected HTTP %d deleting assertion: %s", res.StatusCode, body)
	}
	return nil
}

// DeleteCloudAssertionWithMonitor deletes a Cloud-only monitor-backed assertion
// and its associated monitor entity. The assertion deletion is authoritative:
// if it fails, the error is returned. The monitor deletion is best-effort: if
// the monitor lookup or deletion fails, the error is discarded (the monitor
// becomes an orphan but the assertion resource is removed from Terraform state).
//
// DataHub's deleteAssertion mutation removes the assertion entity but leaves
// the monitor entity in place. Without also deleting the monitor, DataHub Cloud
// enforces a one-active-monitor-per-dataset-per-type constraint that prevents
// future terraform applies from recreating the assertion for the same dataset.
func (c *Client) DeleteCloudAssertionWithMonitor(ctx context.Context, assertionURN string) error {
	monitorURN, _ := c.GetAssertionMonitorURN(ctx, assertionURN)
	if err := c.DeleteAssertion(ctx, assertionURN); err != nil {
		return err
	}
	if monitorURN != "" {
		_ = c.DeleteMonitor(ctx, monitorURN)
	}
	return nil
}

// GetAssertionMonitorURN looks up the monitor entity URN associated with the
// given assertion URN. Returns an empty string (no error) when the assertion
// has no linked monitor (e.g. custom assertions, or any assertion created before
// DataHub's monitor service was available). Returns an error only on network or
// parse failures.
func (c *Client) GetAssertionMonitorURN(ctx context.Context, assertionURN string) (string, error) {
	if c == nil {
		return "", errors.New("client is nil")
	}
	assertionURN = strings.TrimSpace(assertionURN)
	if assertionURN == "" {
		return "", errors.New("URN is required")
	}

	const q = `
query getAssertionMonitor($urn: String!) {
  assertion(urn: $urn) {
    monitor { urn }
  }
}`
	body := map[string]any{
		"query":     q,
		"variables": map[string]any{"urn": assertionURN},
	}
	var raw struct {
		Data struct {
			Assertion *struct {
				Monitor *struct {
					URN string `json:"urn"`
				} `json:"monitor"`
			} `json:"assertion"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := c.doGraphQL(ctx, body, &raw); err != nil {
		return "", err
	}
	if len(raw.Errors) > 0 {
		return "", fmt.Errorf("DataHub API error: %s", raw.Errors[0].Message)
	}
	if raw.Data.Assertion == nil || raw.Data.Assertion.Monitor == nil {
		return "", nil
	}
	return raw.Data.Assertion.Monitor.URN, nil
}

// DeleteMonitor hard-deletes a DataHub monitor entity by URN via the OpenAPI
// v3 entity endpoint. This is a separate step from DeleteAssertion because
// DataHub's deleteAssertion mutation removes the assertion entity but leaves
// the associated monitor entity in place.
func (c *Client) DeleteMonitor(ctx context.Context, monitorURN string) error {
	if c == nil {
		return errors.New("client is nil")
	}
	monitorURN = strings.TrimSpace(monitorURN)
	if monitorURN == "" {
		return errors.New("monitor URN is required")
	}

	path := fmt.Sprintf("/openapi/v3/entity/monitor/%s", monitorURN)
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
		body, _ := io.ReadAll(res.Body)
		return fmt.Errorf("unexpected HTTP %d deleting monitor %q: %s", res.StatusCode, monitorURN, body)
	}
	return nil
}

// waitForAssertionMonitor polls until the monitor entity linked to assertionURN
// is visible. DataHub Cloud creates the monitor asynchronously after the upsert
// mutation returns; without this wait an immediate update fails with "Monitor for
// assertion X does not exist." Poll interval is 500ms; the caller controls the
// timeout via ctx (30s is sufficient in practice).
func (c *Client) waitForAssertionMonitor(ctx context.Context, assertionURN string) error {
	for {
		monURN, err := c.GetAssertionMonitorURN(ctx, assertionURN)
		if err != nil {
			return fmt.Errorf("polling assertion monitor: %w", err)
		}
		if monURN != "" {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("assertion %q: monitor not visible within timeout; retry the apply if the instance is still processing", assertionURN)
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// UpsertCustomAssertionInput groups the inputs for upsertCustomAssertion.
type UpsertCustomAssertionInput struct {
	// ExistingURN is the assertion URN from prior state; empty on create.
	ExistingURN   string
	EntityURN     string
	AssertionType string
	Description   string
	FieldPath     string // optional
	PlatformURN   string
	ExternalURL   string // optional
	Logic         string // optional
}

// UpsertCustomAssertion creates or updates a custom (external) assertion.
// Pass an empty ExistingURN on first create; subsequent calls pass the stored URN.
// Works on both OSS DataHub and DataHub Cloud.
func (c *Client) UpsertCustomAssertion(ctx context.Context, in UpsertCustomAssertionInput) (string, error) {
	if c == nil {
		return "", errors.New("client is nil")
	}

	const q = `
mutation upsertCustomAssertion($urn: String, $input: UpsertCustomAssertionInput!) {
  upsertCustomAssertion(urn: $urn, input: $input) { urn }
}`

	input := map[string]any{
		"entityUrn":   in.EntityURN,
		"type":        in.AssertionType,
		"description": in.Description,
		"platform":    map[string]any{"urn": in.PlatformURN},
	}
	if in.FieldPath != "" {
		input["fieldPath"] = in.FieldPath
	}
	if in.ExternalURL != "" {
		input["externalUrl"] = in.ExternalURL
	}
	if in.Logic != "" {
		input["logic"] = in.Logic
	}

	vars := map[string]any{"input": input}
	if in.ExistingURN != "" {
		vars["urn"] = in.ExistingURN
	}

	body := map[string]any{"query": q, "variables": vars}

	var raw struct {
		Data struct {
			UpsertCustomAssertion struct {
				URN string `json:"urn"`
			} `json:"upsertCustomAssertion"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := c.doGraphQL(ctx, body, &raw); err != nil {
		return "", err
	}
	if len(raw.Errors) > 0 {
		return "", fmt.Errorf("DataHub API error: %s", raw.Errors[0].Message)
	}
	return raw.Data.UpsertCustomAssertion.URN, nil
}

// FreshnessAssertionInput groups inputs for upsertDatasetFreshnessAssertionMonitor.
type FreshnessAssertionInput struct {
	AssertionURN          string // empty on create
	EntityURN             string
	Description           string // optional
	FilterSQL             string // optional row-level SQL filter
	FailureSeverity       string // optional defaultSeverity LOW/MEDIUM/HIGH
	ScheduleType          string // FIXED_INTERVAL or CRON
	FixedIntervalUnit     string // HOUR, DAY, WEEK, MONTH, YEAR
	FixedIntervalMultiple int64
	CronSchedule          string // freshness window cron (for CRON type)
	CronTimezone          string
	EvaluationCron        string
	EvaluationTimezone    string
	SourceType            string // INFORMATION_SCHEMA, QUERY, DATAHUB_DATASET_PROFILE
	OnSuccessActions      []string
	OnFailureActions      []string
	Mode                  string // ACTIVE or PASSIVE
	ExecutorID            string // optional
}

// UpsertFreshnessAssertion creates or updates a freshness assertion monitor.
// Requires DataHub Cloud; returns ErrAssertionCloudOnly on OSS.
func (c *Client) UpsertFreshnessAssertion(ctx context.Context, in FreshnessAssertionInput) (string, error) {
	if c == nil {
		return "", errors.New("client is nil")
	}

	const q = `
mutation upsertDatasetFreshnessAssertionMonitor($assertionUrn: String, $input: UpsertDatasetFreshnessAssertionMonitorInput!) {
  upsertDatasetFreshnessAssertionMonitor(assertionUrn: $assertionUrn, input: $input) { urn }
}`

	schedule := map[string]any{"type": in.ScheduleType}
	switch in.ScheduleType {
	case "FIXED_INTERVAL":
		schedule["fixedInterval"] = map[string]any{
			"unit":     in.FixedIntervalUnit,
			"multiple": in.FixedIntervalMultiple,
		}
	case "CRON":
		schedule["cron"] = map[string]any{
			"cron":     in.CronSchedule,
			"timezone": in.CronTimezone,
		}
	}

	input := map[string]any{
		"entityUrn": in.EntityURN,
		"schedule":  schedule,
		"evaluationSchedule": map[string]any{
			"cron":     in.EvaluationCron,
			"timezone": in.EvaluationTimezone,
		},
		"evaluationParameters": map[string]any{
			"sourceType": in.SourceType,
		},
		"mode": in.Mode,
	}
	if in.Description != "" {
		input["description"] = in.Description
	}
	if in.FilterSQL != "" {
		input["filter"] = map[string]any{"type": "SQL", "sql": in.FilterSQL}
	}
	if in.FailureSeverity != "" {
		input["failureSeverityConfig"] = map[string]any{"defaultSeverity": in.FailureSeverity}
	}
	// Always send actions -- even empty lists -- so that previously-set actions
	// are cleared when the user removes them from config.
	input["actions"] = buildActionsInput(in.OnSuccessActions, in.OnFailureActions)
	if in.ExecutorID != "" {
		input["executorId"] = in.ExecutorID
	}

	vars := map[string]any{"input": input}
	if in.AssertionURN != "" {
		vars["assertionUrn"] = in.AssertionURN
	}

	body := map[string]any{"query": q, "variables": vars}

	var raw struct {
		Data struct {
			UpsertDatasetFreshnessAssertionMonitor struct {
				URN string `json:"urn"`
			} `json:"upsertDatasetFreshnessAssertionMonitor"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := c.doGraphQL(ctx, body, &raw); err != nil {
		return "", err
	}
	if len(raw.Errors) > 0 {
		msg := raw.Errors[0].Message
		if isAssertionCloudOnlyError(msg) {
			return "", ErrAssertionCloudOnly
		}
		return "", fmt.Errorf("DataHub API error: %s", msg)
	}
	assertionURN := raw.Data.UpsertDatasetFreshnessAssertionMonitor.URN
	if in.AssertionURN == "" {
		waitCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		if err := c.waitForAssertionMonitor(waitCtx, assertionURN); err != nil {
			return "", fmt.Errorf("freshness assertion created but monitor not ready: %w", err)
		}
	}
	return assertionURN, nil
}

// VolumeAssertionInput groups inputs for upsertDatasetVolumeAssertionMonitor.
type VolumeAssertionInput struct {
	AssertionURN       string
	EntityURN          string
	Description        string // optional
	FilterSQL          string // optional row-level SQL filter
	VolumeType         string // ROW_COUNT_TOTAL, ROW_COUNT_CHANGE
	ChangeType         string // ABSOLUTE or PERCENTAGE (required for ROW_COUNT_CHANGE)
	Operator           string // BETWEEN, GREATER_THAN, LESS_THAN, EQUAL_TO, etc.
	MinValue           string // for BETWEEN
	MaxValue           string // for BETWEEN
	SingleValue        string // for non-BETWEEN operators
	EvaluationCron     string
	EvaluationTimezone string
	SourceType         string
	OnSuccessActions   []string
	OnFailureActions   []string
	Mode               string
	ExecutorID         string
}

// UpsertVolumeAssertion creates or updates a volume assertion monitor.
// Requires DataHub Cloud; returns ErrAssertionCloudOnly on OSS.
func (c *Client) UpsertVolumeAssertion(ctx context.Context, in VolumeAssertionInput) (string, error) {
	if c == nil {
		return "", errors.New("client is nil")
	}

	const q = `
mutation upsertDatasetVolumeAssertionMonitor($assertionUrn: String, $input: UpsertDatasetVolumeAssertionMonitorInput!) {
  upsertDatasetVolumeAssertionMonitor(assertionUrn: $assertionUrn, input: $input) { urn }
}`

	params := buildAssertionParams(in.Operator, in.MinValue, in.MaxValue, in.SingleValue)

	input := map[string]any{
		"entityUrn": in.EntityURN,
		"type":      in.VolumeType,
		"evaluationSchedule": map[string]any{
			"cron":     in.EvaluationCron,
			"timezone": in.EvaluationTimezone,
		},
		"evaluationParameters": map[string]any{
			"sourceType": in.SourceType,
		},
		"mode": in.Mode,
	}
	if in.Description != "" {
		input["description"] = in.Description
	}
	if in.FilterSQL != "" {
		input["filter"] = map[string]any{"type": "SQL", "sql": in.FilterSQL}
	}
	// Route the threshold to the sub-type the user selected. ROW_COUNT_CHANGE
	// carries an extra change type (ABSOLUTE/PERCENTAGE); both variants reuse the
	// same AssertionStdParameters shape (single value, or min/max for BETWEEN).
	if in.VolumeType == "ROW_COUNT_CHANGE" {
		input["rowCountChange"] = map[string]any{
			"type":       in.ChangeType,
			"operator":   in.Operator,
			"parameters": params,
		}
	} else {
		input["rowCountTotal"] = map[string]any{
			"operator":   in.Operator,
			"parameters": params,
		}
	}
	// Always send actions -- even empty lists -- so that previously-set actions
	// are cleared when the user removes them from config.
	input["actions"] = buildActionsInput(in.OnSuccessActions, in.OnFailureActions)
	if in.ExecutorID != "" {
		input["executorId"] = in.ExecutorID
	}

	vars := map[string]any{"input": input}
	if in.AssertionURN != "" {
		vars["assertionUrn"] = in.AssertionURN
	}

	body := map[string]any{"query": q, "variables": vars}

	var raw struct {
		Data struct {
			UpsertDatasetVolumeAssertionMonitor struct {
				URN string `json:"urn"`
			} `json:"upsertDatasetVolumeAssertionMonitor"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := c.doGraphQL(ctx, body, &raw); err != nil {
		return "", err
	}
	if len(raw.Errors) > 0 {
		msg := raw.Errors[0].Message
		if isAssertionCloudOnlyError(msg) {
			return "", ErrAssertionCloudOnly
		}
		return "", fmt.Errorf("DataHub API error: %s", msg)
	}
	assertionURN := raw.Data.UpsertDatasetVolumeAssertionMonitor.URN
	if in.AssertionURN == "" {
		waitCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		if err := c.waitForAssertionMonitor(waitCtx, assertionURN); err != nil {
			return "", fmt.Errorf("volume assertion created but monitor not ready: %w", err)
		}
	}
	return assertionURN, nil
}

// SQLAssertionInput groups inputs for upsertDatasetSqlAssertionMonitor.
type SQLAssertionInput struct {
	AssertionURN       string
	EntityURN          string
	SQLType            string // METRIC or METRIC_CHANGE
	ChangeType         string // ABSOLUTE or PERCENTAGE (required for METRIC_CHANGE)
	FailureSeverity    string // optional defaultSeverity LOW/MEDIUM/HIGH
	Statement          string
	Operator           string
	Value              string // single result value to compare against
	Description        string
	EvaluationCron     string
	EvaluationTimezone string
	OnSuccessActions   []string
	OnFailureActions   []string
	Mode               string
	ExecutorID         string
}

// UpsertSQLAssertion creates or updates a SQL assertion monitor.
// Requires DataHub Cloud; returns ErrAssertionCloudOnly on OSS.
func (c *Client) UpsertSQLAssertion(ctx context.Context, in SQLAssertionInput) (string, error) {
	if c == nil {
		return "", errors.New("client is nil")
	}

	const q = `
mutation upsertDatasetSqlAssertionMonitor($assertionUrn: String, $input: UpsertDatasetSqlAssertionMonitorInput!) {
  upsertDatasetSqlAssertionMonitor(assertionUrn: $assertionUrn, input: $input) { urn }
}`

	input := map[string]any{
		"entityUrn": in.EntityURN,
		"type":      in.SQLType,
		"statement": in.Statement,
		"operator":  in.Operator,
		"parameters": map[string]any{
			"value": map[string]any{
				"value": in.Value,
				"type":  "NUMBER",
			},
		},
		"evaluationSchedule": map[string]any{
			"cron":     in.EvaluationCron,
			"timezone": in.EvaluationTimezone,
		},
		"mode": in.Mode,
	}
	// METRIC_CHANGE carries an additional change type (ABSOLUTE/PERCENTAGE) as a
	// top-level sibling of statement/operator/parameters.
	if in.ChangeType != "" {
		input["changeType"] = in.ChangeType
	}
	if in.FailureSeverity != "" {
		input["failureSeverityConfig"] = map[string]any{"defaultSeverity": in.FailureSeverity}
	}
	if in.Description != "" {
		input["description"] = in.Description
	}
	// Always send actions -- even empty lists -- so that previously-set actions
	// are cleared when the user removes them from config.
	input["actions"] = buildActionsInput(in.OnSuccessActions, in.OnFailureActions)
	if in.ExecutorID != "" {
		input["executorId"] = in.ExecutorID
	}

	vars := map[string]any{"input": input}
	if in.AssertionURN != "" {
		vars["assertionUrn"] = in.AssertionURN
	}

	body := map[string]any{"query": q, "variables": vars}

	var raw struct {
		Data struct {
			UpsertDatasetSQLAssertionMonitor struct {
				URN string `json:"urn"`
			} `json:"upsertDatasetSqlAssertionMonitor"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := c.doGraphQL(ctx, body, &raw); err != nil {
		return "", err
	}
	if len(raw.Errors) > 0 {
		msg := raw.Errors[0].Message
		if isAssertionCloudOnlyError(msg) {
			return "", ErrAssertionCloudOnly
		}
		return "", fmt.Errorf("DataHub API error: %s", msg)
	}
	assertionURN := raw.Data.UpsertDatasetSQLAssertionMonitor.URN
	if in.AssertionURN == "" {
		waitCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		if err := c.waitForAssertionMonitor(waitCtx, assertionURN); err != nil {
			return "", fmt.Errorf("sql assertion created but monitor not ready: %w", err)
		}
	}
	return assertionURN, nil
}

// SchemaFieldInput is one expected column in a schema assertion's field list.
type SchemaFieldInput struct {
	Path       string
	StdType    string // DataHub standard type, e.g. NUMBER, STRING, BOOLEAN
	NativeType string // platform-native type (optional)
}

// SchemaAssertionInput groups inputs for upsertDatasetSchemaAssertionMonitor.
type SchemaAssertionInput struct {
	AssertionURN       string
	EntityURN          string
	Description        string
	Compatibility      string // EXACT_MATCH, SUPERSET, SUBSET
	Fields             []SchemaFieldInput
	EvaluationCron     string
	EvaluationTimezone string
	OnSuccessActions   []string
	OnFailureActions   []string
	Mode               string
	ExecutorID         string
}

// UpsertSchemaAssertion creates or updates a schema assertion monitor.
// Requires DataHub Cloud; returns ErrAssertionCloudOnly on OSS.
func (c *Client) UpsertSchemaAssertion(ctx context.Context, in SchemaAssertionInput) (string, error) {
	if c == nil {
		return "", errors.New("client is nil")
	}

	const q = `
mutation upsertDatasetSchemaAssertionMonitor($assertionUrn: String, $input: UpsertDatasetSchemaAssertionMonitorInput!) {
  upsertDatasetSchemaAssertionMonitor(assertionUrn: $assertionUrn, input: $input) { urn }
}`

	fields := make([]map[string]any, len(in.Fields))
	for i, f := range in.Fields {
		m := map[string]any{"path": f.Path, "type": f.StdType}
		if f.NativeType != "" {
			m["nativeType"] = f.NativeType
		}
		fields[i] = m
	}

	input := map[string]any{
		"entityUrn": in.EntityURN,
		"assertion": map[string]any{
			"compatibility": in.Compatibility,
			"fields":        fields,
		},
		"evaluationSchedule": map[string]any{
			"cron":     in.EvaluationCron,
			"timezone": in.EvaluationTimezone,
		},
		"mode": in.Mode,
	}
	if in.Description != "" {
		input["description"] = in.Description
	}
	input["actions"] = buildActionsInput(in.OnSuccessActions, in.OnFailureActions)
	if in.ExecutorID != "" {
		input["executorId"] = in.ExecutorID
	}

	vars := map[string]any{"input": input}
	if in.AssertionURN != "" {
		vars["assertionUrn"] = in.AssertionURN
	}
	body := map[string]any{"query": q, "variables": vars}

	var raw struct {
		Data struct {
			UpsertDatasetSchemaAssertionMonitor struct {
				URN string `json:"urn"`
			} `json:"upsertDatasetSchemaAssertionMonitor"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := c.doGraphQL(ctx, body, &raw); err != nil {
		return "", err
	}
	if len(raw.Errors) > 0 {
		msg := raw.Errors[0].Message
		if isAssertionCloudOnlyError(msg) {
			return "", ErrAssertionCloudOnly
		}
		return "", fmt.Errorf("DataHub API error: %s", msg)
	}
	assertionURN := raw.Data.UpsertDatasetSchemaAssertionMonitor.URN
	if in.AssertionURN == "" {
		waitCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		if err := c.waitForAssertionMonitor(waitCtx, assertionURN); err != nil {
			return "", fmt.Errorf("schema assertion created but monitor not ready: %w", err)
		}
	}
	return assertionURN, nil
}

// FieldAssertionInput groups inputs for upsertDatasetFieldAssertionMonitor.
type FieldAssertionInput struct {
	AssertionURN       string
	EntityURN          string
	Description        string
	FieldType          string // FIELD_VALUES or FIELD_METRIC
	FieldPath          string
	StdType            string // SchemaFieldSpec std type (NUMBER, STRING, ...)
	NativeType         string
	Operator           string
	MinValue           string
	MaxValue           string
	SingleValue        string
	SourceType         string // DatasetFieldAssertionSourceType
	FilterSQL          string
	FailureSeverity    string
	EvaluationCron     string
	EvaluationTimezone string
	OnSuccessActions   []string
	OnFailureActions   []string
	Mode               string
	ExecutorID         string
	// FIELD_VALUES only
	TransformType  string // e.g. LENGTH
	FailThreshold  string // COUNT or PERCENTAGE
	FailThresholdN int64
	ExcludeNulls   bool
	// FIELD_METRIC only
	Metric string
}

// UpsertFieldAssertion creates or updates a field (column) assertion monitor.
// Requires DataHub Cloud; returns ErrAssertionCloudOnly on OSS.
func (c *Client) UpsertFieldAssertion(ctx context.Context, in FieldAssertionInput) (string, error) {
	if c == nil {
		return "", errors.New("client is nil")
	}

	const q = `
mutation upsertDatasetFieldAssertionMonitor($assertionUrn: String, $input: UpsertDatasetFieldAssertionMonitorInput!) {
  upsertDatasetFieldAssertionMonitor(assertionUrn: $assertionUrn, input: $input) { urn }
}`

	field := map[string]any{"path": in.FieldPath, "type": in.StdType}
	if in.NativeType != "" {
		field["nativeType"] = in.NativeType
	}
	params := buildAssertionParams(in.Operator, in.MinValue, in.MaxValue, in.SingleValue)

	input := map[string]any{
		"entityUrn": in.EntityURN,
		"type":      in.FieldType,
		"evaluationSchedule": map[string]any{
			"cron":     in.EvaluationCron,
			"timezone": in.EvaluationTimezone,
		},
		"evaluationParameters": map[string]any{
			"sourceType": in.SourceType,
		},
		"mode": in.Mode,
	}
	switch in.FieldType {
	case "FIELD_VALUES":
		fv := map[string]any{
			"field":        field,
			"operator":     in.Operator,
			"parameters":   params,
			"excludeNulls": in.ExcludeNulls,
		}
		if in.TransformType != "" {
			fv["transform"] = map[string]any{"type": in.TransformType}
		}
		if in.FailThreshold != "" {
			fv["failThreshold"] = map[string]any{"type": in.FailThreshold, "value": in.FailThresholdN}
		}
		if in.FailureSeverity != "" {
			fv["failureSeverityConfig"] = map[string]any{"defaultSeverity": in.FailureSeverity}
		}
		input["fieldValuesAssertion"] = fv
	case "FIELD_METRIC":
		fm := map[string]any{
			"field":      field,
			"metric":     in.Metric,
			"operator":   in.Operator,
			"parameters": params,
		}
		if in.FailureSeverity != "" {
			fm["failureSeverityConfig"] = map[string]any{"defaultSeverity": in.FailureSeverity}
		}
		input["fieldMetricAssertion"] = fm
	}
	if in.Description != "" {
		input["description"] = in.Description
	}
	if in.FilterSQL != "" {
		input["filter"] = map[string]any{"type": "SQL", "sql": in.FilterSQL}
	}
	input["actions"] = buildActionsInput(in.OnSuccessActions, in.OnFailureActions)
	if in.ExecutorID != "" {
		input["executorId"] = in.ExecutorID
	}

	vars := map[string]any{"input": input}
	if in.AssertionURN != "" {
		vars["assertionUrn"] = in.AssertionURN
	}
	body := map[string]any{"query": q, "variables": vars}

	var raw struct {
		Data struct {
			UpsertDatasetFieldAssertionMonitor struct {
				URN string `json:"urn"`
			} `json:"upsertDatasetFieldAssertionMonitor"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := c.doGraphQL(ctx, body, &raw); err != nil {
		return "", err
	}
	if len(raw.Errors) > 0 {
		msg := raw.Errors[0].Message
		if isAssertionCloudOnlyError(msg) {
			return "", ErrAssertionCloudOnly
		}
		return "", fmt.Errorf("DataHub API error: %s", msg)
	}
	assertionURN := raw.Data.UpsertDatasetFieldAssertionMonitor.URN
	if in.AssertionURN == "" {
		waitCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		if err := c.waitForAssertionMonitor(waitCtx, assertionURN); err != nil {
			return "", fmt.Errorf("field assertion created but monitor not ready: %w", err)
		}
	}
	return assertionURN, nil
}

// buildActionsInput converts string slices to the AssertionActionsInput shape.
func buildActionsInput(onSuccess, onFailure []string) map[string]any {
	success := make([]map[string]any, len(onSuccess))
	for i, t := range onSuccess {
		success[i] = map[string]any{"type": t}
	}
	failure := make([]map[string]any, len(onFailure))
	for i, t := range onFailure {
		failure[i] = map[string]any{"type": t}
	}
	return map[string]any{
		"onSuccess": success,
		"onFailure": failure,
	}
}

// applyStdParameters copies the read-shape AssertionStdParameters (single value
// or min/max pair) onto a VolumeAssertionInfo. Shared by the rowCountTotal and
// rowCountChange read paths.
func applyStdParameters(vi *VolumeAssertionInfo, p *assertionStdParameters) {
	if p == nil {
		return
	}
	if p.Value != nil {
		vi.Value = p.Value.Value
	}
	if p.MinValue != nil {
		vi.MinValue = p.MinValue.Value
	}
	if p.MaxValue != nil {
		vi.MaxValue = p.MaxValue.Value
	}
}

// applyStdParametersField copies the read-shape AssertionStdParameters onto a
// FieldAssertionInfo (single value, or min/max for BETWEEN).
func applyStdParametersField(fi *FieldAssertionInfo, p *assertionStdParameters) {
	if p == nil {
		return
	}
	if p.Value != nil {
		fi.Value = p.Value.Value
	}
	if p.MinValue != nil {
		fi.MinValue = p.MinValue.Value
	}
	if p.MaxValue != nil {
		fi.MaxValue = p.MaxValue.Value
	}
}

// schemaClassToStd maps the DataHub SchemaFieldDataType class (as it appears on
// read, e.g. "com.linkedin.schema.NumberType") to the standard type string the
// SchemaFieldSpecInput accepts on write (e.g. "NUMBER"). Covers the union
// members whose std name is not just the stripped class name.
var schemaClassToStd = map[string]string{
	"RecordType": "STRUCT",
}

// stdTypeFromSchemaClass extracts the single SchemaFieldDataType class key from a
// read-shape field type object and returns the matching write std type. For the
// common scalar types (NumberType->NUMBER, StringType->STRING, ...) it strips the
// "com.linkedin.schema." prefix and the "Type" suffix and uppercases.
func stdTypeFromSchemaClass(m map[string]json.RawMessage) string {
	for cls := range m {
		short := strings.TrimPrefix(cls, "com.linkedin.schema.")
		if std, ok := schemaClassToStd[short]; ok {
			return std
		}
		return strings.ToUpper(strings.TrimSuffix(short, "Type"))
	}
	return ""
}

// buildAssertionParams converts operator + value strings to AssertionStdParametersInput.
func buildAssertionParams(operator, minVal, maxVal, singleVal string) map[string]any {
	params := map[string]any{}
	if operator == "BETWEEN" {
		if minVal != "" {
			params["minValue"] = map[string]any{"value": minVal, "type": "NUMBER"}
		}
		if maxVal != "" {
			params["maxValue"] = map[string]any{"value": maxVal, "type": "NUMBER"}
		}
	} else if singleVal != "" {
		params["value"] = map[string]any{"value": singleVal, "type": "NUMBER"}
	}
	return params
}

// AssertionMonitorInfo holds the monitor-side configuration for a dataset
// assertion monitor. These fields (the evaluation schedule, source type, and
// monitoring mode) live in the separate Monitor entity rather than in the
// assertion entity, so they cannot be recovered from GetAssertionByURN alone.
// They are required to make ImportState of the Cloud monitor assertion
// resources (freshness, volume, sql) reconstruct a clean plan.
type AssertionMonitorInfo struct {
	MonitorURN         string
	EvaluationCron     string
	EvaluationTimezone string
	SourceType         string // INFORMATION_SCHEMA, QUERY, etc.; empty for SQL
	Mode               string // ACTIVE or PASSIVE
}

// monitorEntity is the OpenAPI v3 response shape for
// GET /openapi/v3/entity/monitor/{urn}.
type monitorEntity struct {
	URN  string `json:"urn"`
	Info *struct {
		Value struct {
			AssertionMonitor *struct {
				Assertions []struct {
					Assertion string `json:"assertion"`
					Schedule  *struct {
						Cron     string `json:"cron"`
						Timezone string `json:"timezone"`
					} `json:"schedule"`
					Parameters *struct {
						DatasetFreshnessParameters *struct {
							SourceType string `json:"sourceType"`
						} `json:"datasetFreshnessParameters"`
						DatasetVolumeParameters *struct {
							SourceType string `json:"sourceType"`
						} `json:"datasetVolumeParameters"`
						DatasetFieldParameters *struct {
							SourceType string `json:"sourceType"`
						} `json:"datasetFieldParameters"`
					} `json:"parameters"`
				} `json:"assertions"`
			} `json:"assertionMonitor"`
			Status *struct {
				Mode string `json:"mode"`
			} `json:"status"`
		} `json:"value"`
	} `json:"monitorInfo,omitempty"`
}

// GetAssertionMonitor returns the monitor-side configuration for a dataset
// assertion, or nil if the assertion has no associated monitor (custom and
// third-party assertions have none). It follows the assertion's incoming
// "Evaluates" relationship to the Monitor entity, then reads that entity from
// the strongly-consistent OpenAPI v3 endpoint and extracts the evaluation
// schedule, source type, and mode for the entry matching assertionURN.
func (c *Client) GetAssertionMonitor(ctx context.Context, assertionURN string) (*AssertionMonitorInfo, error) {
	if c == nil {
		return nil, errors.New("client is nil")
	}
	assertionURN = strings.TrimSpace(assertionURN)
	if assertionURN == "" {
		return nil, errors.New("assertion URN is required")
	}

	monitorURN, err := c.GetAssertionMonitorURN(ctx, assertionURN)
	if err != nil {
		return nil, err
	}
	if monitorURN == "" {
		return nil, nil // no monitor (e.g. custom assertion)
	}

	path := fmt.Sprintf("/openapi/v3/entity/monitor/%s", monitorURN)
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
		body, _ := io.ReadAll(res.Body)
		return nil, fmt.Errorf("unexpected HTTP %d from DataHub monitor API: %s", res.StatusCode, body)
	}

	var entity monitorEntity
	if err := json.NewDecoder(res.Body).Decode(&entity); err != nil {
		return nil, fmt.Errorf("parsing monitor entity response: %w", err)
	}
	if entity.Info == nil || entity.Info.Value.AssertionMonitor == nil {
		return nil, nil
	}

	out := &AssertionMonitorInfo{MonitorURN: monitorURN}
	if entity.Info.Value.Status != nil {
		out.Mode = entity.Info.Value.Status.Mode
	}
	for _, a := range entity.Info.Value.AssertionMonitor.Assertions {
		if a.Assertion != assertionURN {
			continue
		}
		if a.Schedule != nil {
			out.EvaluationCron = a.Schedule.Cron
			out.EvaluationTimezone = a.Schedule.Timezone
		}
		if a.Parameters != nil {
			switch {
			case a.Parameters.DatasetFreshnessParameters != nil:
				out.SourceType = a.Parameters.DatasetFreshnessParameters.SourceType
			case a.Parameters.DatasetVolumeParameters != nil:
				out.SourceType = a.Parameters.DatasetVolumeParameters.SourceType
			case a.Parameters.DatasetFieldParameters != nil:
				out.SourceType = a.Parameters.DatasetFieldParameters.SourceType
			}
		}
		break
	}
	return out, nil
}
