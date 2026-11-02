// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahubtesting

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"

	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// The typed handlers in this mock store only the fields the provider models,
// so an aspect field the provider does not know about cannot exist here, and a
// write that erases one passes every mock test. That is how four resources
// shipped wiping data product assets, glossary term sources, structured
// property platforms and corp user profiles.
//
// unmanagedAspectFields closes the gap without rewriting the typed handlers. A
// test seeds a field on an entity's aspect through
// /test-control/seed-unmanaged-field, standing in for a value written in the
// DataHub UI or by ingestion. From then on the mock gives that field the
// server's real semantics: an OpenAPI v3 aspect POST replaces the whole aspect,
// so a POST of the aspect that omits the field erases it, and a GET of the
// entity returns it inside the aspect's value for as long as it survives.
type unmanagedAspectFields struct {
	mu sync.Mutex
	// fields maps urn -> aspect name -> field -> value.
	fields map[string]map[string]map[string]any
}

func (u *unmanagedAspectFields) tracked() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	return len(u.fields) > 0
}

// wrap applies the semantics to every request under /openapi/v3/entity/.
func (u *unmanagedAspectFields) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/openapi/v3/entity/") || !u.tracked() {
			next.ServeHTTP(w, r)
			return
		}
		switch r.Method {
		case http.MethodPost:
			body, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
			rec := httptest.NewRecorder()
			next.ServeHTTP(rec, r)
			if rec.Code < http.StatusBadRequest {
				u.applyWrite(body)
			}
			copyRecorded(w, rec, rec.Body.Bytes())
		case http.MethodGet:
			rec := httptest.NewRecorder()
			next.ServeHTTP(rec, r)
			out := rec.Body.Bytes()
			if rec.Code == http.StatusOK {
				out = u.injectInto(out)
			}
			copyRecorded(w, rec, out)
		default:
			next.ServeHTTP(w, r)
		}
	})
}

