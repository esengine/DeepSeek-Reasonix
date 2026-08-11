package control

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

func coldResumeFixture(t *testing.T, threshold time.Duration) (*agent.Session, string, *Controller) {
	t.Helper()

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
	if threshold == 0 {
		c.testCacheColdAfter = -1 // force cold
	} else {
		c.testCacheColdAfter = threshold
	}
	c.Resume(loaded, path)
	return loaded, path, c
}

func TestColdResumeDoesNotRewriteOrNetwork(t *testing.T) {
	loaded, path, c := coldResumeFixture(t, 0)

	msgs := loaded.Snapshot()
	if !strings.HasPrefix(msgs[3].Content, "yyy") {
		t.Fatalf("cold resume rewrote tool result: %.60q", msgs[3].Content)
	}
	re, err := agent.LoadSession(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !strings.HasPrefix(re.Messages[3].Content, "yyy") {
		t.Error("cold resume rewrote the saved transcript")
	}
	if c.executor.CacheState() != agent.CacheStateCold {
		t.Fatalf("cache state = %q, want cold", c.executor.CacheState())
	}
	// No network: executor has nil provider; if cold resume called Compact it would panic/fail.
}

func TestColdResumeAfterClonedHistoryStaysInPlace(t *testing.T) {
	dir := t.TempDir()
	saved := agent.NewSession("old sys")
	saved.Add(provider.Message{Role: provider.RoleUser, Content: "task"})
	saved.Add(provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "1", Name: "grep", Arguments: "{}"}}})
	saved.Add(provider.Message{Role: provider.RoleTool, ToolCallID: "1", Name: "grep", Content: strings.Repeat("y", 5000)})
	saved.Add(provider.Message{Role: provider.RoleAssistant, Content: "step done"})
	saved.Add(provider.Message{Role: provider.RoleUser, Content: "next"})
	saved.Add(provider.Message{Role: provider.RoleAssistant, Content: "ok"})
	path := agent.NewSessionPath(dir, "test")
	if err := saved.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := agent.EnsureBranchMeta(path); err != nil {
		t.Fatalf("meta: %v", err)
	}

	loaded, err := agent.LoadSession(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	msgs := loaded.Snapshot()
	msgs[0].Content = "new sys"
	resumed := loaded.CloneWithMessages(msgs)

	exec := agent.New(nil, nil, agent.NewSession("new sys"), agent.Options{ContextWindow: 1000, RecentKeep: 2, ArchiveDir: dir}, event.Discard)
	c := New(Options{Executor: exec, SessionDir: dir, Label: "test"})
	c.testCacheColdAfter = -1 // force cold
	c.Resume(resumed, path)

	if got := c.SessionPath(); got != path {
		t.Fatalf("SessionPath after cold resume = %q, want %q", got, path)
	}
	// Snapshot was not rewritten by cold resume; in-memory clone keeps new sys
	// until an explicit save. Disk still has whatever was last saved unless
	// SnapshotRewrite ran — cold path must not rewrite.
	re, err := agent.LoadSession(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := re.Messages[0].Content; got != "old sys" {
		// Cold resume must not SnapshotRewrite the cloned in-memory system prompt.
		t.Fatalf("system prompt on disk after cold resume = %q, want old sys (no rewrite)", got)
	}
	if !strings.HasPrefix(re.Messages[3].Content, "yyy") {
		t.Fatalf("tool result rewrote on cold resume: %.60q", re.Messages[3].Content)
	}
	if matches, err := filepath.Glob(filepath.Join(dir, "*-recovery-*.jsonl")); err != nil || len(matches) != 0 {
		t.Fatalf("recovery branches after cloned cold resume = %v err=%v, want none", matches, err)
	}
}

func TestWarmResumeLeavesHistoryAlone(t *testing.T) {
	loaded, path, c := coldResumeFixture(t, 24*time.Hour)

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
	if c.executor.CacheState() != agent.CacheStateWarm {
		t.Fatalf("cache state = %q, want warm", c.executor.CacheState())
	}
}

func TestColdResumeUnderThresholdUsesCanonical(t *testing.T) {
	// Small session well under context window: preflight must not compact.
	loaded, path, c := coldResumeFixture(t, 0)
	_ = path
	if c.executor.CacheState() != agent.CacheStateCold {
		t.Fatalf("want cold, got %s", c.executor.CacheState())
	}
	// Model-visible should still be full transcript (no projection yet).
	visible := c.executor.Session().Snapshot()
	if len(visible) != len(loaded.Snapshot()) {
		t.Fatalf("visible/canonical mismatch without pressure")
	}
	if len(c.executor.CacheState()) == 0 {
		t.Fatal("cache state empty")
	}
}
