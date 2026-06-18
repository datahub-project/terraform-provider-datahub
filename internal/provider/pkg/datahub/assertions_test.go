// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahub

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// assertionEntityServer serves the given assertion entity JSON body for any GET
// to /openapi/v3/entity/assertion/{urn} and 404s everything else.
func assertionEntityServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/openapi/v3/entity/assertion/") {
			_, _ = w.Write([]byte(body))
			return
		}
		http.NotFound(w, r)
	}))
}

// TestGetAssertionByURN_VolumeRowCountChange verifies the read parse of a NATIVE
// ROW_COUNT_CHANGE volume assertion. The body matches the shape verified live
// against DataHub Cloud (see docs/design/volume-row-count-change-build.md): the
// threshold lives under volumeAssertion.rowCountChange, carries an ABSOLUTE /
// PERCENTAGE change type, and reuses the AssertionStdParameters value shape.
func TestGetAssertionByURN_VolumeRowCountChange(t *testing.T) {
	const urn = "urn:li:assertion:vol-change"
	body := `{
	  "urn": "` + urn + `",
	  "assertionInfo": { "value": {
	    "type": "VOLUME",
	    "source": { "type": "NATIVE" },
	    "entityUrn": "urn:li:dataset:(urn:li:dataPlatform:bigquery,db.t,PROD)",
	    "volumeAssertion": {
	      "type": "ROW_COUNT_CHANGE",
	      "rowCountChange": {
	        "type": "ABSOLUTE",
	        "operator": "GREATER_THAN_OR_EQUAL_TO",
	        "parameters": { "value": { "type": "NUMBER", "value": "10" } }
	      }
	    }
	  } }
	}`
	server := assertionEntityServer(t, body)
	defer server.Close()

	ai, err := newTestClient(t, server).GetAssertionByURN(t.Context(), urn)
	if err != nil {
		t.Fatalf("GetAssertionByURN() error = %v", err)
	}
	if ai == nil || ai.Volume == nil {
		t.Fatal("GetAssertionByURN() returned no volume assertion")
	}
	if ai.Source != "NATIVE" {
		t.Errorf("Source = %q, want NATIVE", ai.Source)
	}
	if ai.Volume.VolumeType != "ROW_COUNT_CHANGE" {
		t.Errorf("VolumeType = %q, want ROW_COUNT_CHANGE", ai.Volume.VolumeType)
	}
	if ai.Volume.ChangeType != "ABSOLUTE" {
		t.Errorf("ChangeType = %q, want ABSOLUTE", ai.Volume.ChangeType)
	}
	if ai.Volume.Operator != "GREATER_THAN_OR_EQUAL_TO" {
		t.Errorf("Operator = %q, want GREATER_THAN_OR_EQUAL_TO", ai.Volume.Operator)
	}
	if ai.Volume.Value != "10" {
		t.Errorf("Value = %q, want 10", ai.Volume.Value)
	}
}

