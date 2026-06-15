package control

import (
	"strings"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

func coldResumeFixture(t *testing.T, threshold time.Duration) (*agent.Session, string) {
	t.Helper()
	old := cacheColdAfter
	cacheColdAfter = threshold
	t.Cleanup(func() { cacheColdAfter = old })

	dir := t.TempDir()
	loaded := &agent.Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "task"},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "1", Name: "grep", Arguments: "{}"}}},
		{Role: provider.RoleTool, ToolCallID: "1", Name: "grep", Content: strings.Repeat("y", 5000)},
		{Role: provider.RoleAssistant, Content: "step done"},
		{Role: provider.RoleUser, Content: "next"},
		{Role: provider.RoleAssistant, Content: "ok"},
	}}
	path := agent.NewSessionPath(dir, "test")
	if err := loaded.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := agent.EnsureBranchMeta(path); err != nil {
		t.Fatalf("meta: %v", err)
	}

	exec := agent.New(nil, nil, agent.NewSession("sys"), agent.Options{ContextWindow: 1000, RecentKeep: 2, ArchiveDir: dir}, event.Discard)
	c := New(Options{Executor: exec, SessionDir: dir, Label: "test"})
	c.Resume(loaded, path)
	return loaded, path
}

func TestColdResumePrunesAndPersists(t *testing.T) {
	loaded, path := coldResumeFixture(t, 0)

	msgs := loaded.Snapshot()
	if !strings.HasPrefix(msgs[3].Content, "[elided tool result") {
		t.Fatalf("stale tool result not pruned on cold resume: %.60q", msgs[3].Content)
	}
	re, err := agent.LoadSession(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !strings.HasPrefix(re.Messages[3].Content, "[elided tool result") {
		t.Error("pruned transcript was not persisted")
	}
	if re.Messages[3].ToolCallID != "1" {
		t.Error("tool pairing lost in persisted transcript")
	}
}

func TestWarmResumeLeavesHistoryAlone(t *testing.T) {
	loaded, path := coldResumeFixture(t, 24*time.Hour)

	if got := loaded.Snapshot()[3].Content; !strings.HasPrefix(got, "yyy") {
		t.Fatalf("warm resume rewrote history: %.60q", got)
	}
	re, err := agent.LoadSession(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !strings.HasPrefix(re.Messages[3].Content, "yyy") {
		t.Error("warm resume rewrote the saved transcript")
	}
}
