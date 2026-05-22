// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahub

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewDatasourceIngestion(t *testing.T) {
	minimalRecipe := `{"source":{"type":"file","config":{"filename":"/tmp/test.json"}}}`
	baseInput := DatasourceIngestionInput{
		SourceID:   "src-abc123",
		SourceName: "Test Source",
		SourceType: "file",
		RecipeJSON: &minimalRecipe,
	}

	t.Run("success_sends_correct_shape", func(t *testing.T) {
		var gotMethod, gotPath, gotAuth string
		var gotBody []byte
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotMethod = r.Method
			gotPath = r.URL.Path + "?" + r.URL.RawQuery
			gotAuth = r.Header.Get("Authorization")
			gotBody, _ = io.ReadAll(r.Body)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(gotBody)
		}))
		defer server.Close()

		c := newTestClient(t, server)
		_, err := c.NewDatasourceIngestion(t.Context(), baseInput)
		if err != nil {
			t.Fatalf("NewDatasourceIngestion() error = %v", err)
		}
		if gotMethod != http.MethodPost {
			t.Errorf("method = %q, want POST", gotMethod)
		}
		if !strings.Contains(gotPath, "datahubingestionsource") {
			t.Errorf("path = %q, want datahubingestionsource path", gotPath)
		}
		if !strings.HasPrefix(gotAuth, "Bearer ") {
			t.Errorf("Authorization = %q, want Bearer prefix", gotAuth)
		}

		// Body must be a JSON array containing one entity with the expected URN.
		var entities []IngestionSource
		if err := json.Unmarshal(gotBody, &entities); err != nil {
			t.Fatalf("body is not a JSON array of IngestionSource: %v\nbody: %s", err, gotBody)
		}
		if len(entities) != 1 {
			t.Fatalf("entity count = %d, want 1", len(entities))
		}
		if entities[0].Urn != "urn:li:dataHubIngestionSource:src-abc123" {
			t.Errorf("URN = %q, want urn:li:dataHubIngestionSource:src-abc123", entities[0].Urn)
		}
	})

	t.Run("success_with_schedule", func(t *testing.T) {
		var gotBody []byte
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotBody, _ = io.ReadAll(r.Body)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(gotBody)
		}))
		defer server.Close()

		cronInterval := "0 10 * * *"
		timezone := "UTC"
		in := DatasourceIngestionInput{
			SourceID:     "sched-src",
			SourceName:   "Scheduled Source",
			SourceType:   "file",
			RecipeJSON:   &minimalRecipe,
			CronInterval: &cronInterval,
			Timezone:     &timezone,
		}
		c := newTestClient(t, server)
		if _, err := c.NewDatasourceIngestion(t.Context(), in); err != nil {
			t.Fatalf("error = %v", err)
		}

		var entities []IngestionSource
		if err := json.Unmarshal(gotBody, &entities); err != nil {
			t.Fatalf("unmarshal response: %v", err)
		}
		sched := entities[0].DataHubIngestionSourceInfo.Value.Schedule
		if sched == nil {
			t.Fatal("schedule is nil, want non-nil")
		}
		if sched.Interval != "0 10 * * *" {
			t.Errorf("interval = %q, want 0 10 * * *", sched.Interval)
		}
		if sched.Timezone != "UTC" {
			t.Errorf("timezone = %q, want UTC", sched.Timezone)
		}
	})

	t.Run("schedule_defaults_timezone_to_utc", func(t *testing.T) {
		var gotBody []byte
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotBody, _ = io.ReadAll(r.Body)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(gotBody)
		}))
		defer server.Close()

		cronInterval := "0 10 * * *"
		in := DatasourceIngestionInput{
			SourceID:     "sched-src-2",
			SourceName:   "Scheduled Source 2",
			SourceType:   "file",
			RecipeJSON:   &minimalRecipe,
			CronInterval: &cronInterval,
		}
		c := newTestClient(t, server)
		if _, err := c.NewDatasourceIngestion(t.Context(), in); err != nil {
			t.Fatalf("error = %v", err)
		}

		var entities []IngestionSource
		if err := json.Unmarshal(gotBody, &entities); err != nil {
			t.Fatalf("unmarshal response: %v", err)
		}
		sched := entities[0].DataHubIngestionSourceInfo.Value.Schedule
		if sched == nil || sched.Timezone != "UTC" {
			t.Errorf("expected timezone=UTC when unset, got %v", sched)
		}
	})

	t.Run("nil_recipe_returns_error", func(t *testing.T) {
		c := newTestClient(t, httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {})))
		in := DatasourceIngestionInput{SourceID: "x", SourceName: "x", SourceType: "file", RecipeJSON: nil}
		_, err := c.NewDatasourceIngestion(t.Context(), in)
		if err == nil {
			t.Fatal("expected error for nil recipe, got nil")
		}
	})

	t.Run("missing_source_id_returns_error", func(t *testing.T) {
		c := newTestClient(t, httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {})))
		in := DatasourceIngestionInput{SourceName: "x", SourceType: "file", RecipeJSON: &minimalRecipe}
		_, err := c.NewDatasourceIngestion(t.Context(), in)
		if err == nil {
			t.Fatal("expected error for empty source_id, got nil")
		}
	})

	t.Run("server_400_returns_error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "bad request", http.StatusBadRequest)
		}))
		defer server.Close()
		c := newTestClient(t, server)
		_, err := c.NewDatasourceIngestion(t.Context(), baseInput)
		if err == nil {
			t.Fatal("expected error for 400 response, got nil")
		}
	})
}

