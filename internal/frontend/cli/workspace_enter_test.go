package cli

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
)

// `serve --dir` has to leave the working directory and boot's WorkspaceRoot
// naming the same folder, because `write_roots` is derived from the root: a
// phone set the child's directory and the kernel resolved its root some other
// way, and every file tool ended up confined to the app's own folder while the
// sidebar named the shared one. The root is now a value the caller passes on,
// so what this pins is that the two answers agree.
func TestEnterWorkspaceNamesTheRootItSwitchedInto(t *testing.T) {
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	// Restore the cwd before TempDir's RemoveAll runs (LIFO ordering): Windows
	// cannot delete a directory that is still the process working directory.
	t.Cleanup(func() { _ = os.Chdir(orig) })

	// No --dir is no override, and must not move anything.
	if got, err := enterWorkspace(""); got != "" || err != nil {
		t.Fatalf(`enterWorkspace("") = %q, %v; want "", nil`, got, err)
	}
	if cwd, _ := os.Getwd(); cwd != orig {
		t.Fatalf(`enterWorkspace("") moved cwd to %q`, cwd)
	}

	project := testenv.TempDir(t)
	got, err := enterWorkspace(project)
	if err != nil {
		t.Fatalf("enterWorkspace(%q) = %v", project, err)
	}
	// The answer is absolute, and it is the directory that was entered — the
	// two facts a caller hands on as the workspace root.
	if !filepath.IsAbs(got) {
		t.Fatalf("enterWorkspace(%q) = %q, want an absolute path", project, got)
	}
	if cwd := mustGetwd(t); !sameDir(cwd, got) {
		t.Fatalf("cwd = %q after entering %q", cwd, got)
	}
	if !sameDir(got, project) {
		t.Fatalf("enterWorkspace(%q) = %q, want the directory itself", project, got)
	}
}

// A relative --dir is resolved against the directory the process started in,
// not against the one it just changed into. Resolving it twice lands somewhere
// nobody named, which is the failure this ordering exists to prevent.
func TestEnterWorkspaceResolvesARelativeDirOnce(t *testing.T) {
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })

	base := testenv.TempDir(t)
	project := filepath.Join(base, "project")
	if err := os.Mkdir(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(base)

	got, err := enterWorkspace("project")
	if err != nil {
		t.Fatalf(`enterWorkspace("project") = %v`, err)
	}
	if !sameDir(got, project) {
		t.Fatalf(`enterWorkspace("project") = %q, want %q`, got, project)
	}
	if cwd := mustGetwd(t); !sameDir(cwd, project) {
		t.Fatalf("cwd = %q, want %q", cwd, project)
	}
}

// A directory that cannot be entered is reported, not silently replaced by the
// current one: the caller decides, and the two are different places to work.
func TestEnterWorkspaceRefusesWhatItCannotEnter(t *testing.T) {
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })

	missing := filepath.Join(testenv.TempDir(t), "does-not-exist")
	got, err := enterWorkspace(missing)
	if err == nil {
		t.Fatalf("enterWorkspace(%q) = %q, want an error", missing, got)
	}
	if cwd := mustGetwd(t); !sameDir(cwd, orig) {
		t.Fatalf("a refused --dir moved cwd to %q", cwd)
	}
}
