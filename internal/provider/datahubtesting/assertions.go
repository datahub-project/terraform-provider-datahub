// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahubtesting

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
)

// mockAssertion stores the in-memory state for a single assertion entity.
type mockAssertion struct {
	URN           string
	AssertionType string // CUSTOM, FRESHNESS, VOLUME, SQL, DATASET, ...
	Source        string // NATIVE, EXTERNAL, INFERRED (defaults to NATIVE on upsert)
	EntityURN     string
	Description   string // top-level assertionInfo.description (volume/freshness)
	// Type-specific params stored as raw maps for echo-back.
	CustomAssertion *map[string]any
	VolumeAssertion *map[string]any
	FreshnessAssert *map[string]any
	SQLAssertion    *map[string]any
	FieldAssertion  *map[string]any
	SchemaAssertion *map[string]any
	OnSuccess       []string
	OnFailure       []string
}

// assertionCounter generates sequential mock assertion IDs.
var assertionCounter uint64

func nextAssertionID() string {
	id := atomic.AddUint64(&assertionCounter, 1)
	return fmt.Sprintf("mock-assertion-%04d-0000-0000-000000000000", id)
}

// actionsToStrings converts a GraphQL actions list ([]any of {type: string}) to []string.
func actionsToStrings(raw any) []string {
	list, ok := raw.([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(list))
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		t, _ := m["type"].(string)
		if t != "" {
			result = append(result, t)
		}
	}
	return result
}

// handleUpsertCustomAssertion handles the upsertCustomAssertion GraphQL mutation.
func (s *mockServer) handleUpsertCustomAssertion(w http.ResponseWriter, variables map[string]any) {
	urnRaw, _ := variables["urn"].(string)
	input, _ := variables["input"].(map[string]any)

	entityURN, _ := input["entityUrn"].(string)
	assertionType, _ := input["type"].(string)
	description, _ := input["description"].(string)
	fieldPath, _ := input["fieldPath"].(string)
	externalURL, _ := input["externalUrl"].(string)
	logic, _ := input["logic"].(string)

	s.mu.Lock()
	defer s.mu.Unlock()

	urn := urnRaw
	if urn == "" {
		urn = "urn:li:assertion:" + nextAssertionID()
	}

	customParams := map[string]any{
		"type":        assertionType,
		"description": description,
		"fieldPath":   fieldPath,
		"externalUrl": externalURL,
		"logic":       logic,
	}
	if p, ok := input["platform"].(map[string]any); ok {
		customParams["platform"] = p
	}

	s.assertions[urn] = mockAssertion{
		URN:             urn,
		AssertionType:   "CUSTOM",
		Source:          "NATIVE",
		EntityURN:       entityURN,
		CustomAssertion: &customParams,
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{
			"upsertCustomAssertion": map[string]any{"urn": urn},
		},
	})
}

// handleUpsertVolumeAssertion handles the upsertDatasetVolumeAssertionMonitor mutation.
func (s *mockServer) handleUpsertVolumeAssertion(w http.ResponseWriter, variables map[string]any) {
	urnRaw, _ := variables["assertionUrn"].(string)
	input, _ := variables["input"].(map[string]any)

	entityURN, _ := input["entityUrn"].(string)
	volumeType, _ := input["type"].(string)
	description, _ := input["description"].(string)

	var onSuccess, onFailure []string
	if actions, ok := input["actions"].(map[string]any); ok {
		onSuccess = actionsToStrings(actions["onSuccess"])
		onFailure = actionsToStrings(actions["onFailure"])
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	urn := urnRaw
	if urn == "" {
		urn = "urn:li:assertion:" + nextAssertionID()
	}

	volumeParams := map[string]any{"type": volumeType}
	// Store whichever threshold variant the client sent (rowCountTotal for
	// ROW_COUNT_TOTAL, rowCountChange for ROW_COUNT_CHANGE).
	if rc, ok := input["rowCountChange"]; ok {
		volumeParams["rowCountChange"] = rc
	}
	if rc, ok := input["rowCountTotal"]; ok {
		volumeParams["rowCountTotal"] = rc
	}
	if f, ok := input["filter"]; ok {
		volumeParams["filter"] = f
	}

	s.assertions[urn] = mockAssertion{
		URN:             urn,
		AssertionType:   "VOLUME",
		Source:          "NATIVE",
		EntityURN:       entityURN,
		Description:     description,
		VolumeAssertion: &volumeParams,
		OnSuccess:       onSuccess,
		OnFailure:       onFailure,
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{
			"upsertDatasetVolumeAssertionMonitor": map[string]any{"urn": urn},
		},
	})
}