// TestGetAssertionByURN_SQLMetricChange verifies the read parse of a NATIVE
// METRIC_CHANGE sql assertion. The body matches the shape verified live against
// DataHub Cloud: sqlAssertion carries a changeType sibling (ABSOLUTE/PERCENTAGE)
// alongside type/statement/operator, with the description at the top level.
func TestGetAssertionByURN_SQLMetricChange(t *testing.T) {
	const urn = "urn:li:assertion:sql-change"
	body := `{
	  "urn": "` + urn + `",
	  "assertionInfo": { "value": {
	    "type": "SQL",
	    "source": { "type": "NATIVE" },
	    "entityUrn": "urn:li:dataset:(urn:li:dataPlatform:sqlite,db.t,PROD)",
	    "description": "metric must not drop",
	    "sqlAssertion": {
	      "type": "METRIC_CHANGE",
	      "changeType": "ABSOLUTE",
	      "statement": "SELECT COUNT(*) FROM t",
	      "operator": "GREATER_THAN",
	      "parameters": { "value": { "type": "NUMBER", "value": "5" } }
	    }
	  } }
	}`
	server := assertionEntityServer(t, body)
	defer server.Close()

	ai, err := newTestClient(t, server).GetAssertionByURN(t.Context(), urn)
	if err != nil {
		t.Fatalf("GetAssertionByURN() error = %v", err)
	}
	if ai == nil || ai.SQL == nil {
		t.Fatal("GetAssertionByURN() returned no sql assertion")
	}
	if ai.SQL.SQLType != "METRIC_CHANGE" {
		t.Errorf("SQLType = %q, want METRIC_CHANGE", ai.SQL.SQLType)
	}
	if ai.SQL.ChangeType != "ABSOLUTE" {
		t.Errorf("ChangeType = %q, want ABSOLUTE", ai.SQL.ChangeType)
	}
	if ai.SQL.Operator != "GREATER_THAN" {
		t.Errorf("Operator = %q, want GREATER_THAN", ai.SQL.Operator)
	}
	if ai.SQL.Value != "5" {
		t.Errorf("Value = %q, want 5", ai.SQL.Value)
	}
	if ai.SQL.Description != "metric must not drop" {
		t.Errorf("Description = %q, want %q", ai.SQL.Description, "metric must not drop")
	}
}

// TestGetAssertionByURN_FreshnessSinceLastCheck verifies the read parse of a
// NATIVE SINCE_THE_LAST_CHECK freshness assertion: the schedule carries only a
// type with no fixedInterval or cron sub-object (verified live against DataHub
// Cloud), so the fixed-interval and cron fields stay empty.
func TestGetAssertionByURN_FreshnessSinceLastCheck(t *testing.T) {
	const urn = "urn:li:assertion:fresh-stlc"
	body := `{
	  "urn": "` + urn + `",
	  "assertionInfo": { "value": {
	    "type": "FRESHNESS",
	    "source": { "type": "NATIVE" },
	    "entityUrn": "urn:li:dataset:(urn:li:dataPlatform:sqlite,db.t,PROD)",
	    "freshnessAssertion": {
	      "type": "DATASET_CHANGE",
	      "schedule": { "type": "SINCE_THE_LAST_CHECK" }
	    }
	  } }
	}`
	server := assertionEntityServer(t, body)
	defer server.Close()

	ai, err := newTestClient(t, server).GetAssertionByURN(t.Context(), urn)
	if err != nil {
		t.Fatalf("GetAssertionByURN() error = %v", err)
	}
	if ai == nil || ai.Freshness == nil {
		t.Fatal("GetAssertionByURN() returned no freshness assertion")
	}
	if ai.Freshness.ScheduleType != "SINCE_THE_LAST_CHECK" {
		t.Errorf("ScheduleType = %q, want SINCE_THE_LAST_CHECK", ai.Freshness.ScheduleType)
	}
	if ai.Freshness.FixedIntervalUnit != "" || ai.Freshness.FixedIntervalMultiple != 0 {
		t.Errorf("fixed-interval fields = %q/%d, want empty", ai.Freshness.FixedIntervalUnit, ai.Freshness.FixedIntervalMultiple)
	}
	if ai.Freshness.CronSchedule != "" || ai.Freshness.CronTimezone != "" {
		t.Errorf("cron fields = %q/%q, want empty", ai.Freshness.CronSchedule, ai.Freshness.CronTimezone)
	}
}

