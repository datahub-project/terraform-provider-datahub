// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahubtesting

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// These tests pin the version semantics the mock gives every aspect, which the
// provider's conditional writes and the acceptance-level checks both rely on.

type versionProbe struct {
	t    *testing.T
	base string
}

func (p versionProbe) do(method, path string, body any) (int, []byte) {
	p.t.Helper()
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			p.t.Fatal(err)
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, p.base+path, reader) //nolint:noctx
	if err != nil {
		p.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		p.t.Fatal(err)
	}
	defer res.Body.Close()
	out, _ := io.ReadAll(res.Body)
	return res.StatusCode, out
}

func (p versionProbe) version(urn, aspect string) string {
	p.t.Helper()
	path := "/openapi/v3/entity/" + entityPathOf(urn) + "/" + urn + "?aspects=" + aspect + "&systemMetadata=true"
	status, body := p.do(http.MethodGet, path, nil)
	if status != http.StatusOK {
		return versionAbsent
	}
	var entity map[string]json.RawMessage
	if err := json.Unmarshal(body, &entity); err != nil {
		p.t.Fatalf("decoding %s: %v", path, err)
	}
	var a struct {
		SystemMetadata struct {
			Version string `json:"version"`
		} `json:"systemMetadata"`
	}
	if raw, ok := entity[aspect]; ok {
		_ = json.Unmarshal(raw, &a)
	}
	return a.SystemMetadata.Version
}

func ownershipTypeWrite(urn, name, ifVersionMatch string) []map[string]any {
	aspect := map[string]any{"value": map[string]any{"name": name}}
	if ifVersionMatch != "" {
		aspect["headers"] = map[string]string{"If-Version-Match": ifVersionMatch}
	}
	return []map[string]any{{"urn": urn, "ownershipTypeInfo": aspect}}
}

func TestAspectVersions_StampsAndEnforces(t *testing.T) {
	srv := NewServer(t)
	p := versionProbe{t: t, base: srv.URL}
	const urn = "urn:li:ownershipType:versioned"
	const post = "/openapi/v3/entity/ownershiptype?async=false"

	if got := p.version(urn, "ownershipTypeInfo"); got != versionAbsent {
		t.Fatalf("absent entity reports version %q, want %q", got, versionAbsent)
	}
	if status, _ := p.do(http.MethodPost, post, ownershipTypeWrite(urn, "one", "")); status != http.StatusOK {
		t.Fatalf("unconditional first write: %d", status)
	}
	if got := p.version(urn, "ownershipTypeInfo"); got != "1" {
		t.Fatalf("after first write version %q, want 1", got)
	}
	_, body := p.do(http.MethodGet, "/openapi/v3/entity/ownershiptype/"+urn+"?aspects=ownershipTypeInfo", nil)
	if strings.Contains(string(body), "systemMetadata") {
		t.Errorf("GET without systemMetadata=true must not carry it: %s", body)
	}

	if status, _ := p.do(http.MethodPost, post, ownershipTypeWrite(urn, "two", "1")); status != http.StatusOK {
		t.Fatalf("matching conditional write: %d", status)
	}
	if got := p.version(urn, "ownershipTypeInfo"); got != "2" {
		t.Fatalf("after matching write version %q, want 2", got)
	}

	status, body := p.do(http.MethodPost, post, ownershipTypeWrite(urn, "stale", "1"))
	if status != http.StatusPreconditionFailed {
		t.Fatalf("stale conditional write: %d %s, want 412", status, body)
	}
	var e struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &e); err != nil || e.Error != "Precondition Error" ||
		!strings.Contains(e.Message, "Expected version 1, actual version 2") ||
		!strings.Contains(e.Message, "aspectName=ownershipTypeInfo") {
		t.Errorf("412 body does not match the server's shape: %s", body)
	}
	_, body = p.do(http.MethodGet, "/openapi/v3/entity/ownershiptype/"+urn, nil)
	if !strings.Contains(string(body), `"name":"two"`) {
		t.Errorf("rejected write reached the typed store: %s", body)
	}
	if got := p.version(urn, "ownershipTypeInfo"); got != "2" {
		t.Errorf("rejected write changed the version to %q", got)
	}

	if status, _ := p.do(http.MethodPost, post, ownershipTypeWrite(urn, "dup", versionAbsent)); status != http.StatusPreconditionFailed {
		t.Errorf("If-Version-Match -1 on an existing aspect: %d, want 412", status)
	}
	const fresh = "urn:li:ownershipType:fresh"
	if status, _ := p.do(http.MethodPost, post, ownershipTypeWrite(fresh, "new", versionAbsent)); status != http.StatusOK {
		t.Errorf("If-Version-Match -1 on an absent aspect: %d, want 200", status)
	}
	if got := p.version(fresh, "ownershipTypeInfo"); got != "1" {
		t.Errorf("conditional create left version %q, want 1", got)
	}
}

