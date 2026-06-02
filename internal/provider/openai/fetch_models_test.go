package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		json.NewEncoder(w).Encode(modelsResponse{
			Object: "list",
			Data: []modelEntry{
				{ID: "deepseek-v4-pro", Object: "model", OwnedBy: "deepseek"},
				{ID: "deepseek-v4-flash", Object: "model", OwnedBy: "deepseek"},
			},
		})
	}))
	defer srv.Close()

	models, err := FetchModels(context.Background(), srv.URL, "test-key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("want 2 models, got %d", len(models))
	}
	if models[0] != "deepseek-v4-flash" || models[1] != "deepseek-v4-pro" {
		t.Errorf("want sorted [deepseek-v4-flash deepseek-v4-pro], got %v", models)
	}
}

func TestFetchModelsAuthError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"invalid key"}}`, http.StatusUnauthorized)
	}))
	defer srv.Close()

	_, err := FetchModels(context.Background(), srv.URL, "bad-key")
	if err == nil {
		t.Fatal("expected error for bad key")
	}
}

func TestFetchModelsEmptyKey(t *testing.T) {
	_, err := FetchModels(context.Background(), "http://localhost", "")
	if err == nil {
		t.Fatal("expected error for empty key")
	}
}

func TestFetchModelsEmptyBaseURL(t *testing.T) {
	_, err := FetchModels(context.Background(), "", "key")
	if err == nil {
		t.Fatal("expected error for empty base_url")
	}
}

func TestFetchModelsEmptyResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(modelsResponse{Object: "list", Data: nil})
	}))
	defer srv.Close()

	models, err := FetchModels(context.Background(), srv.URL, "key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(models) != 0 {
		t.Errorf("want empty list, got %v", models)
	}
}
