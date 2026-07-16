package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/tool"
	"reasonix/internal/worktree"
)

func TestTaskToolSchemaExposesExplicitIsolationContract(t *testing.T) {
	schema := string((&TaskTool{}).Schema())
	if !strings.Contains(schema, `"run_in_background"`) {
		t.Fatalf("task schema no longer exposes run_in_background:\n%s", schema)
	}
	for _, want := range []string{`"isolation"`, `"none"`, `"worktree"`} {
		if !strings.Contains(schema, want) {
			t.Fatalf("task schema should expose %s:\n%s", want, schema)
		}
	}
}

func TestTaskToolPersistedSubagentRunsDefaultToSharedParentWorkspace(t *testing.T) {
	store := NewSubagentStore(t.TempDir())
	parentWorkspace := t.TempDir()
	task := (&TaskTool{sysPrompt: "system"}).WithTranscripts(store, parentWorkspace, "base-model", "base-effort")
	reg := tool.NewRegistry()

	run1, err := task.prepareTranscriptRun(reg, "", "", "parent-session", "call-1", "", "", parentWorkspace, SubagentIsolationNone, worktree.Resource{})
	if err != nil {
		t.Fatalf("prepare run1: %v", err)
	}
	defer run1.Release()
	run2, err := task.prepareTranscriptRun(reg, "", "", "parent-session", "call-2", "", "", parentWorkspace, SubagentIsolationNone, worktree.Resource{})
	if err != nil {
		t.Fatalf("prepare run2: %v", err)
	}
	defer run2.Release()

	if run1.Meta.WorkspaceRoot != parentWorkspace || run2.Meta.WorkspaceRoot != parentWorkspace {
		t.Fatalf("runs should share parent workspace by default: run1=%q run2=%q parent=%q", run1.Meta.WorkspaceRoot, run2.Meta.WorkspaceRoot, parentWorkspace)
	}
	if run1.Meta.WorkspaceRoot != run2.Meta.WorkspaceRoot {
		t.Fatalf("default runs should not be isolated: run1=%q run2=%q", run1.Meta.WorkspaceRoot, run2.Meta.WorkspaceRoot)
	}

	raw, err := json.Marshal(run1.Meta)
	if err != nil {
		t.Fatal(err)
	}
	var meta map[string]any
	if err := json.Unmarshal(raw, &meta); err != nil {
		t.Fatal(err)
	}
	if meta["isolation"] != "none" {
		t.Fatalf("default isolation = %v, want none; raw=%s", meta["isolation"], string(raw))
	}
	for _, key := range []string{"isolationId", "isolation_id", "worktreeRoot", "worktree_root", "sourceRoot", "source_root"} {
		if _, ok := meta[key]; ok {
			t.Fatalf("default shared subagent metadata should not have %q: %s", key, string(raw))
		}
	}
}

func TestTaskToolPersistsWorktreeIsolationMetadata(t *testing.T) {
	store := NewSubagentStore(t.TempDir())
	parentWorkspace := t.TempDir()
	childWorkspace := t.TempDir()
	task := (&TaskTool{sysPrompt: "system"}).WithTranscripts(store, parentWorkspace, "base-model", "base-effort")
	reg := tool.NewRegistry()
	res := worktree.Resource{
		IsolationID:   "iso-test",
		WorkspaceRoot: childWorkspace,
		WorktreeRoot:  childWorkspace,
		SourceRoot:    parentWorkspace,
		Branch:        "reasonix/subagent-test",
		BaseCommit:    "abc123",
		HeadCommit:    "def456",
	}

	run, err := task.prepareTranscriptRun(reg, "", "", "parent-session", "call-1", "", "", childWorkspace, SubagentIsolationWorktree, res)
	if err != nil {
		t.Fatalf("prepare isolated run: %v", err)
	}
	defer run.Release()

	if run.Meta.WorkspaceRoot != childWorkspace {
		t.Fatalf("workspace root = %q, want %q", run.Meta.WorkspaceRoot, childWorkspace)
	}
	if run.Meta.Isolation != "worktree" || run.Meta.IsolationID != "iso-test" || run.Meta.WorktreeRoot != childWorkspace || run.Meta.SourceRoot != parentWorkspace {
		t.Fatalf("worktree metadata not persisted: %+v", run.Meta)
	}
	if run.Meta.WorktreeBranch != "reasonix/subagent-test" || run.Meta.BaseCommit != "abc123" || run.Meta.HeadCommit != "def456" {
		t.Fatalf("worktree git metadata not persisted: %+v", run.Meta)
	}

	ref := FormatSubagentReference(run)
	for _, want := range []string{"Worktree isolation: iso-test", "Workspace: " + childWorkspace, "Branch: reasonix/subagent-test", "Apply is explicit"} {
		if !strings.Contains(ref, want) {
			t.Fatalf("reference should contain %q:\n%s", want, ref)
		}
	}
}

func TestTaskToolBuildSubRegUsesWorkspaceFactory(t *testing.T) {
	parentWorkspace := t.TempDir()
	childWorkspace := t.TempDir()
	parent := tool.NewRegistry()
	parent.Add(subagentRegistryTool{name: "parent_tool"})
	called := false
	task := (&TaskTool{parentReg: parent, workspaceRoot: parentWorkspace}).WithWorkspaceIsolation(nil, func(ctx context.Context, workspaceRoot string, names []string, childDepth, maxDepth int) (*tool.Registry, error) {
		called = true
		if !cleanPathEqual(workspaceRoot, childWorkspace) {
			t.Fatalf("factory workspace = %q, want %q", workspaceRoot, childWorkspace)
		}
		child := tool.NewRegistry()
		child.Add(subagentRegistryTool{name: "child_tool"})
		return child, nil
	})

	sub, err := task.buildSubRegForWorkspace(context.Background(), childWorkspace, nil, 1)
	if err != nil {
		t.Fatalf("build child registry: %v", err)
	}
	if !called {
		t.Fatal("workspace factory was not called for child worktree root")
	}
	if _, ok := sub.Get("child_tool"); !ok {
		t.Fatalf("child registry missing child tool; got %v", sub.Names())
	}
	if _, ok := sub.Get("parent_tool"); ok {
		t.Fatalf("isolated child registry should be rebuilt for child root, got parent tool in %v", sub.Names())
	}
}

func TestTaskToolNormalizeSubagentIsolation(t *testing.T) {
	for _, raw := range []string{"", "none", " NONE "} {
		got, err := normalizeSubagentIsolation(raw)
		if err != nil || got != SubagentIsolationNone {
			t.Fatalf("normalize %q = %q, %v; want none, nil", raw, got, err)
		}
	}
	got, err := normalizeSubagentIsolation(" WORKTREE ")
	if err != nil || got != SubagentIsolationWorktree {
		t.Fatalf("normalize worktree = %q, %v", got, err)
	}
	if _, err := normalizeSubagentIsolation("sandbox"); err == nil {
		t.Fatal("unknown isolation should be rejected")
	}
}
