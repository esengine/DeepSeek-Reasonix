package checkpoint

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/diff"
)

type recordingConversationApplier struct {
	conversation []byte
	checkpoints  []byte
}

func (a *recordingConversationApplier) ApplyConversationTruncate(_ int, _ []byte) error {
	a.conversation = []byte("truncated")
	return nil
}

func (a *recordingConversationApplier) RestoreConversation(forward []byte) error {
	a.conversation = append([]byte(nil), forward...)
	return nil
}

func (a *recordingConversationApplier) TruncateCheckpoints(_ int) error {
	a.checkpoints = []byte("truncated")
	return nil
}

func (a *recordingConversationApplier) RestoreCheckpoints(backup []byte) error {
	a.checkpoints = append([]byte(nil), backup...)
	return nil
}

func TestRestoreCodeAllOrNothingOnMidPublishFailure(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "a.txt")
	b := filepath.Join(root, "b.txt")
	write(t, a, "a0")
	write(t, b, "b0")

	s := New("", root)
	s.Begin(0, "edit both", 0)
	s.Snapshot(diffChange(a, "a0"))
	s.Snapshot(diffChange(b, "b0"))
	write(t, a, "a1")
	write(t, b, "b1")

	plan, err := s.PrepareRewind(0, RewindCode, 1, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	plan.CanFiles = true
	plan.Conflicts = nil
	plan.DisabledReason = ""
	s.mu.Lock()
	s.plans[plan.PlanID] = preparedPlan{plan: plan, created: plan.CreatedAt}
	s.mu.Unlock()

	_, err = s.CommitRewindWithForward(plan.PlanID, nil, nil, &InjectFail{Phase: "publish_file", AfterFiles: 1})
	if err == nil {
		t.Fatal("expected injected failure")
	}

	if got := read(t, a); got != "a1" {
		t.Fatalf("a = %q, want a1 (compensated)", got)
	}
	if got := read(t, b); got != "b1" {
		t.Fatalf("b = %q, want b1 (compensated)", got)
	}
	if leftovers, err := filepath.Glob(filepath.Join(root, ".*.reasonix-*")); err != nil || len(leftovers) != 0 {
		t.Fatalf("transaction artifacts remain after compensation: %v err=%v", leftovers, err)
	}
}