// handleUpsertFreshnessAssertion handles the upsertDatasetFreshnessAssertionMonitor mutation.
func (s *mockServer) handleUpsertFreshnessAssertion(w http.ResponseWriter, variables map[string]any) {
	urnRaw, _ := variables["assertionUrn"].(string)
	input, _ := variables["input"].(map[string]any)

	entityURN, _ := input["entityUrn"].(string)
	description, _ := input["description"].(string)

	var onSuccess, onFailure []string
	if actions, ok := input["actions"].(map[string]any); ok {
		onSuccess = actionsToStrings(actions["onSuccess"])
		onFailure = actionsToStrings(actions["onFailure"])
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	urn := urnRaw
	if urn == "" {
		urn = "urn:li:assertion:" + nextAssertionID()
	}

	freshnessParams := map[string]any{
		"schedule": input["schedule"],
	}
	if f, ok := input["filter"]; ok {
		freshnessParams["filter"] = f
	}
	if sev, ok := input["failureSeverityConfig"]; ok {
		freshnessParams["failureSeverityConfig"] = sev
	}

	s.assertions[urn] = mockAssertion{
		URN:             urn,
		AssertionType:   "FRESHNESS",
		Source:          "NATIVE",
		EntityURN:       entityURN,
		Description:     description,
		FreshnessAssert: &freshnessParams,
		OnSuccess:       onSuccess,
		OnFailure:       onFailure,
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{
			"upsertDatasetFreshnessAssertionMonitor": map[string]any{"urn": urn},
		},
	})
}

// handleUpsertSQLAssertion handles the upsertDatasetSqlAssertionMonitor mutation.
func (s *mockServer) handleUpsertSQLAssertion(w http.ResponseWriter, variables map[string]any) {
	urnRaw, _ := variables["assertionUrn"].(string)
	input, _ := variables["input"].(map[string]any)

	entityURN, _ := input["entityUrn"].(string)
	sqlType, _ := input["type"].(string)
	changeType, _ := input["changeType"].(string)
	statement, _ := input["statement"].(string)
	operator, _ := input["operator"].(string)
	description, _ := input["description"].(string)

	var onSuccess, onFailure []string
	if actions, ok := input["actions"].(map[string]any); ok {
		onSuccess = actionsToStrings(actions["onSuccess"])
		onFailure = actionsToStrings(actions["onFailure"])
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	urn := urnRaw
	if urn == "" {
		urn = "urn:li:assertion:" + nextAssertionID()
	}

	sqlParams := map[string]any{
		"type":        sqlType,
		"changeType":  changeType,
		"statement":   statement,
		"operator":    operator,
		"description": description,
		"parameters":  input["parameters"],
	}
	if sev, ok := input["failureSeverityConfig"]; ok {
		sqlParams["failureSeverityConfig"] = sev
	}

	s.assertions[urn] = mockAssertion{
		URN:           urn,
		AssertionType: "SQL",
		Source:        "NATIVE",
		EntityURN:     entityURN,
		SQLAssertion:  &sqlParams,
		OnSuccess:     onSuccess,
		OnFailure:     onFailure,
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{
			"upsertDatasetSqlAssertionMonitor": map[string]any{"urn": urn},
		},
	})
}

// handleUpsertSchemaAssertion handles the upsertDatasetSchemaAssertionMonitor mutation.
func (s *mockServer) handleUpsertSchemaAssertion(w http.ResponseWriter, variables map[string]any) {
	urnRaw, _ := variables["assertionUrn"].(string)
	input, _ := variables["input"].(map[string]any)

	entityURN, _ := input["entityUrn"].(string)
	description, _ := input["description"].(string)
	assertion, _ := input["assertion"].(map[string]any)

	var onSuccess, onFailure []string
	if actions, ok := input["actions"].(map[string]any); ok {
		onSuccess = actionsToStrings(actions["onSuccess"])
		onFailure = actionsToStrings(actions["onFailure"])
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	urn := urnRaw
	if urn == "" {
		urn = "urn:li:assertion:" + nextAssertionID()
	}

	schemaParams := map[string]any{}
	if assertion != nil {
		schemaParams["compatibility"] = assertion["compatibility"]
		schemaParams["fields"] = assertion["fields"]
	}

	s.assertions[urn] = mockAssertion{
		URN:             urn,
		AssertionType:   "DATA_SCHEMA",
		Source:          "NATIVE",
		EntityURN:       entityURN,
		Description:     description,
		SchemaAssertion: &schemaParams,
		OnSuccess:       onSuccess,
		OnFailure:       onFailure,
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{
			"upsertDatasetSchemaAssertionMonitor": map[string]any{"urn": urn},
		},
	})
}