func copyRecorded(w http.ResponseWriter, rec *httptest.ResponseRecorder, body []byte) {
	for k, vs := range rec.Header() {
		if k == "Content-Length" {
			continue
		}
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(rec.Code)
	_, _ = w.Write(body)
}

// applyWrite replaces each tracked aspect a POST carries: a seeded field the
// written value includes takes the written value, and one it omits is erased.
func (u *unmanagedAspectFields) applyWrite(body []byte) {
	var entities []map[string]json.RawMessage
	if err := json.Unmarshal(body, &entities); err != nil {
		return
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	for _, entity := range entities {
		var urn string
		if err := json.Unmarshal(entity["urn"], &urn); err != nil {
			continue
		}
		for aspect, fields := range u.fields[urn] {
			raw, ok := entity[aspect]
			if !ok {
				continue
			}
			var written struct {
				Value map[string]any `json:"value"`
			}
			if err := decodeExact(raw, &written); err != nil {
				continue
			}
			for field := range fields {
				if v, ok := written.Value[field]; ok {
					fields[field] = v
				} else {
					delete(fields, field)
				}
			}
		}
	}
}

// injectInto adds the surviving seeded fields to the aspects of an entity GET
// response, adding the aspect itself where the typed handler left it out: on a
// real server an aspect holding a field exists.
func (u *unmanagedAspectFields) injectInto(body []byte) []byte {
	var entity map[string]json.RawMessage
	if err := json.Unmarshal(body, &entity); err != nil {
		return body
	}
	var urn string
	if err := json.Unmarshal(entity["urn"], &urn); err != nil {
		return body
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	changed := false
	for aspect, fields := range u.fields[urn] {
		if len(fields) == 0 {
			continue
		}
		a := map[string]any{}
		if raw, ok := entity[aspect]; ok {
			if err := decodeExact(raw, &a); err != nil {
				continue
			}
		}
		value, _ := a["value"].(map[string]any)
		if value == nil {
			value = map[string]any{}
		}
		for field, v := range fields {
			value[field] = v
		}
		a["value"] = value
		out, err := json.Marshal(a)
		if err != nil {
			continue
		}
		entity[aspect] = out
		changed = true
	}
	if !changed {
		return body
	}
	out, err := json.Marshal(entity)
	if err != nil {
		return body
	}
	return out
}

func decodeExact(raw []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("decoding aspect: %w", err)
	}
	return nil
}

type unmanagedFieldRequest struct {
	URN    string `json:"urn"`
	Aspect string `json:"aspect"`
	Field  string `json:"field"`
	Value  any    `json:"value"`
}

// handleSeedUnmanagedField serves POST /test-control/seed-unmanaged-field,
// which seeds a field, and GET with urn, aspect and field query parameters,
// which returns the field's current value or 404 once a write erased it.
func (u *unmanagedAspectFields) handleSeedUnmanagedField(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var req unmanagedFieldRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.URN == "" || req.Aspect == "" || req.Field == "" {
			http.Error(w, "urn, aspect and field are required", http.StatusBadRequest)
			return
		}
		u.mu.Lock()
		if u.fields == nil {
			u.fields = map[string]map[string]map[string]any{}
		}
		if u.fields[req.URN] == nil {
			u.fields[req.URN] = map[string]map[string]any{}
		}
		if u.fields[req.URN][req.Aspect] == nil {
			u.fields[req.URN][req.Aspect] = map[string]any{}
		}
		u.fields[req.URN][req.Aspect][req.Field] = req.Value
		u.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	case http.MethodGet:
		q := r.URL.Query()
		u.mu.Lock()
		v, ok := u.fields[q.Get("urn")][q.Get("aspect")][q.Get("field")]
		u.mu.Unlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"value": v})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// SeedUnmanagedAspectField seeds field on the aspect of urn in the mock at
// baseURL, standing in for a value the provider does not manage written in
// the DataHub UI or by ingestion. Mock-only.
func SeedUnmanagedAspectField(baseURL, urn, aspect, field string, value any) {
	body, err := json.Marshal(unmanagedFieldRequest{URN: urn, Aspect: aspect, Field: field, Value: value})
	if err != nil {
		panic(fmt.Sprintf("SeedUnmanagedAspectField: %v", err))
	}
	resp, err := http.Post(baseURL+"/test-control/seed-unmanaged-field", "application/json", bytes.NewReader(body)) //nolint:noctx
	if err != nil {
		panic(fmt.Sprintf("SeedUnmanagedAspectField: %v", err))
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		panic(fmt.Sprintf("SeedUnmanagedAspectField: unexpected status %d", resp.StatusCode))
	}
}

// CheckUnmanagedAspectField asserts that a field seeded with
// SeedUnmanagedAspectField still holds want, which fails as soon as an aspect
// write that omitted it has erased it. Mock-only.
func CheckUnmanagedAspectField(urn, aspect, field string, want any) func(*terraform.State) error {
	return func(*terraform.State) error {
		q := url.Values{"urn": {urn}, "aspect": {aspect}, "field": {field}}.Encode()
		resp, err := http.Get(os.Getenv("DATAHUB_GMS_URL") + "/test-control/seed-unmanaged-field?" + q) //nolint:noctx
		if err != nil {
			return fmt.Errorf("reading seeded %s.%s: %w", aspect, field, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusNotFound {
			return fmt.Errorf("%s.%s on %s was erased: an aspect write sent %s without this field, which "+
				"the provider does not manage and must carry over as read", aspect, field, urn, aspect)
		}
		var got struct {
			Value any `json:"value"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
			return fmt.Errorf("decoding seeded %s.%s: %w", aspect, field, err)
		}
		if fmt.Sprint(got.Value) != fmt.Sprint(want) {
			return fmt.Errorf("%s.%s on %s = %v, want it kept as %v", aspect, field, urn, got.Value, want)
		}
		return nil
	}
}
