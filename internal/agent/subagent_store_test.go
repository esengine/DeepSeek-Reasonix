package agent

import (
	"strings"
	"testing"

	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func TestSubagentStoreContinueLoadsSavedTranscript(t *testing.T) {
	store := NewSubagentStore(t.TempDir())
	spec := testSubagentSpec(t, "review")
	run, err := store.PrepareFresh(spec)
	if err != nil {
		t.Fatalf("PrepareFresh: %v", err)
	}
	run.Session.Add(provider.Message{Role: provider.RoleUser, Content: "review diff"})
	run.Session.Add(provider.Message{Role: provider.RoleAssistant, Content: "finding A"})
	if err := store.SaveCompleted(run); err != nil {
		t.Fatalf("SaveCompleted: %v", err)
	}
	run.Release()

	continued, err := store.PrepareContinue(run.Ref, spec)
	if err != nil {
		t.Fatalf("PrepareContinue: %v", err)
	}
	defer continued.Release()
	if continued.Ref != run.Ref {
		t.Fatalf("continued ref = %q, want %q", continued.Ref, run.Ref)
	}
	if got := continued.Session.Snapshot(); len(got) != 3 || got[2].Content != "finding A" {
		t.Fatalf("continued transcript = %+v, want saved messages", got)
	}
}

func TestSubagentStoreForkCreatesIndependentReference(t *testing.T) {
	store := NewSubagentStore(t.TempDir())
	spec := testSubagentSpec(t, "review")
	run, err := store.PrepareFresh(spec)
	if err != nil {
		t.Fatalf("PrepareFresh: %v", err)
	}
	run.Session.Add(provider.Message{Role: provider.RoleUser, Content: "review diff"})
	if err := store.SaveCompleted(run); err != nil {
		t.Fatalf("SaveCompleted: %v", err)
	}
	run.Release()

	forked, err := store.PrepareFork(run.Ref, spec)
	if err != nil {
		t.Fatalf("PrepareFork: %v", err)
	}
	defer forked.Release()
	if forked.Ref == run.Ref {
		t.Fatalf("fork ref should be new, got %q", forked.Ref)
	}
	if got := forked.Session.Snapshot(); len(got) != 2 || got[1].Content != "review diff" {
		t.Fatalf("fork transcript = %+v, want copied messages", got)
	}
}

func TestSubagentStoreForkReleasesSourceLockAfterCopy(t *testing.T) {
	store := NewSubagentStore(t.TempDir())
	spec := testSubagentSpec(t, "review")
	run, err := store.PrepareFresh(spec)
	if err != nil {
		t.Fatalf("PrepareFresh: %v", err)
	}
	run.Session.Add(provider.Message{Role: provider.RoleUser, Content: "review diff"})
	if err := store.SaveCompleted(run); err != nil {
		t.Fatalf("SaveCompleted: %v", err)
	}
	run.Release()

	forked, err := store.PrepareFork(run.Ref, spec)
	if err != nil {
		t.Fatalf("PrepareFork: %v", err)
	}
	defer forked.Release()
	continued, err := store.PrepareContinue(run.Ref, spec)
	if err != nil {
		t.Fatalf("source should not stay locked by fork run: %v", err)
	}
	continued.Release()
}

func TestSubagentStoreRejectsIncompatibleTranscript(t *testing.T) {
	store := NewSubagentStore(t.TempDir())
	spec := testSubagentSpec(t, "review")
	run, err := store.PrepareFresh(spec)
	if err != nil {
		t.Fatalf("PrepareFresh: %v", err)
	}
	if err := store.SaveCompleted(run); err != nil {
		t.Fatalf("SaveCompleted: %v", err)
	}
	run.Release()

	other := spec
	other.Name = "security-review"
	if _, err := store.PrepareContinue(run.Ref, other); err == nil || !strings.Contains(err.Error(), "name") {
		t.Fatalf("PrepareContinue error = %v, want incompatible name", err)
	}
}

func TestSubagentStoreRejectsConcurrentContinue(t *testing.T) {
	store := NewSubagentStore(t.TempDir())
	spec := testSubagentSpec(t, "review")
	run, err := store.PrepareFresh(spec)
	if err != nil {
		t.Fatalf("PrepareFresh: %v", err)
	}
	if err := store.SaveCompleted(run); err != nil {
		t.Fatalf("SaveCompleted: %v", err)
	}
	run.Release()

	first, err := store.PrepareContinue(run.Ref, spec)
	if err != nil {
		t.Fatalf("first PrepareContinue: %v", err)
	}
	defer first.Release()
	if _, err := store.PrepareContinue(run.Ref, spec); err == nil || !strings.Contains(err.Error(), "already running") {
		t.Fatalf("second PrepareContinue error = %v, want lock error", err)
	}
}

func testSubagentSpec(t *testing.T, name string) SubagentSpec {
	t.Helper()
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "read_file", readOnly: true})
	return SubagentSpec{
		Kind:          "skill",
		Name:          name,
		WorkspaceRoot: t.TempDir(),
		SystemPrompt:  "review persona",
		Registry:      reg,
		Model:         "deepseek",
		Effort:        "max",
	}
}