// handleUpsertFieldAssertion handles the upsertDatasetFieldAssertionMonitor mutation.
func (s *mockServer) handleUpsertFieldAssertion(w http.ResponseWriter, variables map[string]any) {
	urnRaw, _ := variables["assertionUrn"].(string)
	input, _ := variables["input"].(map[string]any)

	entityURN, _ := input["entityUrn"].(string)
	description, _ := input["description"].(string)
	fieldType, _ := input["type"].(string)

	var onSuccess, onFailure []string
	if actions, ok := input["actions"].(map[string]any); ok {
		onSuccess = actionsToStrings(actions["onSuccess"])
		onFailure = actionsToStrings(actions["onFailure"])
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	urn := urnRaw
	if urn == "" {
		urn = "urn:li:assertion:" + nextAssertionID()
	}

	fieldParams := map[string]any{"type": fieldType}
	if fv, ok := input["fieldValuesAssertion"]; ok {
		fieldParams["fieldValuesAssertion"] = fv
	}
	if fm, ok := input["fieldMetricAssertion"]; ok {
		fieldParams["fieldMetricAssertion"] = fm
	}
	if f, ok := input["filter"]; ok {
		fieldParams["filter"] = f
	}

	s.assertions[urn] = mockAssertion{
		URN:            urn,
		AssertionType:  "FIELD",
		Source:         "NATIVE",
		EntityURN:      entityURN,
		Description:    description,
		FieldAssertion: &fieldParams,
		OnSuccess:      onSuccess,
		OnFailure:      onFailure,
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{
			"upsertDatasetFieldAssertionMonitor": map[string]any{"urn": urn},
		},
	})
}

// handleGetAssertionMonitor handles the getAssertionMonitor GraphQL query.
// For Cloud-only assertion types (VOLUME, FRESHNESS, SQL), returns a synthetic
// monitor URN so that waitForAssertionMonitor resolves immediately on the create
// path. The monitor DELETE handler accepts any URN as a no-op, so returning a
// synthetic URN here is safe for the delete path too.
// CUSTOM assertions have no monitor entity; nil is returned for those.
func (s *mockServer) handleGetAssertionMonitor(w http.ResponseWriter, variables map[string]any) {
	urn, _ := variables["urn"].(string)

	s.mu.Lock()
	a, exists := s.assertions[urn]
	s.mu.Unlock()

	var monitorVal any
	if exists {
		switch a.AssertionType {
		case "VOLUME", "FRESHNESS", "SQL", "FIELD", "DATA_SCHEMA":
			monitorVal = map[string]any{"urn": "urn:li:monitor:mock-" + urn}
		}
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{
			"assertion": map[string]any{
				"urn":     urn,
				"monitor": monitorVal,
			},
		},
	})
}

// handleMonitorDelete handles DELETE /openapi/v3/entity/monitor/{urn}.
// Monitor entities are not tracked by the mock; this is a no-op that returns 200.
func (s *mockServer) handleMonitorDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.NotFound(w, r)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// handleDeleteAssertion handles the deleteAssertion GraphQL mutation.
func (s *mockServer) handleDeleteAssertion(w http.ResponseWriter, variables map[string]any) {
	urn, _ := variables["urn"].(string)

	s.mu.Lock()
	delete(s.assertions, urn)
	s.mu.Unlock()

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{"deleteAssertion": true},
	})
}

// handleAssertionItem handles GET /openapi/v3/entity/assertion/{urn}.
func (s *mockServer) handleAssertionItem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}

	urn := strings.TrimPrefix(r.URL.Path, "/openapi/v3/entity/assertion/")

	s.mu.Lock()
	a, ok := s.assertions[urn]
	s.mu.Unlock()

	if !ok {
		http.NotFound(w, r)
		return
	}

	entity := buildAssertionEntityJSON(a)
	if aspect := s.globalTagsAspect(a.URN); aspect != nil {
		entity["globalTags"] = aspect
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(entity)
}

