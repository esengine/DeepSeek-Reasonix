package boot

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/tool"
)

func prepareRunningSubagent(t *testing.T, sessionDir string) string {
	t.Helper()
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

func requireSubagentStatus(t *testing.T, sessionDir, ref string, want agent.SubagentStatus) {
	t.Helper()
	meta, err := agent.NewSubagentStore(filepath.Join(sessionDir, "subagents")).LoadMeta(ref)
	if err != nil {
		t.Fatalf("LoadMeta(%s): %v", ref, err)
	}
	if meta.Status != want {
		t.Fatalf("status = %q, want %q", meta.Status, want)
	}
}

func TestNewSubagentStoreCleansStaleRunningOnEveryBuild(t *testing.T) {
	sessionDir := t.TempDir()

	firstRef := prepareRunningSubagent(t, sessionDir)
	if _, err := newSubagentStore(sessionDir, nil, nil); err != nil {
		t.Fatalf("newSubagentStore (first): %v", err)
	}
	requireSubagentStatus(t, sessionDir, firstRef, agent.SubagentInterrupted)

	secondRef := prepareRunningSubagent(t, sessionDir)
	if _, err := newSubagentStore(sessionDir, nil, nil); err != nil {
		t.Fatalf("newSubagentStore (second): %v", err)
	}
	requireSubagentStatus(t, sessionDir, secondRef, agent.SubagentInterrupted)
}

func TestNewSubagentStoreParentProbeDefersThenRecovers(t *testing.T) {
	sessionDir := t.TempDir()
	ref := prepareRunningSubagent(t, sessionDir)
	parentPath := filepath.Join(sessionDir, "parent-session.jsonl")

	if _, err := newSubagentStore(sessionDir, func(path string) bool {
		return filepath.Clean(path) == filepath.Clean(parentPath)
	}, nil); err != nil {
		t.Fatalf("newSubagentStore (live parent): %v", err)
	}
	requireSubagentStatus(t, sessionDir, ref, agent.SubagentRunning)

	if _, err := newSubagentStore(sessionDir, func(string) bool { return false }, nil); err != nil {
		t.Fatalf("newSubagentStore (dead parent): %v", err)
	}
	requireSubagentStatus(t, sessionDir, ref, agent.SubagentInterrupted)
}

func TestNewSubagentStoreNeverCachesCleanupError(t *testing.T) {
	sessionDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(sessionDir, "subagents"), []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	for attempt := 1; attempt <= 2; attempt++ {
		if _, err := newSubagentStore(sessionDir, nil, nil); err == nil {
			t.Fatalf("newSubagentStore attempt %d unexpectedly succeeded", attempt)
		}
	}
}

func TestNewSubagentStoreRunsCleanupInsideSerializedCoordinator(t *testing.T) {
	sessionDir := t.TempDir()
	ref := prepareRunningSubagent(t, sessionDir)

	inside := false
	coordinated := false
	coordinator := func(fn func() error) error {
		coordinated = true
		// The coordinator must be the only lease-active region while cleanup
		// runs: verify cleanup is invoked (and can re-enter) inside it.
		inside = true
		err := fn()
		inside = false
		return err
	}
	store, err := newSubagentStore(sessionDir, nil, coordinator)
	if err != nil {
		t.Fatalf("newSubagentStore (serialized): %v", err)
	}
	if store == nil {
		t.Fatal("newSubagentStore returned nil store")
	}
	if !coordinated {
		t.Fatal("serialized coordinator was not invoked")
	}
	if inside {
		t.Fatal("cleanup did not finish before the coordinator returned")
	}
	requireSubagentStatus(t, sessionDir, ref, agent.SubagentInterrupted)
}

func TestNewSubagentStoreSerializedCoordinatorErrorPropagates(t *testing.T) {
	sessionDir := t.TempDir()
	prepareRunningSubagent(t, sessionDir)
	coordErr := errors.New("coordinator refused")
	_, err := newSubagentStore(sessionDir, nil, func(fn func() error) error {
		return coordErr
	})
	if err == nil {
		t.Fatal("newSubagentStore unexpectedly succeeded despite coordinator error")
	}
	if !errors.Is(err, coordErr) {
		t.Fatalf("coordinator error not propagated: %v", err)
	}
}
