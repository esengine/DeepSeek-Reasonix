package isolation

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/contract/tool"
	"reasonix/internal/state/mutation"
)

func TestIgnoredOutputsOutsideCopyDeclarationsAreRetained(t *testing.T) {
	for _, declared := range []bool{false, true} {
		for _, owner := range []string{"", "owner"} {
			t.Run(testName(declared, owner), func(t *testing.T) {
				root := gitRepo(t)
				write(t, root, ".gitignore", "deps/\n")
				write(t, root, "deps/existing", "input")
				store := NewStore(t.TempDir(), root, opened(t, root))
				if declared {
					store.WithEnvironmentCopies([]string{"deps/existing"})
				}
				entry, err := store.BeginOwned(t.Context(), root, owner)
				if err != nil {
					t.Fatal(err)
				}
				write(t, entry.Cand.WorktreeRoot, "deps/new-package", "task output\n")
				if kept, err := store.Finish(t.Context(), entry); err != nil || !kept {
					t.Fatalf("ignored output not retained: %v %v", kept, err)
				}
				reopened := NewStore(store.managedRoot, root, store.repo)
				reopened.SweepExpired(t.Context(), -time.Second)
				if got := read(t, entry.Cand.WorktreeRoot, "deps/new-package"); got != "task output\n" {
					t.Fatalf("TTL deleted ignored output: %q", got)
				}
				if _, err := reopened.Discard(t.Context(), entry.ID); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func testName(declared bool, owner string) string {
	name := "no-declaration"
	if declared {
		name = "single-file-declaration"
	}
	if owner == "" {
		return name + "/unowned"
	}
	return name + "/owned"
}

func TestPublishingGitChangesCannotEraseUnpublishedEnvironment(t *testing.T) {
	root := gitRepo(t)
	write(t, root, ".gitignore", "deps/\n")
	coordinator, err := mutation.New(root, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	store := NewStore(t.TempDir(), root, opened(t, root)).WithMutationCoordinator(coordinator)
	entry, err := store.BeginOwned(t.Context(), root, "owner")
	if err != nil {
		t.Fatal(err)
	}
	write(t, entry.Cand.WorktreeRoot, "a.txt", "code edit\n")
	write(t, entry.Cand.WorktreeRoot, "deps/result", "unpublished result\n")
	if _, err := store.Finish(t.Context(), entry); err != nil {
		t.Fatal(err)
	}
	_, applyErr := store.Apply(t.Context(), entry.ID)
	_, integrationErr := store.PrepareIntegration(t.Context(), entry.ID, "owner")
	for _, err := range []error{applyErr, integrationErr} {
		var refusal tool.Refusal
		if !errors.As(err, &refusal) || refusal.Code != CodeRecoveryRequired {
			t.Fatalf("unpublished content reached settlement: %v", err)
		}
	}
	if got := read(t, root, "a.txt"); got != "one\n" {
		t.Fatalf("refused settlement wrote source: %q", got)
	}
	if got := read(t, entry.Cand.WorktreeRoot, "deps/result"); got != "unpublished result\n" {
		t.Fatalf("settlement removed environment result: %q", got)
	}
	if _, err := discardReviewed(t, store, entry.ID); err != nil {
		t.Fatal(err)
	}
}

func TestBaselineAndPreparationExtensionsSurviveLifecycleWrites(t *testing.T) {
	root := gitRepo(t)
	write(t, root, ".gitignore", "deps/\n")
	write(t, root, "deps/package", "input")
	store := NewStore(t.TempDir(), root, opened(t, root)).WithEnvironmentCopies([]string{"deps"})
	entry, err := store.BeginOwned(t.Context(), root, "owner")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(entryRecordPath(entry))
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"prepared_environment", "content_baseline"} {
		var nested map[string]json.RawMessage
		if err := json.Unmarshal(fields[key], &nested); err != nil {
			t.Fatal(err)
		}
		nested["future_extension"] = json.RawMessage(`{"retained":true}`)
		fields[key], err = json.Marshal(nested)
		if err != nil {
			t.Fatal(err)
		}
	}
	raw, err = json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(entryRecordPath(entry), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Finish(t.Context(), entry); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(entryRecordPath(entry))
	if err != nil || bytes.Count(after, []byte("future_extension")) != 2 {
		t.Fatalf("lifecycle dropped extensions: %v %s", err, after)
	}
	if _, err := os.Stat(filepath.Join(entry.Cand.WorktreeRoot, "deps/package")); err != nil {
		t.Fatal(err)
	}
	if _, err := discardReviewed(t, store, entry.ID); err != nil {
		t.Fatal(err)
	}
}

func TestDelayedCleanupCannotDeleteNewContentOnAPublishedPath(t *testing.T) {
	store, entry := settledFixture(t)
	interrupted := errors.New("cleanup interrupted after directory removal")
	store.cleanupProbe = func(e *Entry) error {
		if err := os.MkdirAll(e.Cand.WorktreeRoot, 0700); err != nil {
			return err
		}
		write(t, e.Cand.WorktreeRoot, "a.txt", "late work on published path\n")
		write(t, e.Cand.WorktreeRoot, "b.txt", "two\n")
		return interrupted
	}
	if _, err := store.Apply(t.Context(), entry.ID); !errors.Is(err, interrupted) {
		t.Fatalf("cleanup fixture did not reach terminal publication: %v", err)
	}
	store.cleanupProbe = nil
	restarted := NewStore(store.managedRoot, store.workspace, store.repo).WithMutationCoordinator(store.mutations)
	_, err := restarted.Apply(t.Context(), entry.ID)
	var refusal tool.Refusal
	if !errors.As(err, &refusal) || refusal.Code != CodeRecoveryRequired {
		t.Fatalf("late candidate content did not block cleanup: %v", err)
	}
	restarted.SweepExpired(t.Context(), -time.Second)
	if got := read(t, entry.Cand.WorktreeRoot, "a.txt"); got != "late work on published path\n" {
		t.Fatalf("terminal cleanup deleted new content: %q", got)
	}
	if got := read(t, store.workspace, "a.txt"); got != "candidate\n" {
		t.Fatalf("cleanup retry reapplied result: %q", got)
	}
}
