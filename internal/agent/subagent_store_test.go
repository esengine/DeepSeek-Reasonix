package agent

import (
	"os"
	"path/filepath"
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
	if forked.Meta.ParentSession != spec.ParentSession {
		t.Fatalf("fork parent session = %q, want %q", forked.Meta.ParentSession, spec.ParentSession)
	}
}

func TestSubagentStoreRejectsContinueFromDifferentParentSession(t *testing.T) {
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
	other.ParentSession = "other-parent"
	if _, err := store.PrepareContinue(run.Ref, other); err == nil || !strings.Contains(err.Error(), "use fork_from") {
		t.Fatalf("PrepareContinue error = %v, want parent ownership failure", err)
	}
}

func TestSubagentStoreForkFromAncestorSessionCreatesCurrentOwner(t *testing.T) {
	sessionDir := t.TempDir()
	store := NewSubagentStore(filepath.Join(sessionDir, "subagents"))
	spec := testSubagentSpec(t, "review")
	spec.ParentSession = "root"
	run, err := store.PrepareFresh(spec)
	if err != nil {
		t.Fatalf("PrepareFresh: %v", err)
	}
	run.Session.Add(provider.Message{Role: provider.RoleUser, Content: "review diff"})
	if err := store.SaveCompleted(run); err != nil {
		t.Fatalf("SaveCompleted: %v", err)
	}
	run.Release()

	saveTestBranchMeta(t, sessionDir, "root", "")
	saveTestBranchMeta(t, sessionDir, "child", "root")
	other := spec
	other.ParentSession = "child"
	forked, err := store.PrepareFork(run.Ref, other)
	if err != nil {
		t.Fatalf("PrepareFork: %v", err)
	}
	defer forked.Release()
	if forked.Ref == run.Ref {
		t.Fatalf("fork ref should be new, got %q", forked.Ref)
	}
	if forked.Meta.ParentSession != "child" {
		t.Fatalf("fork parent session = %q, want child", forked.Meta.ParentSession)
	}
	sourceMeta, err := store.LoadMeta(run.Ref)
	if err != nil {
		t.Fatalf("LoadMeta source: %v", err)
	}
	if sourceMeta.ParentSession != spec.ParentSession {
		t.Fatalf("source parent session = %q, want %q", sourceMeta.ParentSession, spec.ParentSession)
	}
}

func TestSubagentStoreRejectsForkWhenSourceOwnerMetaMissing(t *testing.T) {
	sessionDir, store, ref, spec := prepareCompletedSubagentForLineageTest(t, "root")
	saveTestBranchMeta(t, sessionDir, "child", "root")

	other := spec
	other.ParentSession = "child"
	if _, err := store.PrepareFork(ref, other); err == nil || !strings.Contains(err.Error(), "lineage could not be verified") {
		t.Fatalf("PrepareFork error = %v, want unverified lineage rejection", err)
	}
}

func TestSubagentStoreRejectsForkWhenSourceOwnerMetaCorrupt(t *testing.T) {
	sessionDir, store, ref, spec := prepareCompletedSubagentForLineageTest(t, "root")
	saveTestBranchMeta(t, sessionDir, "child", "root")
	if err := os.WriteFile(filepath.Join(sessionDir, "root.jsonl.meta"), []byte("{"), 0o644); err != nil {
		t.Fatalf("write corrupt branch meta: %v", err)
	}

	other := spec
	other.ParentSession = "child"
	if _, err := store.PrepareFork(ref, other); err == nil || !strings.Contains(err.Error(), "lineage could not be verified") {
		t.Fatalf("PrepareFork error = %v, want unverified lineage rejection", err)
	}
}

