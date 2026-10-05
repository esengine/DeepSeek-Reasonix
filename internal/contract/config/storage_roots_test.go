package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

func writeStorageSection(t *testing.T, home, body string) {
	t.Helper()
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	InvalidateStorageDirs()
	t.Cleanup(InvalidateStorageDirs)
}

// The chain is the point: a variable outranks the configuration, the
// configuration outranks the default, and every root runs the same order. A
// root that resolved by its own rules is how the old resolvers drifted apart.
func TestRootChainPrefersEnvironmentThenConfigurationThenDefault(t *testing.T) {
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("REASONIX_STATE_HOME", "")
	t.Setenv("REASONIX_CACHE_HOME", "")

	if got := RootDir(RootState); got != home {
		t.Fatalf("default state = %q, want the home root %q", got, home)
	}

	configured := filepath.Join(testenv.TempDir(t), "moved-state")
	writeStorageSection(t, home, "[storage]\nstate = "+quote(configured)+"\n")
	if got := RootDir(RootState); got != configured {
		t.Fatalf("configured state = %q, want %q", got, configured)
	}

	pinned := testenv.TempDir(t)
	t.Setenv("REASONIX_STATE_HOME", pinned)
	if got := RootDir(RootState); got != pinned {
		t.Fatalf("state = %q, want the environment to outrank configuration (%q)", got, pinned)
	}
}

// Immovable is a contract with a reason behind it, so it is enforced rather
// than documented: two isolated runtimes opening one workspace have to find the
// same lock, and nothing may name the location of the file that names locations.
func TestConfigurationCannotMoveAnImmovableRoot(t *testing.T) {
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	elsewhere := testenv.TempDir(t)
	writeStorageSection(t, home,
		"[storage]\nhome = "+quote(elsewhere)+"\nlocks = "+quote(elsewhere)+"\n")

	if got := RootDir(RootHome); got != home {
		t.Fatalf("home = %q, want it to stay at %q", got, home)
	}
	if got := RootDir(RootLocks); got == elsewhere {
		t.Fatalf("locks followed the configuration to %q", got)
	}
	for _, id := range []RootID{RootHome, RootLocks} {
		if RootRelocatable(id) {
			t.Fatalf("%s reports itself relocatable", id)
		}
	}
	for _, id := range []RootID{RootState, RootCache, RootWorktrees} {
		if !RootRelocatable(id) {
			t.Fatalf("%s reports itself immovable", id)
		}
	}
}

// A surface offering to move a root has to say when the environment already
// holds it, instead of accepting an edit that resolution would ignore.
func TestPinnedRootNamesTheVariableHoldingIt(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	t.Setenv("REASONIX_STATE_HOME", "")
	if got := RootPinnedBy(RootState); got != "" {
		t.Fatalf("unpinned state reported %q", got)
	}
	t.Setenv("REASONIX_STATE_HOME", testenv.TempDir(t))
	if got := RootPinnedBy(RootState); got != "REASONIX_STATE_HOME" {
		t.Fatalf("pinned state reported %q", got)
	}
	if got := RootPinnedBy(RootWorktrees); got != "" {
		t.Fatalf("worktrees have no variable of their own, got %q", got)
	}
}

// Both layers accept the same spellings, or a location would mean one thing in
// the environment and another in the file.
func TestConfiguredRootExpandsLikeAnEnvironmentOverride(t *testing.T) {
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("REASONIX_CACHE_HOME", "")
	target := testenv.TempDir(t)
	t.Setenv("STORAGE_TEST_TARGET", target)

	writeStorageSection(t, home, "[storage]\ncache = \"${STORAGE_TEST_TARGET}/derived\"\n")
	want := filepath.Join(target, "derived")
	if got := RootDir(RootCache); got != want {
		t.Fatalf("cache = %q, want the expanded %q", got, want)
	}
}

// A broken config must not take the paths down with it: a user whose file
// cannot be parsed still has to reach the tooling that repairs it.
func TestUnreadableStorageSectionFallsBackInsteadOfFailing(t *testing.T) {
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("REASONIX_STATE_HOME", "")
	writeStorageSection(t, home, "[storage\nstate = broken")

	if got := RootDir(RootState); got != home {
		t.Fatalf("state = %q, want the default %q despite the broken file", got, home)
	}
}

func quote(s string) string {
	out := make([]rune, 0, len(s)+2)
	out = append(out, '"')
	for _, r := range s {
		if r == '\\' || r == '"' {
			out = append(out, '\\')
		}
		out = append(out, r)
	}
	return string(append(out, '"'))
}

