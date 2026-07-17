package agent

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/jobs"
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

func TestTaskToolWriterRunsInsideWorktreeAndContinueReusesResource(t *testing.T) {
	source := initTaskIsolationRepo(t)
	manager := worktree.NewManager(filepath.Join(t.TempDir(), "managed"))
	store := NewSubagentStore(t.TempDir())
	parentReg := tool.NewRegistry()
	provider := writerCallingProvider{}

	task := NewTaskTool(provider, nil, parentReg, 20, 0, 0, 0, 0, 0, 0, 0, "", "sys", nil, 0, "", "", nil).
		WithTranscripts(store, source, "base-model", "base-effort").
		WithWorkspaceIsolation(manager, func(_ context.Context, workspaceRoot string, _ []string, _, _ int) (*tool.Registry, error) {
			reg := tool.NewRegistry()
			reg.Add(worktreeRootWriter{root: workspaceRoot})
			return reg, nil
		})

	out, err := task.Execute(testTaskContext(), []byte(`{"prompt":"write the isolated file","isolation":"worktree"}`))
	if err != nil {
		t.Fatalf("isolated Execute: %v", err)
	}
	ref := subagentRefFromOutput(t, out)
	resources, err := manager.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(resources) != 1 {
		t.Fatalf("managed resources = %d, want 1: %+v", len(resources), resources)
	}
	childPath := filepath.Join(resources[0].WorkspaceRoot, "x")
	if got, err := os.ReadFile(childPath); err != nil || string(got) != "y" {
		t.Fatalf("isolated writer result = %q, %v; want child x=y", got, err)
	}
	if _, err := os.Stat(filepath.Join(source, "x")); !os.IsNotExist(err) {
		t.Fatalf("parent workspace was mutated, stat err = %v", err)
	}

	if _, err := task.Execute(testTaskContext(), []byte(`{"prompt":"continue","continue_from":"`+ref+`"}`)); err != nil {
		t.Fatalf("continued Execute: %v", err)
	}
	after, err := manager.List(context.Background())
	if err != nil {
		t.Fatalf("List after continuation: %v", err)
	}
	if len(after) != 1 || after[0].IsolationID != resources[0].IsolationID || after[0].WorkspaceRoot != resources[0].WorkspaceRoot {
		t.Fatalf("continuation created or changed isolation resource: before=%+v after=%+v", resources, after)
	}
}

func TestTaskToolBackgroundWritersUseIndependentWorktrees(t *testing.T) {
	source := initTaskIsolationRepo(t)
	manager := worktree.NewManager(filepath.Join(t.TempDir(), "managed"))
	store := NewSubagentStore(t.TempDir())
	task := NewTaskTool(writerCallingProvider{}, nil, tool.NewRegistry(), 20, 0, 0, 0, 0, 0, 0, 0, "", "sys", nil, 0, "", "", nil).
		WithTranscripts(store, source, "base-model", "base-effort").
		WithWorkspaceIsolation(manager, func(_ context.Context, workspaceRoot string, _ []string, _, _ int) (*tool.Registry, error) {
			reg := tool.NewRegistry()
			reg.Add(worktreeRootWriter{root: workspaceRoot})
			return reg, nil
		})

	jm := jobs.NewManager(event.Discard)
	defer jm.Close()
	ctx := jobs.WithManager(jobs.WithSession(testTaskContext(), "parent-session"), jm)

	var jobIDs []string
	for _, prompt := range []string{"writer one", "writer two"} {
		out, err := task.Execute(ctx, []byte(`{"prompt":"`+prompt+`","run_in_background":true,"isolation":"worktree"}`))
		if err != nil {
			t.Fatalf("start %q: %v", prompt, err)
		}
		jobID := extractJobID(out)
		if jobID == "" {
			t.Fatalf("start %q returned no job ID:\n%s", prompt, out)
		}
		jobIDs = append(jobIDs, jobID)
	}

	results := jm.WaitForSession(context.Background(), "parent-session", jobIDs, 10)
	if len(results) != 2 {
		t.Fatalf("background results = %d, want 2: %+v", len(results), results)
	}
	for _, result := range results {
		if result.Status != jobs.Done {
			t.Fatalf("background writer %s status = %s, want done: %+v", result.ID, result.Status, result)
		}
	}

	resources, err := manager.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(resources) != 2 {
		t.Fatalf("managed resources = %d, want 2: %+v", len(resources), resources)
	}
	if resources[0].IsolationID == resources[1].IsolationID || cleanPathEqual(resources[0].WorkspaceRoot, resources[1].WorkspaceRoot) {
		t.Fatalf("background writers shared isolation: %+v", resources)
	}
	for _, resource := range resources {
		got, err := os.ReadFile(filepath.Join(resource.WorkspaceRoot, "x"))
		if err != nil || string(got) != "y" {
			t.Fatalf("isolated result for %s = %q, %v; want x=y", resource.IsolationID, got, err)
		}
	}
	if _, err := os.Stat(filepath.Join(source, "x")); !os.IsNotExist(err) {
		t.Fatalf("parent workspace was mutated, stat err = %v", err)
	}
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = source
	if out, err := cmd.CombinedOutput(); err != nil || len(out) != 0 {
		t.Fatalf("parent git status = %q, %v; want clean", out, err)
	}
}

type worktreeRootWriter struct {
	root string
}

func (w worktreeRootWriter) Name() string            { return "write_file" }
func (w worktreeRootWriter) Description() string     { return "write a file" }
func (w worktreeRootWriter) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (w worktreeRootWriter) ReadOnly() bool          { return false }
func (w worktreeRootWriter) Execute(_ context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", err
	}
	path := filepath.Join(w.root, filepath.Clean(p.Path))
	if err := os.WriteFile(path, []byte(p.Content), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func initTaskIsolationRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	commands := [][]string{
		{"init", "-b", "main"},
		{"config", "user.email", "reasonix@example.test"},
		{"config", "user.name", "Reasonix Test"},
	}
	for _, args := range commands {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "seed.txt"), []byte("seed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "seed.txt"}, {"commit", "-m", "seed"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return root
}
