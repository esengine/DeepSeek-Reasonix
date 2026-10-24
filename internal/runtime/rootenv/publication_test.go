package rootenv

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"reasonix/internal/platform/worktree"
	"reasonix/internal/runtime/isolation"
	"reasonix/internal/state/sessionstore"
)

func TestPublicationKeepsLiveSceneAndAdvancesOnlyExportedRootCursor(t *testing.T) {
	s, source := fixture(t)
	path := sessionstore.CanonicalSessionPath(filepath.Join(t.TempDir(), "root.jsonl"))
	e, err := s.Prepare(t.Context(), path, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if err := e.BindSession(t.Context(), path, attachmentAuthority(t, path)); err != nil {
		t.Fatal(err)
	}
	live := e.Record().Candidate.WorkspaceRoot
	write(t, filepath.Join(live, "a"), "root first\n")
	publication, err := e.BeginPublication(t.Context(), path)
	if err != nil || publication == nil {
		t.Fatalf("begin: %+v %v", publication, err)
	}
	store := isolation.NewStore(t.TempDir(), source, s.repo).WithMutationCoordinator(s.mutations)
	if err := e.AcknowledgePublication(t.Context(), path, publication.ID, store); err == nil {
		t.Fatal("missing result advanced cursor")
	}
	entry, err := store.ImportSnapshotResult(t.Context(), publication.Import())
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(live, "a"), "root second\n")
	write(t, filepath.Join(source, "others"), "independent source file\n")
	again, err := e.BeginPublication(t.Context(), path)
	if err != nil || again.ID != publication.ID {
		t.Fatalf("pending export duplicated: %+v %v", again, err)
	}
	if _, err := store.Apply(t.Context(), entry.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.AcknowledgePublication(t.Context(), path, publication.ID, store); err != nil {
		t.Fatal(err)
	}
	if e.Record().Cursor.Tree != publication.Target.Tree {
		t.Fatal("cursor is not the frozen root scene")
	}
	if err := e.AcknowledgePublication(t.Context(), path, publication.ID, store); err != nil {
		t.Fatal("acknowledgment is not idempotent", err)
	}
	next, err := e.BeginPublication(t.Context(), path)
	if err != nil || next == nil || next.ID == publication.ID {
		t.Fatalf("next export: %+v %v", next, err)
	}
	result, err := store.ImportSnapshotResult(t.Context(), next.Import())
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range result.Changes {
		if change.Path == "others" {
			t.Fatal("root diff would delete another writer's new file")
		}
	}
	if _, err := os.Stat(filepath.Join(live, "a")); err != nil {
		t.Fatal("publication deleted live scene", err)
	}
	if raw, _ := os.ReadFile(filepath.Join(source, "a")); string(raw) != "root first\n" {
		t.Fatalf("unpublished scene reached source: %q", raw)
	}
}

func TestPublicationSourceModeConflictCannotBeSilentlyOverwritten(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission metadata")
	}
	s, source := fixture(t)
	path := sessionstore.CanonicalSessionPath(filepath.Join(t.TempDir(), "root.jsonl"))
	e, err := s.Prepare(t.Context(), path, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if err := e.BindSession(t.Context(), path, attachmentAuthority(t, path)); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(e.Record().Candidate.WorkspaceRoot, "a"), "candidate\n")
	p, err := e.BeginPublication(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	store := isolation.NewStore(t.TempDir(), source, s.repo).WithMutationCoordinator(s.mutations)
	entry, err := store.ImportSnapshotResult(t.Context(), p.Import())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(source, "a"), 0640); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Apply(t.Context(), entry.ID); !errors.Is(err, worktree.ErrApplyConflict) {
		t.Fatalf("source permission change was overwritten: %v", err)
	}
	if raw, _ := os.ReadFile(filepath.Join(source, "a")); string(raw) != "base\n" {
		t.Fatalf("conflict changed source bytes: %q", raw)
	}
}