func TestGetIngestionSourceByID(t *testing.T) {
	sampleBody := `{"urn":"urn:li:dataHubIngestionSource:src-abc123","dataHubIngestionSourceInfo":{"value":{"name":"Test","type":"file","config":{"recipe":"{}"}}}}`

	t.Run("success", func(t *testing.T) {
		var gotPath, gotMethod string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			gotMethod = r.Method
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(sampleBody))
		}))
		defer server.Close()

		c := newTestClient(t, server)
		body, err := c.GetIngestionSourceByID(t.Context(), "src-abc123")
		if err != nil {
			t.Fatalf("GetIngestionSourceByID() error = %v", err)
		}
		if !strings.Contains(string(body), "Test") {
			t.Errorf("body = %q, expected to contain 'Test'", body)
		}
		if gotMethod != http.MethodGet {
			t.Errorf("method = %q, want GET", gotMethod)
		}
		if !strings.Contains(gotPath, "src-abc123") {
			t.Errorf("path = %q, want to contain source ID", gotPath)
		}
		if !strings.Contains(gotPath, "urn:li:dataHubIngestionSource") {
			t.Errorf("path = %q, want full URN in path", gotPath)
		}
	})

	t.Run("server_404_returns_error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.NotFound(w, nil)
		}))
		defer server.Close()
		c := newTestClient(t, server)
		_, err := c.GetIngestionSourceByID(t.Context(), "missing")
		if err == nil {
			t.Fatal("expected error for 404, got nil")
		}
	})
}

func TestDeleteIngestionSourceByID(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		var gotMethod, gotPath string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotMethod = r.Method
			gotPath = r.URL.Path
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		c := newTestClient(t, server)
		if err := c.DeleteIngestionSourceByID(t.Context(), "src-to-delete"); err != nil {
			t.Fatalf("DeleteIngestionSourceByID() error = %v", err)
		}
		if gotMethod != http.MethodDelete {
			t.Errorf("method = %q, want DELETE", gotMethod)
		}
		if !strings.Contains(gotPath, "src-to-delete") {
			t.Errorf("path = %q, want to contain source ID", gotPath)
		}
	})

	t.Run("server_error_returns_error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "internal error", http.StatusInternalServerError)
		}))
		defer server.Close()
		c := newTestClient(t, server)
		if err := c.DeleteIngestionSourceByID(t.Context(), "src"); err == nil {
			t.Fatal("expected error for 500 response, got nil")
		}
	})
}
