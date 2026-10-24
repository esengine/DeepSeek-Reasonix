package isolation

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/contract/tool"
	"reasonix/internal/state/mutation"
)

func discardFixture(t *testing.T) (*Store, *Entry) {
	t.Helper()
	root := gitRepo(t)
	write(t, root, ".gitignore", "deps/\n")
	c, err := mutation.New(root, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	s := NewStore(t.TempDir(), root, opened(t, root)).WithMutationCoordinator(c)
	e, err := s.BeginOwned(t.Context(), root, "owner")
	if err != nil {
		t.Fatal(err)
	}
	write(t, e.Cand.WorktreeRoot, "a.txt", "candidate result\n")
	if kept, err := s.Finish(t.Context(), e); err != nil || !kept {
		t.Fatalf("candidate not retained: %v %v", kept, err)
	}
	return s, e
}

func discardReviewed(t *testing.T, store *Store, id string) (*Entry, error) {
	t.Helper()
	preview, err := store.PreviewDiscard(t.Context(), id)
	if err != nil {
		return nil, err
	}
	return store.DiscardExpected(t.Context(), id, preview.Revision)
}

func TestDiscardPreviewRejectsChangedContentIncludingIgnoredOutputs(t *testing.T) {
	for _, path := range []string{"a.txt", "deps/new-result"} {
		t.Run(path, func(t *testing.T) {
			s, e := discardFixture(t)
			preview, err := s.PreviewDiscard(t.Context(), e.ID)
			if err != nil {
				t.Fatal(err)
			}
			write(t, e.Cand.WorktreeRoot, path, "later result\n")
			_, err = s.DiscardExpected(t.Context(), e.ID, preview.Revision)
			var refusal tool.Refusal
			if !errors.As(err, &refusal) || refusal.Code != CodeStaleDiscard {
				t.Fatalf("stale deletion approval not refused: %v", err)
			}
			if got := read(t, e.Cand.WorktreeRoot, path); got != "later result\n" {
				t.Fatalf("late work was removed: %q", got)
			}
			fresh, err := s.PreviewDiscard(t.Context(), e.ID)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.DiscardExpected(t.Context(), e.ID, fresh.Revision); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Lstat(e.Cand.WorktreeRoot); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("freshly approved result was not discarded: %v", err)
			}
		})
	}
}

func TestDiscardCleanupAfterRestartCannotEraseRecreatedCandidate(t *testing.T) {
	s, e := discardFixture(t)
	preview, err := s.PreviewDiscard(t.Context(), e.ID)
	if err != nil {
		t.Fatal(err)
	}
	injected := errors.New("injected interruption after removal")
	s.cleanupProbe = func(e *Entry) error {
		write(t, e.Cand.WorktreeRoot, "a.txt", "recreated work\n")
		return injected
	}
	if _, err := s.DiscardExpected(t.Context(), e.ID, preview.Revision); !errors.Is(err, injected) {
		t.Fatalf("cleanup interruption not injected: %v", err)
	}
	restarted := NewStore(s.managedRoot, s.workspace, s.repo).WithMutationCoordinator(s.mutations)
	restarted.SweepExpired(t.Context(), -time.Second)
	if got := read(t, e.Cand.WorktreeRoot, "a.txt"); got != "recreated work\n" {
		t.Fatalf("restart sweep deleted late work: %q", got)
	}
	if _, err := restarted.Discard(t.Context(), e.ID); err == nil {
		t.Fatal("old discard decision erased recreated work")
	}
	if _, err := os.Stat(filepath.Join(e.Cand.WorktreeRoot, "a.txt")); err != nil {
		t.Fatal(err)
	}
}

func TestNewStrategyDiscardToolRequiresExplicitHostAction(t *testing.T) {
	s, e := discardFixture(t)
	_, err := NewDiscardTool(s).Execute(t.Context(), []byte(`{"id":"`+e.ID+`"}`))
	var refusal tool.Refusal
	if !errors.As(err, &refusal) || refusal.Code != "isolation.user_action_required" {
		t.Fatalf("model may discard retained results: %v", err)
	}
	if _, err := os.Stat(e.Cand.WorktreeRoot); err != nil {
		t.Fatal(err)
	}
}

func TestLegacyStoreCannotDiscardHostManagedResult(t *testing.T) {
	s, e := discardFixture(t)
	legacy := NewStore(s.managedRoot, s.workspace, s.repo)
	for _, discard := range []func() error{
		func() error { _, err := legacy.Discard(t.Context(), e.ID); return err },
		func() error {
			_, err := NewDiscardTool(legacy).Execute(t.Context(), []byte(`{"id":"`+e.ID+`"}`))
			return err
		},
	} {
		var refusal tool.Refusal
		if err := discard(); !errors.As(err, &refusal) || refusal.Code != "isolation.user_action_required" {
			t.Fatalf("legacy caller bypassed result-owned discard policy: %v", err)
		}
	}
	if got := read(t, e.Cand.WorktreeRoot, "a.txt"); got != "candidate result\n" {
		t.Fatal(got)
	}
}

