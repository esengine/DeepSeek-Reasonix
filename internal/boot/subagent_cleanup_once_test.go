package boot

import (
	"path/filepath"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/tool"
)

// TestNewSubagentStoreCleansStaleRunningOncePerSessionDir pins the
// per-process sweep-once contract: repeated controller builds for the same
// session dir must not re-scan the shared subagents directory. Each sweep
// transiently acquires the running subagent's parent session lease, so
// sweeping inside every concurrent desktop tab build makes those builds race
// the probe against their own startup bind and surface spurious
// "already open in another Reasonix window" errors.
func TestNewSubagentStoreCleansStaleRunningOncePerSessionDir(t *testing.T) {
	sessionDir := t.TempDir()
	newRunning := func() string {
		store := agent.NewSubagentStore(filepath.Join(sessionDir, "subagents"))
		spec := agent.SubagentSpec{
			Kind:          "task",
			Name:          "task",
			WorkspaceRoot: t.TempDir(),
			ParentSession: "parent-session",
			SystemPrompt:  "sys",
			Registry:      tool.NewRegistry(),
			Model:         "base-model",
		}
		run, err := store.PrepareFresh(spec)
		if err != nil {
			t.Fatalf("PrepareFresh: %v", err)
		}
		if err := store.MarkRunning(run); err != nil {
			t.Fatalf("MarkRunning: %v", err)
		}
		ref := run.Ref
		run.Release()
		return ref
	}

	// First controller build for this session dir sweeps stale running
	// sub-agents, as it always has.
	firstRef := newRunning()
	if _, err := newSubagentStore(sessionDir); err != nil {
		t.Fatalf("newSubagentStore (first): %v", err)
	}
	meta, err := agent.NewSubagentStore(filepath.Join(sessionDir, "subagents")).LoadMeta(firstRef)
	if err != nil {
		t.Fatalf("LoadMeta(first): %v", err)
	}
	if meta.Status != agent.SubagentInterrupted {
		t.Fatalf("first sweep status = %q, want interrupted", meta.Status)
	}

	// A later build for the same session dir (desktop: every tab rebuild)
	// must not sweep again.
	secondRef := newRunning()
	if _, err := newSubagentStore(sessionDir); err != nil {
		t.Fatalf("newSubagentStore (second): %v", err)
	}
	meta, err = agent.NewSubagentStore(filepath.Join(sessionDir, "subagents")).LoadMeta(secondRef)
	if err != nil {
		t.Fatalf("LoadMeta(second): %v", err)
	}
	if meta.Status != agent.SubagentRunning {
		t.Fatalf("second sweep status = %q, want running (sweep must run once per process)", meta.Status)
	}
}
