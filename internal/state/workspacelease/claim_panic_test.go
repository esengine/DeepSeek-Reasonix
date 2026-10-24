package workspacelease

import (
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/fileutil"
)

func TestCompatibilityClaimPanicBeforeHandoffReleasesItsNewLocks(t *testing.T) {
	root, state := t.TempDir(), t.TempDir()
	path := filepath.Join(root, "a")
	before := fileutil.CrashPoint
	fileutil.CrashPoint = func(op, target string) {
		if op == "atomic-write" && strings.Contains(target, ".claims") {
			panic("claim write failed before ownership handoff")
		}
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Error("fixture did not fail at claim publication")
			}
		}()
		_, _, _ = TryClaimPaths(root, state, "first", []string{path})
	}()
	fileutil.CrashPoint = before
	t.Cleanup(func() { fileutil.CrashPoint = before })
	release, _, err := TryClaimPaths(root, state, "second", []string{path})
	if err != nil {
		t.Fatalf("failed handoff retained physical compatibility ownership: %v", err)
	}
	release()
}
