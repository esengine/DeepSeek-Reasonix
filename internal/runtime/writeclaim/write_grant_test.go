package writeclaim

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
)

// The audit compares observed mutations against everything the run was allowed
// to write. A path the user granted must not surface as an escape.
func TestGrantedPathsCountAsAllowedInTheAudit(t *testing.T) {
	root := testenv.TempDir(t)
	if err := os.MkdirAll(filepath.Join(root, "auth"), 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(root, "package.json")
	if err := os.WriteFile(outside, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	claim, err := NormalizeWritePaths(root, []string{"auth"})
	if err != nil {
		t.Fatal(err)
	}
	grant := NewWriteGrant(claim)
	if grant.Scope().AllowsPath(outside) {
		t.Fatal("an ungranted path is already in scope")
	}
	grant.Add(outside)
	if !grant.Scope().AllowsPath(outside) {
		t.Error("a granted path is missing from the audit scope; it would be reported as an escape")
	}
	// Scheduling still reads only what was declared: nothing added later was
	// proven against the runs that had already started.
	if grant.Declared().AllowsPath(outside) {
		t.Error("a granted path leaked into the declared set that scheduling parallelizes on")
	}
}