func TestHostDiscardOfAbsentCandidateRequiresUnchangedAbsence(t *testing.T) {
	for _, stage := range []string{"preparation", "pending"} {
		t.Run(stage, func(t *testing.T) {
			s, e := discardFixture(t)
			if stage == "preparation" {
				ctx, cancel := context.WithCancel(t.Context())
				s.preparationProbe = func(state State, entry *Entry) {
					if state == Preparing {
						e = entry
						cancel()
					}
				}
				if _, err := s.BeginOwned(ctx, s.workspace, "owner"); !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
			} else if err := os.RemoveAll(e.Cand.WorktreeRoot); err != nil {
				t.Fatal(err)
			}
			preview, err := s.PreviewDiscard(t.Context(), e.ID)
			if err != nil {
				t.Fatal(err)
			}
			write(t, e.Cand.WorktreeRoot, "a.txt", "recreated work\n")
			if _, err := s.DiscardExpected(t.Context(), e.ID, preview.Revision); err == nil {
				t.Fatal("absence preview deleted recreated work")
			}
			if got := read(t, e.Cand.WorktreeRoot, "a.txt"); got != "recreated work\n" {
				t.Fatal(got)
			}
			if err := os.RemoveAll(e.Cand.WorktreeRoot); err != nil {
				t.Fatal(err)
			}
			if _, err := s.DiscardExpected(t.Context(), e.ID, preview.Revision); err != nil {
				t.Fatal(err)
			}
			receipt, err := s.operationSnapshot(t.Context(), e.ID)
			if err != nil || !receipt.CleanupComplete {
				t.Fatalf("absent candidate did not settle: %+v %v", receipt, err)
			}
		})
	}
}

func TestAbsentCandidateFromSubdirectoryWorkspaceCanBeDiscarded(t *testing.T) {
	root := gitRepo(t)
	workspace := filepath.Join(root, "app")
	write(t, workspace, "nested.txt", "source\n")
	cmd := exec.Command("git", "-c", "user.name=t", "-c", "user.email=t@t", "add", "app")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("stage subdirectory: %v %s", err, out)
	}
	cmd = exec.Command("git", "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "subdirectory")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("commit subdirectory: %v %s", err, out)
	}
	coordinator, err := mutation.New(workspace, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	store := NewStore(t.TempDir(), workspace, opened(t, workspace)).WithMutationCoordinator(coordinator)
	entry, err := store.BeginOwned(t.Context(), workspace, "owner")
	if err != nil {
		t.Fatal(err)
	}
	write(t, entry.Cand.WorkspaceRoot, "nested.txt", "result\n")
	if _, err := store.Finish(t.Context(), entry); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(entry.Cand.WorktreeRoot); err != nil {
		t.Fatal(err)
	}
	if _, err := discardReviewed(t, store, entry.ID); err != nil {
		t.Fatal(err)
	}
}

func TestDiscardApprovalSurvivesCleanupInterruptionAndRetainsUnknownFields(t *testing.T) {
	s, e := discardFixture(t)
	preview, err := s.PreviewDiscard(t.Context(), e.ID)
	if err != nil {
		t.Fatal(err)
	}
	injected := errors.New("injected cleanup interruption")
	s.cleanupProbe = func(*Entry) error { return injected }
	if _, err := s.DiscardExpected(t.Context(), e.ID, preview.Revision); !errors.Is(err, injected) {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(entryPath(e.Cand))
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	var content map[string]json.RawMessage
	if err := json.Unmarshal(fields["discard_content"], &content); err != nil {
		t.Fatal(err)
	}
	content["future_annotation"] = json.RawMessage(`"keep"`)
	fields["discard_content"], _ = json.Marshal(content)
	raw, _ = json.Marshal(fields)
	if err := os.WriteFile(entryPath(e.Cand), raw, 0600); err != nil {
		t.Fatal(err)
	}
	restarted := NewStore(s.managedRoot, s.workspace, s.repo).WithMutationCoordinator(s.mutations)
	settled, err := restarted.DiscardExpected(t.Context(), e.ID, preview.Revision)
	if err != nil || !settled.CleanupComplete {
		t.Fatalf("same explicit action was not idempotent after cleanup interruption: %v %v", settled, err)
	}
	raw, err = os.ReadFile(entryPath(e.Cand))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(fields["discard_content"], &content); err != nil || string(content["future_annotation"]) != `"keep"` {
		t.Fatalf("cleanup rewrite dropped unknown proof fields: %v / %s", err, fields["discard_content"])
	}
}
