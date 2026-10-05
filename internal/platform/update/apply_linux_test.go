//go:build linux

package update

import (
	"archive/tar"
	"context"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/platform/installlayout"

	"reasonix/internal/base/tempdir"
)

// The tarball still carries reasonix-guard for v1.18-v1.19 updaters reading the
// same archive. A v1.20+ install must extract it and leave it nowhere on disk,
// or the next launch has two entry points disagreeing about which is current.
func TestVersionedInstallActivatesWithoutPersistingGuard(t *testing.T) {
	root := tempdir.New(t)
	source := tempdir.New(t)
	for _, name := range []string{installlayout.DesktopBinaryName(), installlayout.CLIBinaryName()} {
		if err := os.WriteFile(filepath.Join(source, name), []byte("old-"+name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := installlayout.ActivateVersion(installlayout.ActivationRequest{
		InstallRoot: root,
		Version:     "v1.20.0",
		RequestID:   "seed-linux",
		Members: []installlayout.Member{
			{Name: installlayout.DesktopBinaryName(), Path: filepath.Join(source, installlayout.DesktopBinaryName())},
			{Name: installlayout.CLIBinaryName(), Path: filepath.Join(source, installlayout.CLIBinaryName())},
		},
		RequiredNames: []string{installlayout.DesktopBinaryName(), installlayout.CLIBinaryName()},
	}); err != nil {
		t.Fatal(err)
	}

	var headers []tar.Header
	var bodies [][]byte
	for _, name := range testLine().ArchiveNames() {
		body := []byte("new-" + name)
		headers = append(headers, regularMember(name, body))
		bodies = append(bodies, body)
	}
	artifact := filepath.Join(tempdir.New(t), "release.tar.gz")
	if err := os.WriteFile(artifact, makeArchive(t, headers, bodies), 0o600); err != nil {
		t.Fatal(err)
	}

	// The line is the host's declaration of what a release carries; without it
	// the installer has no members to extract. testLine is the same one the
	// archive above was built from.
	inst := VersionedInstaller{
		Layout: Layout{Root: root, Executable: filepath.Join(root, "reasonix-desktop")},
		Line:   testLine(),
	}
	// The version arrives without its leading v, the way a manifest can spell it.
	if err := inst.Install(context.Background(), Cached{Version: "1.20.1", Path: artifact, Kind: KindTarball}); err != nil {
		t.Fatal(err)
	}
	ptr, err := installlayout.ReadCurrent(root)
	if err != nil || ptr.ActiveVersion != "v1.20.1" {
		t.Fatalf("pointer=%+v err=%v", ptr, err)
	}
	activeDesktop, err := installlayout.ActiveDesktopPath(root)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(activeDesktop)
	if err != nil || string(data) != "new-reasonix-desktop" {
		t.Fatalf("active desktop=%q err=%v", data, err)
	}
	for _, guardPath := range []string{
		filepath.Join(root, "reasonix-guard"),
		filepath.Join(root, "versions", "v1.20.1", "reasonix-guard"),
	} {
		if _, err := os.Lstat(guardPath); !os.IsNotExist(err) {
			t.Fatalf("Guard persisted at %s: %v", guardPath, err)
		}
	}
}

// Rolling back is publishing an older version directory. Nothing on this path
// may ask whether the target is ahead of what is running.
func TestVersionedInstallGoesBackwards(t *testing.T) {
	root := tempdir.New(t)
	source := tempdir.New(t)
	for _, name := range []string{installlayout.DesktopBinaryName(), installlayout.CLIBinaryName()} {
		if err := os.WriteFile(filepath.Join(source, name), []byte("v2-"+name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := installlayout.ActivateVersion(installlayout.ActivationRequest{
		InstallRoot: root,
		Version:     "v2.1.0",
		RequestID:   "seed",
		Members: []installlayout.Member{
			{Name: installlayout.DesktopBinaryName(), Path: filepath.Join(source, installlayout.DesktopBinaryName())},
			{Name: installlayout.CLIBinaryName(), Path: filepath.Join(source, installlayout.CLIBinaryName())},
		},
		RequiredNames: []string{installlayout.DesktopBinaryName(), installlayout.CLIBinaryName()},
	}); err != nil {
		t.Fatal(err)
	}

	var headers []tar.Header
	var bodies [][]byte
	for _, name := range testLine().ArchiveNames() {
		body := []byte("v2.0.0-" + name)
		headers = append(headers, regularMember(name, body))
		bodies = append(bodies, body)
	}
	artifact := filepath.Join(tempdir.New(t), "old.tar.gz")
	if err := os.WriteFile(artifact, makeArchive(t, headers, bodies), 0o600); err != nil {
		t.Fatal(err)
	}

	// The line is the host's declaration of what a release carries; without it
	// the installer has no members to extract. testLine is the same one the
	// archive above was built from.
	inst := VersionedInstaller{
		Layout: Layout{Root: root, Executable: filepath.Join(root, "reasonix-desktop")},
		Line:   testLine(),
	}
	if err := inst.Install(context.Background(), Cached{Version: "v2.0.0", Path: artifact, Kind: KindTarball}); err != nil {
		t.Fatalf("rollback install: %v", err)
	}
	ptr, err := installlayout.ReadCurrent(root)
	if err != nil || ptr.ActiveVersion != "v2.0.0" {
		t.Fatalf("pointer=%+v err=%v, want the older version to be current", ptr, err)
	}
}
