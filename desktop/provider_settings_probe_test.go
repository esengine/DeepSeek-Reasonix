package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/config"
)

func TestProviderDraftProbesDoNotPersistCredentialsOrConfiguration(t *testing.T) {
	isolateDesktopUserDirs(t)
	const keyEnv = "REASONIX_DRAFT_PROBE_TEST_KEY"
	if _, err := config.SetCredential(keyEnv, "saved-key"); err != nil {
		t.Fatal(err)
	}
	t.Setenv(keyEnv, "process-key")
	revision := config.CredentialStoreRevision()
	cachePath := filepath.Join(config.CacheDir(), "model-capabilities-v2.json")
	beforeCache, _ := os.ReadFile(cachePath)
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.Header.Get("Authorization") != "Bearer draft-key" {
			http.Error(w, "wrong credential", http.StatusUnauthorized)
			return
		}
		if r.URL.Path == "/v1/models" {
			fmt.Fprint(w, `{"data":[{"id":"draft-model"}]}`)
			return
		}
		if r.URL.Path != "/exact/chat" {
			http.NotFound(w, r)
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		if body["model"] != "draft-model" || body["tools"] != nil {
			t.Errorf("unexpected probe model/tools: %v", body)
		}
		messages, _ := body["messages"].([]any)
		if len(messages) != 1 || messages[0].(map[string]any)["role"] != "user" {
			t.Errorf("probe includes session/system context: %v", messages)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"OK\"}}]}\n\ndata: [DONE]\n\n")
	}))
	defer srv.Close()
	p := ProviderView{Name: "draft-only", Kind: "openai", BaseURL: srv.URL + "/v1", RequestURL: srv.URL + "/exact/chat", APIKeyEnv: keyEnv, Models: []string{"draft-model"}}
	a := NewApp()
	catalog, err := a.FetchProviderModelCatalogDraft(p, "draft-key")
	if err != nil || len(catalog) != 1 || catalog[0].Model != "draft-model" {
		t.Fatalf("draft catalog: %v, %v", catalog, err)
	}
	if err := a.TestProviderModel(p, "draft-model", "draft-key"); err != nil {
		t.Fatal(err)
	}
	if err := a.TestProviderModel(p, "unlisted-model", "draft-key"); err == nil {
		t.Fatal("unlisted model should be rejected before network access")
	}
	afterCache, _ := os.ReadFile(cachePath)
	if string(beforeCache) != string(afterCache) {
		t.Fatal("draft credentials polluted the saved capability cache")
	}
	if len(paths) != 2 {
		t.Fatalf("unexpected requests: %v", paths)
	}
	if os.Getenv(keyEnv) != "process-key" || config.CredentialStoreRevision() != revision {
		t.Fatal("probe changed process environment or stored credentials")
	}
	cfg, err := config.LoadForRootWithoutCredentialsReadOnly("")
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := cfg.Provider(p.Name); exists {
		t.Fatal("probe persisted the draft provider")
	}
}

func TestProviderModelProbeReportsHTTPFailure(t *testing.T) {
	isolateDesktopUserDirs(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"invalid credential"}}`, http.StatusUnauthorized)
	}))
	defer srv.Close()
	err := NewApp().TestProviderModel(ProviderView{Name: "probe", Kind: "openai", BaseURL: srv.URL, Models: []string{"model"}}, "model", "bad-key")
	if err == nil {
		t.Fatal("failed authentication reported as successful")
	}
}

func TestConfiguredModelInfoUsesPerModelOverrides(t *testing.T) {
	vision := true
	cfg := &config.Config{Providers: []config.ProviderEntry{{
		Name: "custom", Kind: "openai", BaseURL: "https://example.test/v1",
		Models: []string{"model-a", "model-b"}, ContextWindow: 32768,
		ModelOverrides: map[string]config.ProviderModelOverride{"model-b": {ContextWindow: 65536, Vision: &vision}},
	}}}
	first := configuredModelInfo(cfg, "custom", "model-a", false)
	second := configuredModelInfo(cfg, "custom", "model-b", true)
	if first.ContextWindow != 32768 || first.Vision || second.ContextWindow != 65536 || !second.Vision || !second.Current {
		t.Fatalf("model menu metadata disagrees with per-model configuration: %+v, %+v", first, second)
	}
}
