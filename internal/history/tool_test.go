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
