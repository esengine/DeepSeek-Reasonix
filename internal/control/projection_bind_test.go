package control

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func TestNewSessionRebindsProjectionSidecarPath(t *testing.T) {
	dir := t.TempDir()
	oldPath := agent.NewSessionPath(dir, "old")
	sess := agent.NewSession("sys")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "old work"})
	if err := sess.Save(oldPath); err != nil {
		t.Fatal(err)
	}
	// Seed an old projection sidecar.
	if err := agent.SaveCompactionState(oldPath, agent.CompactionState{
		SchemaVersion: 1,
		Projection: agent.ContextProjection{
			Messages:     []provider.Message{{Role: provider.RoleSystem, Content: "sys"}},
			CoveredCount: 1,
		},
		PromptCacheKey: "ws|old|model",
	}); err != nil {
		t.Fatal(err)
	}

	exec := agent.New(nil, nil, agent.NewSession("sys"), agent.Options{
		ContextWindow: 1000,
		SessionPath:   oldPath,
		ModelRef:      "test/model",
		WorkspaceID:   "ws",
	}, event.Discard)
	c := New(Options{Executor: exec, SessionDir: dir, Label: "test", DisableColdResumePrune: true})
	c.Resume(sess, oldPath)
	if got := exec.SessionPath(); got != oldPath {
		t.Fatalf("after resume SessionPath = %q, want %q", got, oldPath)
	}

	if err := c.NewSession(); err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	newPath := c.SessionPath()
	if newPath == "" || newPath == oldPath {
		t.Fatalf("NewSession path = %q, want a distinct path", newPath)
	}
	if got := exec.SessionPath(); got != newPath {
		t.Fatalf("executor SessionPath after NewSession = %q, want %q", got, newPath)
	}
	// Old sidecar must remain; new path must not inherit the old projection body.
	if _, ok, err := agent.LoadCompactionState(oldPath); err != nil || !ok {
		t.Fatalf("old sidecar should remain: ok=%v err=%v", ok, err)
	}
	if st, ok, err := agent.LoadCompactionState(newPath); err != nil {
		t.Fatalf("load new sidecar: %v", err)
	} else if ok && len(st.Projection.Messages) > 0 {
		t.Fatalf("new session should not load old projection: %+v", st.Projection)
	}
	// Writing a projection after rotation must land on the new path.
	if err := agent.SaveCompactionState(exec.SessionPath(), agent.CompactionState{
		SchemaVersion: 1,
		Projection: agent.ContextProjection{
			Messages:          []provider.Message{{Role: provider.RoleSystem, Content: "sys"}},
			CoveredCount:      1,
			CoveredPrefixHash: "x",
		},
	}); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := agent.LoadCompactionState(newPath); err != nil || !ok {
		t.Fatalf("projection must be readable at new path: ok=%v err=%v", ok, err)
	}
	// Ensure we did not overwrite old sidecar content with the new key.
	old, ok, err := agent.LoadCompactionState(oldPath)
	if err != nil || !ok {
		t.Fatalf("old sidecar: ok=%v err=%v", ok, err)
	}
	if old.PromptCacheKey != "ws|old|model" {
		t.Fatalf("old sidecar polluted: %+v", old)
	}
}

func TestClearSessionRebindsProjectionSidecarPath(t *testing.T) {
	dir := t.TempDir()
	path := agent.NewSessionPath(dir, "s")
	sess := agent.NewSession("sys")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "hi"})
	if err := sess.Save(path); err != nil {
		t.Fatal(err)
	}
	exec := agent.New(nil, nil, agent.NewSession("sys"), agent.Options{SessionPath: path}, event.Discard)
	c := New(Options{Executor: exec, SessionDir: dir, Label: "test", DisableColdResumePrune: true})
	c.Resume(sess, path)

	if err := c.ClearSession(); err != nil {
		t.Fatalf("ClearSession: %v", err)
	}
	if got := exec.SessionPath(); got == path || got == "" {
		t.Fatalf("ClearSession left executor path at %q", got)
	}
	if exec.SessionPath() != c.SessionPath() {
		t.Fatalf("executor=%q controller=%q", exec.SessionPath(), c.SessionPath())
	}
}

func TestBranchRebindsProjectionSidecarPath(t *testing.T) {
	dir := t.TempDir()
	path := agent.NewSessionPath(dir, "main")
	sess := agent.NewSession("sys")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "task"})
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "ok"})
	for i := range 30 {
		id := fmt.Sprintf("bulk-%d", i)
		sess.Add(provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: id, Name: "read_file", Arguments: "{}"}}})
		sess.Add(provider.Message{Role: provider.RoleTool, ToolCallID: id, Name: "read_file", Content: strings.Repeat("line\n", 200)})
	}
	if err := sess.Save(path); err != nil {
		t.Fatal(err)
	}
	if _, err := agent.EnsureBranchMeta(path); err != nil {
		t.Fatal(err)
	}
	prov := &scriptedTurns{turns: [][]provider.Chunk{textTurn("summary")}}
	exec := agent.New(prov, tool.NewRegistry(), agent.NewSession("sys"), agent.Options{
		SessionPath: path, ContextWindow: 10_000, CompactRatio: 0.8, RecentKeep: 2,
	}, event.Discard)
	c := New(Options{Executor: exec, SessionDir: dir, Label: "test", DisableColdResumePrune: true})
	c.Resume(sess, path)
	if err := exec.CompactNow(context.Background(), ""); err != nil {
		t.Fatalf("CompactNow: %v", err)
	}
	before := exec.ContextMaintenanceSnapshot()
	if before.ProjectionVersion == 0 {
		t.Fatal("branch fixture installed no projection")
	}

	newPath, err := c.Branch("side")
	if err != nil {
		t.Fatalf("Branch: %v", err)
	}
	if newPath == path {
		t.Fatal("Branch returned the same path")
	}
	if got := exec.SessionPath(); got != newPath {
		t.Fatalf("executor SessionPath after Branch = %q, want %q", got, newPath)
	}
	if got := c.SessionPath(); got != newPath {
		t.Fatalf("controller SessionPath after Branch = %q, want %q", got, newPath)
	}
	after := exec.ContextMaintenanceSnapshot()
	if after.ProjectionVersion != before.ProjectionVersion {
		t.Fatalf("branch projection version %d -> %d", before.ProjectionVersion, after.ProjectionVersion)
	}
	if _, ok, err := agent.LoadCompactionState(newPath); err != nil || !ok {
		t.Fatalf("branch projection sidecar: ok=%v err=%v", ok, err)
	}
}
