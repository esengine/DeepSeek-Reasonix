package agent

import (
	"bufio"
	"os"
	"os/exec"
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

	forked, err := store.prepareFork(run.Ref, spec)
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

func TestSubagentStoreRejectsContinueFromSiblingSession(t *testing.T) {
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
	if _, err := store.PrepareContinue(run.Ref, other); err == nil || !strings.Contains(err.Error(), "not in current parent session") {
		t.Fatalf("PrepareContinue error = %v, want lineage rejection", err)
	}
}

func TestSubagentStoreContinueFromAncestorCopiesIntoCurrentSession(t *testing.T) {
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
	child := spec
	child.ParentSession = "child"
	continued, err := store.PrepareContinue(run.Ref, child)
	if err != nil {
		t.Fatalf("PrepareContinue: %v", err)
	}
	defer continued.Release()
	if continued.Ref == run.Ref {
		t.Fatalf("continued ref should be copied into child session, got source ref %q", continued.Ref)
	}
	if continued.Meta.ParentSession != "child" {
		t.Fatalf("continued parent session = %q, want child", continued.Meta.ParentSession)
	}
	if continued.Meta.ForkedFrom != run.Ref {
		t.Fatalf("forkedFrom = %q, want %q", continued.Meta.ForkedFrom, run.Ref)
	}
	if got := continued.Session.Snapshot(); len(got) != 2 || got[1].Content != "review diff" {
		t.Fatalf("continued transcript = %+v, want copied source transcript", got)
	}
	sourceMeta, err := store.LoadMeta(run.Ref)
	if err != nil {
		t.Fatalf("LoadMeta source: %v", err)
	}
	if sourceMeta.ParentSession != "root" {
		t.Fatalf("source parent session = %q, want root", sourceMeta.ParentSession)
	}
}

func TestSubagentStoreLegacyForkFromAncestorConvertsToContinueCopy(t *testing.T) {
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
	child := spec
	child.ParentSession = "child"
	continued, err := store.PrepareLegacyForkFrom(run.Ref, child)
	if err != nil {
		t.Fatalf("PrepareLegacyForkFrom: %v", err)
	}
	defer continued.Release()
	if continued.Ref == run.Ref {
		t.Fatalf("legacy fork ref should be copied into child session, got source ref %q", continued.Ref)
	}
	if continued.Meta.ParentSession != "child" {
		t.Fatalf("continued parent session = %q, want child", continued.Meta.ParentSession)
	}
	if continued.Meta.ForkedFrom != run.Ref {
		t.Fatalf("forkedFrom = %q, want %q", continued.Meta.ForkedFrom, run.Ref)
	}
	if got := continued.Session.Snapshot(); len(got) != 2 || got[1].Content != "review diff" {
		t.Fatalf("continued transcript = %+v, want copied source transcript", got)
	}
}

func TestSubagentStoreRejectsLegacyForkFromCurrentSession(t *testing.T) {
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

	if _, err := store.PrepareLegacyForkFrom(run.Ref, spec); err == nil || !strings.Contains(err.Error(), "cannot be safely converted") {
		t.Fatalf("PrepareLegacyForkFrom error = %v, want unsafe conversion rejection", err)
	}
}

func TestSubagentStoreContinueFromAncestorReusesCurrentSessionCopy(t *testing.T) {
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
	child := spec
	child.ParentSession = "child"
	first, err := store.PrepareContinue(run.Ref, child)
	if err != nil {
		t.Fatalf("first PrepareContinue: %v", err)
	}
	firstRef := first.Ref
	if err := store.SaveCompleted(first); err != nil {
		t.Fatalf("SaveCompleted first: %v", err)
	}
	first.Release()

	second, err := store.PrepareContinue(run.Ref, child)
	if err != nil {
		t.Fatalf("second PrepareContinue: %v", err)
	}
	defer second.Release()
	if second.Ref != firstRef {
		t.Fatalf("second continuation ref = %q, want existing child copy %q", second.Ref, firstRef)
	}
}

func TestSubagentStoreContinueFromOlderAncestorUsesNearestLineageCopy(t *testing.T) {
	sessionDir := t.TempDir()
	store := NewSubagentStore(filepath.Join(sessionDir, "subagents"))
	spec := testSubagentSpec(t, "review")
	spec.ParentSession = "root"
	rootRun, err := store.PrepareFresh(spec)
	if err != nil {
		t.Fatalf("PrepareFresh root: %v", err)
	}
	rootRun.Session.Add(provider.Message{Role: provider.RoleUser, Content: "root task"})
	if err := store.SaveCompleted(rootRun); err != nil {
		t.Fatalf("SaveCompleted root: %v", err)
	}
	rootRun.Release()

	saveTestBranchMeta(t, sessionDir, "root", "")
	saveTestBranchMeta(t, sessionDir, "child", "root")
	saveTestBranchMeta(t, sessionDir, "grandchild", "child")

	child := spec
	child.ParentSession = "child"
	childRun, err := store.PrepareContinue(rootRun.Ref, child)
	if err != nil {
		t.Fatalf("PrepareContinue child: %v", err)
	}
	childRun.Session.Add(provider.Message{Role: provider.RoleAssistant, Content: "child finding"})
	childRef := childRun.Ref
	if err := store.SaveCompleted(childRun); err != nil {
		t.Fatalf("SaveCompleted child: %v", err)
	}
	childRun.Release()

	grandchild := spec
	grandchild.ParentSession = "grandchild"
	fromRoot, err := store.PrepareContinue(rootRun.Ref, grandchild)
	if err != nil {
		t.Fatalf("PrepareContinue grandchild from root: %v", err)
	}
	grandchildRef := fromRoot.Ref
	if fromRoot.Meta.ForkedFrom != childRef {
		t.Fatalf("grandchild forkedFrom = %q, want nearest child copy %q", fromRoot.Meta.ForkedFrom, childRef)
	}
	if got := fromRoot.Session.Snapshot(); len(got) != 3 || got[2].Content != "child finding" {
		t.Fatalf("grandchild transcript = %+v, want child copy transcript", got)
	}
	if err := store.SaveCompleted(fromRoot); err != nil {
		t.Fatalf("SaveCompleted grandchild: %v", err)
	}
	fromRoot.Release()

	fromChild, err := store.PrepareContinue(childRef, grandchild)
	if err != nil {
		t.Fatalf("PrepareContinue grandchild from child: %v", err)
	}
	defer fromChild.Release()
	if fromChild.Ref != grandchildRef {
		t.Fatalf("grandchild ref from child copy = %q, want existing copy %q", fromChild.Ref, grandchildRef)
	}
}

func TestSubagentStoreRejectsAncestorContinuationWhenCurrentCopyFailed(t *testing.T) {
	sessionDir := t.TempDir()
	store := NewSubagentStore(filepath.Join(sessionDir, "subagents"))
	spec := testSubagentSpec(t, "review")
	spec.ParentSession = "root"
	run, err := store.PrepareFresh(spec)
	if err != nil {
		t.Fatalf("PrepareFresh: %v", err)
	}
	if err := store.SaveCompleted(run); err != nil {
		t.Fatalf("SaveCompleted root: %v", err)
	}
	run.Release()

	saveTestBranchMeta(t, sessionDir, "root", "")
	saveTestBranchMeta(t, sessionDir, "child", "root")
	child := spec
	child.ParentSession = "child"
	copyRun, err := store.PrepareContinue(run.Ref, child)
	if err != nil {
		t.Fatalf("PrepareContinue child: %v", err)
	}
	if err := store.SaveFailed(copyRun); err != nil {
		t.Fatalf("SaveFailed child copy: %v", err)
	}
	copyRun.Release()

	if _, err := store.PrepareContinue(run.Ref, child); err == nil || !strings.Contains(err.Error(), "failed and cannot be continued") {
		t.Fatalf("PrepareContinue error = %v, want failed current copy rejection", err)
	}
}

func TestSubagentStoreRejectsAncestorContinuationWithMultipleCurrentCopies(t *testing.T) {
	sessionDir := t.TempDir()
	store := NewSubagentStore(filepath.Join(sessionDir, "subagents"))
	spec := testSubagentSpec(t, "review")
	spec.ParentSession = "root"
	run, err := store.PrepareFresh(spec)
	if err != nil {
		t.Fatalf("PrepareFresh: %v", err)
	}
	if err := store.SaveCompleted(run); err != nil {
		t.Fatalf("SaveCompleted root: %v", err)
	}
	run.Release()

	saveTestBranchMeta(t, sessionDir, "root", "")
	saveTestBranchMeta(t, sessionDir, "child", "root")
	child := spec
	child.ParentSession = "child"
	first, err := store.PrepareContinue(run.Ref, child)
	if err != nil {
		t.Fatalf("PrepareContinue first: %v", err)
	}
	if err := store.SaveCompleted(first); err != nil {
		t.Fatalf("SaveCompleted first: %v", err)
	}
	first.Release()

	second, err := store.PrepareFresh(child)
	if err != nil {
		t.Fatalf("PrepareFresh second: %v", err)
	}
	second.Meta.ForkedFrom = run.Ref
	if err := store.SaveCompleted(second); err != nil {
		t.Fatalf("SaveCompleted second: %v", err)
	}
	second.Release()

	if _, err := store.PrepareContinue(run.Ref, child); err == nil || !strings.Contains(err.Error(), "multiple copied transcripts") {
		t.Fatalf("PrepareContinue error = %v, want multiple-copy rejection", err)
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
	forked, err := store.prepareFork(run.Ref, other)
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
	if _, err := store.prepareFork(ref, other); err == nil || !strings.Contains(err.Error(), "lineage could not be verified") {
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
	if _, err := store.prepareFork(ref, other); err == nil || !strings.Contains(err.Error(), "lineage could not be verified") {
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
	if _, err := store.prepareFork(ref, other); err == nil || !strings.Contains(err.Error(), "lineage could not be verified") {
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
	if _, err := store.prepareFork(run.Ref, other); err == nil || !strings.Contains(err.Error(), "not in current parent session") {
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
	if _, err := store.prepareFork(run.Ref, other); err == nil || !strings.Contains(err.Error(), "not in current parent session") {
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
	if _, err := store.prepareFork(run.Ref, other); err == nil || !strings.Contains(err.Error(), "lineage could not be verified") {
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

	forked, err := store.prepareFork(run.Ref, spec)
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
	if _, err := store.prepareFork(run.Ref, spec); err == nil || !strings.Contains(err.Error(), "failed and cannot be continued") {
		t.Fatalf("PrepareFork error = %v, want failed ref rejection", err)
	}
}

func TestSubagentStoreCleanupStaleRunningMarksInterrupted(t *testing.T) {
	store := NewSubagentStore(t.TempDir())
	spec := testSubagentSpec(t, "review")
	run, err := store.PrepareFresh(spec)
	if err != nil {
		t.Fatalf("PrepareFresh: %v", err)
	}
	run.Session.Add(provider.Message{Role: provider.RoleUser, Content: "interrupted prompt"})
	if err := store.MarkRunning(run); err != nil {
		t.Fatalf("MarkRunning: %v", err)
	}
	ref := run.Ref
	run.Release()

	cleaned, err := store.CleanupStaleRunning()
	if err != nil {
		t.Fatalf("CleanupStaleRunning: %v", err)
	}
	if cleaned != 1 {
		t.Fatalf("cleaned = %d, want 1", cleaned)
	}
	meta, err := store.LoadMeta(ref)
	if err != nil {
		t.Fatalf("LoadMeta: %v", err)
	}
	if meta.Status != SubagentInterrupted {
		t.Fatalf("status = %q, want interrupted", meta.Status)
	}
	if _, err := store.PrepareContinue(ref, spec); err == nil || !strings.Contains(err.Error(), "interrupted by a previous shutdown or crash") {
		t.Fatalf("PrepareContinue error = %v, want interrupted rejection", err)
	}
	if _, err := store.prepareFork(ref, spec); err == nil || !strings.Contains(err.Error(), "cannot be continued or forked") {
		t.Fatalf("PrepareFork error = %v, want interrupted fork rejection", err)
	}
}

func TestSubagentStoreCleanupStaleRunningSkipsMissingParentProof(t *testing.T) {
	store := NewSubagentStore(t.TempDir())
	spec := testSubagentSpec(t, "review")
	run, err := store.PrepareFresh(spec)
	if err != nil {
		t.Fatalf("PrepareFresh: %v", err)
	}
	if err := store.MarkRunning(run); err != nil {
		t.Fatalf("MarkRunning: %v", err)
	}
	ref := run.Ref
	run.Release()
	meta, err := store.LoadMeta(ref)
	if err != nil {
		t.Fatalf("LoadMeta: %v", err)
	}
	meta.ParentSession = ""
	if err := store.saveMeta(meta); err != nil {
		t.Fatalf("saveMeta without parent: %v", err)
	}

	cleaned, err := store.CleanupStaleRunning()
	if err != nil {
		t.Fatalf("CleanupStaleRunning: %v", err)
	}
	if cleaned != 0 {
		t.Fatalf("cleaned = %d without parent proof, want 0", cleaned)
	}
	meta, err = store.LoadMeta(ref)
	if err != nil {
		t.Fatalf("LoadMeta after cleanup: %v", err)
	}
	if meta.Status != SubagentRunning {
		t.Fatalf("status = %q without parent proof, want running", meta.Status)
	}
}

func TestSubagentStoreCleanupStaleRunningSkipsForeignLiveParent(t *testing.T) {
	sessionDir := t.TempDir()
	store := NewSubagentStore(filepath.Join(sessionDir, "subagents"))
	spec := testSubagentSpec(t, "review")
	spec.ParentSession = "live-parent"
	run, err := store.PrepareFresh(spec)
	if err != nil {
		t.Fatalf("PrepareFresh: %v", err)
	}
	if err := store.MarkRunning(run); err != nil {
		t.Fatalf("MarkRunning: %v", err)
	}
	ref := run.Ref
	run.Release()

	parentPath := filepath.Join(sessionDir, spec.ParentSession+".jsonl")
	cmd := exec.Command(os.Args[0], "-test.run=^TestSubagentStoreForeignLeaseHelper$")
	cmd.Env = append(os.Environ(),
		"REASONIX_SUBAGENT_LEASE_HELPER=1",
		"REASONIX_SUBAGENT_LEASE_PATH="+parentPath,
	)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("StdinPipe: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("StdoutPipe: %v", err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start lease holder: %v", err)
	}
	if line, err := bufio.NewReader(stdout).ReadString('\n'); err != nil || line != "ready\n" {
		_ = stdin.Close()
		_ = cmd.Wait()
		t.Fatalf("lease holder readiness = %q, err = %v", line, err)
	}

	cleaned, err := store.CleanupStaleRunning()
	if err != nil {
		_ = stdin.Close()
		_ = cmd.Wait()
		t.Fatalf("CleanupStaleRunning with foreign holder: %v", err)
	}
	if cleaned != 0 {
		_ = stdin.Close()
		_ = cmd.Wait()
		t.Fatalf("cleaned = %d while foreign parent lease was live, want 0", cleaned)
	}
	meta, err := store.LoadMeta(ref)
	if err != nil {
		_ = stdin.Close()
		_ = cmd.Wait()
		t.Fatalf("LoadMeta with foreign holder: %v", err)
	}
	if meta.Status != SubagentRunning {
		_ = stdin.Close()
		_ = cmd.Wait()
		t.Fatalf("status = %q while foreign parent lease was live, want running", meta.Status)
	}

	if err := stdin.Close(); err != nil {
		t.Fatalf("release lease holder stdin: %v", err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("lease holder exit: %v", err)
	}
	cleaned, err = store.CleanupStaleRunning()
	if err != nil {
		t.Fatalf("CleanupStaleRunning after foreign release: %v", err)
	}
	if cleaned != 1 {
		t.Fatalf("cleaned = %d after foreign release, want 1", cleaned)
	}
	meta, err = store.LoadMeta(ref)
	if err != nil {
		t.Fatalf("LoadMeta after foreign release: %v", err)
	}
	if meta.Status != SubagentInterrupted {
		t.Fatalf("status = %q after foreign release, want interrupted", meta.Status)
	}
}

func TestSubagentStoreForeignLeaseHelper(t *testing.T) {
	if os.Getenv("REASONIX_SUBAGENT_LEASE_HELPER") != "1" {
		return
	}
	lease, err := TryAcquireSessionLease(os.Getenv("REASONIX_SUBAGENT_LEASE_PATH"))
	if err != nil {
		t.Fatalf("TryAcquireSessionLease: %v", err)
	}
	if _, err := os.Stdout.WriteString("ready\n"); err != nil {
		lease.Release()
		t.Fatalf("write readiness: %v", err)
	}
	var release [1]byte
	_, _ = os.Stdin.Read(release[:])
	lease.Release()
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
