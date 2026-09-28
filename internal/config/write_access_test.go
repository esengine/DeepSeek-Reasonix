package config

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// A write-access grant is the user's, so it lands under their home and never
// in the checkout, where the project file could not have granted it anyway.
func TestPersistWorkspaceWriteAccessRecordsBothGrants(t *testing.T) {
	home, root, extra := t.TempDir(), t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "reasonix.toml"), []byte("# keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := PersistWorkspaceWriteAccess(home, root, []string{extra}, "Edit"); err != nil {
		t.Fatal(err)
	}
	grant, err := NewProjectGrantStore(home).Grant(root)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(grant.Allow, []string{"Edit"}) || len(grant.AllowWrite) != 1 {
		t.Fatalf("grant = %+v, want the rule and the directory", grant)
	}
	if raw, _ := os.ReadFile(filepath.Join(root, "reasonix.toml")); string(raw) != "# keep\n" {
		t.Fatalf("the checkout's reasonix.toml changed: %q", raw)
	}
}

func TestPersistWorkspaceWriteAccessDoesNotDuplicateAncestor(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	parent := filepath.Join(t.TempDir(), "home")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := PersistWorkspaceWriteAccess(home, root, []string{parent}, ""); err != nil {
		t.Fatal(err)
	}
	if err := PersistWorkspaceWriteAccess(home, root, []string{filepath.Join(parent, "bin")}, ""); err != nil {
		t.Fatal(err)
	}
	grant, err := NewProjectGrantStore(home).Grant(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(grant.AllowWrite) != 1 {
		t.Fatalf("allow_write = %v, want the ancestor alone", grant.AllowWrite)
	}
}
