package skill

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
)

func cacheStore(t *testing.T) (*Store, string, *time.Time) {
	t.Helper()
	home := testenv.TempDir(t)
	s := New(Options{HomeDir: home, ReasonixHomeDir: home, DisableBuiltins: true})
	now := time.Unix(1000, 0)
	s.scans.now = func() time.Time { return now }
	return s, home, &now
}

func names(s *Store) map[string]bool {
	out := map[string]bool{}
	for _, sk := range s.SlashList() {
		out[sk.Name] = true
	}
	return out
}

func TestRepeatedListingReadsTheDiskOncePerWindow(t *testing.T) {
	s, _, now := cacheStore(t)
	if _, err := s.CreateWithContent("alpha", ScopeGlobal, "---\nname: alpha\ndescription: a\n---\nbody\n"); err != nil {
		t.Fatal(err)
	}
	if !names(s)["alpha"] {
		t.Fatalf("alpha missing: %v", names(s))
	}
	path := filepath.Join(s.globalSkillsRoot(), "beta", SkillFile)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("---\nname: beta\ndescription: b\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if names(s)["beta"] {
		t.Fatal("a file added behind the store's back showed up inside the window: the disk was read again")
	}
	*now = now.Add(scanTTL)
	if !names(s)["beta"] {
		t.Fatal("the outside edit never appeared after the window closed")
	}
}

func TestStoreWritersShowUpImmediately(t *testing.T) {
	s, _, _ := cacheStore(t)
	names(s)
	if _, err := s.CreateWithContent("gamma", ScopeGlobal, "---\nname: gamma\ndescription: g\n---\nbody\n"); err != nil {
		t.Fatal(err)
	}
	if !names(s)["gamma"] {
		t.Fatal("a created skill is missing from the next listing")
	}
	if err := s.Delete("gamma", ScopeGlobal); err != nil {
		t.Fatal(err)
	}
	if names(s)["gamma"] {
		t.Fatal("a deleted skill is still listed")
	}
}
