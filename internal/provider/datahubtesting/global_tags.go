// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahubtesting

import (
	"encoding/json"
	"io"
	"net/http"
)

// globalTagsPayload is the wire shape of a globalTags aspect inside an
// OpenAPI v3 entity POST.
type globalTagsPayload struct {
	URN        string `json:"urn"`
	GlobalTags *struct {
		Value struct {
			Tags []struct {
				Tag string `json:"tag"`
			} `json:"tags"`
		} `json:"value"`
	} `json:"globalTags"`
}

// storeGlobalTagsFromPayload scans an OpenAPI v3 entity POST body for
// globalTags aspects and stores the full lists per URN (whole-aspect replace,
// matching real OpenAPI semantics; an empty list clears). Elements without
// the aspect are ignored. Takes its own lock.
func (s *mockServer) storeGlobalTagsFromPayload(body []byte) {
	var entities []globalTagsPayload
	if err := json.Unmarshal(body, &entities); err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range entities {
		if e.URN == "" || e.GlobalTags == nil {
			continue
		}
		tags := []string{}
		for _, t := range e.GlobalTags.Value.Tags {
			if t.Tag != "" {
				tags = append(tags, t.Tag)
			}
		}
		s.globalTags[e.URN] = tags
	}
}

// globalTagsAspect returns the OpenAPI v3 globalTags aspect for an entity, or
// nil when the aspect was never written. Takes its own lock; call it from
// item handlers AFTER they have released s.mu.
func (s *mockServer) globalTagsAspect(urn string) map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()

	tags, ok := s.globalTags[urn]
	if !ok {
		return nil
	}
	list := make([]map[string]any, 0, len(tags))
	for _, t := range tags {
		list = append(list, map[string]any{"tag": t})
	}
	return map[string]any{
		"value": map[string]any{"tags": list},
	}
}

// handleCorpGroupCollection serves POST /openapi/v3/entity/corpgroup. Group
// creation and property edits go through GraphQL, so the only aspect write
// the provider sends here is globalTags.
func (s *mockServer) handleCorpGroupCollection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	s.storeGlobalTagsFromPayload(body)
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte("[]"))
}