// buildAssertionEntityJSON converts a mockAssertion to the OpenAPI v3 entity JSON shape.
func buildAssertionEntityJSON(a mockAssertion) map[string]any {
	infoValue := map[string]any{
		"type":      a.AssertionType,
		"entityUrn": a.EntityURN,
	}
	if a.Source != "" {
		infoValue["source"] = map[string]any{"type": a.Source}
	}
	// description is a top-level assertionInfo field in the real API. Custom and
	// sql carry it inside their own param maps (handled below); volume and
	// freshness store it on mockAssertion.Description.
	if a.Description != "" {
		infoValue["description"] = a.Description
	}

	switch a.AssertionType {
	case "CUSTOM":
		if a.CustomAssertion != nil {
			ca := *a.CustomAssertion
			// Match the real DataHub API: description and externalUrl are top-level
			// fields in assertionInfo.value, not inside customAssertion. Platform URN
			// is in a separate dataPlatformInstance aspect on the entity.
			if desc, _ := ca["description"].(string); desc != "" {
				infoValue["description"] = desc
			}
			if extURL, _ := ca["externalUrl"].(string); extURL != "" {
				infoValue["externalUrl"] = extURL
			}
			infoValue["customAssertion"] = map[string]any{
				"type":  ca["type"],
				"logic": ca["logic"],
			}
		}
	case "VOLUME":
		if a.VolumeAssertion != nil {
			va := *a.VolumeAssertion
			vol := map[string]any{"type": va["type"]}
			if rc, ok := va["rowCountChange"]; ok {
				vol["rowCountChange"] = rc
			}
			if rc, ok := va["rowCountTotal"]; ok {
				vol["rowCountTotal"] = rc
			}
			if f, ok := va["filter"]; ok {
				vol["filter"] = f
			}
			infoValue["volumeAssertion"] = vol
		}
	case "FRESHNESS":
		if a.FreshnessAssert != nil {
			fa := *a.FreshnessAssert
			fresh := map[string]any{"schedule": fa["schedule"]}
			if f, ok := fa["filter"]; ok {
				fresh["filter"] = f
			}
			if sev, ok := fa["failureSeverityConfig"]; ok {
				fresh["failureSeverityConfig"] = sev
			}
			infoValue["freshnessAssertion"] = fresh
		}
	case "SQL":
		if a.SQLAssertion != nil {
			sa := *a.SQLAssertion
			// description is top-level in assertionInfo.value, matching the real
			// DataHub Cloud API (same pattern as custom assertions).
			if desc, _ := sa["description"].(string); desc != "" {
				infoValue["description"] = desc
			}
			sqlObj := map[string]any{
				"type":       sa["type"],
				"statement":  sa["statement"],
				"operator":   sa["operator"],
				"parameters": sa["parameters"],
			}
			if ct, _ := sa["changeType"].(string); ct != "" {
				sqlObj["changeType"] = ct
			}
			if sev, ok := sa["failureSeverityConfig"]; ok {
				sqlObj["failureSeverityConfig"] = sev
			}
			infoValue["sqlAssertion"] = sqlObj
		}
	case "FIELD":
		if a.FieldAssertion != nil {
			fa := *a.FieldAssertion
			// fieldValuesAssertion / fieldMetricAssertion round-trip as sent.
			infoValue["fieldAssertion"] = fa
		}
	case "DATA_SCHEMA":
		if a.SchemaAssertion != nil {
			sa := *a.SchemaAssertion
			// Reproduce the real read shape: the write field list ({path,type,nativeType})
			// comes back under schema.fields with the std type re-encoded as a
			// SchemaFieldDataType class object, exercising the client's class->std mapping.
			var rfields []map[string]any
			if raw, ok := sa["fields"].([]any); ok {
				for _, fi := range raw {
					fm, _ := fi.(map[string]any)
					path, _ := fm["path"].(string)
					std, _ := fm["type"].(string)
					nt, _ := fm["nativeType"].(string)
					rfields = append(rfields, map[string]any{
						"fieldPath":      path,
						"nativeDataType": nt,
						"type":           map[string]any{"type": map[string]any{mockSchemaClassForStd(std): map[string]any{}}},
					})
				}
			}
			infoValue["schemaAssertion"] = map[string]any{
				"compatibility": sa["compatibility"],
				"schema":        map[string]any{"fields": rfields},
			}
		}
	}

	entity := map[string]any{
		"urn": a.URN,
		"assertionKey": map[string]any{
			"value": map[string]any{
				"assertionId": strings.TrimPrefix(a.URN, "urn:li:assertion:"),
			},
		},
		"assertionInfo": map[string]any{
			"value": infoValue,
		},
	}

	if a.AssertionType == "CUSTOM" && a.CustomAssertion != nil {
		ca := *a.CustomAssertion
		if p, ok := ca["platform"].(map[string]any); ok {
			if urn, _ := p["urn"].(string); urn != "" {
				entity["dataPlatformInstance"] = map[string]any{
					"value": map[string]any{"platform": urn},
				}
			}
		}
	}

	if len(a.OnSuccess) > 0 || len(a.OnFailure) > 0 {
		onSuccess := make([]map[string]any, len(a.OnSuccess))
		for i, t := range a.OnSuccess {
			onSuccess[i] = map[string]any{"type": t}
		}
		onFailure := make([]map[string]any, len(a.OnFailure))
		for i, t := range a.OnFailure {
			onFailure[i] = map[string]any{"type": t}
		}
		entity["assertionActions"] = map[string]any{
			"value": map[string]any{
				"onSuccess": onSuccess,
				"onFailure": onFailure,
			},
		}
	}

	return entity
}

