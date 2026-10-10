package migration

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/state/workspacelist"
)

func seed(t *testing.T, dir, name, file string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name, file), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// defaultRoots puts the OS defaults in a temp tree and clears both redirects,
// so what runs is the path a real install takes.
func defaultRoots(t *testing.T) string {
	t.Helper()
	base := testenv.TempDir(t)
	t.Setenv("HOME", base)
	t.Setenv("USERPROFILE", base)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(base, ".config"))
	t.Setenv("AppData", filepath.Join(base, "AppData"))
	t.Setenv("REASONIX_HOME", "")
	t.Setenv("REASONIX_STATE_HOME", "")
	return base
}

// A run that redirects its roots by environment is a scratch run; it must not
// pull the production install across, which is the rule the legacy importers
// already follow.
func TestAdoptSkipsWhenTheRootsAreRedirectedForThisRun(t *testing.T) {
	base := testenv.TempDir(t)
	home := filepath.Join(base, "home")
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("REASONIX_STATE_HOME", filepath.Join(base, "state"))
	seed(t, home, "appearance", "wp.png")
	if got := AdoptRelocatedStateEntries(event.Discard); len(got) != 0 {
		t.Fatalf("a redirected run adopted %v", got)
	}
}

// The one this exists for: appearance, themes and repair were written under the
// state root before it listed them, so a relocation moved the rest and left
// these where they were. Relocation is a config table, not an environment
// variable — the environment path is the isolated run the guard above refuses.
func TestAdoptBringsAcrossWhatTheOldMoveLeft(t *testing.T) {
	base := defaultRoots(t)
	home := config.ReasonixHomeDir()
	if home == "" {
		t.Skip("no default home on this platform")
	}
	state := filepath.Join(base, "moved-state")
	if err := os.MkdirAll(state, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := "[storage]\nstate = " + strconv.Quote(state) + "\n"
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	config.InvalidateStorageDirs()
	if got := config.MemoryUserDir(); got != state {
		t.Fatalf("relocation did not take: state root is %q, want %q", got, state)
	}

	seed(t, home, "appearance", "wp.png")
	seed(t, home, "themes", "pack.json")
	seed(t, home, "repair", "backup.toml")
	seed(t, home, "sessions", "old.jsonl")
	if err := os.WriteFile(filepath.Join(home, "serve-workspaces.json"), []byte(`{"paths":["/w"]}`), 0o600); err != nil {
		t.Fatal(err)
	}

	adopted := AdoptRelocatedStateEntries(event.Discard)
	if len(adopted) != 4 {
		t.Fatalf("adopted %v, want appearance, themes, repair and the project list", adopted)
	}
	if _, err := os.Stat(filepath.Join(state, "serve-workspaces.json")); err != nil {
		t.Errorf("project list was left behind: %v", err)
	}
	for _, name := range []string{"appearance", "themes", "repair"} {
		if !isDir(filepath.Join(state, name)) {
			t.Errorf("%s was left behind", name)
		}
	}
	// sessions was always owned, so the move already took it; bringing it
	// across would resurrect history the user has deleted since.
	if isDir(filepath.Join(state, "sessions")) {
		t.Error("sessions was adopted; the move already owned it")
	}
}

// relocated stands up a default install whose state root was moved by config,
// the case boot repairs. It returns the previous (home) root and the new one.
func relocated(t *testing.T) (home, state string) {
	t.Helper()
	base := defaultRoots(t)
	home = config.ReasonixHomeDir()
	if home == "" {
		t.Skip("no default home on this platform")
	}
	state = filepath.Join(base, "moved-state")
	for _, dir := range []string{home, state} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cfg := "[storage]\nstate = " + strconv.Quote(state) + "\n"
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	config.InvalidateStorageDirs()
	if config.MemoryUserDir() != state {
		t.Fatalf("relocation did not take: %q", config.MemoryUserDir())
	}
	return home, state
}

func put(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// A directory the new root already has is merged file by file: what it lacks
// comes across, what it holds stays, and the old one is set aside so the merge
// is not repeated.
func TestAdoptMergesIntoADirectoryTheNewRootAlreadyHas(t *testing.T) {
	home, state := relocated(t)
	put(t, filepath.Join(home, "memory", "global", "old.md"), "old")
	put(t, filepath.Join(home, "memory", "global", "shared.md"), "from old")
	put(t, filepath.Join(state, "memory", "global", "shared.md"), "from new")

	AdoptRelocatedStateEntries(event.Discard)
	if got := read(t, filepath.Join(state, "memory", "global", "old.md")); got != "old" {
		t.Errorf("old memory file not merged: %q", got)
	}
	if got := read(t, filepath.Join(state, "memory", "global", "shared.md")); got != "from new" {
		t.Errorf("the new root's file was overwritten: %q", got)
	}
	if exists(filepath.Join(home, "memory")) || !exists(filepath.Join(home, "memory.adopted")) {
		t.Error("the merged directory was not set aside")
	}
	if got := AdoptRelocatedStateEntries(event.Discard); len(got) != 0 {
		t.Errorf("a second run adopted %v", got)
	}
}

// Standing instructions and the other loose files move too; a file the new
// root already has is its own and stays, and the old one is not renamed over it.
func TestAdoptBringsAcrossLooseFilesWithoutOverwriting(t *testing.T) {
	home, state := relocated(t)
	put(t, filepath.Join(home, "AGENTS.md"), "standing rules")
	put(t, filepath.Join(home, "REASONIX.md"), "old reasonix")
	put(t, filepath.Join(state, "REASONIX.md"), "new reasonix")
	put(t, filepath.Join(home, "machine-id.key"), "key")
	put(t, filepath.Join(home, "schedules", "a.json"), "{}")
	put(t, filepath.Join(state, "schedules", "b.json"), "{}")

	AdoptRelocatedStateEntries(event.Discard)
	if got := read(t, filepath.Join(state, "AGENTS.md")); got != "standing rules" {
		t.Errorf("AGENTS.md = %q", got)
	}
	if got := read(t, filepath.Join(state, "REASONIX.md")); got != "new reasonix" {
		t.Errorf("REASONIX.md was overwritten: %q", got)
	}
	if !exists(filepath.Join(home, "REASONIX.md")) {
		t.Error("an old file that lost to the new one was renamed away")
	}
	if !exists(filepath.Join(state, "machine-id.key")) || !exists(filepath.Join(state, "schedules", "a.json")) || !exists(filepath.Join(state, "schedules", "b.json")) {
		t.Error("machine key or schedules not merged")
	}
}

// The list the user rebuilt in the new root keeps its order and launch project;
// projects only the old one knew are appended, once.
func TestAdoptMergesTheProjectListOnce(t *testing.T) {
	home, state := relocated(t)
	oldList := filepath.Join(home, workspacelist.FileName)
	newList := filepath.Join(state, workspacelist.FileName)
	put(t, oldList, `{"paths":["D:\\work\\app","D:\\work\\lib"],"launch":"D:\\work\\app"}`)
	put(t, newList, `{"paths":["D:\\work\\new","D:\\work\\lib"],"launch":"D:\\work\\new"}`)

	if got := AdoptRelocatedStateEntries(event.Discard); len(got) != 1 || got[0] != workspacelist.FileName {
		t.Fatalf("adopted %v", got)
	}
	list, err := workspacelist.Read(newList)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{`D:\work\new`, `D:\work\lib`, `D:\work\app`}
	if len(list.Paths) != 3 || list.Paths[0] != want[0] || list.Paths[1] != want[1] || list.Paths[2] != want[2] || list.Launch != want[0] {
		t.Fatalf("merged list = %+v, want %v launching %s", list, want, want[0])
	}
	if exists(oldList) || !exists(oldList+".adopted") {
		t.Fatal("the old list was not set aside")
	}

	// The user forgets a project; the next run must not bring it back.
	put(t, newList, `{"paths":["D:\\work\\new"]}`)
	if got := AdoptRelocatedStateEntries(event.Discard); len(got) != 0 {
		t.Fatalf("a second run adopted %v", got)
	}
	if list, _ := workspacelist.Read(newList); len(list.Paths) != 1 {
		t.Fatalf("a forgotten project came back: %v", list.Paths)
	}
}

// With the state root where it defaults to, there is no previous root: nothing
// may be renamed or copied onto itself.
func TestAdoptLeavesAnUnmovedInstallAlone(t *testing.T) {
	base := defaultRoots(t)
	_ = base
	home := config.ReasonixHomeDir()
	if home == "" {
		t.Skip("no default home on this platform")
	}
	if config.MemoryUserDir() != home {
		t.Skip("state root does not default to home here")
	}
	put(t, filepath.Join(home, "AGENTS.md"), "rules")
	put(t, filepath.Join(home, "memory", "global", "m.md"), "m")
	put(t, filepath.Join(home, workspacelist.FileName), `{"paths":["/w"]}`)
	if got := AdoptRelocatedStateEntries(event.Discard); len(got) != 0 {
		t.Fatalf("adopted %v on an unmoved install", got)
	}
	for _, name := range []string{"AGENTS.md", "memory", workspacelist.FileName} {
		if !exists(filepath.Join(home, name)) {
			t.Errorf("%s was set aside on an unmoved install", name)
		}
	}
}

// An entry that is a link points at data this run does not own, such as a
// dotfiles checkout. Copying nothing and renaming it would disable it.
func TestAdoptLeavesALinkedEntryInPlace(t *testing.T) {
	home, state := relocated(t)
	target := filepath.Join(t.TempDir(), "dotfiles-memory")
	put(t, filepath.Join(target, "global", "m.md"), "m")
	if err := os.Symlink(target, filepath.Join(home, "memory")); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	if got := AdoptRelocatedStateEntries(event.Discard); len(got) != 0 {
		t.Fatalf("adopted %v", got)
	}
	if _, err := os.Lstat(filepath.Join(home, "memory")); err != nil {
		t.Fatalf("the link was renamed away: %v", err)
	}
	if exists(filepath.Join(home, "memory.adopted")) || exists(filepath.Join(state, "memory")) {
		t.Fatal("a linked entry was adopted")
	}
}

// A directory whose files all already exist in the new root copies nothing, and
// is not empty: it stays where it is rather than being set aside unseen.
func TestAdoptKeepsANonEmptyDirectoryItCopiedNothingFrom(t *testing.T) {
	home, state := relocated(t)
	put(t, filepath.Join(home, "commands", "x.md"), "same")
	put(t, filepath.Join(state, "commands", "x.md"), "same")
	AdoptRelocatedStateEntries(event.Discard)
	if !exists(filepath.Join(home, "commands", "x.md")) || exists(filepath.Join(home, "commands.adopted")) {
		t.Fatal("a directory with nothing copied was set aside")
	}
}

func TestAdoptBringsAcrossUserCommands(t *testing.T) {
	home, state := relocated(t)
	put(t, filepath.Join(home, "commands", "deploy.md"), "d")
	AdoptRelocatedStateEntries(event.Discard)
	if got := read(t, filepath.Join(state, "commands", "deploy.md")); got != "d" {
		t.Fatalf("commands not adopted: %q", got)
	}
}
