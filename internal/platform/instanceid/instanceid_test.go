package instanceid

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstanceIDScopesToTheDataHome(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()
	t.Setenv("REASONIX_HOME", first)
	want := Current()
	t.Setenv("REASONIX_HOME", filepath.Join(first, "."))
	if got := Current(); got != want {
		t.Fatalf("one data home spelled two ways produced %q and %q", got, want)
	}
	t.Setenv("REASONIX_HOME", second)
	if got := Current(); got == want {
		t.Fatalf("two data homes share id %q, so the second launch would be refused a window", got)
	}
}

// A home that has not been created yet is the first launch, and it must hash to
// the same place the second one will find.
func TestInstanceIDResolvesAMissingHomeThroughASymlink(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(real, alias); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	t.Setenv("REASONIX_HOME", filepath.Join(real, "not-created", "home"))
	want := Current()
	t.Setenv("REASONIX_HOME", filepath.Join(alias, "not-created", "home"))
	if got := Current(); got != want {
		t.Fatalf("the same home reached through a symlink produced %q and %q", got, want)
	}
}
