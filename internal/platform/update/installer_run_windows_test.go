//go:build windows

package update

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"

	"reasonix/internal/base/tempdir"
)

func TestRunInstallerStartErrorIsActionableAndPathSafe(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  error
		want string
	}{
		{"declined", windows.ERROR_CANCELLED, "administrator prompt"},
		{"quarantined", windows.ERROR_FILE_NOT_FOUND, "quarantined"},
		{"denied", windows.ERROR_ACCESS_DENIED, "denied"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := &os.PathError{Op: "shellexecute", Path: `C:\Users\Private\ReasonixStudio-installer.exe`, Err: tc.raw}
			err := runInstallerStartError(raw)
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error is not actionable: %v", err)
			}
			if strings.Contains(err.Error(), `C:\Users`) {
				t.Fatalf("error leaks local path: %v", err)
			}
		})
	}
}

// Elevation is the failure this path exists to avoid: CreateProcess reports it
// and ShellExecute does not, so a message naming it means the fix regressed.
func TestRunInstallerStartErrorNamesUnexpectedElevation(t *testing.T) {
	err := runInstallerStartError(windows.ERROR_ELEVATION_REQUIRED)
	if !errors.Is(err, windows.ERROR_ELEVATION_REQUIRED) {
		t.Fatalf("elevation failure lost its cause: %v", err)
	}
}

// A line that never said which installer it ships must not reach a launch call
// chosen for it: guessing is wrong in both directions, and the per-user one
// fails silently — an elevated start writes the administrator's profile.
func TestRunInstallerRequiresAnInstallerDeclaration(t *testing.T) {
	err := (Line{}).RunInstaller(filepath.Join(tempdir.New(t), "absent.exe"))
	if err == nil {
		t.Fatal("an undeclared line was allowed to start an installer")
	}
	if !strings.Contains(err.Error(), "declares no Windows installer") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// Studio ships the per-user Electron installer, so it must never be started
// with the verb that elevates: the upgrade would land in whichever account
// consented and this user would stay on the build they were already running.
func TestStudioLineDeclaresThePerUserInstaller(t *testing.T) {
	if got := StudioLine().Windows.Installer; got != WindowsInstallerPerUser {
		t.Fatalf("Studio's Windows installer = %q, want %q", got, WindowsInstallerPerUser)
	}
}

func TestRunInstallerRejectsMissingInstaller(t *testing.T) {
	for _, kind := range []WindowsInstaller{WindowsInstallerElevated, WindowsInstallerPerUser} {
		declared := Line{Windows: WindowsLine{Installer: kind}}
		if err := declared.RunInstaller(""); err == nil {
			t.Fatalf("%s: empty installer path was accepted", kind)
		}
		missing := filepath.Join(tempdir.New(t), "absent.exe")
		err := declared.RunInstaller(missing)
		if err == nil {
			t.Fatalf("%s: missing installer was accepted", kind)
		}
		if !strings.Contains(err.Error(), "unreadable") {
			t.Fatalf("%s: unexpected error: %v", kind, err)
		}
	}
}
