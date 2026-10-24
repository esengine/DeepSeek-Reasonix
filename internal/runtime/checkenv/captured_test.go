package checkenv

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCapturedInputOnlyReplacesFrozenAuthorizedRegularPath(t *testing.T) {
	for _, mode := range []string{"replace", "deleted", "outside", "denied", "git", "alias"} {
		t.Run(mode, func(t *testing.T) {
			source, frozen := t.TempDir(), t.TempDir()
			path := "capture_test.go"
			policy := treePolicy{}
			if mode == "git" {
				path = ".git/capture_test.go"
			}
			if mode == "outside" {
				path = "../outside_test.go"
			}
			if mode == "denied" {
				policy.denied = []string{path}
			}
			if mode != "deleted" && mode != "outside" {
				if err := os.MkdirAll(filepath.Dir(filepath.Join(frozen, path)), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(frozen, path), []byte("current"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "alias" {
				if err := os.Remove(filepath.Join(frozen, path)); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(source, "source"), filepath.Join(frozen, path)); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
			}
			body := []byte("captured")
			spec := (Spec{SourceRoot: source}).WithCaptured(filepath.Join(source, path), body)
			canonical, err := filepath.EvalSymlinks(source)
			if err != nil {
				t.Fatal(err)
			}
			spec.SourceRoot = canonical
			body[0] = 'X'
			_, err = spec.applyCapture(t.Context(), frozen, policy)
			if mode != "replace" && mode != "deleted" {
				if err == nil {
					t.Fatal("unsafe captured replacement accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(filepath.Join(frozen, path))
			if err != nil || string(raw) != "captured" {
				t.Fatalf("captured bytes changed: %q %v", raw, err)
			}
			if _, err := os.Stat(filepath.Join(source, path)); !os.IsNotExist(err) {
				t.Fatalf("source changed: %v", err)
			}
		})
	}
}
