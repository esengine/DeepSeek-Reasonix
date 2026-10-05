package editor

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestDiscoverRefusesWithAnIdentityWhenNothingIsInstalled(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("LOCALAPPDATA", t.TempDir())
	t.Setenv("ProgramFiles", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	// The system application folder is the machine's own; an empty one stands
	// in for a Mac with nothing installed there.
	_, err := discover("", host{goos: runtime.GOOS, getenv: os.Getenv, applications: t.TempDir()})
	if !errors.Is(err, ErrNoEditor) {
		t.Fatalf("Discover on a bare machine = %v, want ErrNoEditor", err)
	}
}

// A person who names an editor is never second-guessed by a search that finds
// something else first, and a name that resolves to nothing is their name
// failing rather than the machine having none.
func TestConfiguredEditorIsTheOnlyCandidate(t *testing.T) {
	dir := t.TempDir()
	named := filepath.Join(dir, "my-editor")
	if err := os.WriteFile(named, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	spec, err := Discover(named)
	if err != nil {
		t.Fatalf("Discover(%q) = %v", named, err)
	}
	if spec.Executable != named {
		t.Errorf("executable = %q, want the configured %q", spec.Executable, named)
	}

	if _, err := Discover(filepath.Join(dir, "absent")); !errors.Is(err, ErrNoEditor) {
		t.Errorf("a configured editor that does not exist = %v, want ErrNoEditor", err)
	}
}

// The installed paths are the machine's, not PATH's: a Windows box where `code`
// is a .cmd shim should launch the executable the installer placed.
func TestInstalledPathsAreSearchedBeforePath(t *testing.T) {
	if runtime.GOOS != "windows" && runtime.GOOS != "darwin" {
		t.Skip("only windows and darwin have installer paths to search")
	}
	local := t.TempDir()
	f := families[0]
	var placed string
	switch runtime.GOOS {
	case "windows":
		placed = filepath.Join(local, f.windows[0])
	case "darwin":
		placed = filepath.Join(local, "Applications", f.darwin[0])
	}
	if err := os.MkdirAll(filepath.Dir(placed), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(placed, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	getenv := func(k string) string {
		switch k {
		case "LOCALAPPDATA", "HOME":
			return local
		}
		return ""
	}
	got, ok := f.find(host{goos: runtime.GOOS, getenv: getenv, applications: t.TempDir()})
	if !ok || got != placed {
		t.Fatalf("find = %q %v, want the installed %q", got, ok, placed)
	}
}

func TestOpenRefusesSomethingThatIsNotADirectory(t *testing.T) {
	file := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Open(Spec{Executable: "editor"}, file); err == nil {
		t.Fatal("opening a file as a workspace was accepted")
	}
	if err := Open(Spec{}, t.TempDir()); !errors.Is(err, ErrNoEditor) {
		t.Fatalf("opening with no executable = %v, want ErrNoEditor", err)
	}
}
