package sessiontool

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/provider"
)

// writeSessionJSONL writes provider.Messages as JSONL to a file, matching
// the format produced by agent.Session.Save.
func writeSessionJSONL(t *testing.T, path string, msgs []provider.Message) {
	t.Helper()
	ses := agent.NewSession("")
	for _, m := range msgs {
		ses.Add(m)
	}
	if err := ses.Save(path); err != nil {
		t.Fatalf("save session: %v", err)
	}
}

// runTool is a convenience wrapper for calling a tool's Execute with JSON args.
func runTool(t *testing.T, tl interface {
	Execute(context.Context, json.RawMessage) (string, error)
	Name() string
}, m map[string]any) string {
	t.Helper()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	out, err := tl.Execute(context.Background(), json.RawMessage(b))
	if err != nil {
		t.Fatalf("%s: %v", tl.Name(), err)
	}
	return out
}

// ---- list_sessions tests ----------------------------------------------------

func TestListSessions_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	tool := NewListSessionsTool(dir)
	out, err := tool.Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "No sessions found") {
		t.Errorf("expected 'No sessions found', got: %s", out)
	}
}

func TestToolSchemasAreValidJSON(t *testing.T) {
	dir := t.TempDir()
	for _, tool := range []struct {
		name   string
		schema json.RawMessage
	}{
		{name: "list_sessions", schema: NewListSessionsTool(dir).Schema()},
		{name: "read_session", schema: NewReadSessionTool(dir).Schema()},
	} {
		if !json.Valid(tool.schema) {
			t.Fatalf("%s schema is invalid JSON: %s", tool.name, tool.schema)
		}
	}
}

func TestListSessions_OnlyCleanupPending(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "20260618-120000.000000000-test-model.jsonl")
	writeSessionJSONL(t, sessionPath, []provider.Message{
		{Role: provider.RoleUser, Content: "hello"},
	})
	if err := agent.MarkCleanupPending(sessionPath, "delete"); err != nil {
		t.Fatal(err)
	}

	tool := NewListSessionsTool(dir)
	out, err := tool.Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "No sessions found") {
		t.Errorf("cleanup-pending session should be excluded, got: %s", out)
	}
}

func TestListSessions_SingleSession(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "20260618-120000.000000000-test-model.jsonl")
	writeSessionJSONL(t, sessionPath, []provider.Message{
		{Role: provider.RoleUser, Content: "hello"},
		{Role: provider.RoleAssistant, Content: "world"},
	})

	tool := NewListSessionsTool(dir)
	out := runTool(t, tool, map[string]any{})

	if !strings.Contains(out, "test-model") {
		t.Errorf("expected model name in output, got: %s", out)
	}
	if !strings.Contains(out, "1 turn") && !strings.Contains(out, "| 1 |") {
		t.Errorf("expected turn count in output, got: %s", out)
	}
}

// ---- read_session tests -----------------------------------------------------

func TestReadSession_ValidSession(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "session.jsonl")
	writeSessionJSONL(t, sessionPath, []provider.Message{
		{Role: provider.RoleUser, Content: "user hello"},
		{Role: provider.RoleAssistant, Content: "assistant response"},
	})

	tool := NewReadSessionTool(dir)
	out := runTool(t, tool, map[string]any{"session": "session.jsonl"})

	if !strings.Contains(out, "user hello") {
		t.Errorf("expected user content, got: %s", out)
	}
	if !strings.Contains(out, "assistant response") {
		t.Errorf("expected assistant content, got: %s", out)
	}
}

func TestReadSession_ExcludesSystemPrompt(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "session.jsonl")
	writeSessionJSONL(t, sessionPath, []provider.Message{
		{Role: provider.RoleSystem, Content: "SECRET_SYSTEM_PROMPT"},
		{Role: provider.RoleUser, Content: "hello"},
		{Role: provider.RoleAssistant, Content: "hi"},
	})

	tool := NewReadSessionTool(dir)
	out := runTool(t, tool, map[string]any{"session": "session.jsonl"})

	if strings.Contains(out, "SECRET_SYSTEM_PROMPT") {
		t.Errorf("system prompt should be excluded, got: %s", out)
	}
}

