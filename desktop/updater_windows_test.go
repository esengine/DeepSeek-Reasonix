//go:build windows

package main

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestInstallerCommandShowsUpdateProgressAndPassesUnquotedDFlagLast(t *testing.T) {
	cmd := installerCommand(`C:\Temp\reasonix-update-1.exe`, `D:\Tools\Reasonix App`)
	if cmd.SysProcAttr == nil {
		t.Fatal("expected a raw command line forcing the install dir")
	}
	got := cmd.SysProcAttr.CmdLine
	want := `"C:\Temp\reasonix-update-1.exe" /REASONIXUPDATE=1 /D=D:\Tools\Reasonix App`
	if got != want {
		t.Fatalf("CmdLine = %q, want %q", got, want)
	}
	if cmd.SysProcAttr.HideWindow {
		t.Fatal("NSIS update progress window must remain visible")
	}
}

func TestInstallerCommandWithoutDirSkipsDFlag(t *testing.T) {
	cmd := installerCommand(`C:\Temp\reasonix-update-1.exe`, "")
	if cmd.SysProcAttr == nil {
		t.Fatal("expected a raw command line for visible updater installs")
	}
	got := cmd.SysProcAttr.CmdLine
	want := `"C:\Temp\reasonix-update-1.exe" /REASONIXUPDATE=1`
	if got != want {
		t.Fatalf("CmdLine = %q, want %q", got, want)
	}
}

func TestWindowsHelperStartRetriesTransientSecurityScan(t *testing.T) {
	originalBackoff := windowsHelperStartBackoff
	windowsHelperStartBackoff = func(int) time.Duration { return 0 }
	t.Cleanup(func() { windowsHelperStartBackoff = originalBackoff })

	calls := 0
	err := retryWindowsUpdateHelperStart(func() error {
		calls++
		if calls < 3 {
			return &os.PathError{Op: "fork/exec", Path: `C:\private\helper.exe`, Err: windows.ERROR_SHARING_VIOLATION}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("retryWindowsUpdateHelperStart: %v", err)
	}
	if calls != 3 {
		t.Fatalf("start calls = %d, want 3", calls)
	}
}

func TestWindowsHelperStartErrorIsActionableAndPathSafe(t *testing.T) {
	raw := &os.PathError{Op: "fork/exec", Path: `C:\Users\Private\helper.exe`, Err: windows.ERROR_ACCESS_DENIED}
	err := windowsUpdateHelperStartError(raw)
	if !strings.Contains(err.Error(), "security software") {
		t.Fatalf("error is not actionable: %v", err)
	}
	if strings.Contains(err.Error(), `C:\Users`) {
		t.Fatalf("error leaks local path: %v", err)
	}
	if !errors.Is(raw, windows.ERROR_ACCESS_DENIED) {
		t.Fatal("test setup does not expose the wrapped Windows error")
	}
}

func TestWindowsPEMachineMatchesSupportedArchitectures(t *testing.T) {
	for _, arch := range []string{"amd64", "arm64", "386"} {
		if machine, ok := windowsPEMachine(arch); !ok || machine == 0 {
			t.Fatalf("windowsPEMachine(%q) = (0x%x, %v)", arch, machine, ok)
		}
	}
	if _, ok := windowsPEMachine("mips"); ok {
		t.Fatal("unsupported architecture was accepted")
	}
	if err := validateWindowsUpdateHelper([]byte("not a PE image"), "amd64"); err == nil {
		t.Fatal("malformed helper image was accepted")
	}
}
