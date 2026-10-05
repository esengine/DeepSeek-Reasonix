package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/assembly/boot"
	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
)

func TestRevisionParseRefusalMasksCredentials(t *testing.T) {
	body, _ := json.Marshal(map[string]string{"input": "reasonix mcp add neutral --http https://host/mcp --header 'Authorization: Bearer fixturesecret'"})
	w := httptest.NewRecorder()
	(&Server{}).mcpParse(w, httptest.NewRequest(http.MethodPost, "/mcp/parse", strings.NewReader(string(body))))
	if w.Code != 400 || strings.Contains(w.Body.String(), "fixturesecret") {
		t.Fatalf("refusal leaked: %d %s", w.Code, w.Body.String())
	}
}

func TestRevisionInstallRoundTripThroughBoot(t *testing.T) {
	root, home := testenv.TempDir(t), testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	t.Chdir(root)
	resolver := &provider.StaticResolver{Descriptors: []provider.Descriptor{{Ref: "neutral/fixture", Model: "fixture", Default: true}}, Providers: map[string]provider.Provider{"neutral/fixture": credentialPreviewProvider{}}}
	ctrl, err := boot.Build(t.Context(), boot.Options{Home: home, WorkspaceRoot: root, Model: "neutral/fixture", ProviderResolver: resolver, SessionDir: filepath.Join(root, "sessions"), TokenMode: boot.TokenModeFull, Sink: event.Discard})
	if err != nil {
		t.Fatal(err)
	}
	defer ctrl.Close()
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Basic fixturesecret" || r.URL.RawQuery != "%74oken=fixturesecret" || r.URL.Path != "/token/fixturesecret" {
			t.Error("operational request changed")
		}
		var req struct {
			ID     *int   `json:"id"`
			Method string `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			return
		}
		if req.ID == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		var result any = map[string]any{}
		switch req.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": "2025-06-18", "serverInfo": map[string]any{"name": "neutral", "version": "0"}, "capabilities": map[string]any{"tools": map[string]any{}}}
		case "tools/list":
			result = map[string]any{"tools": []any{}}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}))
	defer remote.Close()
	endpoint := remote.URL + "/token/fixturesecret?%74oken=fixturesecret#fixturesecret"
	envValue := "https://host/mcp Authorization: Basic fixturesecret"
	input, _ := json.Marshal(map[string]any{"mcpServers": map[string]any{"neutral": map[string]any{"url": endpoint, "headers": map[string]string{"Authorization": "Basic fixturesecret"}, "env": map[string]string{"ORDINARY": envValue}}}})
	body, _ := json.Marshal(map[string]string{"input": string(input)})
	s := New(ctrl, NewBroadcaster(), config.ServeConfig{})
	w := httptest.NewRecorder()
	s.mcpParse(w, httptest.NewRequest(http.MethodPost, "/mcp/parse", strings.NewReader(string(body))))
	var preview struct {
		Servers []draftServer `json:"servers"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &preview); err != nil || len(preview.Servers) != 1 {
		t.Fatalf("parse: %s", w.Body.String())
	}
	for _, display := range []string{preview.Servers[0].DisplayURL, preview.Servers[0].DisplayEnv["ORDINARY"], preview.Servers[0].DisplayHeaders["Authorization"]} {
		if strings.Contains(display, "fixturesecret") {
			t.Fatal("preview credential leaked")
		}
	}
	body, _ = json.Marshal(map[string]any{"server": preview.Servers[0], "scope": "project"})
	w = httptest.NewRecorder()
	s.mcpInstall(w, httptest.NewRequest(http.MethodPost, "/mcp/install", strings.NewReader(string(body))))
	var installed struct {
		State string `json:"state"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &installed)
	if w.Code != 200 || installed.State != "ready" {
		t.Fatalf("install: %d %s", w.Code, w.Body.String())
	}
	cfg := config.LoadForEdit(filepath.Join(root, "reasonix.toml"))
	if len(cfg.Plugins) != 1 || cfg.Plugins[0].URL != endpoint || cfg.Plugins[0].Headers["Authorization"] != "Basic fixturesecret" || cfg.Plugins[0].Env["ORDINARY"] != envValue {
		t.Fatalf("persisted credentials changed: %+v", cfg.Plugins)
	}
}
