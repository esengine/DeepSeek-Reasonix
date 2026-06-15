package history

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/provider"
)

func TestHistoryToolSearchAndAroundAreUsable(t *testing.T) {
	sessionDir := t.TempDir()
	path := filepath.Join(sessionDir, "decision.jsonl")
	writeSession(t, path, []provider.Message{
		{Role: provider.RoleUser, Content: "Should history use vector embeddings?"},
		{Role: provider.RoleAssistant, Content: "Decision: keep history retrieval lightweight with BM25 and no vector database."},
		{Role: provider.RoleUser, Content: "Great, port that to Reasonix."},
	})

	tl := NewTool(Options{SessionDir: sessionDir})
	if tl.Name() != "history" || !tl.ReadOnly() {
		t.Fatalf("unexpected tool identity: name=%q readonly=%v", tl.Name(), tl.ReadOnly())
	}
	if !json.Valid(tl.Schema()) {
		t.Fatal("history schema is not valid JSON")
	}

	out, err := tl.Execute(context.Background(), []byte(`{"operation":"search","query":"BM25 vector database","limit":5}`))
	if err != nil {
		t.Fatalf("Execute search: %v", err)
	}
	for _, want := range []string{
		"History search results",
		"decision.jsonl",
		"message_index=1",
		"keep history retrieval lightweight",
		`Use operation="around"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("search output missing %q:\n%s", want, out)
		}
	}

	args, _ := json.Marshal(map[string]any{
		"operation":     "around",
		"session_path":  path,
		"message_index": 1,
		"before":        1,
		"after":         1,
	})
	out, err = tl.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute around: %v", err)
	}
	for _, want := range []string{
		"History around",
		"[0 user]",
		"[1 assistant]",
		"[2 user]",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("around output missing %q:\n%s", want, out)
		}
	}
}

func TestHistoryToolSchemaIsCacheStable(t *testing.T) {
	tl := NewTool(Options{SessionDir: t.TempDir()})
	if got, want := tl.Description(), "Search saved local session history with lightweight BM25 retrieval, then read messages around a hit. Use search when past decisions, failed attempts, commands, or tool inputs may help the current task; use around with a returned session_path and message_index to inspect the nearby transcript. By default it searches user text, assistant text, tool inputs, and tool errors; normal tool outputs are excluded unless kind includes tool_output."; got != want {
		t.Fatalf("history description changed; this is provider-visible and affects prompt-cache shape.\nwant: %q\n got: %q", want, got)
	}
	const wantSchema = `{
		"type": "object",
		"properties": {
			"operation": {"type": "string", "enum": ["search", "around"], "description": "search ranks saved history; around returns nearby messages for a search hit."},
			"query": {"type": "string", "description": "Search query for operation=search."},
			"scope": {"type": "string", "enum": ["project", "global"], "description": "project searches the current session directory; global also includes compacted-history archives."},
			"kind": {"type": "array", "items": {"type": "string", "enum": ["user_text", "assistant_text", "tool_input", "tool_error", "tool_output"]}, "description": "History parts to search. Defaults to user_text, assistant_text, tool_input, and tool_error."},
			"tool_name": {"type": "string", "description": "Optional tool-name filter for tool_input, tool_error, or tool_output."},
			"limit": {"type": "integer", "description": "Maximum search hits to return, default 8, max 20."},
			"session_path": {"type": "string", "description": "Path from a search hit. Required for operation=around."},
			"message_index": {"type": "integer", "description": "Message index from a search hit. Required for operation=around."},
			"before": {"type": "integer", "description": "Messages before message_index for operation=around, default 3, max 10."},
			"after": {"type": "integer", "description": "Messages after message_index for operation=around, default 3, max 10."}
		},
		"required": ["operation"]
	}`
	if got := string(tl.Schema()); got != wantSchema {
		t.Fatalf("history schema changed; this is provider-visible and affects prompt-cache shape.\nwant:\n%s\n got:\n%s", wantSchema, got)
	}
}

func TestHistoryToolValidatesInputs(t *testing.T) {
	tl := NewTool(Options{SessionDir: t.TempDir()})
	for _, tc := range []struct {
		name string
		args string
	}{
		{"missing operation", `{}`},
		{"unknown operation", `{"operation":"scan"}`},
		{"around missing index", `{"operation":"around","session_path":"/tmp/session.jsonl"}`},
		{"bad json", `{"operation":`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tl.Execute(context.Background(), []byte(tc.args)); err == nil {
				t.Fatalf("Execute(%s) error = nil, want validation error", tc.args)
			}
		})
	}
}
