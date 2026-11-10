package agent

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestResolveHopsFollowsDanglingLinksAndBoundsTheChain(t *testing.T) {
	root, err := filepath.EvalSymlinks(testenv.TempDir(t))
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "sub", "Missing.cs")
	link := func(name, to string) string {
		p := filepath.Join(root, name)
		if err := os.Symlink(to, p); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		return p
	}
	one := link("one", target)
	two := link("two", "one")
	if got, err := resolveThroughExisting(one); err != nil || got != target {
		t.Errorf("dangling link = (%q, %v), want %q", got, err, target)
	}
	if got, err := resolveThroughExisting(two); err != nil || got != target {
		t.Errorf("link to a dangling link = (%q, %v), want %q", got, err, target)
	}
	loop := link("loop", "loop")
	if got, err := resolveThroughExisting(loop); err == nil {
		t.Errorf("a link loop resolved to %q, want an error so the extent is recorded unestablished", got)
	}
}
