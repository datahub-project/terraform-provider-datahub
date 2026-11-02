// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahubtesting

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// DataHub keeps a version on every stored aspect (systemMetadata.version: "1"
// on the first write, incremented on every write that changes the value) and
// honours an If-Version-Match precondition in the per-aspect "headers" object
// of an OpenAPI v3 POST: the write lands only if the stored version equals the
// header, otherwise the whole request fails with HTTP 412 and nothing in it is
// written. A read-modify-write that sends the version it read can therefore
// never overwrite a change made between its read and its write. The typed
// handlers in this mock know nothing of versions, so without this layer a
// writer that dropped the precondition would pass every mock test.
//
// aspectVersions gives the mock those semantics, in the request path, for
// every entity type at once. It is installed outermost in NewServer, so it
// sees GET bodies after every other overlay has shaped them and POST bodies
// before any typed handler consumes them. The layer is always on: enforcement
// only bites a POST that carries the header, so a writer that sends none is
// unaffected, and that is precisely the writer the ledger exists to expose.
//
// Simplifications, each deliberate: a write with an unchanged body still
// bumps the version (the provider reads immediately before every write, so
// this can never cause a false 412); GraphQL mutations that change an aspect
// do not bump its version (same reason); and an aspect the typed handler
// serves but no POST through this layer created is taken to be at version 1,
// which is the server's own fallback for rows written before versions existed
// and is what lets GraphQL-created domains, glossary entities and tags be
// written conditionally.
type aspectVersions struct {
	mu sync.Mutex
	// versions maps urn -> aspect -> stored version.
	versions map[string]map[string]int
	// armed maps urn -> aspect -> a pending out-of-band write, applied after
	// the next GET that serves that aspect so the following POST conflicts.
	armed map[string]map[string]armedConflict
	// ledger records every aspect of every POST under /openapi/v3/entity/.
	ledger []AspectWriteRecord
	// unmanaged is the overlay a concurrent edit's field is seeded through.
	unmanaged *unmanagedAspectFields
}

type armedConflict struct {
	Field string
	Value any
}

// AspectWriteRecord is one aspect of one OpenAPI v3 POST as the mock saw it.
type AspectWriteRecord struct {
	Seq    int    `json:"seq"`
	URN    string `json:"urn"`
	Aspect string `json:"aspect"`
	// HeaderPresent reports whether the aspect carried If-Version-Match.
	HeaderPresent bool `json:"headerPresent"`
	// Header is the If-Version-Match value sent, or "" if absent.
	Header string `json:"header"`
	// Actual is the version the mock held when the POST arrived ("-1" if the
	// aspect did not exist).
	Actual string `json:"actual"`
	// Status is the HTTP status the POST received.
	Status int `json:"status"`
}

const versionAbsent = "-1"

func (v *aspectVersions) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/openapi/v3/entity/") {
			next.ServeHTTP(w, r)
			return
		}
		switch r.Method {
		case http.MethodGet:
			rec := httptest.NewRecorder()
			next.ServeHTTP(rec, r)
			out := rec.Body.Bytes()
			if rec.Code == http.StatusOK {
				out = v.afterRead(out, r.URL.Query().Get("systemMetadata") == "true")
			}
			copyRecorded(w, rec, out)
		case http.MethodPost:
			body, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			// Resolve every aspect's stored version before the typed handler
			// runs, so the precondition check, the ledger and the bump all
			// see the pre-write state.
			posted := parsePosted(body)
			actuals := make([]string, len(posted))
			for i, p := range posted {
				actuals[i] = v.current(next, p.urn, p.aspect)
			}
			if status, resp := checkPreconditions(posted, actuals); status != 0 {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				_, _ = w.Write(resp)
				v.record(posted, actuals, status)
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
			rec := httptest.NewRecorder()
			next.ServeHTTP(rec, r)
			v.record(posted, actuals, rec.Code)
			if rec.Code < http.StatusBadRequest {
				v.bump(posted)
			}
			copyRecorded(w, rec, rec.Body.Bytes())
		default:
			next.ServeHTTP(w, r)
		}
	})
}