func TestSubagentStoreRejectsForkWhenSourceOwnerMetaIDDiffers(t *testing.T) {
	sessionDir, store, ref, spec := prepareCompletedSubagentForLineageTest(t, "root")
	saveTestBranchMeta(t, sessionDir, "child", "root")
	if err := SaveBranchMeta(filepath.Join(sessionDir, "root.jsonl"), BranchMeta{ID: "other-root"}); err != nil {
		t.Fatalf("SaveBranchMeta(root): %v", err)
	}

	other := spec
	other.ParentSession = "child"
	if _, err := store.PrepareFork(ref, other); err == nil || !strings.Contains(err.Error(), "lineage could not be verified") {
		t.Fatalf("PrepareFork error = %v, want unverified lineage rejection", err)
	}
}

func TestSubagentStoreRejectsForkFromSiblingSession(t *testing.T) {
	sessionDir := t.TempDir()
	store := NewSubagentStore(filepath.Join(sessionDir, "subagents"))
	spec := testSubagentSpec(t, "review")
	spec.ParentSession = "left"
	run, err := store.PrepareFresh(spec)
	if err != nil {
		t.Fatalf("PrepareFresh: %v", err)
	}
	if err := store.SaveCompleted(run); err != nil {
		t.Fatalf("SaveCompleted: %v", err)
	}
	run.Release()

	saveTestBranchMeta(t, sessionDir, "root", "")
	saveTestBranchMeta(t, sessionDir, "left", "root")
	saveTestBranchMeta(t, sessionDir, "right", "root")
	other := spec
	other.ParentSession = "right"
	if _, err := store.PrepareFork(run.Ref, other); err == nil || !strings.Contains(err.Error(), "not in current parent session") {
		t.Fatalf("PrepareFork error = %v, want lineage rejection", err)
	}
}

func TestSubagentStoreRejectsForkFromUnrelatedSession(t *testing.T) {
	sessionDir := t.TempDir()
	store := NewSubagentStore(filepath.Join(sessionDir, "subagents"))
	spec := testSubagentSpec(t, "review")
	spec.ParentSession = "source"
	run, err := store.PrepareFresh(spec)
	if err != nil {
		t.Fatalf("PrepareFresh: %v", err)
	}
	if err := store.SaveCompleted(run); err != nil {
		t.Fatalf("SaveCompleted: %v", err)
	}
	run.Release()

	saveTestBranchMeta(t, sessionDir, "root", "")
	saveTestBranchMeta(t, sessionDir, "current", "root")
	other := spec
	other.ParentSession = "current"
	if _, err := store.PrepareFork(run.Ref, other); err == nil || !strings.Contains(err.Error(), "not in current parent session") {
		t.Fatalf("PrepareFork error = %v, want unrelated session rejection", err)
	}
}

