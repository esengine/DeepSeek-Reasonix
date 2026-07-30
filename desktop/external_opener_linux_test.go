//go:build !darwin && !windows

package main

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func writeLinuxDesktopFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestParseLinuxDesktopEntryReadsDesktopEntrySectionOnly(t *testing.T) {
	dir := t.TempDir()
	path := writeLinuxDesktopFile(t, dir, "code.desktop", `[Desktop Entry]
Type=Application
Name=Visual Studio Code
Icon=vscode

[Desktop Action new-window]
Name=New Window
Icon=new-window
`)
	entry, ok := parseLinuxDesktopEntry(path)
	if !ok {
		t.Fatal("valid desktop entry was rejected")
	}
	want := linuxDesktopEntry{Path: path, ID: "code", Name: "Visual Studio Code", Icon: "vscode"}
	if entry != want {
		t.Fatalf("parseLinuxDesktopEntry = %+v, want %+v", entry, want)
	}
}

func TestParseLinuxDesktopEntryRejectsHiddenAndNonApplications(t *testing.T) {
	dir := t.TempDir()
	for name, content := range map[string]string{
		"hidden.desktop":        "[Desktop Entry]\nType=Application\nName=Ghost\nHidden=true\n",
		"link.desktop":          "[Desktop Entry]\nType=Link\nName=Bookmark\n",
		"unnamed.desktop":       "[Desktop Entry]\nType=Application\n",
		"wrong-section.desktop": "[Desktop Action open]\nName=Open\n",
	} {
		path := writeLinuxDesktopFile(t, dir, name, content)
		if entry, ok := parseLinuxDesktopEntry(path); ok {
			t.Errorf("%s was accepted as %+v, want rejection", name, entry)
		}
	}
}

func TestFindLinuxDesktopEntryMatchesNormalizedAliasesThenSubstrings(t *testing.T) {
	entries := []linuxDesktopEntry{
		{ID: "org.gnome.Terminal", Name: "Terminal"},
		{ID: "code", Name: "Visual Studio Code"},
		{ID: "jetbrains-idea", Name: "IntelliJ IDEA Ultimate Edition"},
		{ID: "kitty", Name: "kitty"},
	}
	if entry, ok := findLinuxDesktopEntry(entries, "org.gnome.terminal"); !ok || entry.ID != "org.gnome.Terminal" {
		t.Fatalf("normalized id lookup = (%+v, %v), want org.gnome.Terminal", entry, ok)
	}
	if entry, ok := findLinuxDesktopEntry(entries, "visual-studio-code"); !ok || entry.ID != "code" {
		t.Fatalf("normalized name lookup = (%+v, %v), want code", entry, ok)
	}
	if entry, ok := findLinuxDesktopEntry(entries, "intellij-idea"); !ok || entry.ID != "jetbrains-idea" {
		t.Fatalf("substring lookup = (%+v, %v), want jetbrains-idea", entry, ok)
	}
	if entry, ok := findLinuxDesktopEntry(entries, "kit"); ok {
		t.Fatalf("short alias %q unexpectedly matched %+v via substring", "kit", entry)
	}
}

func TestLinuxExternalOpenerCommandPerTerminalArguments(t *testing.T) {
	const workdir = "/tmp/reasonix workspace"
	cases := []struct {
		mode   string
		target string
		want   []string
	}{
		{"path", "/usr/bin/code", []string{"/usr/bin/code", workdir}},
		{"ghostty", "/usr/bin/ghostty", []string{"/usr/bin/ghostty", "--working-directory=" + workdir}},
		{"gnome-terminal", "/usr/bin/gnome-terminal", []string{"/usr/bin/gnome-terminal", "--working-directory=" + workdir}},
		{"konsole", "/usr/bin/konsole", []string{"/usr/bin/konsole", "--workdir", workdir}},
		{"kitty", "/usr/bin/kitty", []string{"/usr/bin/kitty", "--directory", workdir}},
		{"alacritty", "/usr/bin/alacritty", []string{"/usr/bin/alacritty", "--working-directory", workdir}},
		{"cwd", "/usr/bin/x-terminal-emulator", []string{"/usr/bin/x-terminal-emulator"}},
	}
	for _, tc := range cases {
		spec := externalOpenerSpec{Target: tc.target, LaunchMode: tc.mode}
		cmd, err := linuxExternalOpenerCommand(spec, workdir)
		if err != nil {
			t.Errorf("%s: linuxExternalOpenerCommand error = %v", tc.mode, err)
			continue
		}
		if !reflect.DeepEqual(cmd.Args, tc.want) {
			t.Errorf("%s: args = %#v, want %#v", tc.mode, cmd.Args, tc.want)
		}
		if cmd.Dir != workdir {
			t.Errorf("%s: dir = %q, want workspace directory", tc.mode, cmd.Dir)
		}
	}
}

func TestLinuxExternalOpenerCommandLaunchesDesktopEntriesViaGio(t *testing.T) {
	binDir := t.TempDir()
	gio := filepath.Join(binDir, "gio")
	if err := os.WriteFile(gio, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)

	spec := externalOpenerSpec{Target: "/usr/share/applications/code.desktop", LaunchMode: "gio"}
	cmd, err := linuxExternalOpenerCommand(spec, "/tmp/project")
	if err != nil {
		t.Fatalf("linuxExternalOpenerCommand error = %v", err)
	}
	want := []string{gio, "launch", "/usr/share/applications/code.desktop", "/tmp/project"}
	if !reflect.DeepEqual(cmd.Args, want) {
		t.Fatalf("gio args = %#v, want %#v", cmd.Args, want)
	}

	t.Setenv("PATH", t.TempDir())
	if _, err := linuxExternalOpenerCommand(spec, "/tmp/project"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing gio error = %v, want os.ErrNotExist", err)
	}
}

func TestResolveLinuxDesktopIconAcceptsOnlyExistingAbsoluteFiles(t *testing.T) {
	dir := t.TempDir()
	icon := filepath.Join(dir, "vscode.png")
	if err := os.WriteFile(icon, []byte("png"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := resolveLinuxDesktopIcon(icon); got != icon {
		t.Fatalf("existing absolute icon = %q, want %q", got, icon)
	}
	if got := resolveLinuxDesktopIcon(filepath.Join(dir, "missing.png")); got != "" {
		t.Fatalf("missing absolute icon = %q, want empty", got)
	}
	if got := resolveLinuxDesktopIcon(dir); got != "" {
		t.Fatalf("directory icon = %q, want empty", got)
	}
}
