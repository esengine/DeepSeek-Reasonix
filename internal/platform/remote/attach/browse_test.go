package attach

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/platform/remote"
	"reasonix/internal/platform/remote/sftpfs"
)

// sameFarPath reports whether two spellings name one directory. A temp dir is
// reached through a symlink on macOS, and whether the far side answers with the
// spelling it was handed or with what that resolves to is its business — the
// subject here is which directory it named, so both sides are resolved before
// they are compared.
func sameFarPath(got, want string) bool {
	resolve := func(p string) string {
		if r, err := filepath.EvalSymlinks(filepath.FromSlash(p)); err == nil {
			return filepath.ToSlash(r)
		}
		return filepath.ToSlash(p)
	}
	return resolve(got) == resolve(want)
}

func names(l Listing) []string {
	out := make([]string, 0, len(l.Folders))
	for _, f := range l.Folders {
		out = append(out, f.Name)
	}
	return out
}

// The point of browsing over the file layer rather than over a pane: nothing is
// installed and no kernel is started, so a folder can be chosen on a machine
// this build has never run on.
func TestBrowseListsFoldersWithoutStartingAKernel(t *testing.T) {
	m := fakeMachine(t)
	p := testPool(t, m)
	m.workspace(t, "training")
	m.workspace(t, "eval")
	if err := os.WriteFile(filepath.Join(m.home, "notes.md"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := p.Browse(t.Context(), "box", "")
	if err != nil {
		t.Fatalf("browse: %v", err)
	}
	if want := filepath.ToSlash(m.home); !sameFarPath(got.Path, want) {
		t.Fatalf("listed %q, want the login home %q", got.Path, want)
	}
	if !slices.Equal(names(got), []string{"eval", "training"}) {
		t.Fatalf("folders = %v, want the two directories in name order", names(got))
	}
	if n := m.launches(); n != 0 {
		t.Fatalf("browsing started %d remote kernels, want none", n)
	}
}

// Stepping into a folder is a second call. A link released at the answer would
// re-dial for it — and on a passphrase-protected key, ask for it again.
func TestBrowsingTwiceReusesTheOneConnection(t *testing.T) {
	m := fakeMachine(t)
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	var dials atomic.Int32
	p := NewPool(t.Context(), Options{Dial: func(host string, prompts Prompts) (*remote.Client, error) {
		dials.Add(1)
		return m.dial(host, prompts)
	}})
	defer p.Close()
	m.workspace(t, "training")

	home, err := p.Browse(t.Context(), "box", "")
	if err != nil {
		t.Fatalf("browse home: %v", err)
	}
	if len(home.Folders) != 1 {
		t.Fatalf("home listed %v, want the one folder", names(home))
	}
	if _, err := p.Browse(t.Context(), "box", home.Folders[0].Path); err != nil {
		t.Fatalf("browse into the folder: %v", err)
	}
	if n := dials.Load(); n != 1 {
		t.Fatalf("the picker dialed %d times for two steps, want 1", n)
	}
}

// The link a picker held is not a link a pane holds: nothing keeps it once the
// window has moved on, or a machine looked at once would stay logged in.
func TestABrowsedConnectionIsReleasedOnItsOwn(t *testing.T) {
	m := fakeMachine(t)
	p := testPool(t, m)
	if _, err := p.Browse(t.Context(), "box", ""); err != nil {
		t.Fatalf("browse: %v", err)
	}
	p.mu.Lock()
	l := p.links["box"]
	p.mu.Unlock()
	if l == nil {
		t.Fatal("the connection was dropped at the answer, so the next step re-dials")
	}
	if l.refs != 1 {
		t.Fatalf("the picker holds %d references, want the one it releases on a timer", l.refs)
	}
}

// Walking up is the far machine's answer, not this one's string surgery: a
// Windows host answers a mac with a drive letter no filepath here can cut.
func TestBrowseCarriesTheParentToWalkUpThrough(t *testing.T) {
	m := fakeMachine(t)
	p := testPool(t, m)
	inner := m.workspace(t, filepath.Join("training", "runs"))

	got, err := p.Browse(t.Context(), "box", inner)
	if err != nil {
		t.Fatalf("browse: %v", err)
	}
	if want := filepath.ToSlash(filepath.Join(m.home, "training")); !sameFarPath(got.Parent, want) {
		t.Fatalf("parent = %q, want %q", got.Parent, want)
	}

	root, err := p.Browse(t.Context(), "box", "/")
	if err != nil {
		t.Fatalf("browse root: %v", err)
	}
	if root.Parent != "" {
		t.Fatalf("the root reported %q above it", root.Parent)
	}
}

func TestBrowseReportsAPathTheFarMachineDoesNotHave(t *testing.T) {
	m := fakeMachine(t)
	p := testPool(t, m)
	if _, err := p.Browse(t.Context(), "box", "/no/such/folder"); err == nil {
		t.Fatal("a missing folder listed as if it were there")
	}
}

func TestBrowseNeedsAHost(t *testing.T) {
	p := NewPool(context.Background(), Options{})
	defer p.Close()
	if _, err := p.Browse(context.Background(), "  ", ""); err == nil {
		t.Fatal("browsing nowhere reported success")
	}
}

// The shaping, without a machine: it runs where fakeMachine cannot.
func TestListingOfKeepsFoldersAndSaysWhenItStopped(t *testing.T) {
	dir := func(name string) sftpfs.Entry {
		return sftpfs.Entry{Name: name, Path: "/home/ada/" + name, IsDir: true, Mode: fs.ModeDir}
	}
	got := listingOf("/home/ada", []sftpfs.Entry{
		dir("train"),
		{Name: "notes.md", Path: "/home/ada/notes.md"},
		dir("Eval"),
		dir(".config"),
	})
	if !slices.Equal(names(got), []string{".config", "Eval", "train"}) {
		t.Fatalf("folders = %v, want the three directories in name order", names(got))
	}
	if got.Parent != "/home" {
		t.Fatalf("parent = %q, want /home", got.Parent)
	}
	if got.Truncated {
		t.Fatal("a listing that fit reported that it was cut")
	}

	var many []sftpfs.Entry
	for i := range browseCap + 10 {
		many = append(many, dir(string(rune('a'+i%26))+string(rune('a'+i/26))))
	}
	big := listingOf("/home/ada", many)
	if len(big.Folders) != browseCap || !big.Truncated {
		t.Fatalf("a folder of %d listed %d entries (truncated=%v), want %d and a say-so",
			len(many), len(big.Folders), big.Truncated, browseCap)
	}
}
