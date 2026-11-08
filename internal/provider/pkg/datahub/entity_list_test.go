// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahub

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

// entityListServer serves a two-page scroll for one entity type and records
// every request's query parameters.
func entityListServer(t *testing.T, path string, pages []string) (*httptest.Server, *[]map[string]string) {
	t.Helper()
	var seen []map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != path {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		q := map[string]string{}
		for k := range r.URL.Query() {
			q[k] = r.URL.Query().Get(k)
		}
		seen = append(seen, q)
		idx := 0
		if q["scrollId"] == "page-2" {
			idx = 1
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(pages[idx]))
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

// TestListURNsBypassSearchCache pins the reason these lists use the OpenAPI v3
// scroll at all: every request must carry skipCache=true, or DataHub Cloud can
// answer a repeated enumeration from a search result cache that writes do not
// evict, hiding entities created minutes earlier.
func TestListURNsBypassSearchCache(t *testing.T) {
	cases := []struct {
		name   string
		path   string
		aspect string
		pages  []string
		list   func(c *Client) ([]string, error)
		want   []string
	}{
		{
			name:   "ingestion sources",
			path:   "/openapi/v3/entity/datahubingestionsource",
			aspect: "dataHubIngestionSourceInfo",
			pages: []string{
				`{"scrollId":"page-2","entities":[` +
					`{"urn":"urn:li:dataHubIngestionSource:a","dataHubIngestionSourceInfo":{"value":{"name":"a"}}},` +
					`{"urn":"urn:li:dataHubIngestionSource:skeleton"}]}`,
				`{"entities":[{"urn":"urn:li:dataHubIngestionSource:b","dataHubIngestionSourceInfo":{"value":{"name":"b"}}}]}`,
			},
			list: func(c *Client) ([]string, error) { return c.ListIngestionSourceURNs(t.Context()) },
			want: []string{"urn:li:dataHubIngestionSource:a", "urn:li:dataHubIngestionSource:b"},
		},
		{
			name:   "secrets",
			path:   "/openapi/v3/entity/datahubsecret",
			aspect: "dataHubSecretValue",
			pages: []string{
				`{"scrollId":"page-2","entities":[` +
					`{"urn":"urn:li:dataHubSecret:A","dataHubSecretValue":{"value":{"name":"A","value":"enc"}}},` +
					`{"urn":"urn:li:dataHubSecret:skeleton"}]}`,
				`{"entities":[{"urn":"urn:li:dataHubSecret:B","dataHubSecretValue":{"value":{"name":"B","value":"enc"}}}]}`,
			},
			list: func(c *Client) ([]string, error) { return c.ListSecretURNs(t.Context()) },
			want: []string{"urn:li:dataHubSecret:A", "urn:li:dataHubSecret:B"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, seen := entityListServer(t, tc.path, tc.pages)
			got, err := tc.list(newTestClient(t, srv))
			if err != nil {
				t.Fatalf("list: %v", err)
			}
			// The skeleton (no requested aspect) is dropped; both pages are read.
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			if len(*seen) != 2 {
				t.Fatalf("got %d requests, want 2 (one per page)", len(*seen))
			}
			for i, q := range *seen {
				if q["skipCache"] != "true" {
					t.Errorf("request %d: skipCache=%q, want \"true\"", i, q["skipCache"])
				}
				if q["aspects"] != tc.aspect {
					t.Errorf("request %d: aspects=%q, want %q", i, q["aspects"], tc.aspect)
				}
				if q["query"] != "*" {
					t.Errorf("request %d: query=%q, want \"*\"", i, q["query"])
				}
			}
			if (*seen)[1]["scrollId"] != "page-2" {
				t.Errorf("second request scrollId=%q, want the first page's", (*seen)[1]["scrollId"])
			}
		})
	}
}

func TestListURNsHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	_, err := newTestClient(t, srv).ListSecretURNs(t.Context())
	if err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Fatalf("got err %v, want an HTTP 500 error", err)
	}
}

func TestListURNsStopsOnEndlessScroll(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"scrollId":"again","entities":[{"urn":"urn:li:dataHubSecret:X","dataHubSecretValue":{"value":{}}}]}`))
	}))
	defer srv.Close()
	_, err := newTestClient(t, srv).ListSecretURNs(t.Context())
	if err == nil || !strings.Contains(err.Error(), "still paging") {
		t.Fatalf("got err %v, want the page-limit error", err)
	}
}
