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
	AssertionType string // CUSTOM, FRESHNESS, VOLUME, SQL
	EntityURN     string
	// Type-specific params stored as raw maps for echo-back.
	CustomAssertion *map[string]any
	VolumeAssertion *map[string]any
	FreshnessAssert *map[string]any
	SQLAssertion    *map[string]any
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

	volumeParams := map[string]any{
		"type":          volumeType,
		"rowCountTotal": input["rowCountTotal"],
	}

	s.assertions[urn] = mockAssertion{
		URN:             urn,
		AssertionType:   "VOLUME",
		EntityURN:       entityURN,
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

	s.assertions[urn] = mockAssertion{
		URN:             urn,
		AssertionType:   "FRESHNESS",
		EntityURN:       entityURN,
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
		"statement":   statement,
		"operator":    operator,
		"description": description,
		"parameters":  input["parameters"],
	}

	s.assertions[urn] = mockAssertion{
		URN:           urn,
		AssertionType: "SQL",
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

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(buildAssertionEntityJSON(a))
}

// buildAssertionEntityJSON converts a mockAssertion to the OpenAPI v3 entity JSON shape.
func buildAssertionEntityJSON(a mockAssertion) map[string]any {
	infoValue := map[string]any{
		"type":      a.AssertionType,
		"entityUrn": a.EntityURN,
	}

	switch a.AssertionType {
	case "CUSTOM":
		if a.CustomAssertion != nil {
			ca := *a.CustomAssertion
			infoValue["customAssertion"] = map[string]any{
				"type":        ca["type"],
				"description": ca["description"],
				"fieldPath":   ca["fieldPath"],
				"platform":    ca["platform"],
				"externalUrl": ca["externalUrl"],
				"logic":       ca["logic"],
			}
		}
	case "VOLUME":
		if a.VolumeAssertion != nil {
			va := *a.VolumeAssertion
			infoValue["volumeAssertion"] = map[string]any{
				"type":          va["type"],
				"rowCountTotal": va["rowCountTotal"],
			}
		}
	case "FRESHNESS":
		if a.FreshnessAssert != nil {
			fa := *a.FreshnessAssert
			infoValue["freshnessAssertion"] = map[string]any{
				"schedule": fa["schedule"],
			}
		}
	case "SQL":
		if a.SQLAssertion != nil {
			sa := *a.SQLAssertion
			infoValue["sqlAssertion"] = map[string]any{
				"type":        sa["type"],
				"statement":   sa["statement"],
				"operator":    sa["operator"],
				"description": sa["description"],
				"parameters":  sa["parameters"],
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
