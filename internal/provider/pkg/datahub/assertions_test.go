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
