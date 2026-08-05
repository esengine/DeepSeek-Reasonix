//go:build windows

package main

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"

	"reasonix/internal/repair"
)

func TestInstallerCommandShowsUpdateProgressAndPassesUnquotedDFlagLast(t *testing.T) {
	cmd := installerCommand(`C:\Temp\reasonix-update-1.exe`, `D:\Tools\Reasonix App`)
	if cmd.SysProcAttr == nil {
		t.Fatal("expected a raw command line forcing the install dir")
	}
	got := cmd.SysProcAttr.CmdLine
	want := `"C:\Temp\reasonix-update-1.exe" /REASONIXUPDATE=1 /REASONIXSTAGE=1 /D=D:\Tools\Reasonix App`
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
	want := `"C:\Temp\reasonix-update-1.exe" /REASONIXUPDATE=1 /REASONIXSTAGE=1`
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

func TestClaimVerifiedWindowsUpdateHelperExecutionFreezesPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reasonix-update-helper.exe")
	content := []byte("verified-helper")
	if err := os.WriteFile(path, content, 0o700); err != nil {
		t.Fatal(err)
	}
	expected := sha256.Sum256(content)
	release, err := claimVerifiedWindowsUpdateHelperExecution(path, expected)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("tampered"), 0o700); err == nil {
		release()
		t.Fatal("copied helper remained writable while execution claim was held")
	}
	if err := os.Rename(path, path+".replaced"); err == nil {
		release()
		t.Fatal("copied helper remained renameable while execution claim was held")
	}
	release()
	release()
	if err := os.WriteFile(path, []byte("replacement"), 0o700); err != nil {
		t.Fatalf("copied helper remained frozen after claim release: %v", err)
	}
}

func TestClaimVerifiedWindowsUpdateHelperExecutionRejectsHashDrift(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reasonix-update-helper.exe")
	if err := os.WriteFile(path, []byte("tampered"), 0o700); err != nil {
		t.Fatal(err)
	}
	if release, err := claimVerifiedWindowsUpdateHelperExecution(path, sha256.Sum256([]byte("expected"))); err == nil {
		release()
		t.Fatal("copied helper with the wrong SHA-256 received an execution claim")
	}
}

func TestStageWindowsUpdateHelperCopyAllocatesExclusiveNodes(t *testing.T) {
	dir := t.TempDir()
	content := []byte("verified-helper")
	first, err := stageWindowsUpdateHelperCopy(dir, content)
	if err != nil {
		t.Fatal(err)
	}
	second, err := stageWindowsUpdateHelperCopy(dir, content)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatalf("exclusive helper copies reused path %q", first)
	}
	for _, path := range []string{first, second} {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != string(content) {
			t.Fatalf("staged helper %q = %q, %v", path, got, err)
		}
	}
}

func TestPreparedWindowsUpdateHelperSHA256BindsReleaseUnitMember(t *testing.T) {
	installDir := t.TempDir()
	helper := filepath.Join(installDir, windowsUpdateHelperFileName)
	expected := strings.Repeat("a", sha256.Size*2)
	prepared := &repair.UpdateTransaction{
		SchemaVersion: 1,
		TargetKind:    "file",
		TargetPath:    filepath.Join(installDir, "reasonix-desktop.exe"),
		ToVersion:     "v2",
		Platform:      "windows/amd64",
		CreatedAt:     "2026-07-29T00:00:00Z",
		Files: []repair.UpdateTransactionFile{{
			TargetPath: helper,
			SHA256:     expected,
		}},
	}
	got, err := preparedWindowsUpdateHelperSHA256(prepared, installDir)
	if err != nil || got != expected {
		t.Fatalf("prepared helper SHA-256 = %q, %v", got, err)
	}
	prepared.Files[0].MissingBefore = true
	if _, err := preparedWindowsUpdateHelperSHA256(prepared, installDir); err == nil {
		t.Fatal("prepared transaction with a missing helper authorized execution")
	}
}

func TestPrepareWindowsUpdateHelperRejectsPreparedHashDrift(t *testing.T) {
	installDir := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(installDir, windowsUpdateHelperFileName),
		[]byte("changed-helper"),
		0o700,
	); err != nil {
		t.Fatal(err)
	}
	prepared := sha256.Sum256([]byte("prepared-helper"))
	_, _, err := prepareWindowsUpdateHelper(installDir, fmt.Sprintf("%x", prepared))
	if err == nil || !strings.Contains(err.Error(), "changed after transaction prepare") {
		t.Fatalf("changed packaged helper = %v", err)
	}
}
