package isolation

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"reasonix/internal/contract/tool"
	"reasonix/internal/platform/gitcmd"
)

func TestPreparationUsesOneCapturedSnapshotAcrossCandidates(t *testing.T) {
	root, managed := gitRepo(t), t.TempDir()
	store := NewStore(managed, root, opened(t, root))
	prepared, err := store.Prepare(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	write(t, root, "a.txt", "source moved\n")
	var entries []*Entry
	t.Cleanup(func() {
		for _, e := range entries {
			if err := store.Abort(context.Background(), e); err != nil {
				t.Error(err)
			}
		}
	})
	for range 2 {
		e, err := prepared.Begin(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		entries = append(entries, e)
		if got := read(t, e.Cand.WorkspaceRoot, "a.txt"); got != "one\n" {
			t.Errorf("preparation refreshed the source: %q", got)
		}
		if e.Workspace != root || filepath.Dir(filepath.Dir(e.Cand.WorktreeRoot)) != managed {
			t.Errorf("candidate escaped its Store: %+v", e)
		}
	}
	if entries[0].ID == entries[1].ID || entries[0].Cand.WorktreeRoot == entries[1].Cand.WorktreeRoot {
		t.Fatal("preparation reused a result identity or candidate")
	}
	if entries[0].Snap.Commit != entries[1].Snap.Commit || entries[0].Snap.Tree != entries[1].Snap.Tree {
		t.Fatal("candidate baselines differ")
	}
	legacy, err := store.Begin(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	entries = append(entries, legacy)
	if got := read(t, legacy.Cand.WorkspaceRoot, "a.txt"); got != "source moved\n" {
		t.Errorf("ordinary Begin did not capture fresh source: %q", got)
	}
	if legacy.Snap.Commit == entries[0].Snap.Commit || legacy.Snap.Tree == entries[0].Snap.Tree {
		t.Fatal("ordinary Begin reused an older preparation")
	}
	if len(store.Pending()) != 0 {
		t.Fatal("preparation published unfinished results")
	}
}

func TestPreparationResultsUseExistingSettlementAndRecovery(t *testing.T) {
	root, managed := gitRepo(t), t.TempDir()
	store := NewStore(managed, root, opened(t, root))
	p, err := store.Prepare(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	e, err := p.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	write(t, e.Cand.WorkspaceRoot, "a.txt", "candidate\n")
	if kept, err := store.Finish(t.Context(), e); err != nil || !kept {
		t.Fatalf("Finish: kept=%v error=%v", kept, err)
	}
	rebuilt := NewStore(managed, root, opened(t, root))
	if ids := rebuilt.Pending(); len(ids) != 1 || ids[0] != e.ID {
		t.Fatalf("ordinary pending reader did not find prepared result: %v", ids)
	}
	write(t, root, "a.txt", "user edit\n")
	var refusal tool.Refusal
	if _, err := rebuilt.Apply(t.Context(), e.ID); !errors.As(err, &refusal) || refusal.Code != CodeApplyConflict {
		t.Fatalf("stale prepared result applied: %v", err)
	}
	if got := read(t, root, "a.txt"); got != "user edit\n" {
		t.Fatalf("stale apply changed user bytes: %q", got)
	}
	if ids := rebuilt.Pending(); len(ids) != 1 || ids[0] != e.ID {
		t.Fatalf("conflicted result disappeared: %v", ids)
	}
	if _, err := rebuilt.Discard(t.Context(), e.ID); err != nil {
		t.Fatal(err)
	}
	if len(rebuilt.Pending()) != 0 {
		t.Fatal("discard left a prepared result published")
	}
}

func TestPreparationRejectsUnavailableAndCancelledCalls(t *testing.T) {
	var store *Store
	if p, err := store.Prepare(t.Context()); p != nil || !errors.Is(err, errUnavailable) {
		t.Fatalf("nil Store preparation: %v %v", p, err)
	}
	for _, p := range []*Preparation{nil, {}} {
		if e, err := p.Begin(t.Context()); e != nil || !errors.Is(err, errUnavailable) {
			t.Fatalf("unbound preparation: %v %v", e, err)
		}
	}
	root, managed := gitRepo(t), t.TempDir()
	store = NewStore(managed, root, opened(t, root))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if p, err := store.Prepare(ctx); p != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled capture: %v %v", p, err)
	}
	p, err := store.Prepare(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if e, err := p.Begin(ctx); e != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled candidate creation: %v %v", e, err)
	}
	files, err := os.ReadDir(managed)
	if err != nil || len(files) != 0 {
		t.Fatalf("cancelled preparation created candidates: %v %v", files, err)
	}
	if e, err := p.Begin(t.Context()); err != nil {
		t.Fatal(err)
	} else if err := store.Abort(t.Context(), e); err != nil {
		t.Fatal(err)
	}
}

func TestPreparationPreservesSubdirectoryIdentity(t *testing.T) {
	root := gitRepo(t)
	write(t, root, "nested/a.txt", "nested\n")
	for _, args := range [][]string{{"add", "nested/a.txt"}, {"commit", "-q", "-m", "nested fixture"}} {
		cmd := gitcmd.Command(t.Context(), root, args...)
		cmd.Env = append(cmd.Env, "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	sub := filepath.Join(root, "nested")
	repo, err := gitcmd.Open(t.Context(), sub)
	if err != nil {
		t.Fatal(err)
	}
	store := NewStore(t.TempDir(), sub, repo)
	p, err := store.Prepare(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	e, err := p.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := store.Abort(context.Background(), e); err != nil {
			t.Error(err)
		}
	}()
	if e.Workspace != sub || e.Snap.Prefix != "nested" || filepath.Base(e.Cand.WorkspaceRoot) != "nested" {
		t.Fatalf("lost source subdirectory: %+v", e)
	}
	if got := read(t, e.Cand.WorkspaceRoot, "a.txt"); got != "nested\n" {
		t.Fatalf("subdirectory candidate bytes: %q", got)
	}
}

func TestPreparationRejectsMismatchedStoreWorkspace(t *testing.T) {
	root, other, managed := gitRepo(t), gitRepo(t), t.TempDir()
	store := NewStore(managed, other, opened(t, root))
	if p, err := store.Prepare(t.Context()); p != nil || err == nil {
		t.Fatalf("mismatched source/workspace preparation: %v %v", p, err)
	}
	if files, err := os.ReadDir(managed); err != nil || len(files) != 0 {
		t.Fatalf("mismatched source created candidates: %v %v", files, err)
	}
}

func TestPreparationRejectsReplacedSourceDirectories(t *testing.T) {
	for _, what := range []string{"workspace", "git_metadata"} {
		t.Run(what, func(t *testing.T) {
			root := gitRepo(t)
			store := NewStore(t.TempDir(), root, opened(t, root))
			p, err := store.Prepare(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			path := root
			if what == "git_metadata" {
				path = filepath.Join(root, ".git")
			}
			parked := path + ".parked"
			if err := os.Rename(path, parked); err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := os.RemoveAll(path); err != nil {
					t.Error(err)
				}
				if err := os.Rename(parked, path); err != nil {
					t.Error(err)
				}
			}()
			if err := os.CopyFS(path, os.DirFS(parked)); err != nil {
				t.Fatal(err)
			}
			e, err := p.Begin(t.Context())
			if e != nil {
				if cleanupErr := store.Abort(t.Context(), e); cleanupErr != nil {
					t.Error(cleanupErr)
				}
			}
			if e != nil || !errors.Is(err, errUnavailable) {
				t.Fatalf("preparation accepted replaced %s: %v %v", what, e, err)
			}
		})
	}
}

func TestPreparationRejectsReplacedGitIdentity(t *testing.T) {
	root := gitRepo(t)
	store := NewStore(t.TempDir(), root, opened(t, root))
	p, err := store.Prepare(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	gitDir := filepath.Join(root, ".git")
	if err := os.Rename(gitDir, gitDir+".parked"); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Rename(gitDir+".parked", gitDir); err != nil {
			t.Error(err)
		}
	}()
	if e, err := p.Begin(t.Context()); e != nil || err == nil {
		t.Fatalf("preparation accepted missing source identity: %v %v", e, err)
	}
}

func TestPreparationConcurrentCandidatesDoNotShareResultState(t *testing.T) {
	root := gitRepo(t)
	store := NewStore(t.TempDir(), root, opened(t, root))
	p, err := store.Prepare(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	entries, errs := make([]*Entry, 2), make([]error, 2)
	for i := range entries {
		wg.Go(func() { entries[i], errs[i] = p.Begin(t.Context()) })
	}
	wg.Wait()
	for i, e := range entries {
		if errs[i] != nil {
			t.Errorf("candidate %d: %v", i, errs[i])
			continue
		}
		defer func() {
			if err := store.Abort(context.Background(), e); err != nil {
				t.Error(err)
			}
		}()
	}
	if t.Failed() {
		return
	}
	write(t, entries[0].Cand.WorkspaceRoot, "a.txt", "private\n")
	if read(t, entries[1].Cand.WorkspaceRoot, "a.txt") != "one\n" || read(t, root, "a.txt") != "one\n" {
		t.Fatal("candidate write leaked to sibling or source")
	}
	if entries[0].ID == entries[1].ID || entries[0].Cand.WorktreeRoot == entries[1].Cand.WorktreeRoot {
		t.Fatal("concurrent creation reused result state")
	}
}