// A relocation has to survive the save: the renderer rewrites the whole file
// from the struct, so a section it has no line for is silently dropped.
func TestStorageSectionSurvivesASaveAndReload(t *testing.T) {
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("REASONIX_STATE_HOME", "")
	t.Setenv("REASONIX_CACHE_HOME", "")
	t.Cleanup(InvalidateStorageDirs)

	target := filepath.Join(testenv.TempDir(t), "moved")
	c := Default()
	if err := c.SetStorageDir(RootState, target); err != nil {
		t.Fatalf("set storage: %v", err)
	}
	path := filepath.Join(home, "config.toml")
	if err := c.SaveTo(path); err != nil {
		t.Fatalf("save: %v", err)
	}

	InvalidateStorageDirs()
	if got := RootDir(RootState); got != target {
		t.Fatalf("state resolved to %q after save, want %q", got, target)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "[storage]") {
		t.Fatalf("saved config has no [storage] section:\n%s", body)
	}
}

// Clearing a root returns it to its default rather than writing an empty
// location nothing can resolve.
func TestClearingAStorageDirRestoresTheDefault(t *testing.T) {
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("REASONIX_STATE_HOME", "")
	t.Cleanup(InvalidateStorageDirs)

	c := Default()
	if err := c.SetStorageDir(RootState, filepath.Join(testenv.TempDir(t), "moved")); err != nil {
		t.Fatal(err)
	}
	if err := c.SetStorageDir(RootState, ""); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if got := c.StorageDir(RootState); got != "" {
		t.Fatalf("cleared root still configured as %q", got)
	}
	if err := c.SaveTo(filepath.Join(home, "config.toml")); err != nil {
		t.Fatal(err)
	}
	InvalidateStorageDirs()
	if got := RootDir(RootState); got != home {
		t.Fatalf("state = %q after clearing, want the default %q", got, home)
	}
}

// The setter rules on what configuration can rule on, and refuses the rest
// with a reason rather than writing a line resolution would ignore.
func TestSetStorageDirRefusesWhatItCannotHonour(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	c := Default()
	if err := c.SetStorageDir(RootLocks, testenv.TempDir(t)); err == nil {
		t.Fatal("locks accepted a relocation")
	}
	if err := c.SetStorageDir(RootHome, testenv.TempDir(t)); err == nil {
		t.Fatal("home accepted a relocation")
	}
	if err := c.SetStorageDir(RootID("nowhere"), testenv.TempDir(t)); err == nil {
		t.Fatal("an undeclared root accepted a relocation")
	}
	t.Setenv("REASONIX_CACHE_HOME", testenv.TempDir(t))
	if err := c.SetStorageDir(RootCache, testenv.TempDir(t)); err == nil {
		t.Fatal("a root the environment pins accepted a relocation the environment would override")
	}
}

// A project checkout must not be able to redirect where this machine keeps its
// data — the same rule [secrets] and [remote] carry.
func TestProjectConfigCannotRelocateAnything(t *testing.T) {
	c := Default()
	c.Storage = map[string]string{string(RootState): "/from/the/repo"}
	if body := RenderTOMLForScope(c, RenderScopeProject); strings.Contains(body, "[storage]") {
		t.Fatalf("project scope rendered a storage section:\n%s", body)
	}
}

// The defaults are a compatibility contract, not a preference: a root that
// resolves one directory deeper than it used to orphans everything the last
// release wrote there. Pinned here so a refactor of the chain cannot move them
// quietly — this caught the cache picking up a "cache" suffix it never had.
func TestDefaultRootDirectoriesAreWhereTheyAlwaysWere(t *testing.T) {
	for _, name := range []string{"REASONIX_HOME", "REASONIX_STATE_HOME", "REASONIX_CACHE_HOME"} {
		t.Setenv(name, "")
	}
	t.Cleanup(InvalidateStorageDirs)
	InvalidateStorageDirs()

	cacheBase := osUserCacheDir()
	if cacheBase == "" {
		t.Skip("no OS cache directory on this machine")
	}
	if got, want := RootDir(RootCache), filepath.Join(cacheBase, "reasonix"); got != want {
		t.Errorf("cache = %q, want %q", got, want)
	}
	if got, want := RootDir(RootLocks), filepath.Join(cacheBase, "reasonix"); got != want {
		t.Errorf("locks = %q, want %q", got, want)
	}
	if got, want := RootDir(RootState), RootDir(RootHome); got != want {
		t.Errorf("state = %q, want the home root %q", got, want)
	}
	// A pinned home is the one case that takes the cache along, one level down.
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	if got, want := RootDir(RootCache), filepath.Join(home, "cache"); got != want {
		t.Errorf("cache under a pinned home = %q, want %q", got, want)
	}
}

// The locks root shares the OS cache directory with the cache root, so it owns
// only its own two subdirectories — otherwise a size report counts the whole
// cache twice and a reader sees storage that is not there.
func TestLocksRootOwnsOnlyItsLockDirectories(t *testing.T) {
	owned := RootOwns(RootLocks)
	if len(owned) != 2 {
		t.Fatalf("locks owns %v, want its two lock directories", owned)
	}
	base := RootDir(RootLocks)
	for _, dir := range []string{WorkspaceLeaseDir(), RepairMutationLockDir()} {
		rel, err := filepath.Rel(base, dir)
		if err != nil || !slices.Contains(owned, rel) {
			t.Fatalf("%q is not among the declared entries %v", dir, owned)
		}
	}
}