func TestReadSession_ExcludesReasoningContent(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "session.jsonl")
	writeSessionJSONL(t, sessionPath, []provider.Message{
		{Role: provider.RoleUser, Content: "hello"},
		{Role: provider.RoleAssistant, Content: "answer", ReasoningContent: "PASS_should_not_appear"},
	})

	tool := NewReadSessionTool(dir)
	out := runTool(t, tool, map[string]any{"session": "session.jsonl"})

	if strings.Contains(out, "PASS_should_not_appear") {
		t.Errorf("reasoning content should be excluded, got: %s", out)
	}
}

func TestReadSession_TruncatesLongContent(t *testing.T) {
	dir := t.TempDir()
	longContent := strings.Repeat("a", 5000)
	sessionPath := filepath.Join(dir, "session.jsonl")
	writeSessionJSONL(t, sessionPath, []provider.Message{
		{Role: provider.RoleUser, Content: "hello"},
		{Role: provider.RoleAssistant, Content: longContent},
	})

	tool := NewReadSessionTool(dir)
	out := runTool(t, tool, map[string]any{"session": "session.jsonl"})

	if len(out) > 2500 {
		t.Errorf("output too long (%d chars) for truncated content", len(out))
	}
	if !strings.Contains(out, "...") {
		t.Errorf("expected truncation marker '...' in output")
	}
}

func TestReadSession_RespectsMaxTurns(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "session.jsonl")
	var msgs []provider.Message
	for i := 0; i < 10; i++ {
		msgs = append(msgs,
			provider.Message{Role: provider.RoleUser, Content: "turn"},
			provider.Message{Role: provider.RoleAssistant, Content: "answer"},
		)
	}
	writeSessionJSONL(t, sessionPath, msgs)

	tool := NewReadSessionTool(dir)
	out := runTool(t, tool, map[string]any{"session": "session.jsonl", "max_turns": 2})

	if !strings.Contains(out, "truncated") {
		t.Errorf("expected truncation notice with max_turns=2, got: %s", out)
	}
	if strings.Contains(out, "User (turn 3)") {
		t.Errorf("should not show turn 3 with max_turns=2, got: %s", out)
	}
}

func TestReadSession_MaxTurnsZeroNoLimit(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "session.jsonl")
	var msgs []provider.Message
	for i := 0; i < 60; i++ {
		msgs = append(msgs,
			provider.Message{Role: provider.RoleUser, Content: "turn"},
			provider.Message{Role: provider.RoleAssistant, Content: "answer"},
		)
	}
	writeSessionJSONL(t, sessionPath, msgs)

	tool := NewReadSessionTool(dir)
	out := runTool(t, tool, map[string]any{"session": "session.jsonl", "max_turns": 0})

	if strings.Contains(out, "truncated") {
		t.Errorf("max_turns=0 should show all turns, got truncation notice")
	}
	if !strings.Contains(out, "User (turn 60)") {
		t.Errorf("expected turn 60 with max_turns=0, got: %s", out)
	}
}

func TestReadSession_RejectsCleanupPending(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "session.jsonl")
	writeSessionJSONL(t, sessionPath, []provider.Message{
		{Role: provider.RoleUser, Content: "data"},
	})
	if err := agent.MarkCleanupPending(sessionPath, "delete"); err != nil {
		t.Fatal(err)
	}

	tool := NewReadSessionTool(dir)
	_, err := tool.Execute(context.Background(), json.RawMessage(`{"session":"session.jsonl"}`))
	if err == nil {
		t.Fatal("expected error for cleanup-pending session, got nil")
	}
	if !strings.Contains(err.Error(), "pending cleanup") {
		t.Errorf("expected 'pending cleanup' error, got: %v", err)
	}
}

func TestReadSession_RejectsPathTraversal(t *testing.T) {
	dir := t.TempDir()
	tool := NewReadSessionTool(dir)
	_, err := tool.Execute(context.Background(), json.RawMessage(`{"session":"../../etc/passwd"}`))
	if err == nil {
		t.Fatal("expected error for path traversal, got nil")
	}
	if !strings.Contains(err.Error(), "outside the session directory") {
		t.Errorf("expected 'outside the session directory' error, got: %v", err)
	}
}

