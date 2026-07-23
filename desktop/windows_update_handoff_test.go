package main

import (
	"os"
	"strings"
	"testing"
)

func TestInstallerCommandLineUsesVisibleUpdateModeAndKeepsDFlagLast(t *testing.T) {
	got := installerCommandLine(`C:\Temp\Reasonix Installer.exe`, `D:\Tools\Reasonix App`)
	want := `"C:\Temp\Reasonix Installer.exe" /REASONIXUPDATE=1 /D=D:\Tools\Reasonix App`
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
		`D:\Tools\Reasonix App`,
		`D:\Tools\Reasonix App\reasonix-desktop.exe`,
		"v1.6.0",
	)
	want := []string{
		"--parent-pid", "4242",
		"--installer", `C:\Users\Jane Doe\AppData\Local\Reasonix\updates\Reasonix-windows-amd64-installer.exe`,
		"--to-version", "v1.6.0",
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
		`!define REASONIX_GUARD "reasonix-guard.exe"`,
		`!define REASONIX_LAUNCHER "reasonix-launcher.exe"`,
		`!define REASONIX_CLI "reasonix-cli.exe"`,
		`!define REASONIX_PORTABLE_ENTRY "Reasonix.exe"`,
		"Var ReasonixUpdateMode",
		`${GetOptions} $R0 "/REASONIXUPDATE=" $R1`,
		"Function reasonix.skipSetupPageForUpdate",
		"Function reasonix.showUpdateProgress",
		`!define MUI_PAGE_CUSTOMFUNCTION_PRE reasonix.skipFinishPageForUpdate`,
		"Function reasonix.skipFinishPageForUpdate",
		`StrCmp $ReasonixUpdateMode "1" 0 reasonix_show_finish_page`,
		"SetAutoClose true",
		"BringToFront",
		`LangString reasonixUpdateTitle ${LANG_ENGLISH} "Updating Reasonix"`,
		`LangString reasonixUpdateTitle ${LANG_SIMPCHINESE} "正在更新 Reasonix"`,
		`LangString reasonixUpdateTitle ${LANG_TRADCHINESE} "正在更新 Reasonix"`,
		`LangString reasonixUpdateSubtitle ${LANG_ENGLISH} "Installing the verified update. Reasonix will restart automatically."`,
		`LangString reasonixUpdateSubtitle ${LANG_SIMPCHINESE} "正在安装已验证的更新，完成后 Reasonix 将自动重启。"`,
		`LangString reasonixUpdateSubtitle ${LANG_TRADCHINESE} "正在安裝已驗證的更新，完成後 Reasonix 將自動重新啟動。"`,
		"Function reasonix.waitForExecutableUnlock",
		`FileOpen $1 "$INSTDIR\${PRODUCT_EXECUTABLE}" a`,
		`FileOpen $1 "$INSTDIR\${REASONIX_GUARD}" a`,
		`FileOpen $1 "$INSTDIR\${REASONIX_LAUNCHER}" a`,
		`FileOpen $1 "$INSTDIR\${REASONIX_CLI}" a`,
		`FileOpen $1 "$INSTDIR\${REASONIX_PORTABLE_ENTRY}" a`,
		"SetErrorLevel 1618",
		"Call reasonix.waitForExecutableUnlock",
		`File "/oname=${REASONIX_UPDATE_HELPER}" "${REASONIX_UPDATE_HELPER}"`,
		`File "/oname=${REASONIX_CLI}" "${REASONIX_CLI}"`,
		`File "/oname=${REASONIX_PORTABLE_ENTRY}" "${REASONIX_LAUNCHER}"`,
		`Delete "$INSTDIR\${REASONIX_UPDATE_HELPER}"`,
		`Delete "$INSTDIR\${REASONIX_CLI}"`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("project.nsi missing %q", want)
		}
	}
	finishPageHook := strings.Index(script, "!define MUI_PAGE_CUSTOMFUNCTION_PRE reasonix.skipFinishPageForUpdate")
	finishPage := strings.Index(script, "!insertmacro MUI_PAGE_FINISH")
	if finishPageHook < 0 || finishPage < 0 || finishPageHook > finishPage {
		t.Fatalf("update-only finish page hook must be attached to MUI_PAGE_FINISH (hook=%d page=%d)", finishPageHook, finishPage)
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
		`go build -trimpath -o "$windows_resource_tool" ./cmd/windows-resource`,
		`GOOS=windows GOARCH="$arch" go build`,
		`./cmd/update-helper`,
		`build/windows/installer/$UPDATE_HELPER`,
		`stamp_windows_executable "build/windows/installer/$UPDATE_HELPER"`,
		`cp "$helper" "$staging/$UPDATE_HELPER"`,
		`"$ROOT/scripts/verify-windows-portable.sh" "$staging"`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("desktop-build.sh missing %q", want)
		}
	}
}

func TestWindowsUpdateRequiresObservedHelperHandoff(t *testing.T) {
	data, err := os.ReadFile("updater_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	if !strings.Contains(source, "return startWindowsUpdateHelper(") {
		t.Fatal("Windows update handoff does not require the observed helper path")
	}
	if strings.Contains(source, "return installerCommand(installerPath, installDir).Start()") {
		t.Fatal("Windows update silently falls back to an unobserved installer")
	}
	if !strings.Contains(source, "cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}") {
		t.Fatal("Windows handoff helper should stay hidden while NSIS shows update progress")
	}
	helperData, err := os.ReadFile("cmd/update-helper/main_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	helperSource := string(helperData)
	if strings.Contains(helperSource, "installerCommandLine(installer, installDir), HideWindow: true") {
		t.Fatal("update helper still hides the NSIS progress window")
	}
}
