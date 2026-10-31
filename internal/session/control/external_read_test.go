package control

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
)

// A path outside the workspace is read only where a grant covers it. Naming a
// path is not a grant: that is what keeps a pasted path from being a way to
// read the disk.
func TestReadableExternalPathAnswersOnlyWhereAGrantCoversIt(t *testing.T) {
	workspace := testenv.TempDir(t)
	brought := testenv.TempDir(t)
	elsewhere := testenv.TempDir(t)
	for dir, name := range map[string]string{brought: "moon.png", elsewhere: "secret.png"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("png"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	c := &Controller{controllerDeps: controllerDeps{workspaceRoot: workspace}}

	// The workspace is not an external path: its own reader answers it.
	if _, _, ok := c.ReadableExternalPath(filepath.Join(workspace, "a.png")); ok {
		t.Fatal("a workspace file is not an external path")
	}
	// A path no grant covers is not readable at all.
	if _, _, ok := c.ReadableExternalPath(filepath.Join(elsewhere, "secret.png")); ok {
		t.Fatal("a path no grant covers must not be readable")
	}
	// A relative path names nothing outside the tree.
	if _, _, ok := c.ReadableExternalPath("elsewhere/secret.png"); ok {
		t.Fatal("a relative path is not an external path")
	}

	// The picture the person brought into this session is readable — that is
	// what bringing it in is for.
	if _, _, err := c.DroppedRef(filepath.Join(brought, "moon.png")); err != nil {
		t.Fatal(err)
	}
	root, rel, ok := c.ReadableExternalPath(filepath.Join(brought, "moon.png"))
	if !ok {
		t.Fatal("the picture the session was given must be readable")
	}
	if filepath.Base(root) != filepath.Base(brought) || rel != "moon.png" {
		t.Fatalf("readable at (%q, %q), want the folder it is in and its name", root, rel)
	}
	// The grant is the file that was brought in, not the folder around it.
	if _, _, ok := c.ReadableExternalPath(filepath.Join(brought, "other.png")); ok {
		t.Fatal("the grant is the picture that was brought in, not its folder")
	}
	// A path that leaves the granted place is not under it.
	if _, _, ok := c.ReadableExternalPath(filepath.Join(brought, "..", "elsewhere", "secret.png")); ok {
		t.Fatal("a path climbing out of the granted place must not be readable")
	}
}
