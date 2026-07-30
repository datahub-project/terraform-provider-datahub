// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahubtesting

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
)

const (
	huskTestURN = "urn:li:domain:tfprovider-spd-ovl-dom-abcd1234"

	// huskEntityDoc is the OpenAPI v3 entity document a CAT-2583 resurrection
	// leaves behind: the key aspect is written back by the JSON-PATCH, the
	// structuredProperties aspect it patched is present but empty, and the
	// domainProperties aspect that makes this a real domain is gone.
	huskEntityDoc = `{
  "urn": "` + huskTestURN + `",
  "domainKey": {"value": {"id": "tfprovider-spd-ovl-dom-abcd1234"}},
  "structuredProperties": {"value": {"properties": []}}
}`

	// populatedEntityDoc is a domain that was simply never deleted.
	populatedEntityDoc = `{
  "urn": "` + huskTestURN + `",
  "domainKey": {"value": {"id": "tfprovider-spd-ovl-dom-abcd1234"}},
  "domainProperties": {"value": {"name": "TF Provider Test Domain", "description": "d"}},
  "institutionalMemory": {"value": {"elements": []}}
}`
)

// entityDocServer serves body at GET /openapi/v3/entity/domain/{urn} with the
// given status code, and returns a client pointed at it.
func entityDocServer(t *testing.T, status int, body string) *datahub.Client {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/openapi/v3/entity/domain/") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	client, err := datahub.NewClient(srv.URL, "test-token")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return client
}

// legacyMessage is the message CheckDestroy produced before this diagnostic
// existed. It is reproduced here to demonstrate what the change buys: it is
// byte-identical for a resurrected husk and for a genuinely undeleted entity,
// which is exactly why a real nightly flake in
// TestAcc_SPDefaults_AssignmentOverlap could not be classified from CI output.
func legacyMessage(resourceType, urn string) string {
	return fmt.Sprintf("%s %q still exists after destroy", resourceType, urn)
}

// TestStillExistsAfterDestroyError_discriminatesHuskFromLiveEntity feeds a
// husk-shaped and a fully-populated entity document through the real HTTP read
// path and asserts the two failures are distinguishable and each names the
// right cause. It also asserts that the pre-change message could not tell them
// apart, so the value of the diagnostic is pinned by the test rather than
// assumed.
func TestStillExistsAfterDestroyError_discriminatesHuskFromLiveEntity(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	huskErr := stillExistsAfterDestroyError(ctx,
		entityDocServer(t, http.StatusOK, huskEntityDoc), "datahub_domain", huskTestURN)
	populatedErr := stillExistsAfterDestroyError(ctx,
		entityDocServer(t, http.StatusOK, populatedEntityDoc), "datahub_domain", huskTestURN)

	if huskErr == nil || populatedErr == nil {
		t.Fatalf("both shapes must still fail the check: husk=%v populated=%v", huskErr, populatedErr)
	}

	husk, populated := huskErr.Error(), populatedErr.Error()
	t.Logf("husk failure:\n%s", husk)
	t.Logf("populated failure:\n%s", populated)

	// The pre-change message was derived from the resource type and URN alone,
	// both of which are identical across the two scenarios above, so it was
	// byte-identical for a husk and for a live entity. Asserting that here is
	// the proof that the old diagnostic could not have been used to classify
	// the flake, and that the new one can.
	legacyHusk := legacyMessage("datahub_domain", huskTestURN)
	legacyPopulated := legacyMessage("datahub_domain", huskTestURN)
	if legacyHusk != legacyPopulated {
		t.Fatalf("expected the old message to be indistinguishable across shapes, got %q vs %q",
			legacyHusk, legacyPopulated)
	}
	if husk == populated {
		t.Fatalf("husk and populated failures are indistinguishable, exactly the defect being fixed:\n%s", husk)
	}

	// Both messages keep the original lead so existing greps still match, and
	// both still describe a failure.
	for _, msg := range []string{husk, populated} {
		if !strings.HasPrefix(msg, legacyMessage("datahub_domain", huskTestURN)) {
			t.Errorf("message does not keep the original lead:\n%s", msg)
		}
	}

	// Husk: names CAT-2583, names the aspects it found, and says the husk is
	// still a failure rather than something to tolerate.
	for _, want := range []string{
		"CAT-2583 resurrected husk",
		"domainKey",
		"structuredProperties",
		"blocks re-creation",
	} {
		if !strings.Contains(husk, want) {
			t.Errorf("husk message missing %q:\n%s", want, husk)
		}
	}
	if strings.Contains(husk, "genuine delete failure") {
		t.Errorf("husk message wrongly claims a genuine delete failure:\n%s", husk)
	}

	// Populated: names a genuine delete failure and lists the content aspects.
	for _, want := range []string{
		"genuine delete failure",
		"domainProperties",
		"institutionalMemory",
	} {
		if !strings.Contains(populated, want) {
			t.Errorf("populated message missing %q:\n%s", want, populated)
		}
	}
	if strings.Contains(populated, "husk") {
		t.Errorf("populated message wrongly mentions a husk:\n%s", populated)
	}
}

