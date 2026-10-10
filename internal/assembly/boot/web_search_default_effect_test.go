package boot

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
)

func conversationToolTypes(t *testing.T, searchModel string) []string {
	t.Helper()
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	bodies := make(chan map[string]any, 8)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		bodies <- body
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"type\":\"message_start\",\"message\":{\"id\":\"m\",\"usage\":{\"input_tokens\":1}}}\n\n"+
			"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n"+
			"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"}}\n\n"+
			"data: {\"type\":\"message_stop\"}\n\n")
	}))
	defer srv.Close()
	extra := ""
	if searchModel != "" {
		extra = fmt.Sprintf("web_search_model = %q\n", searchModel)
	}
	writeFile(t, filepath.Dir(config.UserConfigPath()), "config.toml", fmt.Sprintf(`default_model = "chat/main"
[agent]
system_prompt = "BASE"
%s[[providers]]
name = "chat"
kind = "anthropic"
base_url = %q
model = "main"
api_key = "fixture"
web_search = true
[[providers]]
name = "search"
kind = "anthropic"
base_url = %q
model = "finder"
api_key = "fixture"
web_search = true
`, extra, srv.URL, srv.URL))
	ctrl, err := Build(context.Background(), Options{Sink: event.Discard, Model: "chat/main", WorkspaceRoot: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer ctrl.Close()
	if err := ctrl.Run(context.Background(), "hello"); err != nil {
		t.Fatal(err)
	}
	body := <-bodies
	var types []string
	tools, _ := body["tools"].([]any)
	for _, raw := range tools {
		tl, _ := raw.(map[string]any)
		if tl["type"] == "web_search_20250305" {
			types = append(types, "native")
		}
	}
	return types
}

func TestEffectUnsetSearchModelKeepsTheConversationsBuiltInSearch(t *testing.T) {
	if got := conversationToolTypes(t, ""); len(got) != 1 {
		t.Fatalf("default configuration lost the built-in search tool: %v", got)
	}
}

func TestEffectAssignedSearchModelIsTheOnlySearchRoute(t *testing.T) {
	if got := conversationToolTypes(t, "search/finder"); len(got) != 0 {
		t.Fatalf("conversation request still carries the built-in search tool: %v", got)
	}
}