// TestGetAssertionByURN_Schema verifies the read parse of a NATIVE schema
// assertion, including the class->std type mapping: on read the field std type
// is a SchemaFieldDataType class object (verified live), not the plain string
// sent on write, so NumberType must map back to NUMBER and StringType to STRING.
func TestGetAssertionByURN_Schema(t *testing.T) {
	const urn = "urn:li:assertion:schema-1"
	body := `{
	  "urn": "` + urn + `",
	  "assertionInfo": { "value": {
	    "type": "DATA_SCHEMA",
	    "source": { "type": "NATIVE" },
	    "entityUrn": "urn:li:dataset:(urn:li:dataPlatform:sqlite,db.t,PROD)",
	    "schemaAssertion": {
	      "compatibility": "SUPERSET",
	      "schema": { "fields": [
	        { "fieldPath": "id", "nativeDataType": "INTEGER", "type": { "type": { "com.linkedin.schema.NumberType": {} } } },
	        { "fieldPath": "email", "nativeDataType": "VARCHAR", "type": { "type": { "com.linkedin.schema.StringType": {} } } }
	      ] }
	    }
	  } }
	}`
	server := assertionEntityServer(t, body)
	defer server.Close()

	ai, err := newTestClient(t, server).GetAssertionByURN(t.Context(), urn)
	if err != nil {
		t.Fatalf("GetAssertionByURN() error = %v", err)
	}
	if ai == nil || ai.Schema == nil {
		t.Fatal("GetAssertionByURN() returned no schema assertion")
	}
	if ai.Schema.Compatibility != "SUPERSET" {
		t.Errorf("Compatibility = %q, want SUPERSET", ai.Schema.Compatibility)
	}
	if len(ai.Schema.Fields) != 2 {
		t.Fatalf("got %d fields, want 2", len(ai.Schema.Fields))
	}
	if ai.Schema.Fields[0].Path != "id" || ai.Schema.Fields[0].StdType != "NUMBER" || ai.Schema.Fields[0].NativeType != "INTEGER" {
		t.Errorf("field[0] = %+v, want {id NUMBER INTEGER}", ai.Schema.Fields[0])
	}
	if ai.Schema.Fields[1].StdType != "STRING" {
		t.Errorf("field[1] StdType = %q, want STRING", ai.Schema.Fields[1].StdType)
	}
}

// TestGetAssertionByURN_FieldMetric verifies the read parse of a NATIVE
// FIELD_METRIC assertion: the field spec round-trips as {path,type,nativeType}.
func TestGetAssertionByURN_FieldMetric(t *testing.T) {
	const urn = "urn:li:assertion:field-1"
	body := `{
	  "urn": "` + urn + `",
	  "assertionInfo": { "value": {
	    "type": "FIELD",
	    "source": { "type": "NATIVE" },
	    "entityUrn": "urn:li:dataset:(urn:li:dataPlatform:sqlite,db.t,PROD)",
	    "fieldAssertion": {
	      "type": "FIELD_METRIC",
	      "fieldMetricAssertion": {
	        "field": { "path": "id", "type": "NUMBER", "nativeType": "INTEGER" },
	        "metric": "NULL_COUNT",
	        "operator": "EQUAL_TO",
	        "parameters": { "value": { "type": "NUMBER", "value": "0" } },
	        "failureSeverityConfig": { "rules": [], "defaultSeverity": "HIGH" }
	      }
	    }
	  } }
	}`
	server := assertionEntityServer(t, body)
	defer server.Close()

	ai, err := newTestClient(t, server).GetAssertionByURN(t.Context(), urn)
	if err != nil {
		t.Fatalf("GetAssertionByURN() error = %v", err)
	}
	if ai == nil || ai.Field == nil {
		t.Fatal("GetAssertionByURN() returned no field assertion")
	}
	f := ai.Field
	if f.FieldType != "FIELD_METRIC" || f.FieldPath != "id" || f.StdType != "NUMBER" {
		t.Errorf("field = %+v, want FIELD_METRIC/id/NUMBER", f)
	}
	if f.Metric != "NULL_COUNT" || f.Operator != "EQUAL_TO" || f.Value != "0" {
		t.Errorf("metric/op/value = %q/%q/%q, want NULL_COUNT/EQUAL_TO/0", f.Metric, f.Operator, f.Value)
	}
	if f.FailureSeverity != "HIGH" {
		t.Errorf("FailureSeverity = %q, want HIGH (field failureSeverityConfig must round-trip)", f.FailureSeverity)
	}
}

