package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/contract/config"
)

func TestUserChoseDirRejectsWhatALauncherInherits(t *testing.T) {
	base := t.TempDir()
	app := filepath.Join(base, "ReasonixStudio")
	exe := filepath.Join(app, "Reasonix Studio.exe")
	cases := []struct {
		name, cwd, exe string
		want           bool
	}{
		{"install directory", app, exe, false},
		{"below the install directory", filepath.Join(app, "resources"), exe, false},
		{"filesystem root", filepath.VolumeName(base) + string(filepath.Separator), exe, false},
		{"a project elsewhere", filepath.Join(base, "project"), exe, true},
		{"a sibling sharing the name prefix", app + "-notes", exe, true},
		{"unpackaged run keeps the directory", app, "", true},
	}
	for _, c := range cases {
		if got := userChoseDir(c.cwd, c.exe); got != c.want {
			t.Errorf("%s: userChoseDir(%q) = %v, want %v", c.name, c.cwd, got, c.want)
		}
	}
}

func TestPickLaunchWorkspaceFallbackOrder(t *testing.T) {
	base := t.TempDir()
	app := filepath.Join(base, "ReasonixStudio")
	exe := filepath.Join(app, "Reasonix Studio.exe")
	mk := func(name string) string {
		d := filepath.Join(base, name)
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		return d
	}
	a, b := mk("a"), mk("b")
	auto := filepath.Join(base, "auto")
	gone := filepath.Join(base, "gone")
	proj := mk("proj")

	cases := []struct {
		name       string
		cwd        string
		remembered []string
		want       string
	}{
		{"chosen directory wins over the list", proj, []string{a}, proj},
		{"list head", app, []string{a, b}, a},
		{"stale head skipped", app, []string{gone, b}, b},
		{"all stale falls to the auto workspace", app, []string{gone}, auto},
		{"empty list falls to the auto workspace", app, nil, auto},
	}
	for _, c := range cases {
		if got := pickLaunchWorkspace(c.cwd, exe, c.remembered, auto); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestLaunchWorkspaceKeepsLaunchProjectAfterSidebarReorder(t *testing.T) {
	base := t.TempDir()
	home := filepath.Join(base, "state")
	app := filepath.Join(base, "app")
	a, b := filepath.Join(base, "a"), filepath.Join(base, "b")
	for _, dir := range []string{home, app, a, b} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("REASONIX_HOME", home)
	t.Chdir(app)
	data, _ := json.Marshal(map[string]any{"paths": []string{b, a}, "launch": a})
	if err := os.WriteFile(filepath.Join(home, "serve-workspaces.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(app, "Reasonix Studio")
	if got := launchWorkspace(exe); got != a {
		t.Fatalf("launchWorkspace = %q, want the pre-reorder launch project %q", got, a)
	}
	if err := os.Remove(a); err != nil {
		t.Fatal(err)
	}
	if got := launchWorkspace(exe); got != b {
		t.Fatalf("missing launch project = %q, want next remembered folder %q", got, b)
	}
}

func TestLaunchWithNothingChosenOrRememberedOpensTheAutoWorkspaceNotHome(t *testing.T) {
	base := t.TempDir()
	state := filepath.Join(base, "state")
	home := filepath.Join(base, "home")
	app := filepath.Join(base, "app")
	for _, dir := range []string{state, home, app} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("REASONIX_HOME", state)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Chdir(app)
	exe := filepath.Join(app, "Reasonix Studio")

	got := launchWorkspace(exe)
	if got == home {
		t.Fatalf("a first launch opened the home directory %q", got)
	}
	if want := config.AutoWorkspaceDir(); got != want {
		t.Fatalf("launchWorkspace = %q, want the auto workspace %q", got, want)
	}
	if st, err := os.Stat(got); err != nil || !st.IsDir() {
		t.Fatalf("the auto workspace was not created: %v", err)
	}
	if rel, err := filepath.Rel(state, got); err != nil || strings.HasPrefix(rel, "..") {
		t.Fatalf("the auto workspace %q is not under the state root %q", got, state)
	}
}

func TestPickLaunchWorkspaceCreatesTheAutoWorkspaceLast(t *testing.T) {
	base := t.TempDir()
	app := filepath.Join(base, "app")
	exe := filepath.Join(app, "Reasonix Studio")
	auto := filepath.Join(base, "state", "workspaces", "auto")
	if got := pickLaunchWorkspace(app, exe, nil, auto); got != auto {
		t.Fatalf("got %q, want %q", got, auto)
	}
	if st, err := os.Stat(auto); err != nil || !st.IsDir() {
		t.Fatalf("not created: %v", err)
	}
	chosen := filepath.Join(base, "proj")
	if err := os.MkdirAll(chosen, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := pickLaunchWorkspace(chosen, exe, nil, auto); got != chosen {
		t.Fatalf("a chosen directory must win over the auto workspace: %q", got)
	}
}
