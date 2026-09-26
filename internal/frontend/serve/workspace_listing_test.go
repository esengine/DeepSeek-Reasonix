package serve

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"reasonix/internal/base/testenv"
)

func listingTree(t *testing.T) string {
	t.Helper()
	root := testenv.TempDir(t)
	for _, dir := range []string{"alpha/inner", "zeta", ".git", "node_modules/pkg", "vendor/mod", ".cache"} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(dir)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, file := range []string{"alpha/a.txt", "zeta/z.txt", "top.md", ".env", "node_modules/pkg/i.js"} {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(file)), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// lockDir makes dir unreadable for the rest of the test, or skips where the
// platform or the user makes that impossible to arrange with a mode bit.
func lockDir(t *testing.T, dir string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("a mode bit does not deny listing a directory on Windows")
	}
	if err := os.Chmod(dir, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	if _, err := os.ReadDir(dir); err == nil {
		t.Skip("this user can read a mode-0 directory")
	}
}

func TestListingAFolderReadsThatFolder(t *testing.T) {
	root := listingTree(t)
	top, err := listFolder(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(top.Directories, []string{"alpha", "zeta"}) || !slices.Equal(top.Files, []string{"top.md"}) {
		t.Fatalf("root = %+v, want alpha and zeta beside top.md, with dot entries and dependency trees left out", top)
	}
	alpha, err := listFolder(root, "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(alpha.Directories, []string{"alpha/inner"}) || !slices.Equal(alpha.Files, []string{"alpha/a.txt"}) {
		t.Fatalf("alpha = %+v", alpha)
	}
}

// Opening one folder must not depend on every other folder being readable:
// the walk it replaced failed every expansion over one locked sibling.
func TestAnUnreadableSiblingDoesNotStopAFolderOpening(t *testing.T) {
	root := listingTree(t)
	locked := filepath.Join(root, "locked")
	if err := os.Mkdir(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	lockDir(t, locked)
	got, err := listFolder(root, "zeta")
	if err != nil {
		t.Fatalf("opening zeta beside an unreadable folder: %v", err)
	}
	if !slices.Equal(got.Files, []string{"zeta/z.txt"}) {
		t.Fatalf("zeta = %+v", got)
	}
}

func TestASearchPassesOverAnUnreadableFolder(t *testing.T) {
	root := listingTree(t)
	locked := filepath.Join(root, "locked")
	if err := os.Mkdir(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	lockDir(t, locked)
	got, err := searchFiles(root, ".txt")
	if err != nil {
		t.Fatalf("search beside an unreadable folder: %v", err)
	}
	if !slices.Equal(got.Files, []string{"alpha/a.txt", "zeta/z.txt"}) {
		t.Fatalf("search = %+v", got.Files)
	}
}

func TestAMissingFolderIsNotFound(t *testing.T) {
	if _, err := listFolder(listingTree(t), "gone"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v, want not-exist", err)
	}
}

// A link inside the tree that points out of it is not a folder of the
// workspace, whatever its name says.
func TestAFolderLinkedOutOfTheTreeIsRefused(t *testing.T) {
	root := listingTree(t)
	outside := testenv.TempDir(t)
	if err := os.WriteFile(filepath.Join(outside, "private.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Skipf("cannot create a directory link here: %v", err)
	}
	if _, err := listFolder(root, "escape"); !errors.Is(err, errListingOutsideTree) {
		t.Fatalf("err = %v, want the folder refused as outside the workspace", err)
	}
}

// A link is listed as what it resolves to: a linked folder opens like any
// other folder, and a link the explorer could never open is not offered.
func TestALinkIsListedAsWhatItResolvesTo(t *testing.T) {
	root := listingTree(t)
	outside := testenv.TempDir(t)
	links := map[string]string{
		"folder-link": filepath.Join(root, "alpha"),
		"file-link":   filepath.Join(root, "top.md"),
		"escape":      outside,
		"dangling":    filepath.Join(root, "gone"),
	}
	for name, target := range links {
		if err := os.Symlink(target, filepath.Join(root, name)); err != nil {
			t.Skipf("cannot create a link here: %v", err)
		}
	}
	top, err := listFolder(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(top.Directories, []string{"alpha", "folder-link", "zeta"}) {
		t.Fatalf("directories = %v, want the linked folder beside alpha and zeta", top.Directories)
	}
	if !slices.Equal(top.Files, []string{"file-link", "top.md"}) {
		t.Fatalf("files = %v, want the linked file beside top.md and no link that leaves the tree or dangles", top.Files)
	}
	inner, err := listFolder(root, "folder-link")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(inner.Directories, []string{"folder-link/inner"}) || !slices.Equal(inner.Files, []string{"folder-link/a.txt"}) {
		t.Fatalf("folder-link = %+v", inner)
	}
}

// A search lists files, so a linked folder is not one of its hits, and a link
// leaving the tree is passed over as the explorer passes it over.
func TestASearchListsOnlyLinksToFilesInTheTree(t *testing.T) {
	root := listingTree(t)
	outside := testenv.TempDir(t)
	if err := os.WriteFile(filepath.Join(outside, "far.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	links := map[string]string{
		"txt-folder": filepath.Join(root, "alpha"),
		"near.txt":   filepath.Join(root, "top.md"),
		"far.txt":    filepath.Join(outside, "far.txt"),
	}
	for name, target := range links {
		if err := os.Symlink(target, filepath.Join(root, name)); err != nil {
			t.Skipf("cannot create a link here: %v", err)
		}
	}
	got, err := searchFiles(root, "txt")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got.Files, []string{"alpha/a.txt", "near.txt", "zeta/z.txt"}) {
		t.Fatalf("search = %v", got.Files)
	}
}