func TestReadSession_ToolResultsOmittedByDefault(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "session.jsonl")
	writeSessionJSONL(t, sessionPath, []provider.Message{
		{Role: provider.RoleUser, Content: "list files"},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{
			{ID: "call1", Name: "ls", Arguments: `{"path":"."}`},
		}},
		{Role: provider.RoleTool, Name: "ls", Content: "SECRET_FILE_CONTENT", ToolCallID: "call1"},
		{Role: provider.RoleAssistant, Content: "here are the files"},
	})

	tool := NewReadSessionTool(dir)
	out := runTool(t, tool, map[string]any{"session": "session.jsonl"})

	if !strings.Contains(out, "Tool Calls") {
		t.Errorf("expected Tool Calls section, got: %s", out)
	}
	if !strings.Contains(out, "Tool Result: ls") {
		t.Errorf("expected Tool Result header, got: %s", out)
	}
	if strings.Contains(out, "SECRET_FILE_CONTENT") {
		t.Errorf("tool result content should be omitted by default, got: %s", out)
	}
}

func TestReadSession_ToolResultsWithOptIn(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "session.jsonl")
	writeSessionJSONL(t, sessionPath, []provider.Message{
		{Role: provider.RoleUser, Content: "list files"},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{
			{ID: "call1", Name: "ls", Arguments: `{"path":"."}`},
		}},
		{Role: provider.RoleTool, Name: "ls", Content: "file1.txt\nfile2.go", ToolCallID: "call1"},
		{Role: provider.RoleAssistant, Content: "here are the files"},
	})

	tool := NewReadSessionTool(dir)
	out := runTool(t, tool, map[string]any{"session": "session.jsonl", "show_tool_results": true})

	if !strings.Contains(out, "file1.txt") {
		t.Errorf("expected tool result content with opt-in, got: %s", out)
	}
}

// ---- helper tests -----------------------------------------------------------

func TestModelFromPath(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"20260618-231556.000000000-gpt-4.jsonl", "gpt-4"},
		{"20260618-231556.000000000-claude-sonnet-4-20250514.jsonl", "claude-sonnet-4-20250514"},
		{"plain.jsonl", "(unknown)"},
		{"no-dash.jsonl", "dash"},
		{"20260618-231556.jsonl", "231556"},
	}
	for _, tt := range tests {
		got := modelFromPath(tt.path)
		if got != tt.want {
			t.Errorf("modelFromPath(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestTruncateRunes(t *testing.T) {
	tests := []struct {
		s    string
		max  int
		want string
	}{
		{"hello", 10, "hello"},
		{"hello world", 5, "hello..."},
		{"", 10, ""},
		{"   spaced   ", 10, "spaced"},
	}
	for _, tt := range tests {
		got := truncateRunes(tt.s, tt.max)
		if got != tt.want {
			t.Errorf("truncateRunes(%q, %d) = %q, want %q", tt.s, tt.max, got, tt.want)
		}
	}
}

// TestCleanupPendingContract verifies that our tools use the SAME marker
// contract as agent.MarkCleanupPending / agent.IsCleanupPending.
func TestCleanupPendingContract(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "session.jsonl")
	writeSessionJSONL(t, sessionPath, []provider.Message{
		{Role: provider.RoleUser, Content: "data"},
	})

	// Mark cleanup-pending using the REAL agent function
	if err := agent.MarkCleanupPending(sessionPath, "delete"); err != nil {
		t.Fatal(err)
	}

	// Verify both agent and our read_session detect it
	if !agent.IsCleanupPending(sessionPath) {
		t.Fatal("agent.IsCleanupPending should detect marker created by agent.MarkCleanupPending")
	}

	tool := NewReadSessionTool(dir)
	_, err := tool.Execute(context.Background(), json.RawMessage(`{"session":"session.jsonl"}`))
	if err == nil {
		t.Fatal("read_session should reject cleanup-pending session created by agent.MarkCleanupPending")
	}
}
