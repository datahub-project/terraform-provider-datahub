// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahub

import (
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
)

// assertion search fixture covering the filtering dimensions: source
// (NATIVE/EXTERNAL/INFERRED), type, and sub-shape.
const assertionSearchFixture = `{"data":{"searchAcrossEntities":{"total":8,"searchResults":[
  {"entity":{"urn":"urn:li:assertion:vol-native-total","info":{"type":"VOLUME","source":{"type":"NATIVE"},"volumeAssertion":{"type":"ROW_COUNT_TOTAL"}}}},
  {"entity":{"urn":"urn:li:assertion:vol-native-change","info":{"type":"VOLUME","source":{"type":"NATIVE"},"volumeAssertion":{"type":"ROW_COUNT_CHANGE"}}}},
  {"entity":{"urn":"urn:li:assertion:vol-external-total","info":{"type":"VOLUME","source":{"type":"EXTERNAL"},"volumeAssertion":{"type":"ROW_COUNT_TOTAL"}}}},
  {"entity":{"urn":"urn:li:assertion:fresh-native-fixed","info":{"type":"FRESHNESS","source":{"type":"NATIVE"},"freshnessAssertion":{"schedule":{"type":"FIXED_INTERVAL"}}}}},
  {"entity":{"urn":"urn:li:assertion:fresh-native-sincelast","info":{"type":"FRESHNESS","source":{"type":"NATIVE"},"freshnessAssertion":{"schedule":{"type":"SINCE_THE_LAST_CHECK"}}}}},
  {"entity":{"urn":"urn:li:assertion:sql-native-metric","info":{"type":"SQL","source":{"type":"NATIVE"},"sqlAssertion":{"type":"METRIC"}}}},
  {"entity":{"urn":"urn:li:assertion:dataset-external","info":{"type":"DATASET","source":{"type":"EXTERNAL"}}}},
  {"entity":{"urn":"urn:li:assertion:custom-external","info":{"type":"CUSTOM","source":{"type":"EXTERNAL"}}}}
]}}}`

func assertionFixtureServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(assertionSearchFixture))
	}))
}

func assertEqualURNs(t *testing.T, got, want []string) {
	t.Helper()
	g := append([]string(nil), got...)
	sort.Strings(g)
	sort.Strings(want)
	if strings.Join(g, ",") != strings.Join(want, ",") {
		t.Errorf("URNs = %v, want %v", g, want)
	}
}

func TestListVolumeAssertionURNs_NativeTotalAndChange(t *testing.T) {
	server := assertionFixtureServer(t)
	defer server.Close()
	got, err := newTestClient(t, server).ListVolumeAssertionURNs(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	// NATIVE volume assertions of either modeled sub-shape (ROW_COUNT_TOTAL and
	// ROW_COUNT_CHANGE); the EXTERNAL one is still excluded by source.
	assertEqualURNs(t, got, []string{
		"urn:li:assertion:vol-native-total",
		"urn:li:assertion:vol-native-change",
	})
}

func TestListFreshnessAssertionURNs_NativeSupportedScheduleOnly(t *testing.T) {
	server := assertionFixtureServer(t)
	defer server.Close()
	got, err := newTestClient(t, server).ListFreshnessAssertionURNs(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	// Excludes SINCE_THE_LAST_CHECK (unsupported schedule).
	assertEqualURNs(t, got, []string{"urn:li:assertion:fresh-native-fixed"})
}

func TestListSQLAssertionURNs_NativeMetricOnly(t *testing.T) {
	server := assertionFixtureServer(t)
	defer server.Close()
	got, err := newTestClient(t, server).ListSQLAssertionURNs(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	assertEqualURNs(t, got, []string{"urn:li:assertion:sql-native-metric"})
}

func TestListCustomAssertionURNs_TypeOnlyNotSourceFiltered(t *testing.T) {
	server := assertionFixtureServer(t)
	defer server.Close()
	got, err := newTestClient(t, server).ListCustomAssertionURNs(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	// Custom assertions are external-by-design: kept despite source==EXTERNAL.
	// The DATASET/EXTERNAL ingested test is excluded by the type filter.
	assertEqualURNs(t, got, []string{"urn:li:assertion:custom-external"})
}