func TestAspectVersions_TypedAspectsDefaultToOne(t *testing.T) {
	srv := NewServer(t)
	p := versionProbe{t: t, base: srv.URL}
	status, _ := p.do(http.MethodPost, "/api/graphql", map[string]any{
		"query":     "mutation createDomain($input: CreateDomainInput!) { createDomain(input: $input) }",
		"variables": map[string]any{"input": map[string]any{"id": "gql-made", "name": "GQL"}},
	})
	if status != http.StatusOK {
		t.Fatalf("createDomain: %d", status)
	}
	const urn = "urn:li:domain:gql-made"
	if got := p.version(urn, "domainProperties"); got != "1" {
		t.Fatalf("an aspect the typed handler serves but no POST created reports %q, want 1 (the server's fallback)", got)
	}
	body := []map[string]any{{"urn": urn, "domainProperties": map[string]any{
		"value":   map[string]any{"name": "GQL renamed"},
		"headers": map[string]string{"If-Version-Match": "1"},
	}}}
	if status, resp := p.do(http.MethodPost, "/openapi/v3/entity/domain?async=false", body); status != http.StatusOK {
		t.Fatalf("conditional write against the fallback version: %d %s", status, resp)
	}
	if got := p.version(urn, "domainProperties"); got != "2" {
		t.Errorf("version after write %q, want 2", got)
	}
}

func TestAspectVersions_SeedersDoNotBump(t *testing.T) {
	srv := NewServer(t)
	p := versionProbe{t: t, base: srv.URL}
	const urn = "urn:li:ownershipType:seeded"
	p.do(http.MethodPost, "/openapi/v3/entity/ownershiptype?async=false", ownershipTypeWrite(urn, "one", ""))
	SeedUnmanagedAspectField(srv.URL, urn, "ownershipTypeInfo", "extra", "x")
	if got := p.version(urn, "ownershipTypeInfo"); got != "1" {
		t.Errorf("seeding an unmanaged field bumped the version to %q; seeds describe state that was already true", got)
	}
	// A seeded aspect the typed handler does not serve exists from the
	// provider's point of view, at the fallback version.
	SeedUnmanagedAspectField(srv.URL, urn, "status", "removed", false)
	if got := p.version(urn, "status"); got != "1" {
		t.Errorf("seeded-only aspect reports %q, want 1", got)
	}
}

