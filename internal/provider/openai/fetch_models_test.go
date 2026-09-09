package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"reasonix/internal/netclient"
	"reasonix/internal/provider"
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

func TestDiscoveryUnknownAndConflictingDuplicates(t *testing.T) {
	cases := []struct {
		name    string
		entries []string
		state   string
	}{
		{"missing", []string{`{"id":"x"}`}, "unknown"},
		{"invalid standard blocks positive alias", []string{`{"id":"x","input_modalities":null,"vision":true}`}, "unknown"},
		{"invalid array", []string{`{"id":"x","input_modalities":["text",42],"supports_vision":true}`}, "unknown"},
		{"invalid bool", []string{`{"id":"x","vision":null}`}, "unknown"},
		{"empty capabilities", []string{`{"id":"x","capabilities":{},"vision":true}`}, "unknown"},
		{"conflict sticky", []string{`{"id":"x","vision":true}`, `{"id":"x","vision":false}`, `{"id":"x","vision":true}`, `{"id":"x"}`}, "unknown"},
		{"unknown does not override known", []string{`{"id":"x"}`, `{"id":"x","vision":true}`}, "supported"},
		{"unknown does not erase negative", []string{`{"id":"x"}`, `{"id":"x","vision":false}`}, "unsupported"},
		{"standard wins", []string{`{"id":"x","input_modalities":["text"],"vision":true}`}, "unsupported"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var permutations func([]string, int)
			permutations = func(entries []string, i int) {
				if i < len(entries) {
					for j := i; j < len(entries); j++ {
						entries[i], entries[j] = entries[j], entries[i]
						permutations(entries, i+1)
						entries[i], entries[j] = entries[j], entries[i]
					}
					return
				}
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					fmt.Fprint(w, `{"data":[`+strings.Join(entries, ",")+`]}`)
				}))
				catalog, err := FetchModelCatalog(context.Background(), srv.URL, "", nil)
				srv.Close()
				if err != nil || len(catalog) != 1 {
					t.Fatalf("catalog=%+v err=%v", catalog, err)
				}
				state := "unsupported"
				if catalog[0].InputModalities == nil {
					state = "unknown"
				} else if catalog[0].SupportsInput(provider.ModalityImage) {
					state = "supported"
				}
				if state != tc.state {
					t.Fatalf("entries %v: got %s want %s", entries, state, tc.state)
				}
			}
			permutations(append([]string(nil), tc.entries...), 0)
		})
	}
}

func TestFetchModelCatalogParsesInputModalitiesAndAliases(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{
			map[string]any{"id": "canonical", "input_modalities": []string{"text", "image"}},
			map[string]any{"id": "nested", "modalities": map[string]any{"input": []string{"text", "image"}}},
			map[string]any{"id": "vision-bool", "supports_vision": true},
			map[string]any{"id": "text-only", "capabilities": map[string]any{"vision": false}},
			map[string]any{"id": "missing"},
		}})
	}))
	defer srv.Close()

	got, err := FetchModelCatalog(context.Background(), srv.URL, "key", nil)
	if err != nil {
		t.Fatalf("FetchModelCatalog: %v", err)
	}
	byID := map[string][]string{}
	for _, model := range got {
		modalities := make([]string, len(model.InputModalities))
		for i, modality := range model.InputModalities {
			modalities[i] = string(modality)
		}
		byID[model.ID] = modalities
	}
	if got := byID["canonical"]; len(got) != 2 || got[1] != "image" {
		t.Fatalf("canonical modalities = %v", got)
	}
	if got := byID["nested"]; len(got) != 2 || got[1] != "image" {
		t.Fatalf("nested modalities = %v", got)
	}
	if got := byID["vision-bool"]; len(got) != 2 || got[1] != "image" {
		t.Fatalf("vision bool modalities = %v", got)
	}
	if got := byID["text-only"]; len(got) != 1 || got[0] != "text" {
		t.Fatalf("text-only modalities = %v", got)
	}
	if got := byID["missing"]; len(got) != 0 {
		t.Fatalf("missing modalities = %v, want unknown", got)
	}
}

func TestFetchModelCatalogCanonicalFieldWinsOverAlias(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{
			map[string]any{"id": "model", "input_modalities": []string{"text"}, "supports_vision": true},
		}})
	}))
	defer srv.Close()

	got, err := FetchModelCatalog(context.Background(), srv.URL, "key", nil)
	if err != nil {
		t.Fatalf("FetchModelCatalog: %v", err)
	}
	if len(got) != 1 || len(got[0].InputModalities) != 1 || got[0].InputModalities[0] != "text" {
		t.Fatalf("catalog = %+v, canonical field should win", got)
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

// TestFetchModelsRoutesThroughConfiguredProxy pins the #9560 fix: model
// discovery must ride the same network policy as chat requests. The fake
// gateway host only resolves through the proxy, so success proves the proxy
// transport was used; the plain spec must fail to reach it directly.
func TestFetchModelsRoutesThroughConfiguredProxy(t *testing.T) {
	const gateway = "http://reasonix-fetch-probe.invalid/v1"
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.String(), gateway) {
			http.Error(w, "unexpected proxied target "+r.URL.String(), http.StatusBadRequest)
			return
		}
		if r.Header.Get("Authorization") != "Bearer proxied-key" {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"data":   []map[string]string{{"id": "model-a", "object": "model"}},
		})
	}))
	defer proxy.Close()

	spec := netclient.ProxySpec{Mode: netclient.ModeCustom, URL: proxy.URL}
	if err := netclient.Validate(spec); err != nil {
		t.Fatalf("proxy spec: %v", err)
	}

	models, err := FetchModelsWithOptions(context.Background(), gateway, "proxied-key", FetchModelsOptions{Proxy: spec})
	if err != nil {
		t.Fatalf("fetch through proxy: %v", err)
	}
	if fmt.Sprint(models) != "[model-a]" {
		t.Fatalf("models = %v, want [model-a]", models)
	}

	if _, err := FetchModelsWithOptions(context.Background(), gateway, "proxied-key", FetchModelsOptions{}); err == nil {
		t.Fatal("direct fetch to a proxy-only host must fail, otherwise this test proves nothing")
	}
}