func TestRecoverCommittingTransaction(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(t.TempDir(), "sess.ckpt")
	a := filepath.Join(root, "a.txt")
	write(t, a, "v0")

	s := New(dir, root)
	s.Begin(0, "p", 0)
	s.Snapshot(diffChange(a, "v0"))
	write(t, a, "v1")

	tx := &TransactionManifest{
		SchemaVersion: SchemaV2,
		ID:            "tx-crash",
		WorkspaceRoot: root,
		State:         TxCommitting,
		Kind:          "rewind",
		Turn:          0,
		Scope:         RewindCode,
		Targets: []TransactionTarget{{
			Path:           a,
			AbsPath:        a,
			Action:         "write",
			Published:      true,
			RestoreExisted: true,
			RestoreSHA:     Digest([]byte("v0")),
			ForwardExisted: true,
			ForwardSHA:     Digest([]byte("v1")),
		}},
	}
	ref, err := s.blobs.Put([]byte("v1"))
	if err != nil {
		t.Fatal(err)
	}
	tx.Targets[0].ForwardBlob = ref
	if err := os.WriteFile(a, []byte("v0"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.persistTransaction(tx); err != nil {
		t.Fatal(err)
	}

	s2 := New(dir, root)
	_ = s2.RecoverTransactions()
	if got := read(t, a); got != "v1" {
		t.Fatalf("after recovery a = %q, want v1", got)
	}
}

func TestRecoverCrashAfterPublishBeforeProgressPersistence(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(t.TempDir(), "sess.ckpt")
	a := filepath.Join(root, "a.txt")
	write(t, a, "before")
	s := New(dir, root)
	s.Begin(0, "edit", 0)
	s.CaptureBefore(a, CaptureBeforeOpts{Source: CaptureBeforeMutation})
	write(t, a, "after")
	s.CaptureAfter(a, CaptureAfterOpts{Seq: 1, Source: CaptureAfterMutation})

	plan, err := s.PrepareRewind(0, RewindCode, 1, 0, false)
	if err != nil || !plan.CanFiles {
		t.Fatalf("prepare: plan=%+v err=%v", plan, err)
	}
	if _, err := s.CommitRewindWithForward(plan.PlanID, nil, nil, &InjectFail{Phase: "after_publish_before_progress", AfterFiles: 0}); err == nil {
		t.Fatal("expected simulated crash")
	}
	if got := read(t, a); got != "before" {
		t.Fatalf("simulated crash did not occur after publish: %q", got)
	}

	_ = New(dir, root) // startup recovery runs while loading the store
	if got := read(t, a); got != "after" {
		t.Fatalf("crash recovery left partial rewind: got %q want after", got)
	}
}

func TestRecoverCrashAfterConversationRestoresBothSidesBeforeFileCompensation(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(t.TempDir(), "sess.ckpt")
	a := filepath.Join(root, "a.txt")
	write(t, a, "before")
	s := New(dir, root)
	s.Begin(0, "edit", 1)
	s.CaptureBefore(a, CaptureBeforeOpts{Source: CaptureBeforeMutation})
	write(t, a, "after")
	s.CaptureAfter(a, CaptureAfterOpts{Seq: 1, Source: CaptureAfterMutation})

	plan, err := s.PrepareRewind(0, RewindBoth, 1, 1, true)
	if err != nil || !plan.CanFiles || !plan.CanConversation {
		t.Fatalf("prepare: plan=%+v err=%v", plan, err)
	}
	forward, _ := json.Marshal([]string{"full conversation"})
	applier := &recordingConversationApplier{conversation: append([]byte(nil), forward...)}
	if _, err := s.CommitRewindWithForward(plan.PlanID, forward, applier, &InjectFail{Phase: "after_conversation_before_finalize"}); err == nil {
		t.Fatal("expected simulated crash")
	}
	if got := read(t, a); got != "before" {
		t.Fatalf("crash point file = %q, want published rewind", got)
	}
	if string(applier.conversation) != "truncated" || string(applier.checkpoints) != "truncated" {
		t.Fatalf("crash point did not include conversation mutation: conversation=%q checkpoints=%q", applier.conversation, applier.checkpoints)
	}

	s2 := New(dir, root)
	if got := read(t, a); got != "before" {
		t.Fatalf("store-only startup must defer combined recovery, got file %q", got)
	}
	recovered := &recordingConversationApplier{conversation: []byte("truncated"), checkpoints: []byte("truncated")}
	notes := s2.RecoverTransactionsWithApplier(recovered)
	if len(notes) == 0 {
		t.Fatal("expected a recovery note")
	}
	if got := read(t, a); got != "after" {
		t.Fatalf("recovery file = %q, want forward image", got)
	}
	if !bytes.Equal(recovered.conversation, forward) {
		t.Fatalf("conversation recovery = %q, want %q", recovered.conversation, forward)
	}
	if len(recovered.checkpoints) == 0 || bytes.Equal(recovered.checkpoints, []byte("truncated")) {
		t.Fatalf("checkpoint backup was not restored: %q", recovered.checkpoints)
	}
	var manifest TransactionManifest
	if err := readJSONFile(s2.txManifestPath(planTransactionID(t, dir)), &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.State != TxAborted {
		t.Fatalf("recovered transaction state = %s, want aborted", manifest.State)
	}
}

func planTransactionID(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(dir, "transactions"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("transaction manifests = %d, want 1", len(entries))
	}
	return entries[0].Name()[:len(entries[0].Name())-len(".json")]
}

func TestBackgroundWriterStartingAfterPreviewBlocksCommit(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "a.txt")
	write(t, a, "before")
	s := New("", root)
	s.Begin(0, "edit", 0)
	s.CaptureBefore(a, CaptureBeforeOpts{Source: CaptureBeforeMutation})
	write(t, a, "after")
	s.CaptureAfter(a, CaptureAfterOpts{Seq: 1, Source: CaptureAfterMutation})
	plan, err := s.PrepareRewind(0, RewindCode, 1, 0, false)
	if err != nil || !plan.CanFiles {
		t.Fatalf("prepare: plan=%+v err=%v", plan, err)
	}
	observer := NewMutationObserver(ObserverOptions{Store: s})
	if err := observer.RegisterWriter("bg-1", "background_subagent", 0); err != nil {
		t.Fatal(err)
	}
	result, err := s.CommitRewindWithForward(plan.PlanID, nil, nil, nil)
	if err == nil || len(result.Conflicts) == 0 || result.Conflicts[0].Reason != ConflictBusyWriter {
		t.Fatalf("commit during background writer: result=%+v err=%v", result, err)
	}
	if got := read(t, a); got != "after" {
		t.Fatalf("blocked commit changed file to %q", got)
	}
	observer.UnregisterWriter("bg-1")
	fresh, err := s.PrepareRewind(0, RewindCode, 1, 0, false)
	if err != nil || !fresh.CanFiles {
		t.Fatalf("fresh prepare after writer: plan=%+v err=%v", fresh, err)
	}
	if result, err := s.CommitRewindWithForward(fresh.PlanID, nil, nil, nil); err != nil || !result.OK {
		t.Fatalf("commit after writer: result=%+v err=%v", result, err)
	}
}

func TestCaptureRejectsAncestorSymlink(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	write(t, filepath.Join(outside, "secret.txt"), "secret")
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	_, gap, err := CapturePath(filepath.Join(root, "link", "secret.txt"), CaptureOptions{WorkspaceRoot: root, ReadContent: true})
	if err == nil || gap == nil || gap.Reason != GapSymlink {
		t.Fatalf("ancestor symlink capture: gap=%+v err=%v", gap, err)
	}
}

func TestPublishRejectsAncestorSwappedToSymlink(t *testing.T) {
	root := t.TempDir()
	out := t.TempDir()
	dir := filepath.Join(root, "dir")
	target := filepath.Join(dir, "a.txt")
	write(t, target, "inside")
	write(t, filepath.Join(out, "a.txt"), "outside")
	s := New("", root)
	tmp, backup := transactionSiblingPaths(target, "swap", 0)
	if err := s.writePublishTemp(tmp, []byte("rewound"), 0o644); err != nil {
		t.Fatal(err)
	}
	moved := filepath.Join(root, "moved")
	if err := os.Rename(dir, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(out, dir); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	targetSpec := &TransactionTarget{Path: "dir/a.txt", AbsPath: target, PublishTmp: tmp, BackupPath: backup, Action: "write", RestoreMode: 0o644}
	if err := s.publishTarget(targetSpec); err == nil {
		t.Fatal("publish through swapped ancestor symlink succeeded")
	}
	if got := read(t, filepath.Join(out, "a.txt")); got != "outside" {
		t.Fatalf("outside file changed to %q", got)
	}
	if got := read(t, filepath.Join(moved, "a.txt")); got != "inside" {
		t.Fatalf("original workspace file changed to %q", got)
	}
}

func TestFileRevertRejectsStalePreviewEvenWithOldOverwriteApproval(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "a.txt")
	write(t, a, "before")
	s := New("", root)
	s.Begin(0, "edit", 0)
	s.CaptureBefore(a, CaptureBeforeOpts{Source: CaptureBeforeMutation})
	write(t, a, "owned")
	s.CaptureAfter(a, CaptureAfterOpts{Seq: 1, Source: CaptureAfterMutation})

	plan, err := s.PrepareFileRevert(a, 1)
	if err != nil || !plan.CanFiles {
		t.Fatalf("prepare: plan=%+v err=%v", plan, err)
	}
	write(t, a, "external")
	if _, err := s.CommitFileRevert(plan.PlanID, ResolveOverwriteCheckpoint); err == nil {
		t.Fatal("stale overwrite approval must not authorize a later external edit")
	}
	if got := read(t, a); got != "external" {
		t.Fatalf("stale commit changed file to %q", got)
	}

	fresh, err := s.PrepareFileRevert(a, 1)
	if err != nil || len(fresh.Conflicts) == 0 {
		t.Fatalf("fresh preview should expose external conflict: plan=%+v err=%v", fresh, err)
	}
	result, err := s.CommitFileRevert(fresh.PlanID, ResolveOverwriteCheckpoint)
	if err != nil || !result.OK {
		t.Fatalf("fresh explicit overwrite failed: result=%+v err=%v", result, err)
	}
	if got := read(t, a); got != "before" {
		t.Fatalf("fresh confirmed revert = %q, want before", got)
	}
}

func TestUndoRestoresEmptyForwardFile(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "a.txt")
	write(t, a, "before")
	s := New("", root)
	s.Begin(0, "empty", 0)
	s.CaptureBefore(a, CaptureBeforeOpts{Source: CaptureBeforeMutation})
	write(t, a, "")
	s.CaptureAfter(a, CaptureAfterOpts{Seq: 1, Source: CaptureAfterMutation})
	plan, err := s.PrepareRewind(0, RewindCode, 1, 0, false)
	if err != nil || !plan.CanFiles {
		t.Fatalf("prepare: plan=%+v err=%v", plan, err)
	}
	result, err := s.CommitRewindWithForward(plan.PlanID, nil, nil, nil)
	if err != nil || !result.OK {
		t.Fatalf("rewind: result=%+v err=%v", result, err)
	}
	undo, err := s.UndoRewind(result.TransactionID, nil)
	if err != nil || !undo.OK {
		t.Fatalf("undo: result=%+v err=%v", undo, err)
	}
	if got := read(t, a); got != "" {
		t.Fatalf("undo restored %q, want empty file", got)
	}
}

func TestPrecheckDetectsManualEdit(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "a.txt")
	write(t, a, "v0")
	s := New("", root)
	s.Begin(0, "p", 0)
	s.CaptureBeforeFromChange(diffChange(a, "v0"), CaptureBeforeOpts{Source: CapturePreviewer})
	write(t, a, "v1")
	s.CaptureAfter(a, CaptureAfterOpts{Seq: 1, Source: CaptureAfterMutation})
	write(t, a, "manual")

	plan, err := s.PrepareRewind(0, RewindCode, 1, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	if plan.CanFiles {
		t.Fatalf("expected CanFiles=false on manual edit, plan=%+v", plan)
	}
	if len(plan.Conflicts) == 0 {
		t.Fatal("expected conflicts")
	}
	if _, err := s.CommitRewindWithForward(plan.PlanID, nil, nil, nil); err == nil {
		t.Fatal("commit should fail")
	}
	if got := read(t, a); got != "manual" {
		t.Fatalf("a = %q, want manual", got)
	}
}

func TestTransactionCrashRecoveryPreparedIsAbandoned(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(t.TempDir(), "sess.ckpt")
	s := New(dir, root)
	tx := &TransactionManifest{
		SchemaVersion: SchemaV2,
		ID:            "tx-prep",
		WorkspaceRoot: root,
		State:         TxPrepared,
		Kind:          "rewind",
	}
	if err := s.persistTransaction(tx); err != nil {
		t.Fatal(err)
	}
	_ = New(dir, root)
	var loaded TransactionManifest
	if err := readJSONFile(s.txManifestPath("tx-prep"), &loaded); err != nil {
		t.Fatal(err)
	}
	if loaded.State != TxAborted {
		t.Fatalf("state = %s, want aborted", loaded.State)
	}
}

func TestCompensationRecoversCrashBetweenBackupAndPublishRenames(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "a.txt")
	write(t, target, "forward")
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	mode := uint32(info.Mode().Perm())
	publish, backup := transactionSiblingPaths(target, "tx-crash-gap", 0)
	write(t, publish, "restore")
	if err := os.Rename(target, backup); err != nil {
		t.Fatal(err)
	}

	targetSpec := TransactionTarget{
		Path: "a.txt", AbsPath: target, Action: "write", Published: true,
		RestoreExisted: true, RestoreSHA: Digest([]byte("restore")), RestoreMode: mode,
		ForwardExisted: true, ForwardSHA: Digest([]byte("forward")), ForwardMode: mode,
		ForwardInline: []byte("forward"), PublishTmp: publish, BackupPath: backup,
	}
	store := New("", root)
	if err := store.compensatePublished([]TransactionTarget{targetSpec}, []FileStage{{Path: "a.txt"}}); err != nil {
		t.Fatalf("compensate crash gap: %v", err)
	}
	if got := read(t, target); got != "forward" {
		t.Fatalf("target after compensation = %q, want forward", got)
	}
	if _, err := os.Stat(publish); !os.IsNotExist(err) {
		t.Fatalf("publish temp remains after compensation: %v", err)
	}
}

