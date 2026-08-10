package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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
		json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"data": []map[string]string{
				{"id": "model-b", "object": "model"},
				{"id": "model-a", "object": "model"},
			},
		})
	}))
	defer srv.Close()

	models, err := FetchModels(context.Background(), srv.URL, "test-key", nil)
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

func TestFetchModelsSendsCustomHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("HTTP-Referer") != "https://app.example" || r.Header.Get("X-Title") != "Reasonix" {
			http.Error(w, `{"error":"missing headers"}`, http.StatusForbidden)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]string{{"id": "model-a"}},
		})
	}))
	defer srv.Close()

	models, err := FetchModels(context.Background(), srv.URL, "key", map[string]string{
		"HTTP-Referer": "https://app.example",
		"X-Title":      "Reasonix",
	})
	if err != nil {
		t.Fatalf("FetchModels: %v", err)
	}
	if len(models) != 1 || models[0] != "model-a" {
		t.Fatalf("models = %v, want [model-a]", models)
	}
}

func TestFetchModelsWithOptionsUsesXAPIKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("x-api-key"); got != "anthropic-key" {
			http.Error(w, `{"error":"missing x-api-key"}`, http.StatusUnauthorized)
			return
		}
		if got := r.Header.Get("Authorization"); got != "" {
			http.Error(w, `{"error":"unexpected bearer"}`, http.StatusUnauthorized)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]string{{"id": "anthropic-model"}},
		})
	}))
	defer srv.Close()

	models, err := FetchModelsWithOptions(context.Background(), srv.URL, "anthropic-key", FetchModelsOptions{
		AuthMode: ModelFetchAuthXAPIKey,
	})
	if err != nil {
		t.Fatalf("FetchModelsWithOptions: %v", err)
	}
	if len(models) != 1 || models[0] != "anthropic-model" {
		t.Fatalf("models = %v, want [anthropic-model]", models)
	}
}

func TestApplyAPIKeyHeaderUsesMiMoAPIKeyHeader(t *testing.T) {
	h := http.Header{}
	applyAPIKeyHeader(h, "https://api.xiaomimimo.com/v1", "mimo-key")
	if got := h.Get("api-key"); got != "mimo-key" {
		t.Fatalf("api-key = %q, want mimo-key", got)
	}
	if got := h.Get("Authorization"); got != "" {
		t.Fatalf("Authorization = %q, want omitted for MiMo", got)
	}

	h = http.Header{}
	applyAPIKeyHeader(h, "https://api.deepseek.com", "deepseek-key")
	if got := h.Get("Authorization"); got != "Bearer deepseek-key" {
		t.Fatalf("Authorization = %q, want Bearer deepseek-key", got)
	}
	if got := h.Get("api-key"); got != "" {
		t.Fatalf("api-key = %q, want omitted for standard OpenAI-compatible providers", got)
	}
}

func TestFetchModelsAuthError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"invalid key"}}`, http.StatusUnauthorized)
	}))
	defer srv.Close()

	_, err := FetchModels(context.Background(), srv.URL, "bad-key", nil)
	if err == nil {
		t.Fatal("expected error for bad key")
	}
}

func TestFetchModelsLargeResponse(t *testing.T) {
	// A model list larger than the old 256 KB cap should succeed.
	// OpenRouter returns ~531 KB (338 models); this test generates
	// enough entries to exceed 256 KB and confirms they are all parsed.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data := make([]map[string]string, 8000)
		for i := range data {
			data[i] = map[string]string{"id": fmt.Sprintf("model-%04d", i), "object": "model"}
		}
		json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": data})
	}))
	defer srv.Close()

	models, err := FetchModels(context.Background(), srv.URL, "key", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(models) != 8000 {
		t.Fatalf("want 8000 models, got %d", len(models))
	}
}

func TestFetchModelsResponseTooLarge(t *testing.T) {
	// A response larger than fetchModelsMaxBody should return a clear
	// error rather than a cryptic JSON parse failure.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		padding := strings.Repeat("x", fetchModelsMaxBody+1024)
		fmt.Fprintf(w, `{"object":"list","data":[{"id":"%s","object":"model"}]}`, padding)
	}))
	defer srv.Close()

	_, err := FetchModels(context.Background(), srv.URL, "key", nil)
	if err == nil {
		t.Fatal("expected error for oversized response")
	}
	if !strings.Contains(err.Error(), "too large") {
		t.Errorf("error should mention the size limit, got: %v", err)
	}
}
