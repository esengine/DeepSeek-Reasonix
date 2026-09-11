package sessiontool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/agent"
)

func writeTitleTestSession(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(`{"role":"user","content":"hello"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSetSessionTitleUsesHostBoundCurrentSession(t *testing.T) {
	dir := t.TempDir()
	current := writeTitleTestSession(t, dir, "current.jsonl")
	other := writeTitleTestSession(t, dir, "other.jsonl")
	var projectedDir, projectedPath, projectedTitle string
	tool := NewSetSessionTitleTool(dir, func() string { return current }, func(sessionDir, sessionPath, title string) error {
		projectedDir, projectedPath, projectedTitle = sessionDir, sessionPath, title
		return nil
	})

	got, err := tool.Execute(context.Background(), json.RawMessage(`{"title":"  Current work  ","session":"other.jsonl"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(got, "Current work") {
		t.Fatalf("result = %q", got)
	}
	meta, ok, err := agent.LoadBranchMeta(current)
	if err != nil || !ok || meta.CustomTitle != "Current work" {
		t.Fatalf("current meta = %+v, ok=%v, err=%v", meta, ok, err)
	}
	if _, ok, err := agent.LoadBranchMeta(other); err != nil || ok {
		t.Fatalf("other session changed: ok=%v err=%v", ok, err)
	}
	if projectedDir != dir || projectedPath != current || projectedTitle != "Current work" {
		t.Fatalf("projection = %q %q %q", projectedDir, projectedPath, projectedTitle)
	}
}

func TestSetSessionTitleClearsTitle(t *testing.T) {
	dir := t.TempDir()
	path := writeTitleTestSession(t, dir, "current.jsonl")
	if err := agent.RenameSession(path, "Before"); err != nil {
		t.Fatal(err)
	}
	tool := NewSetSessionTitleTool(dir, func() string { return path }, nil)
	if _, err := tool.Execute(context.Background(), json.RawMessage(`{"title":""}`)); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	meta, ok, err := agent.LoadBranchMeta(path)
	if err != nil || !ok || meta.CustomTitle != "" {
		t.Fatalf("meta = %+v, ok=%v, err=%v", meta, ok, err)
	}
}

func TestSetSessionTitleRejectsUnavailableOrInvalidCurrentPath(t *testing.T) {
	dir := t.TempDir()
	tests := []struct {
		name string
		path string
	}{
		{name: "empty"},
		{name: "directory", path: dir},
		{name: "non transcript", path: filepath.Join(dir, "notes.txt")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := NewSetSessionTitleTool(dir, func() string { return tt.path }, nil)
			if _, err := tool.Execute(context.Background(), json.RawMessage(`{"title":"Title"}`)); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestSetSessionTitleRequiresTitleAndRejectsSubagent(t *testing.T) {
	dir := t.TempDir()
	path := writeTitleTestSession(t, dir, "current.jsonl")
	tool := NewSetSessionTitleTool(dir, func() string { return path }, nil)
	if _, err := tool.Execute(context.Background(), json.RawMessage(`{}`)); err == nil {
		t.Fatal("missing title should fail")
	}
	tooLong, _ := json.Marshal(map[string]string{"title": strings.Repeat("界", setSessionTitleMaxRunes+1)})
	if _, err := tool.Execute(context.Background(), tooLong); err == nil {
		t.Fatal("oversized title should fail")
	}
	ctx := agent.WithSubagentDepth(context.Background(), 1)
	if _, err := tool.Execute(ctx, json.RawMessage(`{"title":"Wrong owner"}`)); err == nil {
		t.Fatal("subagent should not rename the parent session")
	}
}