// afterRead stamps systemMetadata.version on every aspect in a successful
// entity GET when the caller asked for it, records version 1 for any aspect
// served that this layer has not seen written, and then applies any armed
// concurrent edit so the caller's next write conflicts.
func (v *aspectVersions) afterRead(body []byte, withSystemMetadata bool) []byte {
	var entity map[string]json.RawMessage
	if err := json.Unmarshal(body, &entity); err != nil {
		return body
	}
	var urn string
	if err := json.Unmarshal(entity["urn"], &urn); err != nil || urn == "" {
		return body
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.versions == nil {
		v.versions = map[string]map[string]int{}
	}
	if v.versions[urn] == nil {
		v.versions[urn] = map[string]int{}
	}
	changed := false
	for aspect, raw := range entity {
		if aspect == "urn" {
			continue
		}
		if _, ok := v.versions[urn][aspect]; !ok {
			v.versions[urn][aspect] = 1
		}
		if !withSystemMetadata {
			continue
		}
		var a map[string]any
		if err := decodeExact(raw, &a); err != nil {
			continue
		}
		a["systemMetadata"] = map[string]any{"version": strconv.Itoa(v.versions[urn][aspect])}
		out, err := json.Marshal(a)
		if err != nil {
			continue
		}
		entity[aspect] = out
		changed = true
	}
	// An armed conflict stands in for a write that lands between the
	// provider's read-for-write and its write. The read-for-write is the one
	// that asks for systemMetadata; Terraform's refresh reads do not, and
	// firing on those would hand the writer the bumped version and no
	// conflict at all.
	if !withSystemMetadata {
		return body
	}
	for aspect, arm := range v.armed[urn] {
		if _, served := entity[aspect]; !served {
			continue
		}
		v.versions[urn][aspect]++
		if arm.Field != "" && v.unmanaged != nil {
			v.unmanaged.seed(urn, aspect, arm.Field, arm.Value)
		}
		delete(v.armed[urn], aspect)
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

type postedAspect struct {
	urn, aspect   string
	header        string
	headerPresent bool
}

func parsePosted(body []byte) []postedAspect {
	var entities []map[string]json.RawMessage
	if err := json.Unmarshal(body, &entities); err != nil {
		return nil
	}
	var out []postedAspect
	for _, entity := range entities {
		var urn string
		if err := json.Unmarshal(entity["urn"], &urn); err != nil {
			continue
		}
		aspects := make([]string, 0, len(entity))
		for aspect := range entity {
			if aspect != "urn" {
				aspects = append(aspects, aspect)
			}
		}
		sort.Strings(aspects)
		for _, aspect := range aspects {
			var a struct {
				Headers map[string]string `json:"headers"`
			}
			_ = json.Unmarshal(entity[aspect], &a)
			p := postedAspect{urn: urn, aspect: aspect}
			for k, val := range a.Headers {
				if strings.EqualFold(k, "If-Version-Match") {
					p.header, p.headerPresent = val, true
				}
			}
			out = append(out, p)
		}
	}
	return out
}

// current returns the version this layer holds for an aspect. The typed
// handler is consulted through a shadow read every time: an aspect it no
// longer serves has been deleted (a GraphQL delete mutation, or a DELETE,
// neither of which this layer sees), and a deleted aspect is absent again
// whatever version it last had, as on the server. One it serves that this
// layer has never seen written is at the server's fallback version 1. The
// shadow read bypasses wrap, so it neither stamps nor consumes an armed
// conflict.
func (v *aspectVersions) current(next http.Handler, urn, aspect string) string {
	path := "/openapi/v3/entity/" + entityPathOf(urn) + "/" + url.PathEscape(urn) + "?aspects=" + url.QueryEscape(aspect)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	next.ServeHTTP(rec, req)
	present := false
	if rec.Code == http.StatusOK {
		var entity map[string]json.RawMessage
		if err := json.Unmarshal(rec.Body.Bytes(), &entity); err == nil {
			_, present = entity[aspect]
		}
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if !present {
		delete(v.versions[urn], aspect)
		return versionAbsent
	}
	if v.versions == nil {
		v.versions = map[string]map[string]int{}
	}
	if v.versions[urn] == nil {
		v.versions[urn] = map[string]int{}
	}
	if n, ok := v.versions[urn][aspect]; ok {
		return strconv.Itoa(n)
	}
	v.versions[urn][aspect] = 1
	return "1"
}

// entityPathOf maps a URN to the lowercase entity path the mock routes on.
func entityPathOf(urn string) string {
	parts := strings.SplitN(urn, ":", 4)
	if len(parts) < 3 {
		return ""
	}
	return strings.ToLower(parts[2])
}

// checkPreconditions returns a 412 status and body if any aspect in the POST
// carries an If-Version-Match that disagrees with the stored version, in the
// shape the real server uses. Zero means the write may proceed.
func checkPreconditions(posted []postedAspect, actuals []string) (int, []byte) {
	for i, p := range posted {
		if !p.headerPresent {
			continue
		}
		actual := actuals[i]
		if p.header == actual {
			continue
		}
		msg := fmt.Sprintf("Failed to validate MCP due to: ValidationExceptionCollection{EntityAspect:(%s,%s) "+
			"Exceptions: [AspectValidationException(item=ChangeItemImpl{changeType=UPSERT}, changeType=UPSERT, "+
			"entityUrn=%s, aspectName=%s, subType=PRECONDITION, msg=Expected version %s, actual version %s)]}",
			p.urn, p.aspect, p.urn, p.aspect, p.header, actual)
		resp, _ := json.Marshal(map[string]string{"error": "Precondition Error", "message": msg})
		return http.StatusPreconditionFailed, resp
	}
	return 0, nil
}

func (v *aspectVersions) record(posted []postedAspect, actuals []string, status int) {
	v.mu.Lock()
	defer v.mu.Unlock()
	for i, p := range posted {
		v.ledger = append(v.ledger, AspectWriteRecord{
			Seq: len(v.ledger) + 1, URN: p.urn, Aspect: p.aspect,
			HeaderPresent: p.headerPresent, Header: p.header, Actual: actuals[i], Status: status,
		})
	}
}

// bump advances the version of every aspect a successful POST carried.
func (v *aspectVersions) bump(posted []postedAspect) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.versions == nil {
		v.versions = map[string]map[string]int{}
	}
	for _, p := range posted {
		if v.versions[p.urn] == nil {
			v.versions[p.urn] = map[string]int{}
		}
		v.versions[p.urn][p.aspect]++
	}
}

type armConflictRequest struct {
	URN    string `json:"urn"`
	Aspect string `json:"aspect"`
	Field  string `json:"field"`
	Value  any    `json:"value"`
}

// handleArmConflict serves POST /test-control/arm-aspect-conflict: after the
// next GET that serves the named aspect, the mock bumps its version and, when
// field is set, seeds field=value on it, as if another producer wrote the
// aspect between the caller's read and its write.
func (v *aspectVersions) handleArmConflict(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req armConflictRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.URN == "" || req.Aspect == "" {
		http.Error(w, "urn and aspect are required", http.StatusBadRequest)
		return
	}
	v.mu.Lock()
	if v.armed == nil {
		v.armed = map[string]map[string]armedConflict{}
	}
	if v.armed[req.URN] == nil {
		v.armed[req.URN] = map[string]armedConflict{}
	}
	v.armed[req.URN][req.Aspect] = armedConflict{Field: req.Field, Value: req.Value}
	v.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

// handleLedger serves GET /test-control/aspect-write-ledger?urn=&aspect=
// (either filter optional) and DELETE to clear it.
func (v *aspectVersions) handleLedger(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		q := r.URL.Query()
		v.mu.Lock()
		out := make([]AspectWriteRecord, 0, len(v.ledger))
		for _, rec := range v.ledger {
			if (q.Get("urn") == "" || rec.URN == q.Get("urn")) && (q.Get("aspect") == "" || rec.Aspect == q.Get("aspect")) {
				out = append(out, rec)
			}
		}
		v.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	case http.MethodDelete:
		v.mu.Lock()
		v.ledger = nil
		v.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// ArmConcurrentAspectWrite makes the mock at baseURL behave as if another
// producer wrote aspect on urn between the provider's next read of it and the
// write that follows: the version advances and, when field is non-empty,
// field=value appears on the aspect. The provider's write then meets a 412
// and must re-read, re-merge and retry. Mock-only.
func ArmConcurrentAspectWrite(baseURL, urn, aspect, field string, value any) {
	body, err := json.Marshal(armConflictRequest{URN: urn, Aspect: aspect, Field: field, Value: value})
	if err != nil {
		panic(fmt.Sprintf("ArmConcurrentAspectWrite: %v", err))
	}
	resp, err := http.Post(baseURL+"/test-control/arm-aspect-conflict", "application/json", bytes.NewReader(body)) //nolint:noctx
	if err != nil {
		panic(fmt.Sprintf("ArmConcurrentAspectWrite: %v", err))
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		panic(fmt.Sprintf("ArmConcurrentAspectWrite: unexpected status %d", resp.StatusCode))
	}
}

// AspectWriteLedger returns every OpenAPI v3 aspect POST the mock at baseURL
// has seen for urn and aspect (either may be empty to match all). Mock-only.
func AspectWriteLedger(baseURL, urn, aspect string) ([]AspectWriteRecord, error) {
	q := url.Values{"urn": {urn}, "aspect": {aspect}}.Encode()
	resp, err := http.Get(baseURL + "/test-control/aspect-write-ledger?" + q) //nolint:noctx
	if err != nil {
		return nil, fmt.Errorf("reading aspect write ledger: %w", err)
	}
	defer resp.Body.Close()
	var out []AspectWriteRecord
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decoding aspect write ledger: %w", err)
	}
	return out, nil
}

// CheckAspectWriteVersioned asserts that every POST of aspect on urn carried
// If-Version-Match equal to the version the mock held, so the write could
// not have overwritten a change it had not read. Mock-only.
func CheckAspectWriteVersioned(urn, aspect string) func(*terraform.State) error {
	return func(*terraform.State) error {
		recs, err := AspectWriteLedger(os.Getenv("DATAHUB_GMS_URL"), urn, aspect)
		if err != nil {
			return err
		}
		if len(recs) == 0 {
			return fmt.Errorf("no OpenAPI v3 write of %s on %s was recorded", aspect, urn)
		}
		for _, rec := range recs {
			if !rec.HeaderPresent {
				return fmt.Errorf("write #%d of %s on %s carried no If-Version-Match: a read-modify-write "+
					"without the version it read can overwrite a concurrent change unseen", rec.Seq, aspect, urn)
			}
			if rec.Status < http.StatusBadRequest && rec.Header != rec.Actual {
				return fmt.Errorf("write #%d of %s on %s succeeded with If-Version-Match %s against stored "+
					"version %s; the mock's precondition check is broken", rec.Seq, aspect, urn, rec.Header, rec.Actual)
			}
		}
		return nil
	}
}

// CheckAspectWriteRecovered asserts that the writes of aspect on urn show a
// 412 followed by a successful write whose If-Version-Match equals the
// version the mock then held: the provider met the armed conflict, re-read,
// and retried with the fresh version. Mock-only.
func CheckAspectWriteRecovered(urn, aspect string) func(*terraform.State) error {
	return func(*terraform.State) error {
		recs, err := AspectWriteLedger(os.Getenv("DATAHUB_GMS_URL"), urn, aspect)
		if err != nil {
			return err
		}
		sawConflict := false
		for _, rec := range recs {
			if !rec.HeaderPresent {
				return fmt.Errorf("write #%d of %s on %s carried no If-Version-Match", rec.Seq, aspect, urn)
			}
			switch {
			case rec.Status == http.StatusPreconditionFailed:
				sawConflict = true
			case rec.Status < http.StatusBadRequest && sawConflict:
				if rec.Header != rec.Actual {
					return fmt.Errorf("retry #%d of %s on %s sent version %s but the mock held %s", rec.Seq, aspect, urn, rec.Header, rec.Actual)
				}
				return nil
			}
		}
		if !sawConflict {
			return fmt.Errorf("no write of %s on %s met the armed conflict (412); records: %+v", aspect, urn, recs)
		}
		return fmt.Errorf("the write of %s on %s met a 412 but no successful retry followed; records: %+v", aspect, urn, recs)
	}
}
