package serve

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"reasonix/internal/assembly/boot"
	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"

	_ "reasonix/internal/model/openai"
)

type refreshEndpoint struct {
	mu     sync.Mutex
	auth   []string
	bodies []map[string]any
}

func (e *refreshEndpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var decoded map[string]any
	_ = json.Unmarshal(body, &decoded)
	e.mu.Lock()
	e.auth = append(e.auth, r.Header.Get("Authorization"))
	e.bodies = append(e.bodies, decoded)
	e.mu.Unlock()

	w.Header().Set("Content-Type", "text/event-stream")
	fmt.Fprint(w, "data: {\"id\":\"c\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1,\"total_tokens\":2}}\n\n")
	fmt.Fprint(w, "data: [DONE]\n\n")
}

func (e *refreshEndpoint) snapshot() (auth []string, bodies []map[string]any) {
	e.mu.Lock()
	defer e.mu.Unlock()
	auth = append(auth, e.auth...)
	bodies = append(bodies, e.bodies...)
	return auth, bodies
}

// TestEffectProviderRefreshThroughBootBuild pins provider request behavior:
// a same-selection switch reuses the live controller and warm prefix, while a
// changed key/base_url rebuilds and the next request uses the new endpoint.
func TestEffectProviderRefreshThroughBootBuild(t *testing.T) {
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	root := testenv.TempDir(t)
	t.Chdir(root)

	writeCredential := func(value string) {
		t.Helper()
		path := config.UserCredentialsPath()
		if path == "" {
			t.Fatal("credentials path is empty")
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("REFRESH_KEY="+value+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeCredential("first-key")

	first := &refreshEndpoint{}
	firstServer := httptest.NewServer(first)
	defer firstServer.Close()
	second := &refreshEndpoint{}
	secondServer := httptest.NewServer(second)
	defer secondServer.Close()

	configPath := config.UserConfigPath()
	if configPath == "" {
		t.Fatal("user config path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatal(err)
	}
	writeConfig := func(baseURL string) {
		t.Helper()
		body := fmt.Sprintf(`default_model = "refresh/model-x"

[agent]
system_prompt = "REFRESH EFFECT BASE"

[codegraph]
enabled = false

[[providers]]
name = "refresh"
kind = "openai"
base_url = %q
model = "model-x"
api_key_env = "REFRESH_KEY"
reasoning_protocol = "none"
`, baseURL)
		if err := os.WriteFile(configPath, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeConfig(firstServer.URL + "/v1")

	ctrl, err := boot.Build(context.Background(), boot.Options{
		WorkspaceRoot: root,
		Sink:          event.Discard,
		RequireKey:    true,
	})
	if err != nil {
		t.Fatalf("boot.Build: %v", err)
	}
	if err := ctrl.NewSession(); err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	original := ctrl
	defer original.Close()

	server := New(ctrl, NewBroadcaster(), config.ServeConfig{})
	if err := ctrl.Run(context.Background(), "warm the prefix"); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	auth, bodies := first.snapshot()
	if len(auth) != 1 || auth[0] != "Bearer first-key" {
		t.Fatalf("first provider auth = %v, want Bearer first-key", auth)
	}
	if len(bodies) != 1 {
		t.Fatalf("first provider requests = %d, want 1", len(bodies))
	}
	firstPrefix := requestSystemPrefix(t, bodies[0])

	if err := server.switchModelRequested(context.Background(), "refresh/model-x"); err != nil {
		t.Fatalf("same-selection switch: %v", err)
	}
	if got := server.Controller(); got != original {
		t.Fatal("same selection replaced the controller, losing the warm prefix")
	}
	if auth, _ = first.snapshot(); len(auth) != 1 {
		t.Fatalf("same selection made a cache-cold request: auth requests = %d, want 1", len(auth))
	}
	if err := original.Run(context.Background(), "reuse the prefix"); err != nil {
		t.Fatalf("second Run: %v", err)
	}
	_, bodies = first.snapshot()
	if len(bodies) != 2 {
		t.Fatalf("same selection provider requests = %d, want the original warm pair", len(bodies))
	}
	if got := requestSystemPrefix(t, bodies[1]); got != firstPrefix {
		t.Fatalf("same selection moved the provider prefix:\nfirst:  %.160q\nsecond: %.160q", firstPrefix, got)
	}

	writeCredential("second-key")
	if err := server.switchModelRequested(context.Background(), "refresh/model-x"); err != nil {
		t.Fatalf("refreshed provider switch: %v", err)
	}
	refreshed := server.Controller()
	if refreshed == original {
		t.Fatal("changed key left the stale controller running")
	}
	defer refreshed.Close()
	if err := refreshed.Run(context.Background(), "use the rotated key"); err != nil {
		t.Fatalf("refreshed Run: %v", err)
	}
	auth, bodies = first.snapshot()
	if len(auth) != 3 || auth[2] != "Bearer second-key" {
		t.Fatalf("rotated-key provider auth = %v, want Bearer second-key", auth)
	}
	if len(bodies) != 3 {
		t.Fatalf("rotated-key provider requests = %d, want 3", len(bodies))
	}

	writeConfig(secondServer.URL + "/v1")
	if err := server.switchModelRequested(context.Background(), "refresh/model-x"); err != nil {
		t.Fatalf("changed base URL switch: %v", err)
	}
	moved := server.Controller()
	if moved == refreshed {
		t.Fatal("changed base_url left the stale controller running")
	}
	defer moved.Close()
	if err := moved.Run(context.Background(), "use the refreshed endpoint"); err != nil {
		t.Fatalf("moved Run: %v", err)
	}
	auth, bodies = second.snapshot()
	if len(auth) != 1 || auth[0] != "Bearer second-key" {
		t.Fatalf("refreshed endpoint auth = %v, want Bearer second-key", auth)
	}
	if len(bodies) != 1 {
		t.Fatalf("refreshed endpoint requests = %d, want 1", len(bodies))
	}
}

func requestSystemPrefix(t *testing.T, body map[string]any) string {
	t.Helper()
	raw, ok := body["messages"].([]any)
	if !ok {
		t.Fatalf("provider request has no messages: %#v", body)
	}
	for _, item := range raw {
		message, ok := item.(map[string]any)
		if !ok || message["role"] != "system" {
			continue
		}
		content, _ := message["content"].(string)
		return content
	}
	t.Fatalf("provider request has no system message: %#v", body)
	return ""
}
