//go:build linux

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDirIsWritable(t *testing.T) {
	dir := t.TempDir()
	if !dirIsWritable(dir) {
		t.Fatal("temp dir should be writable")
	}
	// A non-existent path is not writable.
	if dirIsWritable(filepath.Join(dir, "missing-subdir")) {
		t.Fatal("missing dir should not be writable")
	}
}

func TestDetectLinuxInstallProfilePortableWritable(t *testing.T) {
	// When the test binary itself is not dpkg-owned (almost always true in CI
	// workspaces), detection should land on portable if the binary dir is writable.
	profile := detectLinuxInstallProfile()
	if profile.Mode == installModeDeb {
		// Running from a real .deb install in the developer's environment.
		if !profile.RequiresElev || !profile.CanSelfUpdate {
			t.Fatalf("deb profile incomplete: %+v", profile)
		}
		return
	}
	if profile.Mode == installModePortable {
		if !profile.CanSelfUpdate || profile.ArtifactKind != artifactKindTarball {
			t.Fatalf("portable profile incomplete: %+v", profile)
		}
		return
	}
	if profile.Mode != installModeManual {
		t.Fatalf("unexpected mode: %+v", profile)
	}
}

func TestIsDpkgOwnedReasonixRejectsEmpty(t *testing.T) {
	if isDpkgOwnedReasonix("") {
		t.Fatal("empty path must not be dpkg-owned")
	}
	if isDpkgOwnedReasonix("relative/path") {
		t.Fatal("relative path must not be dpkg-owned")
	}
	// A random absolute path that is not packaged.
	if isDpkgOwnedReasonix(filepath.Join(t.TempDir(), "reasonix-desktop")) {
		t.Fatal("unpackaged path must not be dpkg-owned")
	}
}

func TestLinuxDebHelperReadyAbsentInTemp(t *testing.T) {
	// Unless the developer machine has the system helper installed, readiness
	// should be false in typical test environments.
	if _, err := os.Stat(linuxUpdateHelperPath); err == nil {
		if !linuxDebHelperReady() {
			t.Fatal("helper present but readiness false")
		}
		return
	}
	if linuxDebHelperReady() {
		t.Fatal("helper absent but readiness true")
	}
}
