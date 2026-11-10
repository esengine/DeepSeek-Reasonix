//go:build windows || linux

package computer

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"reasonix/internal/base/testenv"
)

func executableCatalogue(file string) nativeApplication {
	identity := file
	if runtime.GOOS == "linux" {
		identity = "example.desktop"
	}
	return nativeApplication{Bundle: identity, LaunchPath: file}
}

func TestCUALaunchRejectsScriptsAndMissingExecutableBeforeDispatch(t *testing.T) {
	file := filepath.Join(testenv.TempDir(t), "installed.exe")
	if err := os.WriteFile(file, []byte("#!/bin/sh\necho unsafe\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := applicationLaunch(executableCatalogue(file)); CodeOf(err) != CodeUnsupported {
		t.Fatalf("script accepted: %v", err)
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if _, err := applicationLaunch(executableCatalogue(file)); CodeOf(err) != CodeUnsupported {
		t.Fatalf("missing executable accepted: %v", err)
	}
}

func TestCUALaunchResolvesLinksBeforeCheckingForbiddenExecutable(t *testing.T) {
	alias := filepath.Join(testenv.TempDir(t), "approved.exe")
	target := "/bin/sh"
	if runtime.GOOS == "windows" {
		target = filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe")
	}
	if err := os.Symlink(target, alias); err != nil {
		t.Skipf("creating symlink unavailable: %v", err)
	}
	if _, err := applicationLaunch(executableCatalogue(alias)); CodeOf(err) != CodeAppRefused {
		t.Fatalf("symlink target was not refused before launch: %v", err)
	}
}