// TestGetAssertionByURN_FieldValues verifies the read parse of a NATIVE
// FIELD_VALUES assertion, including failThreshold and excludeNulls.
func TestGetAssertionByURN_FieldValues(t *testing.T) {
	const urn = "urn:li:assertion:field-2"
	body := `{
	  "urn": "` + urn + `",
	  "assertionInfo": { "value": {
	    "type": "FIELD",
	    "source": { "type": "NATIVE" },
	    "entityUrn": "urn:li:dataset:(urn:li:dataPlatform:bigquery,db.t,PROD)",
	    "fieldAssertion": {
	      "type": "FIELD_VALUES",
	      "fieldValuesAssertion": {
	        "field": { "path": "id", "type": "NUMBER", "nativeType": "INTEGER" },
	        "operator": "GREATER_THAN_OR_EQUAL_TO",
	        "parameters": { "value": { "type": "NUMBER", "value": "0" } },
	        "failThreshold": { "type": "COUNT", "value": 0 },
	        "excludeNulls": true
	      }
	    }
	  } }
	}`
	server := assertionEntityServer(t, body)
	defer server.Close()

	ai, err := newTestClient(t, server).GetAssertionByURN(t.Context(), urn)
	if err != nil {
		t.Fatalf("GetAssertionByURN() error = %v", err)
	}
	if ai == nil || ai.Field == nil {
		t.Fatal("GetAssertionByURN() returned no field assertion")
	}
	f := ai.Field
	if f.FieldType != "FIELD_VALUES" || f.Operator != "GREATER_THAN_OR_EQUAL_TO" || f.Value != "0" {
		t.Errorf("field = %+v, want FIELD_VALUES/GTE/0", f)
	}
	if f.FailThreshold != "COUNT" || f.FailThresholdN != 0 || !f.ExcludeNulls || !f.HasExcludeNull {
		t.Errorf("threshold/excludeNulls = %q/%d/%v/%v, want COUNT/0/true/true", f.FailThreshold, f.FailThresholdN, f.ExcludeNulls, f.HasExcludeNull)
	}
}

// TestGetAssertionByURN_VolumeRowCountChangeBetween verifies the BETWEEN variant
// of a ROW_COUNT_CHANGE assertion parses into min/max rather than a single value.
func TestGetAssertionByURN_VolumeRowCountChangeBetween(t *testing.T) {
	const urn = "urn:li:assertion:vol-change-between"
	body := `{
	  "urn": "` + urn + `",
	  "assertionInfo": { "value": {
	    "type": "VOLUME",
	    "source": { "type": "NATIVE" },
	    "entityUrn": "urn:li:dataset:(urn:li:dataPlatform:bigquery,db.t,PROD)",
	    "volumeAssertion": {
	      "type": "ROW_COUNT_CHANGE",
	      "rowCountChange": {
	        "type": "PERCENTAGE",
	        "operator": "BETWEEN",
	        "parameters": {
	          "minValue": { "type": "NUMBER", "value": "5" },
	          "maxValue": { "type": "NUMBER", "value": "25" }
	        }
	      }
	    }
	  } }
	}`
	server := assertionEntityServer(t, body)
	defer server.Close()

	ai, err := newTestClient(t, server).GetAssertionByURN(t.Context(), urn)
	if err != nil {
		t.Fatalf("GetAssertionByURN() error = %v", err)
	}
	if ai == nil || ai.Volume == nil {
		t.Fatal("GetAssertionByURN() returned no volume assertion")
	}
	if ai.Volume.ChangeType != "PERCENTAGE" {
		t.Errorf("ChangeType = %q, want PERCENTAGE", ai.Volume.ChangeType)
	}
	if ai.Volume.Operator != "BETWEEN" {
		t.Errorf("Operator = %q, want BETWEEN", ai.Volume.Operator)
	}
	if ai.Volume.MinValue != "5" || ai.Volume.MaxValue != "25" {
		t.Errorf("Min/Max = %q/%q, want 5/25", ai.Volume.MinValue, ai.Volume.MaxValue)
	}
	if ai.Volume.Value != "" {
		t.Errorf("Value = %q, want empty for BETWEEN", ai.Volume.Value)
	}
}
