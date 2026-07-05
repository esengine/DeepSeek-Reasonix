package main

import (
	"os"
	"strings"
	"testing"
)

func TestInstallerCommandLineIsSilentAndKeepsDFlagLast(t *testing.T) {
	got := installerCommandLine(`C:\Temp\Reasonix Installer.exe`, `D:\Tools\Reasonix App`)
	want := `"C:\Temp\Reasonix Installer.exe" /S /D=D:\Tools\Reasonix App`
	if got != want {
		t.Fatalf("installerCommandLine = %q, want %q", got, want)
	}
	if !strings.HasSuffix(got, `/D=D:\Tools\Reasonix App`) {
		t.Fatalf("/D= must be the final unquoted NSIS token, got %q", got)
	}
}

func TestWindowsUpdateHandoffArgsCarryParentInstallAndRelaunch(t *testing.T) {
	got := windowsUpdateHandoffArgs(
		4242,
		`C:\Users\Jane Doe\AppData\Local\Reasonix\updates\Reasonix-windows-amd64-installer.exe`,
		`D:\Tools\Reasonix App`,
		`D:\Tools\Reasonix App\reasonix-desktop.exe`,
	)
	want := []string{
		"--parent-pid", "4242",
		"--installer", `C:\Users\Jane Doe\AppData\Local\Reasonix\updates\Reasonix-windows-amd64-installer.exe`,
		"--install-dir", `D:\Tools\Reasonix App`,
		"--relaunch", `D:\Tools\Reasonix App\reasonix-desktop.exe`,
	}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("args = %#v, want %#v", got, want)
	}
}

func TestWindowsInstallerScriptWaitsBeforeCopyingExecutable(t *testing.T) {
	data, err := os.ReadFile("build/windows/installer/project.nsi")
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)
	for _, want := range []string{
		`!define REASONIX_UPDATE_HELPER "reasonix-update-helper.exe"`,
		"Function reasonix.waitForExecutableUnlock",
		`FileOpen $1 "$INSTDIR\${PRODUCT_EXECUTABLE}" a`,
		"SetErrorLevel 1618",
		"Call reasonix.waitForExecutableUnlock",
		`File "/oname=${REASONIX_UPDATE_HELPER}" "${REASONIX_UPDATE_HELPER}"`,
		`Delete "$INSTDIR\${REASONIX_UPDATE_HELPER}"`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("project.nsi missing %q", want)
		}
	}
	wait := strings.Index(script, "Call reasonix.waitForExecutableUnlock")
	copyFiles := strings.Index(script, "!insertmacro wails.files")
	if wait < 0 || copyFiles < 0 || wait > copyFiles {
		t.Fatalf("installer must wait for the running exe to unlock before wails.files (wait=%d copy=%d)", wait, copyFiles)
	}
}

func TestDesktopBuildScriptCompilesAndPackagesWindowsUpdateHelper(t *testing.T) {
	data, err := os.ReadFile("../scripts/desktop-build.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)
	for _, want := range []string{
		`UPDATE_HELPER="reasonix-update-helper.exe"`,
		`GOOS=windows GOARCH="$arch" go build`,
		`./cmd/update-helper`,
		`build/windows/installer/$UPDATE_HELPER`,
		`cp "$helper" "$staging/$UPDATE_HELPER"`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("desktop-build.sh missing %q", want)
		}
	}
}