// mockSchemaClassForStd maps a write std type (NUMBER, STRING, ...) to the
// SchemaFieldDataType class the real read shape uses, so the mock reproduces the
// write/read type encoding mismatch the client must reverse. Mirror of the
// client's stdTypeFromSchemaClass.
func mockSchemaClassForStd(std string) string {
	special := map[string]string{"STRUCT": "RecordType"}
	short, ok := special[std]
	if !ok {
		// NUMBER -> Number, STRING -> String, ...
		short = strings.ToUpper(std[:1]) + strings.ToLower(std[1:]) + "Type"
	}
	return "com.linkedin.schema." + short
}

// assertionSearchInfo builds the assertionInfo shape returned in
// searchAcrossEntities results: the type, source, and the sub-shape
// discriminator the type-routed enumerators filter on.
func assertionSearchInfo(a mockAssertion) map[string]any {
	info := map[string]any{"type": a.AssertionType}
	if a.Source != "" {
		info["source"] = map[string]any{"type": a.Source}
	}
	if a.VolumeAssertion != nil {
		if t, ok := (*a.VolumeAssertion)["type"]; ok {
			info["volumeAssertion"] = map[string]any{"type": t}
		}
	}
	if a.SQLAssertion != nil {
		if t, ok := (*a.SQLAssertion)["type"]; ok {
			info["sqlAssertion"] = map[string]any{"type": t}
		}
	}
	if a.FreshnessAssert != nil {
		if sched, ok := (*a.FreshnessAssert)["schedule"].(map[string]any); ok {
			info["freshnessAssertion"] = map[string]any{"schedule": map[string]any{"type": sched["type"]}}
		}
	}
	return info
}

// handleSeedAssertion injects an assertion into the mock store with an explicit
// source and sub-shape -- used by tests to create assertions the normal API
// cannot, e.g. an EXTERNAL (ingested) assertion, to exercise source-based
// enumeration filtering and the import guard.
//
//	POST /test-control/seed-assertion
//	{"urn":"...","type":"VOLUME","source":"EXTERNAL","subType":"ROW_COUNT_TOTAL"}
func (s *mockServer) handleSeedAssertion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		URN     string `json:"urn"`
		Type    string `json:"type"`
		Source  string `json:"source"`
		SubType string `json:"subType"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.URN == "" || body.Type == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	a := mockAssertion{URN: body.URN, AssertionType: body.Type, Source: body.Source}
	switch body.Type {
	case "VOLUME":
		m := map[string]any{"type": body.SubType}
		a.VolumeAssertion = &m
	case "SQL":
		m := map[string]any{"type": body.SubType}
		a.SQLAssertion = &m
	case "FRESHNESS":
		m := map[string]any{"schedule": map[string]any{"type": body.SubType}}
		a.FreshnessAssert = &m
	}
	s.mu.Lock()
	s.assertions[body.URN] = a
	s.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}
