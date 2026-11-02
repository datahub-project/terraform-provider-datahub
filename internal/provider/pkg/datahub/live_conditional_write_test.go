// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahub

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

// TestLiveConditionalWriteProbe records how a real DataHub server behaves for
// the OpenAPI v3 conditional-write mechanism (If-Version-Match in the
// per-aspect "headers" object, systemMetadata.version on reads). It runs only
// when DATAHUB_GMS_URL is set, so it is part of every live acceptance run and
// can be pointed at any instance:
//
//	DATAHUB_GMS_URL=... DATAHUB_GMS_TOKEN=... go test ./internal/provider/pkg/datahub -run TestLiveConditionalWriteProbe -v
//
// Most subtests log what they observed and assert only the facts the
// provider's conditional-write path depends on. The logged answers are
// recorded in docs/design/conditional-writes.md; a change in them on a newer
// server is worth knowing about before it is worth failing on.
func TestLiveConditionalWriteProbe(t *testing.T) {
	gmsURL := os.Getenv("DATAHUB_GMS_URL")
	if gmsURL == "" {
		t.Skip("DATAHUB_GMS_URL not set; live probe skipped")
	}
	c, err := NewClient(gmsURL, os.Getenv("DATAHUB_GMS_TOKEN"))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	ctx := context.Background()
	p := &probe{t: t, c: c, ctx: ctx}

	urn := fmt.Sprintf("urn:li:ownershipType:tfprobe-%d-%04d", time.Now().Unix(), rand.Intn(10000))
	t.Cleanup(func() {
		status, body := p.do(http.MethodDelete, "/openapi/v3/entity/ownershiptype/"+url.PathEscape(urn), nil)
		t.Logf("cleanup DELETE %s -> %d %s", urn, status, trim(body))
	})

	const aspect = "ownershipTypeInfo"
	stamp := map[string]any{"time": time.Now().UnixMilli(), "actor": "urn:li:corpuser:__datahub_system"}
	value := func(desc string) map[string]any {
		return map[string]any{
			"name":         "TF probe",
			"description":  desc,
			"created":      stamp,
			"lastModified": stamp,
		}
	}
	entry := func(v map[string]any, ifVersionMatch string) []map[string]any {
		a := map[string]any{"value": v}
		if ifVersionMatch != "" {
			a["headers"] = map[string]string{"If-Version-Match": ifVersionMatch}
		}
		return []map[string]any{{"urn": urn, aspect: a}}
	}
	post := func(body any) (int, string) {
		return p.do(http.MethodPost, "/openapi/v3/entity/ownershiptype?async=false&systemMetadata=true", body)
	}
	get := func(query string) (int, string) {
		return p.do(http.MethodGet, "/openapi/v3/entity/ownershiptype/"+url.PathEscape(urn)+"?"+query, nil)
	}

	t.Run("Q1_missing_entity_get_shape", func(t *testing.T) {
		status, body := get("aspects=" + aspect + "&systemMetadata=true")
		t.Logf("GET absent entity -> %d %s", status, trim(body))
		if status != http.StatusNotFound {
			t.Errorf("expected 404 for an absent entity, got %d", status)
		}
	})

	t.Run("Q2_first_write_with_minus_one", func(t *testing.T) {
		status, body := post(entry(value("v1"), "-1"))
		t.Logf("POST If-Version-Match=-1 on absent aspect -> %d %s", status, trim(body))
		if status < 200 || status >= 300 {
			t.Fatalf("first conditional write failed: %d", status)
		}
		t.Logf("POST response carries systemMetadata.version: %q", versionIn(body))
	})

	t.Run("Q2_get_version_after_first_write", func(t *testing.T) {
		status, body := get("aspects=" + aspect + "&systemMetadata=true")
		v := versionIn(body)
		t.Logf("GET after first write -> %d version=%q keys=%v", status, v, topKeys(body))
		if v != "1" {
			t.Errorf("expected systemMetadata.version \"1\" after first write, got %q", v)
		}
		status, body = get("aspects=" + aspect)
		t.Logf("GET without systemMetadata=true -> %d version=%q", status, versionIn(body))
	})

	t.Run("Q1b_comma_joined_aspects", func(t *testing.T) {
		status, body := get("aspects=" + aspect + ",ownershipTypeKey&systemMetadata=true")
		t.Logf("GET aspects=a,b -> %d keys=%v", status, topKeys(body))
		status, body = get("aspects=" + aspect + "&aspects=ownershipTypeKey&systemMetadata=true")
		t.Logf("GET aspects=a&aspects=b -> %d keys=%v", status, topKeys(body))
	})

	t.Run("Q1c_existing_entity_missing_aspect", func(t *testing.T) {
		status, body := get("aspects=globalTags&systemMetadata=true")
		t.Logf("GET existing entity, absent aspect -> %d keys=%v body=%s", status, topKeys(body), trim(body))
	})

	t.Run("Q3_mismatch_is_412", func(t *testing.T) {
		status, body := post(entry(value("v-stale"), "99"))
		t.Logf("POST If-Version-Match=99 -> %d %s", status, trim(body))
		if status != http.StatusPreconditionFailed {
			t.Errorf("expected 412 on version mismatch, got %d", status)
		}
		var e struct {
			Error   string `json:"error"`
			Message string `json:"message"`
		}
		if json.Unmarshal([]byte(body), &e) == nil {
			t.Logf("412 error=%q message=%q", e.Error, e.Message)
		}
		_, body = get("aspects=" + aspect + "&systemMetadata=true")
		if got := descriptionIn(body, aspect); got != "v1" {
			t.Errorf("mismatched write must not persist; description is %q", got)
		}
		t.Logf("version after rejected write: %q", versionIn(body))
	})

	t.Run("Q3b_minus_one_rejected_when_exists", func(t *testing.T) {
		status, body := post(entry(value("v-dup"), "-1"))
		t.Logf("POST If-Version-Match=-1 on existing aspect -> %d %s", status, trim(body))
		if status != http.StatusPreconditionFailed {
			t.Errorf("expected 412 for -1 on an existing aspect, got %d", status)
		}
	})

	t.Run("Q9_identical_write_keeps_version", func(t *testing.T) {
		_, body := get("aspects=" + aspect + "&systemMetadata=true")
		before := versionIn(body)
		var stored map[string]json.RawMessage
		_ = json.Unmarshal([]byte(body), &stored)
		var cur struct {
			Value map[string]any `json:"value"`
		}
		_ = json.Unmarshal(stored[aspect], &cur)
		status, resp := post(entry(cur.Value, before))
		_, body = get("aspects=" + aspect + "&systemMetadata=true")
		t.Logf("identical body with correct version -> %d; version before=%q after=%q; response version=%q", status, before, versionIn(body), versionIn(resp))
	})

	t.Run("Q9b_changed_write_bumps_version", func(t *testing.T) {
		_, body := get("aspects=" + aspect + "&systemMetadata=true")
		before := versionIn(body)
		status, resp := post(entry(value("v2"), before))
		_, body = get("aspects=" + aspect + "&systemMetadata=true")
		after := versionIn(body)
		t.Logf("changed body with correct version -> %d; version before=%q after=%q; response version=%q", status, before, after, versionIn(resp))
		if status < 200 || status >= 300 {
			t.Errorf("conditional update with the correct version failed: %d %s", status, trim(resp))
		}
		if after == before {
			t.Errorf("version did not advance after a changed write (before=%q after=%q)", before, after)
		}
	})

	t.Run("Q8_time_header_shapes", func(t *testing.T) {
		_, body := get("aspects=" + aspect + "&systemMetadata=true")
		v := versionIn(body)
		past := time.Now().Add(-time.Hour)
		for _, h := range []struct{ name, val string }{
			{"If-Unmodified-Since", past.UTC().Format(time.RFC3339)},
			{"If-Unmodified-Since", past.UTC().Format(http.TimeFormat)},
		} {
			e := entry(value("v-time"), "")
			a, ok := e[0][aspect].(map[string]any)
			if !ok {
				t.Fatalf("entry shape: %T", e[0][aspect])
			}
			a["headers"] = map[string]string{h.name: h.val}
			status, resp := post(e)
			t.Logf("%s: %s -> %d %s", h.name, h.val, status, trim(resp))
		}
		// Restore a known version-matched state for later subtests.
		_, body = get("aspects=" + aspect + "&systemMetadata=true")
		t.Logf("version after time-header probes: %q (was %q)", versionIn(body), v)
	})

	t.Run("Q10_soft_delete_read", func(t *testing.T) {
		status, resp := p.do(http.MethodPost, "/openapi/v3/entity/ownershiptype?async=false",
			[]map[string]any{{"urn": urn, "status": map[string]any{"value": map[string]any{"removed": true}}}})
		t.Logf("soft delete via status aspect -> %d %s", status, trim(resp))
		s1, b1 := get("aspects=" + aspect + "&systemMetadata=true")
		t.Logf("GET soft-deleted, default -> %d version=%q keys=%v", s1, versionIn(b1), topKeys(b1))
		s2, b2 := get("aspects=" + aspect + "&systemMetadata=true&includeSoftDelete=true")
		t.Logf("GET soft-deleted, includeSoftDelete=true -> %d version=%q keys=%v", s2, versionIn(b2), topKeys(b2))
		s3, b3 := post(entry(value("v-after-softdelete"), "-1"))
		t.Logf("POST If-Version-Match=-1 on soft-deleted entity -> %d %s", s3, trim(b3))
		status, _ = p.do(http.MethodPost, "/openapi/v3/entity/ownershiptype?async=false",
			[]map[string]any{{"urn": urn, "status": map[string]any{"value": map[string]any{"removed": false}}}})
		t.Logf("restore status.removed=false -> %d", status)
	})

	t.Run("Q5_server_config_flags", func(t *testing.T) {
		status, body := p.do(http.MethodGet, "/config", nil)
		lower := strings.ToLower(body)
		t.Logf("GET /config -> %d (%d bytes); mentions optimistic=%v lock=%v", status, len(body),
			strings.Contains(lower, "optimistic"), strings.Contains(lower, "lock"))
		var cfg map[string]any
		if json.Unmarshal([]byte(body), &cfg) == nil {
			for _, k := range []string{"versions", "datahub", "managedIngestion"} {
				if v, ok := cfg[k]; ok {
					b, _ := json.Marshal(v)
					t.Logf("config.%s=%s", k, trim(string(b)))
				}
			}
		}
	})

	t.Run("Q5b_spurious_412_rate", func(t *testing.T) {
		const n = 20
		conflicts, failures := 0, 0
		for i := 0; i < n; i++ {
			_, body := get("aspects=" + aspect + "&systemMetadata=true")
			v := versionIn(body)
			status, resp := post(entry(value(fmt.Sprintf("loop-%d", i)), v))
			switch {
			case status == http.StatusPreconditionFailed:
				conflicts++
				t.Logf("iteration %d: 412 with version %q: %s", i, v, trim(resp))
			case status < 200 || status >= 300:
				failures++
				t.Logf("iteration %d: %d %s", i, status, trim(resp))
			}
		}
		t.Logf("%d sequential read-then-conditional-write cycles: %d spurious 412, %d other failures", n, conflicts, failures)
		if failures > 0 {
			t.Errorf("%d non-conflict failures in the sequential loop", failures)
		}
	})
}

