package boot

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

func TestModelSettingsChildCreatedAfterSaveInheritsAcceptedRunSnapshot(t *testing.T) {
	isolateConfigHome(t)
	root := robustTempDir(t)
	t.Chdir(root)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	entered, release := make(chan struct{}), make(chan struct{})
	var first, unblock sync.Once
	t.Cleanup(func() { unblock.Do(func() { close(release) }) })
	type observed struct{ model, key string }
	requests := make(chan observed, 16)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Model    string `json:"model"`
			Messages []struct {
				Role string `json:"role"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		requests <- observed{request.Model, r.Header.Get("Authorization")}
		first.Do(func() {
			close(entered)
			select {
			case <-release:
			case <-r.Context().Done():
			}
		})
		w.Header().Set("Content-Type", "text/event-stream")
		if request.Model == "parent" && request.Messages[len(request.Messages)-1].Role != "tool" {
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"child-task\",\"type\":\"function\",\"function\":{\"name\":\"task\",\"arguments\":\"{\\\"prompt\\\":\\\"answer the delegated task\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n")
			return
		}
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"snapshot child answer\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	if _, err := config.SetCredential("SNAPSHOT_CHILD_KEY", "old-key"); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.DefaultModel, cfg.Agent.SubagentModel = "p/parent", "p/old-child"
	cfg.Providers = []config.ProviderEntry{{Name: "p", Kind: "openai", BaseURL: server.URL, Model: "parent", Models: []string{"parent", "old-child", "new-child"}, APIKeyEnv: "SNAPSHOT_CHILD_KEY"}}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatal(err)
	}
	old, err := Build(ctx, Options{WorkspaceRoot: root, Sink: event.Discard})
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	done := make(chan error, 1)
	go func() { done <- old.Run(ctx, "delegate this task") }()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	cfg.Agent.SubagentModel = "p/new-child"
	if _, err := config.SetCredential("SNAPSHOT_CHILD_KEY", "new-key"); err != nil {
		t.Fatal(err)
	}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatal(err)
	}
	unblock.Do(func() { close(release) })
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	assertRequests := func(child, key string) {
		t.Helper()
		for _, model := range []string{"parent", child, "parent"} {
			select {
			case got := <-requests:
				if got.model != model || got.key != "Bearer "+key {
					t.Fatalf("request used another snapshot: model=%s expected=%s keyMatches=%v", got.model, model, got.key == "Bearer "+key)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
		}
	}
	assertRequests("old-child", "old-key")
	var childResult strings.Builder
	for _, message := range old.History() {
		if message.Role == provider.RoleTool {
			childResult.WriteString(message.Content)
		}
	}
	if !strings.Contains(childResult.String(), "snapshot child answer") {
		t.Fatal("the real task tool did not finish its child")
	}
	next, err := Build(ctx, Options{WorkspaceRoot: root, Sink: event.Discard})
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	if err := next.Run(ctx, "delegate the next task"); err != nil {
		t.Fatal(err)
	}
	assertRequests("new-child", "new-key")
}
