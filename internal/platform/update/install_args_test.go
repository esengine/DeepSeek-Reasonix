package update

import (
	"os"
	"strings"
	"testing"
)

func TestInstallerCommandLineUsesVisibleUpdateModeAndKeepsDFlagLast(t *testing.T) {
	got := installerCommandLine(`C:\Temp\Reasonix Installer.exe`, `D:\Tools\Reasonix App`)
	want := `"C:\Temp\Reasonix Installer.exe" /REASONIXUPDATE=1 /REASONIXSTAGE=1 /D=D:\Tools\Reasonix App`
	if got != want {
		t.Fatalf("installerCommandLine = %q, want %q", got, want)
	}
	if strings.Contains(got, " /S") {
		t.Fatalf("auto-update must expose progress instead of using silent mode, got %q", got)
	}
	if !strings.HasSuffix(got, `/D=D:\Tools\Reasonix App`) {
		t.Fatalf("/D= must be the final unquoted NSIS token, got %q", got)
	}
}

func TestWindowsUpdateHandoffArgsCarryParentInstallAndRelaunch(t *testing.T) {
	got := windowsUpdateHandoffArgs(
		4242,
		`C:\Users\Jane Doe\AppData\Local\Reasonix\updates\Reasonix-windows-amd64-installer.exe`,
		strings.Repeat("a", 64),
		`D:\Tools\Reasonix App`,
		`D:\Tools\Reasonix App\reasonix-desktop.exe`,
		"v1.6.0",
		"2026-07-29T00:00:00Z",
		"transaction-1",
	)
	want := []string{
		"--parent-pid", "4242",
		"--installer", `C:\Users\Jane Doe\AppData\Local\Reasonix\updates\Reasonix-windows-amd64-installer.exe`,
		"--installer-sha256", strings.Repeat("a", 64),
		"--to-version", "v1.6.0",
		"--created-at", "2026-07-29T00:00:00Z",
		"--transaction-id", "transaction-1",
		"--install-dir", `D:\Tools\Reasonix App`,
		"--relaunch", `D:\Tools\Reasonix App\reasonix-desktop.exe`,
	}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("args = %#v, want %#v", got, want)
	}
}

func TestWindowsVersionedUpdateHandoffArgsDoNotRequireLegacyPendingIdentity(t *testing.T) {
	got := windowsVersionedUpdateHandoffArgs(
		4242,
		`C:\Temp\Reasonix-installer.exe`,
		strings.Repeat("b", 64),
		`D:\Tools\Reasonix`,
		`D:\Tools\Reasonix\reasonix-launcher.exe`,
		"v1.20.0",
	)
	want := []string{
		"--parent-pid", "4242",
		"--installer", `C:\Temp\Reasonix-installer.exe`,
		"--installer-sha256", strings.Repeat("b", 64),
		"--to-version", "v1.20.0",
		"--install-layout", "versioned-v1",
		"--install-dir", `D:\Tools\Reasonix`,
		"--relaunch", `D:\Tools\Reasonix\reasonix-launcher.exe`,
	}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("args = %#v, want %#v", got, want)
	}
	for _, legacy := range []string{"--created-at", "--transaction-id"} {
		if strings.Contains(strings.Join(got, " "), legacy) {
			t.Fatalf("versioned handoff must not carry legacy field %s", legacy)
		}
	}
}

func TestWindowsUpdateRequiresObservedHelperHandoff(t *testing.T) {
	data, err := os.ReadFile("install_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	if !strings.Contains(source, "return startWindowsUpdateHelper(") {
		t.Fatal("Windows update handoff does not require the observed helper path")
	}
	if strings.Contains(source, "return installerCommand(") {
		t.Fatal("Windows update silently falls back to an unobserved installer")
	}
	if !strings.Contains(source, "cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}") {
		t.Fatal("Windows handoff helper should stay hidden while NSIS shows update progress")
	}
	// The helper is still built by Studio's release line and so still lives in
	// the desktop module; this contract spans both sides of that line.
	helperData, err := os.ReadFile("../../../desktop/cmd/update-helper/main_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	helperSource := string(helperData)
	if !strings.Contains(helperSource, "reconcileWindowsUninstallRegistrationFn(installDir, toVersion)") {
		t.Fatal("versioned Windows activation must refresh its managed uninstall registration")
	}
	if strings.Contains(helperSource, "installerCommandLine(installer, installDir), HideWindow: true") {
		t.Fatal("update helper still hides the NSIS progress window")
	}
}
