package isolation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/platform/gitcmd"
)

func TestAmbiguousResultIdentityRefusesEveryExecutionEntry(t *testing.T) {
	for _, tc := range []struct {
		name       string
		otherIndex int
	}{{"same_workspace", -1}, {"other_first", 0}, {"other_last", 1}} {
		t.Run(tc.name, func(t *testing.T) {
			root, managed := t.TempDir(), t.TempDir()
			originals := map[string][]byte{}
			for index, state := range []State{Integrated, Discarded} {
				workspace := root
				if index == tc.otherIndex {
					workspace = t.TempDir()
				}
				candidate := filepath.Join(managed, []string{"a", "b"}[index], "worktree")
				d := durableEntry{Version: 2, ID: "iso_12345678", Workspace: workspace, ResumeOwner: "owner", State: state, CleanupComplete: true, Tree: "sealed"}
				d.Cand.WorktreeRoot, d.Cand.WorkspaceRoot = candidate, workspace
				body, err := json.Marshal(d)
				if err != nil {
					t.Fatal(err)
				}
				path := entryPath(d.Cand)
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, body, 0600); err != nil {
					t.Fatal(err)
				}
				originals[path] = body
			}
			s := NewStore(managed, root, gitcmd.Repo{})
			catalog, err := s.Catalog(t.Context())
			if err != nil || len(catalog.Results) != 1 || len(catalog.Results[0].Actions) != 0 || catalog.Results[0].Reason == "" {
				t.Fatalf("ambiguous catalog is actionable: %+v %v", catalog, err)
			}
			for name, operation := range map[string]func() error{
				"apply":   func() error { _, err := s.Apply(t.Context(), "iso_12345678"); return err },
				"resume":  func() error { _, err := s.Resume(t.Context(), "iso_12345678", "owner"); return err },
				"discard": func() error { _, err := s.Discard(t.Context(), "iso_12345678"); return err },
				"preview": func() error { _, err := s.PreviewDiscard(t.Context(), "iso_12345678"); return err },
				"lookup":  func() error { _, err := s.lookup("iso_12345678"); return err },
			} {
				if err := operation(); refusalCodeOf(err) != CodeUnsupportedState {
					t.Errorf("%s accepted ambiguous identity: %v", name, err)
				}
			}
			for path, body := range originals {
				after, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(body, after) {
					t.Fatalf("ambiguous receipt was changed: %s %v", path, err)
				}
			}
		})
	}
}

func TestLiveResultCannotSaveFinishOrAbortAmbiguousIdentity(t *testing.T) {
	repo := gitRepo(t)
	s := NewStore(t.TempDir(), repo, opened(t, repo))
	e, err := s.BeginOwned(t.Context(), repo, "owner")
	if err != nil {
		t.Fatal(err)
	}
	duplicate := filepath.Join(s.managedRoot, "duplicate", entryFile)
	t.Cleanup(func() { _ = os.Remove(duplicate); s.Abort(context.Background(), e) })
	write(t, e.Cand.WorkspaceRoot, "a.txt", "unsealed work\n")
	before, err := os.ReadFile(entryRecordPath(e))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(duplicate), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(duplicate, before, 0600); err != nil {
		t.Fatal(err)
	}
	if err := persist(e); refusalCodeOf(err) != CodeUnsupportedState {
		t.Fatalf("ordinary save accepted duplicate: %v", err)
	}
	s.Abort(t.Context(), e)
	if _, err := os.Stat(e.Cand.WorktreeRoot); err != nil {
		t.Fatal("abort removed ambiguous candidate", err)
	}
	if _, err := s.Finish(t.Context(), e); refusalCodeOf(err) != CodeUnsupportedState {
		t.Fatalf("finish accepted duplicate: %v", err)
	}
	if read(t, e.Cand.WorkspaceRoot, "a.txt") != "unsealed work\n" || read(t, repo, "a.txt") != "one\n" {
		t.Fatal("ambiguous candidate or source was modified")
	}
	for _, path := range []string{entryRecordPath(e), duplicate} {
		after, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("ambiguous live record changed: %s %v", path, err)
		}
	}
}

func TestIdenticalTerminalPromotionPairCanFinishRecovery(t *testing.T) {
	repo := gitRepo(t)
	s := NewStore(t.TempDir(), repo, opened(t, repo))
	e, err := s.BeginOwned(t.Context(), repo, "owner")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Abort(t.Context(), e) })
	write(t, e.Cand.WorkspaceRoot, "a.txt", "retained\n")
	if _, err := s.Finish(t.Context(), e); err != nil {
		t.Fatal(err)
	}
	e.State = Integrated
	if err := persist(e); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(entryRecordPath(e))
	if err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(filepath.Dir(e.Cand.WorktreeRoot), legacyEntryFile)
	if err := os.WriteFile(legacy, body, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Apply(t.Context(), e.ID); err != nil {
		t.Fatalf("identical promotion pair cannot recover: %v", err)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatalf("legacy promotion marker retained: %v", err)
	}
}

func TestDiscardPromotionPairCanRefreshApprovalAfterRestart(t *testing.T) {
	for _, stage := range []string{"before_copy", "after_copy"} {
		t.Run(stage, func(t *testing.T) { testDiscardPromotionRestart(t, stage) })
	}
}

func testDiscardPromotionRestart(t *testing.T, stage string) {
	t.Helper()
	s, e := discardFixture(t)
	legacy := filepath.Join(filepath.Dir(e.Cand.WorktreeRoot), legacyEntryFile)
	if err := os.Rename(entryPath(e.Cand), legacy); err != nil {
		t.Fatal(err)
	}
	interrupted := errors.New("promotion interrupted")
	s.promotionProbe = func(*Entry) error { return interrupted }
	if _, err := discardReviewed(t, s, e.ID); !errors.Is(err, interrupted) {
		t.Fatal(err)
	}
	if stage == "before_copy" {
		if err := os.Remove(entryPath(e.Cand)); err != nil {
			t.Fatal(err)
		}
	}
	restarted := NewStore(s.managedRoot, s.workspace, s.repo).WithMutationCoordinator(s.mutations)
	settled, err := discardReviewed(t, restarted, e.ID)
	if err != nil || !settled.CleanupComplete {
		t.Fatalf("promotion retry cannot refresh approval: %+v %v", settled, err)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatalf("legacy receipt retained: %v", err)
	}
	if read(t, s.workspace, "a.txt") != "one\n" {
		t.Fatal("discard changed source workspace")
	}
}
