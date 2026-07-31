// Copyright (c) 2026, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package internal

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// TestMainSetup is used by the integration tests running shell scripts
// either via our interpreter or via real shells,
// to ensure a reasonably clean and consistent environment.
func TestMainSetup() {
	// Set the locale to computer-friendly English and UTF-8.
	// Some systems like macOS miss C.UTF8, so fall back to the US English locale.
	if out, _ := exec.Command("locale", "-a").Output(); strings.Contains(
		strings.ToLower(string(out)), "c.utf",
	) {
		os.Setenv("LANGUAGE", "C.UTF-8")
		os.Setenv("LC_ALL", "C.UTF-8")
	} else {
		os.Setenv("LANGUAGE", "en_US.UTF-8")
		os.Setenv("LC_ALL", "en_US.UTF-8")
	}

	// Bash prints the pwd after changing directories when CDPATH is set.
	os.Unsetenv("CDPATH")

	pathDir, err := os.MkdirTemp("", "interp-bin-")
	if err != nil {
		panic(err)
	}

	// These short names are commonly used as variables.
	// Ensure they are unset as env vars.
	// We can't easily remove names from $PATH,
	// so do the next best thing: override each name with a failing script.
	for _, s := range []string{
		"a", "b", "c", "d", "e", "f", "foo", "bar",
	} {
		os.Unsetenv(s)
		pathFile := filepath.Join(pathDir, s)
		if err := os.WriteFile(pathFile, []byte("#!/bin/sh\necho NO_SUCH_COMMAND; exit 1"), 0o777); err != nil {
			panic(err)
		}
	}

	os.Setenv("PATH", pathDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}