func TestPublicationFreezesDeclaredInputsBeforeRootContinues(t *testing.T) {
	s, source := fixture(t)
	write(t, filepath.Join(source, ".gitignore"), "deps/\n")
	if err := os.Mkdir(filepath.Join(source, "deps"), 0700); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(source, "deps", "input"), "initial dependency\n")
	path := sessionstore.CanonicalSessionPath(filepath.Join(t.TempDir(), "root.jsonl"))
	e, err := s.Prepare(t.Context(), path, []string{"deps"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if err := e.BindSession(t.Context(), path, attachmentAuthority(t, path)); err != nil {
		t.Fatal(err)
	}
	live := e.Record().Candidate.WorkspaceRoot
	write(t, filepath.Join(live, "a"), "exported\n")
	p, err := e.BeginPublication(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(live, "a"), "continued\n")
	write(t, filepath.Join(live, "deps", "input"), "new dependency\n")
	store := isolation.NewStore(t.TempDir(), source, s.repo).WithMutationCoordinator(s.mutations).WithEnvironmentCopies([]string{"deps"})
	entry, err := store.ImportSnapshotResult(t.Context(), p.Import())
	if err != nil {
		t.Fatal(err)
	}
	if raw, _ := os.ReadFile(filepath.Join(entry.Cand.WorktreeRoot, "deps", "input")); string(raw) != "initial dependency\n" {
		t.Fatalf("import read live dependency: %q", raw)
	}
	if raw, _ := os.ReadFile(filepath.Join(entry.Cand.WorktreeRoot, "a")); string(raw) != "exported\n" {
		t.Fatalf("import read live source: %q", raw)
	}
}

func TestUnsupportedPermissionDeltaCannotAdvanceRootPublication(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission metadata")
	}
	s, _ := fixture(t)
	path := sessionstore.CanonicalSessionPath(filepath.Join(t.TempDir(), "root.jsonl"))
	e, err := s.Prepare(t.Context(), path, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if err := e.BindSession(t.Context(), path, attachmentAuthority(t, path)); err != nil {
		t.Fatal(err)
	}
	before := e.Record()
	if err := os.Chmod(filepath.Join(before.Candidate.WorkspaceRoot, "a"), 0640); err != nil {
		t.Fatal(err)
	}
	if p, err := e.BeginPublication(t.Context(), path); err == nil || p != nil {
		t.Fatalf("unsupported mode was publishable: %+v %v", p, err)
	}
	if after := e.Record(); after.Cursor != before.Cursor || after.Publication != nil {
		t.Fatal("unsupported mode advanced durable publication")
	}
}

func TestDiscardedPublicationCanRetryWithoutReusingOrDeletingOldResult(t *testing.T) {
	s, source := fixture(t)
	path := sessionstore.CanonicalSessionPath(filepath.Join(t.TempDir(), "root.jsonl"))
	e, err := s.Prepare(t.Context(), path, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if err := e.BindSession(t.Context(), path, attachmentAuthority(t, path)); err != nil {
		t.Fatal(err)
	}
	before := e.Record().Cursor
	write(t, filepath.Join(e.Record().Candidate.WorkspaceRoot, "a"), "retained change\n")
	p, err := e.BeginPublication(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	store := isolation.NewStore(t.TempDir(), source, s.repo).WithMutationCoordinator(s.mutations)
	entry, err := store.ImportSnapshotResult(t.Context(), p.Import())
	if err != nil {
		t.Fatal(err)
	}
	preview, err := store.PreviewDiscard(t.Context(), entry.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.DiscardExpected(t.Context(), entry.ID, preview.Revision); err != nil {
		t.Fatal(err)
	}
	if err := e.RetryPublication(t.Context(), path, p.ID, store); err != nil {
		t.Fatal(err)
	}
	next, err := e.BeginPublication(t.Context(), path)
	if err != nil || next.ID == p.ID || next.ResultID == p.ResultID || e.Record().Cursor != before {
		t.Fatalf("retry changed cursor or reused identity: %+v %v", next, err)
	}
	if state, err := store.ImportedResultState(t.Context(), p.Import()); err != nil || state != isolation.Discarded {
		t.Fatalf("old result lost: %s %v", state, err)
	}
	raw, err := os.ReadFile(filepath.Join(s.environmentDir(e.Record().ID), "publication-history-v1", p.ID+".json"))
	var archived Publication
	if err != nil || json.Unmarshal(raw, &archived) != nil || archived.ID != p.ID {
		t.Fatalf("old publication lost: %s %v", raw, err)
	}
}

func TestPublicationSurvivesRestartBetweenSourceCommitAndCursorAck(t *testing.T) {
	s, source := fixture(t)
	path := sessionstore.CanonicalSessionPath(filepath.Join(t.TempDir(), "root.jsonl"))
	e, err := s.Prepare(t.Context(), path, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.BindSession(t.Context(), path, attachmentAuthority(t, path)); err != nil {
		t.Fatal(err)
	}
	r := e.Record()
	write(t, filepath.Join(r.Candidate.WorkspaceRoot, "a"), "published\n")
	publication, err := e.BeginPublication(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	store := isolation.NewStore(t.TempDir(), source, s.repo).WithMutationCoordinator(s.mutations)
	entry, err := store.ImportSnapshotResult(t.Context(), publication.Import())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Apply(t.Context(), entry.ID); err != nil {
		t.Fatal(err)
	}
	e.Close()
	restored, err := s.Open(t.Context(), r.ID, path)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	pending, err := restored.BeginPublication(t.Context(), path)
	if err != nil || pending.ID != publication.ID {
		t.Fatalf("restart lost exported result: %+v %v", pending, err)
	}
	if err := restored.AcknowledgePublication(t.Context(), path, pending.ID, store); err != nil {
		t.Fatal(err)
	}
	if unchanged, err := restored.BeginPublication(t.Context(), path); err != nil || unchanged != nil {
		t.Fatalf("committed result exported twice: %+v %v", unchanged, err)
	}
}
