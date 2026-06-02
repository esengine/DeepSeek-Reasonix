package config

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProviderEntryFetchModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"data": []map[string]string{
				{"id": "model-b", "object": "model"},
				{"id": "model-a", "object": "model"},
			},
		})
	}))
	defer srv.Close()

	t.Setenv("TEST_API_KEY", "sk-test")

	e := ProviderEntry{
		Name:      "test",
		Kind:      "openai",
		BaseURL:   srv.URL,
		APIKeyEnv: "TEST_API_KEY",
	}

	models, err := e.FetchModels(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("want 2 models, got %d", len(models))
	}
	if models[0] != "model-a" || models[1] != "model-b" {
		t.Errorf("want sorted [model-a model-b], got %v", models)
	}
}

func TestProviderEntryFetchModelsNoKey(t *testing.T) {
	e := ProviderEntry{
		Name:      "test",
		BaseURL:   "http://localhost",
		APIKeyEnv: "NONEXISTENT_KEY",
	}
	_, err := e.FetchModels(context.Background())
	if err == nil {
		t.Fatal("expected error for missing key")
	}
}

func TestProviderEntryRefreshModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"data": []map[string]string{
				{"id": "new-model", "object": "model"},
			},
		})
	}))
	defer srv.Close()

	t.Setenv("TEST_API_KEY", "sk-test")

	e := ProviderEntry{
		Name:      "test",
		Kind:      "openai",
		BaseURL:   srv.URL,
		APIKeyEnv: "TEST_API_KEY",
		Model:     "old-model",
		Models:    []string{"old-model"},
		Default:   "old-model",
	}

	models, changed, err := e.RefreshModels(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !changed {
		t.Error("expected changed=true")
	}
	if len(models) != 1 || models[0] != "new-model" {
		t.Errorf("want [new-model], got %v", models)
	}
	if e.Model != "new-model" {
		t.Errorf("want Model=new-model, got %s", e.Model)
	}
	if e.Default != "new-model" {
		t.Errorf("want Default=new-model, got %s", e.Default)
	}
}

func TestProviderEntryRefreshModelsKeepsDefault(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"data": []map[string]string{
				{"id": "keep-me", "object": "model"},
				{"id": "also-me", "object": "model"},
			},
		})
	}))
	defer srv.Close()

	t.Setenv("TEST_API_KEY", "sk-test")

	e := ProviderEntry{
		Name:      "test",
		BaseURL:   srv.URL,
		APIKeyEnv: "TEST_API_KEY",
		Models:    []string{"also-me", "keep-me"}, // sorted order (alphabetical)
		Default:   "keep-me",
	}

	_, changed, err := e.RefreshModels(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if changed {
		t.Error("expected changed=false (same models)")
	}
	if e.Default != "keep-me" {
		t.Errorf("want Default=keep-me, got %s", e.Default)
	}
}