func TestAspectVersions_ArmedConflictAndLedger(t *testing.T) {
	srv := NewServer(t)
	t.Setenv("DATAHUB_GMS_URL", srv.URL)
	p := versionProbe{t: t, base: srv.URL}
	const urn = "urn:li:ownershipType:raced"
	const post = "/openapi/v3/entity/ownershiptype?async=false"
	p.do(http.MethodPost, post, ownershipTypeWrite(urn, "one", versionAbsent))

	ArmConcurrentAspectWrite(srv.URL, urn, "ownershipTypeInfo", "theirs", "edit")
	read := p.version(urn, "ownershipTypeInfo")
	if read != "1" {
		t.Fatalf("the read that triggers the armed conflict must still see the old version, got %q", read)
	}
	if status, _ := p.do(http.MethodPost, post, ownershipTypeWrite(urn, "mine", read)); status != http.StatusPreconditionFailed {
		t.Fatalf("write after the armed conflict: %d, want 412", status)
	}
	_, body := p.do(http.MethodGet, "/openapi/v3/entity/ownershiptype/"+urn+"?aspects=ownershipTypeInfo&systemMetadata=true", nil)
	if !strings.Contains(string(body), `"theirs":"edit"`) {
		t.Errorf("the concurrent writer's field is missing from the re-read: %s", body)
	}
	reread := p.version(urn, "ownershipTypeInfo")
	if reread != "2" {
		t.Fatalf("re-read version %q, want 2", reread)
	}
	if status, _ := p.do(http.MethodPost, post, ownershipTypeWrite(urn, "mine", reread)); status != http.StatusOK {
		t.Fatalf("retry with the fresh version: %d", status)
	}
	if err := CheckAspectWriteRecovered(urn, "ownershipTypeInfo")(nil); err != nil {
		t.Errorf("CheckAspectWriteRecovered: %v", err)
	}
	if err := CheckAspectWriteVersioned(urn, "ownershipTypeInfo")(nil); err != nil {
		t.Errorf("CheckAspectWriteVersioned: %v", err)
	}
	p.do(http.MethodPost, post, ownershipTypeWrite(urn, "unguarded", ""))
	if err := CheckAspectWriteVersioned(urn, "ownershipTypeInfo")(nil); err == nil {
		t.Errorf("CheckAspectWriteVersioned must fail once a write without If-Version-Match is recorded")
	}

	recs, err := AspectWriteLedger(srv.URL, urn, "ownershipTypeInfo")
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		present bool
		header  string
		actual  string
		status  int
	}{
		{true, versionAbsent, versionAbsent, http.StatusOK},
		{true, "1", "2", http.StatusPreconditionFailed},
		{true, "2", "2", http.StatusOK},
		{false, "", "3", http.StatusOK},
	}
	if len(recs) != len(want) {
		t.Fatalf("ledger has %d records, want %d: %+v", len(recs), len(want), recs)
	}
	for i, w := range want {
		r := recs[i]
		if r.HeaderPresent != w.present || r.Header != w.header || r.Actual != w.actual || r.Status != w.status {
			t.Errorf("ledger[%d] = %+v, want present=%v header=%q actual=%q status=%d", i, r, w.present, w.header, w.actual, w.status)
		}
	}
}

func TestAspectVersions_BatchFailsWhole(t *testing.T) {
	srv := NewServer(t)
	p := versionProbe{t: t, base: srv.URL}
	const urn = "urn:li:structuredProperty:io.batch"
	const post = "/openapi/v3/entity/structuredproperty?async=false"
	def := map[string]any{"qualifiedName": "io.batch", "valueType": "urn:li:dataType:datahub.string", "entityTypes": []string{"urn:li:entityType:datahub.dataset"}, "version": "20260101000000"}
	first := []map[string]any{{"urn": urn,
		"propertyDefinition":         map[string]any{"value": def},
		"structuredPropertySettings": map[string]any{"value": map[string]any{"isHidden": false}},
	}}
	if status, body := p.do(http.MethodPost, post, first); status != http.StatusOK {
		t.Fatalf("seed write: %d %s", status, body)
	}
	stale := []map[string]any{{"urn": urn,
		"propertyDefinition":         map[string]any{"value": def, "headers": map[string]string{"If-Version-Match": "1"}},
		"structuredPropertySettings": map[string]any{"value": map[string]any{"isHidden": true}, "headers": map[string]string{"If-Version-Match": "7"}},
	}}
	if status, _ := p.do(http.MethodPost, post, stale); status != http.StatusPreconditionFailed {
		t.Fatalf("batch with one stale aspect: %d, want 412", status)
	}
	if got := p.version(urn, "propertyDefinition"); got != "1" {
		t.Errorf("the valid half of a rejected batch bumped to %q; the server rolls back the whole request", got)
	}
	_, body := p.do(http.MethodGet, "/openapi/v3/entity/structuredproperty/"+urn, nil)
	if strings.Contains(string(body), `"isHidden":true`) {
		t.Errorf("the rejected batch's settings reached the typed store: %s", body)
	}
}
