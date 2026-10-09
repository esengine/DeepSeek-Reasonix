package serve

import (
	"context"
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

func TestMCPPreviewSeparatesDisplayFromInstallValues(t *testing.T) {
	root := testenv.TempDir(t)
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	t.Chdir(root)
	resolver := &provider.StaticResolver{Descriptors: []provider.Descriptor{{Ref: "neutral/fixture", Model: "fixture", Default: true}}, Providers: map[string]provider.Provider{"neutral/fixture": credentialPreviewProvider{}}}
	ctrl, err := boot.Build(t.Context(), boot.Options{Home: home, WorkspaceRoot: root, Model: "neutral/fixture", ProviderResolver: resolver, SessionDir: filepath.Join(root, "sessions"), TokenMode: boot.TokenModeFull, Sink: event.Discard})
	if err != nil {
		t.Fatal(err)
	}
	defer ctrl.Close()
	input := `{"mcpServers":{"neutral":{"url":"https://host/mcp?%74oken=fixture-secret","env":{"PASSWORD":"fixture-secret"},"headers":{"Authorization":"fixture-secret"}}}}`
	body, _ := json.Marshal(map[string]string{"input": input})
	w := httptest.NewRecorder()
	New(ctrl, NewBroadcaster(), config.ServeConfig{}).mcpParse(w, httptest.NewRequest(http.MethodPost, "/mcp/parse", strings.NewReader(string(body))))
	var response struct {
		Servers []map[string]any `json:"servers"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Servers) != 1 {
		t.Fatalf("response: %s", w.Body.String())
	}
	s := response.Servers[0]
	if !strings.Contains(s["url"].(string), "fixture-secret") {
		t.Fatal("install URL lost")
	}
	for _, key := range []string{"displayUrl", "displayEnv", "displayHeaders"} {
		value, ok := s[key]
		if !ok {
			t.Errorf("missing %s", key)
			continue
		}
		b, _ := json.Marshal(value)
		if strings.Contains(string(b), "fixture-secret") {
			t.Errorf("%s leaked", key)
		}
	}
	for _, input := range []string{
		`{"mcpServers":{"neutral":{"url":"https://host/mcp?client.token=fixturesecret#client.password=fixturesecret"}}}`,
		`{"mcpServers":{"neutral":{"url":"https://host/mcp?next=https%3A%2F%2Fu%3Afixturesecret%40host%2Fmcp"}}}`,
		`{"mcpServers":{"neutral":{"url":"https://host/mcp#redirect=https://u:fixturesecret@host/mcp"}}}`,
		`{"mcpServers":{"neutral":{"url":"https://host/mcp","env":{"ORDINARY":"https://host/mcp Bearer fixturesecret"}}}}`,
		`{"mcpServers":{"neutral":{"url":"https://host/mcp","env":{"ORDINARY":"https://host/mcp\rTOKEN=fixturesecret"}}}}`,
		`{"mcpServers":{"neutral":{"url":"https://host/mcp","env":{"ORDINARY":"https://host/mcp\fTOKEN=fixturesecret"}}}}`,
		"https://host/token/fixturesecret",
		"https://host/mcp#fixturesecret",
		"node --header 'Authorization: Bearer fixturesecret'",
		"node -e KEY=fixturesecret",
		"node -eKEY=fixturesecret",
		"node -HAuthorization:Basicfixturesecret",
		`{"mcpServers":{"neutral":{"url":"https://host/mcp","env":{"ORDINARY":"https://host/mcp Authorization: Basic fixturesecret"}}}}`,
		`{"mcpServers":{"neutral":{"command":"node --header 'Authorization: Bearer fixturesecret'"}}}`,
		"reasonix mcp add neutral --http https://host/mcp --header 'Authorization: Bearer fixturesecret'",
	} {
		body, _ := json.Marshal(map[string]string{"input": input})
		w := httptest.NewRecorder()
		New(ctrl, NewBroadcaster(), config.ServeConfig{}).mcpParse(w, httptest.NewRequest(http.MethodPost, "/mcp/parse", strings.NewReader(string(body))))
		if w.Code == 400 {
			if strings.Contains(w.Body.String(), "fixturesecret") {
				t.Fatalf("refusal leaked: %s", w.Body.String())
			}
			continue
		}
		var wire map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &wire); err != nil {
			t.Fatal(err)
		}
		for _, row := range wire["servers"].([]any) {
			server := row.(map[string]any)
			operational, _ := json.Marshal([]any{server["url"], server["command"], server["args"], server["env"], server["headers"]})
			if !strings.Contains(string(operational), "fixturesecret") {
				t.Fatal("operational credential lost")
			}
			for _, key := range []string{"url", "command", "args", "env", "headers"} {
				delete(server, key)
			}
		}
		display, _ := json.Marshal(wire)
		if strings.Contains(string(display), "fixturesecret") {
			t.Fatalf("preview leaked: %s", display)
		}
	}
}

type credentialPreviewProvider struct{}

func (credentialPreviewProvider) Name() string { return "credential-preview" }
func (credentialPreviewProvider) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	chunks := make(chan provider.Chunk)
	close(chunks)
	return chunks, nil
}