func TestLegacyFileRevertIsRefusedWithoutOwnershipFingerprint(t *testing.T) {
	root := t.TempDir()
	dir := t.TempDir()
	path := filepath.Join(root, "a.txt")
	write(t, path, "manual")
	before := "before"
	legacy := Checkpoint{Turn: 0, Time: time.Now(), Files: []FileSnap{{Path: "a.txt", Content: &before}}}
	raw, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "turn-0.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}

	store := New(dir, root)
	plan, err := store.PrepareFileRevert("a.txt", 1)
	if err != nil {
		t.Fatal(err)
	}
	if plan.CanFiles || plan.PlanID != "" {
		t.Fatalf("legacy single-file revert was authorized: %+v", plan)
	}
	if _, err := store.CommitFileRevert(plan.PlanID, ResolveOverwriteCheckpoint); err == nil {
		t.Fatal("legacy file revert commit must be refused")
	}
	if got := read(t, path); got != "manual" {
		t.Fatalf("legacy refusal changed file to %q", got)
	}
}

func TestFileRevertRequiresLatestOwnershipFingerprint(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "a.txt")
	write(t, path, "before")
	store := New("", root)
	observer := NewMutationObserver(ObserverOptions{Store: store})
	store.Begin(0, "first", 0)
	observer.BeforeMutation("a.txt", "edit", CaptureBeforeMutation)
	write(t, path, "middle")
	observer.AfterMutation("a.txt", "edit")
	store.Begin(1, "second", 2)
	observer.BeforeMutation("a.txt", "edit", CaptureBeforeMutation)
	write(t, path, "after")
	// Simulate a writer whose mandatory after observation could not establish
	// an identity. The earlier fingerprint must not be reused as current proof.

	state, ok := store.FileState("a.txt")
	if !ok {
		t.Fatal("expected earliest session preimage")
	}
	if state.Owned {
		t.Fatal("stale earlier after fingerprint still marked file session-owned")
	}
	plan, err := store.PrepareFileRevert("a.txt", 1)
	if err != nil {
		t.Fatal(err)
	}
	if plan.CanFiles || plan.PlanID != "" {
		t.Fatalf("missing latest ownership proof enabled file revert: %+v", plan)
	}
}

