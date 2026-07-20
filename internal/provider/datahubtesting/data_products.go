// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahubtesting

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// mockDataProduct mirrors the data product shape the provider sends and reads.
type mockDataProduct struct {
	URN              string
	ID               string
	Name             string
	Description      string
	ExternalURL      string
	CustomProperties map[string]string
	Domain           string
}

// handleDataProductCollection handles POST /openapi/v3/entity/dataproduct
// (no trailing slash). The provider uses this to write the
// dataProductProperties (and optionally domains) aspects on create and update.
func (s *mockServer) handleDataProductCollection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, _ := io.ReadAll(r.Body)
	var entities []map[string]any
	if err := json.Unmarshal(body, &entities); err != nil || len(entities) == 0 {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	// Aspect writes that only carry globalTags must not touch the product
	// record (real OpenAPI semantics: aspects are independent).
	s.storeGlobalTagsFromPayload(body)

	s.mu.Lock()
	for _, entity := range entities {
		urn, _ := entity["urn"].(string)
		if urn == "" {
			continue
		}
		if _, hasProps := entity["dataProductProperties"]; !hasProps {
			if _, hasDomains := entity["domains"]; !hasDomains {
				continue // aspect-only write (e.g. globalTags)
			}
		}
		id := strings.TrimPrefix(urn, "urn:li:dataProduct:")

		// Read existing entry to preserve fields not in this write.
		existing := s.dataProducts[id]
		existing.URN = urn
		existing.ID = id

		if propsRaw, ok := entity["dataProductProperties"].(map[string]any); ok {
			if valueRaw, ok := propsRaw["value"].(map[string]any); ok {
				if n, ok := valueRaw["name"].(string); ok {
					existing.Name = n
				}
				if d, ok := valueRaw["description"].(string); ok {
					existing.Description = d
				} else {
					existing.Description = ""
				}
				if u, ok := valueRaw["externalUrl"].(string); ok {
					existing.ExternalURL = u
				} else {
					existing.ExternalURL = ""
				}
				if cpRaw, ok := valueRaw["customProperties"].(map[string]any); ok {
					cp := make(map[string]string, len(cpRaw))
					for k, v := range cpRaw {
						if sv, ok := v.(string); ok {
							cp[k] = sv
						}
					}
					existing.CustomProperties = cp
				} else {
					existing.CustomProperties = nil
				}
			}
		}

		if domainsRaw, ok := entity["domains"].(map[string]any); ok {
			if valueRaw, ok := domainsRaw["value"].(map[string]any); ok {
				if domList, ok := valueRaw["domains"].([]any); ok {
					if len(domList) > 0 {
						if domStr, ok := domList[0].(string); ok {
							existing.Domain = domStr
						}
					} else {
						// Empty domain list clears the domain.
						existing.Domain = ""
					}
				}
			}
		}

		s.dataProducts[id] = existing
	}
	s.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(body)
}

// handleDataProductItem serves GET /openapi/v3/entity/dataproduct/{urn},
// returning the same aspect shape as the real OpenAPI v3 endpoint.
func (s *mockServer) handleDataProductItem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	urn := strings.TrimPrefix(r.URL.Path, "/openapi/v3/entity/dataproduct/")
	id := strings.TrimPrefix(urn, "urn:li:dataProduct:")

	s.mu.Lock()
	dp, ok := s.dataProducts[id]
	s.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}

	propsValue := map[string]any{
		"name":        dp.Name,
		"description": dp.Description,
		"externalUrl": dp.ExternalURL,
	}
	if len(dp.CustomProperties) > 0 {
		cp := make(map[string]any, len(dp.CustomProperties))
		for k, v := range dp.CustomProperties {
			cp[k] = v
		}
		propsValue["customProperties"] = cp
	}

	entity := map[string]any{
		"urn": dp.URN,
		"dataProductKey": map[string]any{
			"value": map[string]any{"id": dp.ID},
		},
		"dataProductProperties": map[string]any{
			"value": propsValue,
		},
	}

	if dp.Domain != "" {
		entity["domains"] = map[string]any{
			"value": map[string]any{
				"domains": []string{dp.Domain},
			},
		}
	}
	if aspect := s.structuredPropertiesAspect(dp.URN); aspect != nil {
		entity["structuredProperties"] = aspect
	}
	if aspect := s.globalTagsAspect(dp.URN); aspect != nil {
		entity["globalTags"] = aspect
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(entity)
}

// handleDeleteDataProduct serves the deleteDataProduct GraphQL mutation.
func (s *mockServer) handleDeleteDataProduct(w http.ResponseWriter, variables map[string]any) {
	urn, _ := variables["urn"].(string)
	id := strings.TrimPrefix(urn, "urn:li:dataProduct:")

	s.mu.Lock()
	delete(s.dataProducts, id)
	s.mu.Unlock()

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{"deleteDataProduct": true},
	})
}
