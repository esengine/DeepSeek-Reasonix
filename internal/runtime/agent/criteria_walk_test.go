package agent

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/base/fileutil"
	"reasonix/internal/base/testenv"
	"reasonix/internal/safety/evidence"
)

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// What the concurrent walk lists is what a sequential walk of the same tree
// lists: version-control stores are not entered, the root is walked even if it
// is itself named like one, and only names that may hold criteria are returned.
func TestCriterionCandidatesAreWhatASequentialWalkFinds(t *testing.T) {
	root := testenv.TempDir(t)
	writeTree(t, root, map[string]string{
		"a_test.go":                  criterionBefore,
		"pkg/b_test.go":              criterionBefore,
		"pkg/deep/er/c_test.go":      criterionBefore,
		"pkg/readme.md":              "x",
		".git/hooks/ignored_test.go": criterionBefore,
		"vendor/x/d_test.go":         criterionBefore,
	})
	var want []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if fileutil.IsVCSStoreDir(d.Name()) && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		if evidence.PathMayHoldTestCriteria(path) {
			want = append(want, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(want)
	got := criterionCandidates(t.Context(), root, os.ReadDir)
	if !slices.Equal(got, want) {
		t.Fatalf("candidates = %v, want %v", got, want)
	}
	if len(got) != 4 {
		t.Fatalf("fixture drifted: %v", got)
	}
}

// A directory that answers slowly must not hold the rest of the tree behind it:
// two directory reads have to be in flight at once. A walk that asks one
// question at a time never gets the second one asked, and the first times out.
func TestCriterionCandidatesReadDirectoriesConcurrently(t *testing.T) {
	root := testenv.TempDir(t)
	writeTree(t, root, map[string]string{"one/a_test.go": criterionBefore, "two/b_test.go": criterionBefore})
	var inFlight atomic.Int32
	overlapped := make(chan struct{})
	var once atomic.Bool
	readDir := func(path string) ([]os.DirEntry, error) {
		if path != root {
			if inFlight.Add(1) >= 2 && once.CompareAndSwap(false, true) {
				close(overlapped)
			}
			defer inFlight.Add(-1)
			select {
			case <-overlapped:
			case <-time.After(5 * time.Second):
				t.Error("two directory reads were never in flight together")
			}
		}
		return os.ReadDir(path)
	}
	got := criterionCandidates(t.Context(), root, readDir)
	if len(got) != 2 {
		t.Fatalf("candidates = %v", got)
	}
}

// A cancelled capture stops reading and returns what it has instead of walking
// on, the way the sequential walk stopped on a stop.
func TestCriterionCandidatesStopOnCancel(t *testing.T) {
	root := testenv.TempDir(t)
	writeTree(t, root, map[string]string{"a_test.go": criterionBefore})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if got := criterionCandidates(ctx, root, os.ReadDir); len(got) != 0 {
		t.Fatalf("a cancelled walk listed %v", got)
	}
}

// Only a file that carries a criterion is kept, and an unreadable one holds none.
func TestCriterionBytesKeepOnlyWhatHoldsACriterion(t *testing.T) {
	root := testenv.TempDir(t)
	writeTree(t, root, map[string]string{
		"holds_test.go": criterionBefore,
		"empty_test.go": "package p\n",
	})
	paths := []string{
		filepath.Join(root, "holds_test.go"),
		filepath.Join(root, "empty_test.go"),
		filepath.Join(root, "missing_test.go"),
	}
	got := criterionBytes(t.Context(), paths)
	if len(got) != 1 || string(got[paths[0]]) != criterionBefore {
		t.Fatalf("kept %v", got)
	}
}