type probe struct {
	t   *testing.T
	c   *Client
	ctx context.Context
}

func (p *probe) do(method, path string, body any) (int, string) {
	p.t.Helper()
	req, err := p.c.NewRequest(p.ctx, method, path, body)
	if err != nil {
		p.t.Fatalf("NewRequest %s %s: %v", method, path, err)
	}
	res, err := p.c.Do(req)
	if err != nil {
		p.t.Fatalf("Do %s %s: %v", method, path, err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(b)
}

// versionIn returns systemMetadata.version for aspect from an entity body (a
// single object or a one-element array, which is what the POST returns).
func versionIn(body string) string {
	const aspect = "ownershipTypeInfo"
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &obj); err != nil {
		var arr []map[string]json.RawMessage
		if json.Unmarshal([]byte(body), &arr) != nil || len(arr) == 0 {
			return ""
		}
		obj = arr[0]
	}
	var a struct {
		SystemMetadata struct {
			Version string `json:"version"`
		} `json:"systemMetadata"`
	}
	_ = json.Unmarshal(obj[aspect], &a)
	return a.SystemMetadata.Version
}

func descriptionIn(body, aspect string) string {
	var obj map[string]json.RawMessage
	_ = json.Unmarshal([]byte(body), &obj)
	var a struct {
		Value struct {
			Description string `json:"description"`
		} `json:"value"`
	}
	_ = json.Unmarshal(obj[aspect], &a)
	return a.Value.Description
}

func topKeys(body string) []string {
	var obj map[string]json.RawMessage
	if json.Unmarshal([]byte(body), &obj) != nil {
		return nil
	}
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	return keys
}

func trim(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 600 {
		return s[:600] + "..."
	}
	return s
}