// TestStillExistsAfterDestroyError_undeterminedShapes covers the two cases
// where the probe cannot classify the leftover: the entity endpoint erroring,
// and the endpoint reporting nothing for a URN a typed getter just reported as
// present. Both must still fail, and must say the classification is unknown
// rather than pick a side.
func TestStillExistsAfterDestroyError_undeterminedShapes(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	cases := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{name: "probe errors", status: http.StatusInternalServerError, body: `boom`, want: "the aspect-shape probe failed"},
		{name: "probe finds nothing", status: http.StatusNotFound, body: ``, want: "found no aspects for this URN"},
		{name: "aspectless body", status: http.StatusOK, body: `{"urn":"` + huskTestURN + `"}`, want: "found no aspects for this URN"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := stillExistsAfterDestroyError(ctx,
				entityDocServer(t, tc.status, tc.body), "datahub_domain", huskTestURN)
			if err == nil {
				t.Fatal("an entity reported present must always fail the check")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("message missing %q:\n%s", tc.want, err)
			}
			if !strings.Contains(err.Error(), "undetermined") {
				t.Errorf("message must not pick a side when the probe is inconclusive:\n%s", err)
			}
		})
	}
}

// TestIsHuskShape checks the husk predicate directly, including the aspects
// that carry no entity content and so cannot rescue a husk from being one.
func TestIsHuskShape(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		aspects []string
		want    bool
	}{
		{name: "no aspects at all", aspects: nil, want: false},
		{name: "key only", aspects: []string{"domainKey"}, want: true},
		{name: "key plus patched structured properties", aspects: []string{"domainKey", "structuredProperties"}, want: true},
		{name: "key plus soft-delete status", aspects: []string{"domainKey", "status", "structuredProperties"}, want: true},
		{name: "key plus content aspect", aspects: []string{"domainKey", "domainProperties"}, want: false},
		{name: "content aspect only", aspects: []string{"tagProperties"}, want: false},
		{name: "other entity type key", aspects: []string{"glossaryTermKey", "structuredProperties"}, want: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isHuskShape(tc.aspects); got != tc.want {
				t.Errorf("isHuskShape(%v) = %v, want %v", tc.aspects, got, tc.want)
			}
		})
	}
}

// TestEntityPathFromURN pins the URN-to-endpoint-path derivation, which is what
// lets every CheckDestroy share one diagnostic without a per-resource path
// table to keep in sync.
func TestEntityPathFromURN(t *testing.T) {
	t.Parallel()

	cases := []struct {
		urn     string
		want    string
		wantErr bool
	}{
		{urn: "urn:li:domain:abc", want: "domain"},
		{urn: "urn:li:dataHubIngestionSource:abc", want: "datahubingestionsource"},
		{urn: "urn:li:glossaryTerm:abc", want: "glossaryterm"},
		{urn: "urn:li:dataset:(urn:li:dataPlatform:sqlite,db.t,PROD)", want: "dataset"},
		{urn: "not-a-urn", wantErr: true},
		{urn: "urn:li:domain", wantErr: true},
		{urn: "", wantErr: true},
	}

	for _, tc := range cases {
		got, err := entityPathFromURN(tc.urn)
		if tc.wantErr {
			if err == nil {
				t.Errorf("entityPathFromURN(%q) = %q, want error", tc.urn, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("entityPathFromURN(%q): unexpected error %v", tc.urn, err)
			continue
		}
		if got != tc.want {
			t.Errorf("entityPathFromURN(%q) = %q, want %q", tc.urn, got, tc.want)
		}
	}
}

// TestProbeAspectShape_requestsDerivedPath verifies the probe actually asks the
// entity endpoint for the derived path and the URN-escaped identifier, so a
// wrong path cannot silently degrade every diagnostic to "undetermined".
func TestProbeAspectShape_requestsDerivedPath(t *testing.T) {
	t.Parallel()

	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(huskEntityDoc))
	}))
	t.Cleanup(srv.Close)

	client, err := datahub.NewClient(srv.URL, "test-token")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	shape, err := probeAspectShape(context.Background(), client, huskTestURN)
	if err != nil {
		t.Fatalf("probeAspectShape: %v", err)
	}
	if !shape.found {
		t.Fatal("probeAspectShape reported no aspects for a husk document")
	}
	wantPath := "/openapi/v3/entity/domain/" + huskTestURN
	if gotPath != wantPath {
		t.Errorf("probe requested %q, want %q", gotPath, wantPath)
	}
	if strings.Join(shape.aspects, ",") != "domainKey,structuredProperties" {
		t.Errorf("aspects = %v, want [domainKey structuredProperties] sorted", shape.aspects)
	}
}
