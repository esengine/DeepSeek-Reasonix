package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
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
	a, b, home := mk("a"), mk("b"), mk("home")
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
		{"all stale falls to home", app, []string{gone}, home},
		{"empty list falls to home", app, nil, home},
	}
	for _, c := range cases {
		if got := pickLaunchWorkspace(c.cwd, exe, c.remembered, home); got != c.want {
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