func TestSubagentStoreRejectsForkWhenLineageCannotBeProven(t *testing.T) {
	sessionDir := t.TempDir()
	store := NewSubagentStore(filepath.Join(sessionDir, "subagents"))
	spec := testSubagentSpec(t, "review")
	spec.ParentSession = "root"
	run, err := store.PrepareFresh(spec)
	if err != nil {
		t.Fatalf("PrepareFresh: %v", err)
	}
	if err := store.SaveCompleted(run); err != nil {
		t.Fatalf("SaveCompleted: %v", err)
	}
	run.Release()

	other := spec
	other.ParentSession = "child"
	if _, err := store.PrepareFork(run.Ref, other); err == nil || !strings.Contains(err.Error(), "lineage could not be verified") {
		t.Fatalf("PrepareFork error = %v, want unverified lineage rejection", err)
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

func TestSubagentStoreSaveFailedPersistsTranscriptAndRejectsReuse(t *testing.T) {
	store := NewSubagentStore(t.TempDir())
	spec := testSubagentSpec(t, "review")
	run, err := store.PrepareFresh(spec)
	if err != nil {
		t.Fatalf("PrepareFresh: %v", err)
	}
	run.Session.Add(provider.Message{Role: provider.RoleUser, Content: "failed continuation"})
	if err := store.SaveFailed(run); err != nil {
		t.Fatalf("SaveFailed: %v", err)
	}
	run.Release()

	loaded, err := LoadSession(store.sessionPath(run.Ref))
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if got := loaded.Snapshot(); len(got) != 2 || got[1].Content != "failed continuation" {
		t.Fatalf("failed transcript = %+v, want persisted failed prompt", got)
	}
	meta, err := store.LoadMeta(run.Ref)
	if err != nil {
		t.Fatalf("LoadMeta: %v", err)
	}
	if meta.Status != SubagentFailed {
		t.Fatalf("status = %q, want failed", meta.Status)
	}
	if _, err := store.PrepareContinue(run.Ref, spec); err == nil || !strings.Contains(err.Error(), "failed and cannot be continued") {
		t.Fatalf("PrepareContinue error = %v, want failed ref rejection", err)
	}
	if _, err := store.PrepareFork(run.Ref, spec); err == nil || !strings.Contains(err.Error(), "failed and cannot be continued") {
		t.Fatalf("PrepareFork error = %v, want failed ref rejection", err)
	}
}

func TestSubagentStoreSkipsSaveForDestroyedParent(t *testing.T) {
	store := NewSubagentStore(t.TempDir()).WithDestroyedChecker(func(parentSession string) bool {
		return parentSession == "parent-session"
	})
	spec := testSubagentSpec(t, "review")
	run, err := store.PrepareFresh(spec)
	if err != nil {
		t.Fatalf("PrepareFresh: %v", err)
	}
	run.Session.Add(provider.Message{Role: provider.RoleUser, Content: "answer after destroy"})
	if err := store.SaveCompleted(run); err != nil {
		t.Fatalf("SaveCompleted: %v", err)
	}
	if _, err := os.Stat(store.sessionPath(run.Ref)); !os.IsNotExist(err) {
		t.Fatalf("destroyed parent should not save session, stat err = %v", err)
	}
	if _, err := os.Stat(store.metaPath(run.Ref)); !os.IsNotExist(err) {
		t.Fatalf("destroyed parent should not save meta, stat err = %v", err)
	}
	if err := store.SaveFailed(run); err != nil {
		t.Fatalf("SaveFailed: %v", err)
	}
	if _, err := os.Stat(store.sessionPath(run.Ref)); !os.IsNotExist(err) {
		t.Fatalf("destroyed parent should still not save session, stat err = %v", err)
	}
	run.Release()
}

func testSubagentSpec(t *testing.T, name string) SubagentSpec {
	t.Helper()
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "read_file", readOnly: true})
	return SubagentSpec{
		Kind:          "skill",
		Name:          name,
		WorkspaceRoot: t.TempDir(),
		ParentSession: "parent-session",
		SystemPrompt:  "review persona",
		Registry:      reg,
		Model:         "deepseek",
		Effort:        "max",
	}
}

func saveTestBranchMeta(t *testing.T, sessionDir, id, parent string) {
	t.Helper()
	if err := SaveBranchMeta(filepath.Join(sessionDir, id+".jsonl"), BranchMeta{ParentID: parent}); err != nil {
		t.Fatalf("SaveBranchMeta(%s): %v", id, err)
	}
}

func prepareCompletedSubagentForLineageTest(t *testing.T, parentSession string) (string, *SubagentStore, string, SubagentSpec) {
	t.Helper()
	sessionDir := t.TempDir()
	store := NewSubagentStore(filepath.Join(sessionDir, "subagents"))
	spec := testSubagentSpec(t, "review")
	spec.ParentSession = parentSession
	run, err := store.PrepareFresh(spec)
	if err != nil {
		t.Fatalf("PrepareFresh: %v", err)
	}
	if err := store.SaveCompleted(run); err != nil {
		t.Fatalf("SaveCompleted: %v", err)
	}
	run.Release()
	return sessionDir, store, run.Ref, spec
}
