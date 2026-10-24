package worktree

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/fileutil"
)

func TestPostimageValidatorSeesTheImagesActuallyPublished(t *testing.T) {
	for _, mode := range []os.FileMode{0600, 0640, 0644} {
		t.Run(mode.String(), func(t *testing.T) {
			source, snap, candidate, finish := isolatedCandidate(t)
			writeRepoFile(t, candidate.WorktreeRoot, "a.txt", "candidate\n")
			tree, changes := finish()
			if err := os.Chmod(filepath.Join(source, "a.txt"), mode); err != nil {
				t.Fatal(err)
			}
			var projected fileutil.ContentVersion
			ctx := WithApplyPostimageValidator(t.Context(), func(_ context.Context, versions map[string]fileutil.ContentVersion) error {
				projected = versions["a.txt"]
				if got := readRepoFile(t, source, "a.txt"); got != "one\n" {
					t.Fatal("postimage was checked after source publication")
				}
				if _, err := os.Lstat(journalPath(snap.Repo)); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("postimage checked after journal write: %v", err)
				}
				return nil
			})
			if err := ApplyPaths(ctx, snap, tree, changes); err != nil {
				t.Fatal(err)
			}
			actual, err := fileutil.ReadContentVersion(t.Context(), filepath.Join(source, "a.txt"))
			if err != nil || actual != projected {
				t.Fatalf("published image differs from validated one: projected=%+v actual=%+v err=%v", projected, actual, err)
			}
		})
	}
}

func TestPostimageRefusalLeavesSourceAndJournalUntouched(t *testing.T) {
	source, snap, candidate, finish := isolatedCandidate(t)
	writeRepoFile(t, candidate.WorktreeRoot, "a.txt", "candidate\n")
	tree, changes := finish()
	refused := errors.New("verified image differs")
	ctx := WithApplyPostimageValidator(t.Context(), func(context.Context, map[string]fileutil.ContentVersion) error { return refused })
	if err := ApplyPaths(ctx, snap, tree, changes); !errors.Is(err, refused) {
		t.Fatalf("postimage refusal lost: %v", err)
	}
	if got := readRepoFile(t, source, "a.txt"); got != "one\n" {
		t.Fatal("refused publication changed source")
	}
	if _, err := os.Lstat(journalPath(snap.Repo)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("refused publication left journal: %v", err)
	}
}
