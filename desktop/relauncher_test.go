package main

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/installlayout"
)

func seedDesktopVersionedLayout(t *testing.T, root, version, payload string) string {
	t.Helper()
	src := t.TempDir()
	members := make([]installlayout.Member, 0, len(installlayout.AllowedVersionMembers()))
	for _, name := range installlayout.AllowedVersionMembers() {
		path := filepath.Join(src, version+"-"+name)
		if err := os.WriteFile(path, []byte(payload+"-"+name), 0o755); err != nil {
			t.Fatal(err)
		}
		members = append(members, installlayout.Member{Name: name, Path: path})
	}
	launcherSrc := filepath.Join(src, version+"-launcher")
	if err := os.WriteFile(launcherSrc, []byte(payload+"-launcher"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := installlayout.ActivateVersion(installlayout.ActivationRequest{
		InstallRoot: root,
		Version:     version,
		RequestID:   "desktop-relaunch-" + version,
		Members:     members,
		RootMembers: []installlayout.Member{
			{Name: installlayout.LauncherBinaryName(), Path: launcherSrc},
		},
		RequiredRootNames: []string{installlayout.LauncherBinaryName()},
	}); err != nil {
		t.Fatal(err)
	}
	desktop, err := installlayout.ActiveDesktopPath(root)
	if err != nil {
		t.Fatal(err)
	}
	return desktop
}

func TestRelaunchTargetUsesLauncherInsteadOfRetainedDesktop(t *testing.T) {
	root := t.TempDir()
	oldDesktop := seedDesktopVersionedLayout(t, root, "v1.24.0", "old")
	_ = seedDesktopVersionedLayout(t, root, "v1.24.1", "new")
	got, err := relaunchTarget(oldDesktop)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, installlayout.LauncherBinaryName())
	if got != want {
		t.Fatalf("relaunchTarget = %s, want %s", got, want)
	}
}

func TestRelaunchTargetAllowsCurrentDesktopFallback(t *testing.T) {
	for _, versioned := range []bool{false, true} {
		name := "flat"
		if versioned {
			name = "versioned"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			exe := filepath.Join(root, installlayout.DesktopBinaryName())
			if versioned {
				exe = seedDesktopVersionedLayout(t, root, "v1.24.1", "active")
				if err := os.Remove(filepath.Join(root, installlayout.LauncherBinaryName())); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(exe, []byte("desktop"), 0o755); err != nil {
				t.Fatal(err)
			}
			got, err := relaunchTarget(exe)
			if err != nil {
				t.Fatal(err)
			}
			if got != exe {
				t.Fatalf("relaunchTarget = %s, want current desktop %s", got, exe)
			}
		})
	}
}

func TestSupersededDesktopNeedsRelaunch(t *testing.T) {
	root := t.TempDir()
	oldDesktop := seedDesktopVersionedLayout(t, root, "v1.24.0", "old")
	newDesktop := seedDesktopVersionedLayout(t, root, "v1.24.1", "new")
	if supersededDesktopNeedsRelaunch(newDesktop) {
		t.Fatal("active desktop should not relaunch")
	}
	if !supersededDesktopNeedsRelaunch(oldDesktop) {
		t.Fatal("retained previous desktop must relaunch through the launcher")
	}
	t.Setenv("REASONIX_DEV", "1")
	if supersededDesktopNeedsRelaunch(oldDesktop) {
		t.Fatal("REASONIX_DEV must skip superseded relaunch")
	}
}

func TestLauncherPathForExecutableDoesNotReturnRetainedDesktop(t *testing.T) {
	root := t.TempDir()
	oldDesktop := seedDesktopVersionedLayout(t, root, "v1.24.0", "old")
	_ = seedDesktopVersionedLayout(t, root, "v1.24.1", "new")
	got := launcherPathForExecutable(oldDesktop)
	want := filepath.Join(root, installlayout.LauncherBinaryName())
	if got != want {
		t.Fatalf("launcherPathForExecutable = %s, want %s", got, want)
	}
}