func TestUndoRejectsPermissionOnlyChange(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "a.txt")
	write(t, path, "before")
	store := New("", root)
	observer := NewMutationObserver(ObserverOptions{Store: store})
	store.Begin(0, "edit", 0)
	observer.BeforeMutation("a.txt", "edit", CaptureBeforeMutation)
	write(t, path, "after")
	observer.AfterMutation("a.txt", "edit")
	plan, err := store.PrepareRewind(0, RewindCode, 1, 0, false)
	if err != nil || !plan.CanFiles {
		t.Fatalf("prepare: plan=%+v err=%v", plan, err)
	}
	result, err := store.CommitRewindWithForward(plan.PlanID, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	wantMode := os.FileMode(0o600)
	if before.Mode().Perm() == wantMode {
		wantMode = 0o644
	}
	if err := os.Chmod(path, wantMode); err != nil {
		t.Fatal(err)
	}
	changed, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if changed.Mode().Perm() == before.Mode().Perm() {
		t.Skip("filesystem does not expose permission-only changes")
	}
	if _, err := store.UndoRewind(result.TransactionID, nil); err == nil {
		t.Fatal("undo overwrote a permission-only user change")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != wantMode {
		t.Fatalf("mode after refused undo = %o, want %o", got, wantMode)
	}
}

func TestRewindDeduplicatesEquivalentPathForms(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "a.txt")
	write(t, path, "before")
	store := New("", root)
	observer := NewMutationObserver(ObserverOptions{Store: store})
	store.Begin(0, "first", 0)
	observer.BeforeMutation("a.txt", "edit", CaptureBeforeMutation)
	write(t, path, "middle")
	observer.AfterMutation("a.txt", "edit")
	store.Begin(1, "second", 2)
	observer.BeforeMutation(path, "edit", CaptureBeforeMutation)
	write(t, path, "after")
	observer.AfterMutation(path, "edit")

	plan, err := store.PrepareRewind(0, RewindCode, 2, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.CanFiles || plan.FileCount != 1 || len(plan.Files) != 1 {
		t.Fatalf("equivalent paths were not one rewind target: %+v", plan)
	}
	result, err := store.CommitRewindWithForward(plan.PlanID, nil, nil, nil)
	if err != nil || !result.OK {
		t.Fatalf("commit: result=%+v err=%v", result, err)
	}
	if got := read(t, path); got != "before" {
		t.Fatalf("rewind = %q, want before", got)
	}
}

type failCheckpointRestoreApplier struct {
	conversation string
}

func (a *failCheckpointRestoreApplier) ApplyConversationTruncate(_ int, _ []byte) error {
	a.conversation = "rewound"
	return nil
}
func (a *failCheckpointRestoreApplier) RestoreConversation(_ []byte) error {
	a.conversation = "forward"
	return nil
}
func (a *failCheckpointRestoreApplier) TruncateCheckpoints(_ int) error { return nil }
func (a *failCheckpointRestoreApplier) RestoreCheckpoints(_ []byte) error {
	return errors.New("injected checkpoint restore failure")
}

func TestUndoCheckpointRestoreFailureRestoresOriginalRewind(t *testing.T) {
	store := New("", t.TempDir())
	applier := &failCheckpointRestoreApplier{conversation: "rewound"}
	original := &TransactionManifest{
		ID: "original", State: TxCommitted, Kind: "rewind", Scope: RewindBoth,
		HasBoundary: true, BoundaryIndex: 2, TruncateFrom: 1,
		ConversationForward: []byte(`{"messages":["forward"]}`),
		CheckpointBackup:    []byte(`[{"turn":1}]`),
	}
	undo := &TransactionManifest{
		ID: "undo", State: TxPrepared, Kind: "undo", Scope: RewindBoth,
		ParentTransaction: original.ID,
	}
	if _, err := store.commitUndoTransaction(undo, original, applier); err == nil {
		t.Fatal("expected injected checkpoint restore failure")
	}
	if applier.conversation != "rewound" {
		t.Fatalf("failed undo left conversation in %q state, want rewound", applier.conversation)
	}
}

func TestFailedFileCompensationRemainsRecoverable(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "a.txt")
	write(t, target, "external")
	tx := &TransactionManifest{
		ID: "tx-pending-compensation", State: TxCommitting, Kind: "rewind",
		Targets: []TransactionTarget{{
			Path: "a.txt", AbsPath: target, Action: "write", Published: true,
			RestoreExisted: true, RestoreSHA: Digest([]byte("restore")),
			ForwardExisted: true, ForwardSHA: Digest([]byte("forward")), ForwardInline: []byte("forward"),
		}},
	}
	store := New("", root)
	if err := store.failTransaction(tx, tx.Targets, []FileStage{{Path: "a.txt"}}, errors.New("injected failure")); err == nil {
		t.Fatal("expected compensation failure")
	}
	if tx.State != TxCommitting {
		t.Fatalf("transaction state = %s, want committing for startup retry", tx.State)
	}
}

func diffChange(path, old string) diff.Change {
	return diff.Change{Path: path, Kind: diff.Modify, OldText: old}
}
