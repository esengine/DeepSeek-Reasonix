package main

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

func TestWindowsInstallerScriptWaitsBeforeCopyingExecutable(t *testing.T) {
	data, err := os.ReadFile("build/windows/installer/project.nsi")
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)
	for _, want := range []string{
		`!define REASONIX_LEGACY_UNINST_KEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\Reasonix"`,
		`!define REASONIX_LEGACY_PRODUCT_KEY "Software\reasonix\Reasonix"`,
		`!define REASONIX_UPDATE_HELPER "reasonix-update-helper.exe"`,
		`!define REASONIX_GUARD "reasonix-guard.exe"`,
		`!define REASONIX_LAUNCHER "reasonix-launcher.exe"`,
		`!define REASONIX_CLI "reasonix-cli.exe"`,
		`!define REASONIX_PORTABLE_ENTRY "Reasonix.exe"`,
		`!define REASONIX_LAYOUT_INSTALLER "reasonix-layout-installer.exe"`,
		`!define REASONIX_PAYLOAD_MANIFEST "reasonix-payload.json"`,
		`!define REASONIX_PAYLOAD_SIGNATURE "reasonix-payload.json.minisig"`,
		"Var ReasonixUpdateMode",
		"Var ReasonixStageMode",
		`${GetOptions} $R0 "/REASONIXUPDATE=" $R1`,
		`${GetOptions} $R0 "/REASONIXSTAGE=" $R2`,
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
		`FileOpen $1 "$INSTDIR\versions\v${INFO_PRODUCTVERSION}\${PRODUCT_EXECUTABLE}" a`,
		`FileOpen $1 "$INSTDIR\${REASONIX_GUARD}" a`,
		`FileOpen $1 "$INSTDIR\${REASONIX_LAUNCHER}" a`,
		`FileOpen $1 "$INSTDIR\${REASONIX_CLI}" a`,
		`FileOpen $1 "$INSTDIR\${REASONIX_PORTABLE_ENTRY}" a`,
		"SetErrorLevel 1618",
		"Call reasonix.waitForExecutableUnlock",
		`File "/oname=${REASONIX_UPDATE_HELPER}" "${REASONIX_UPDATE_HELPER}"`,
		`File "/oname=${REASONIX_CLI}" "${REASONIX_CLI}"`,
		`File "/oname=${REASONIX_LAYOUT_INSTALLER}" "${REASONIX_GUARD}"`,
		`nsExec::ExecToLog /OEM`,
		`Reasonix layout activator output:`,
		`--activate-staging "$R9" --no-relaunch`,
		`File "/oname=${REASONIX_PAYLOAD_MANIFEST}" "${REASONIX_PAYLOAD_MANIFEST}"`,
		`File "/oname=${REASONIX_PAYLOAD_SIGNATURE}" "${REASONIX_PAYLOAD_SIGNATURE}"`,
		`Delete "$INSTDIR\${REASONIX_UPDATE_HELPER}"`,
		`Delete "$INSTDIR\${REASONIX_CLI}"`,
		`DeleteRegValue HKCU "${REASONIX_LEGACY_PRODUCT_KEY}" ""`,
		`!insertmacro reasonix.deleteLegacyInstallerStateIfOwned`,
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
	stageBranch := strings.Index(script, "StrCmp $ReasonixStageMode \"1\" reasonix_stage_payload")
	if stageBranch < 0 || stageBranch > copyFiles {
		t.Fatalf("staging mode must bypass live executable unlock before payload extraction (branch=%d copy=%d)", stageBranch, copyFiles)
	}
	if !strings.Contains(script, "Goto reasonix_section_done") {
		t.Fatal("staging mode must skip registry, shortcuts, associations, and uninstaller")
	}
	if strings.Contains(script, `FileOpen $0 "$INSTDIR\current.json" w`) {
		t.Fatal("normal installer must delegate the current.json commit to the atomic Go activator")
	}
	writeCurrent := strings.Index(script, `!insertmacro reasonix.writeUninstaller`)
	deleteLegacy := strings.Index(script, `!insertmacro reasonix.deleteLegacyInstallerStateIfOwned`)
	if writeCurrent < 0 || deleteLegacy < 0 || writeCurrent > deleteLegacy {
		t.Fatalf("installer must write the current uninstall entry before reconciling owned legacy state (write=%d delete=%d)", writeCurrent, deleteLegacy)
	}
	legacyMacro := script[strings.Index(script, `!macro reasonix.deleteLegacyInstallerStateIfOwned`):strings.Index(script, `!macro reasonix.deleteUninstaller`)]
	deleteLegacyLocation := strings.Index(legacyMacro, `DeleteRegValue HKCU "${REASONIX_LEGACY_PRODUCT_KEY}" ""`)
	deleteLegacyAlias := strings.Index(legacyMacro, `DeleteRegKey HKCU "${REASONIX_LEGACY_UNINST_KEY}"`)
	if deleteLegacyLocation < 0 || deleteLegacyAlias < 0 || deleteLegacyLocation > deleteLegacyAlias {
		t.Fatalf("installer must clear the same-root Tauri install-location breadcrumb before deleting its uninstall alias (location=%d alias=%d)", deleteLegacyLocation, deleteLegacyAlias)
	}
	metadataBranch := strings.Index(script, `reasonix_stage_payload:`)
	metadataFile := strings.Index(script, `File "/oname=${REASONIX_PAYLOAD_MANIFEST}"`)
	normalInstall := strings.Index(script, `reasonix_normal_install:`)
	if metadataBranch < 0 || metadataFile < 0 || normalInstall < 0 || metadataBranch > metadataFile || metadataFile > normalInstall {
		t.Fatalf("payload manifest must be extracted only in staging mode (branch=%d file=%d)", metadataBranch, metadataFile)
	}
}

func TestWindowsInstallerUsesPreviousDirectoryAsManualInstallDefault(t *testing.T) {
	data, err := os.ReadFile("build/windows/installer/project.nsi")
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)
	for _, want := range []string{
		`InstallDirRegKey HKCU "${UNINST_KEY}" "InstallLocation"`,
		`InstallDir "${REASONIX_DEFAULT_INSTALLDIR}"`,
		`!insertmacro MUI_PAGE_DIRECTORY`,
		`!define MUI_PAGE_CUSTOMFUNCTION_PRE reasonix.skipSetupPageForUpdate`,
		`StrCmp $ReasonixUpdateMode "1" 0 reasonix_show_setup_page`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("project.nsi missing manual-install path contract %q", want)
		}
	}
	page := strings.Index(script, "!insertmacro MUI_PAGE_DIRECTORY")
	pageHook := strings.Index(script, "!define MUI_PAGE_CUSTOMFUNCTION_PRE reasonix.skipSetupPageForUpdate\n!insertmacro MUI_PAGE_DIRECTORY")
	if page < 0 || pageHook < 0 || pageHook > page {
		t.Fatal("directory selection page must remain available for manual installs")
	}
	if strings.Contains(script, `StrCpy $ReasonixUpdateMode "1"
	Goto reasonix_show_setup_page`) {
		t.Fatal("automatic updates must not reopen the manual directory selection page")
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
		`cp "build/windows/installer/$UPDATE_HELPER" "$payload_dir/$UPDATE_HELPER"`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("desktop-build.sh missing %q", want)
		}
	}

	packageData, err := os.ReadFile("../scripts/package-windows-desktop.sh")
	if err != nil {
		t.Fatal(err)
	}
	packager := string(packageData)
	for _, want := range []string{
		`cp "$PAYLOAD/$UPDATE_HELPER" "$INSTALLER_DIR/$UPDATE_HELPER"`,
		`cp "$PAYLOAD/$UPDATE_HELPER" "$portable_staging/versions/$version_label/$UPDATE_HELPER"`,
		`"$ROOT/scripts/verify-windows-portable.sh" "$portable_staging"`,
	} {
		if !strings.Contains(packager, want) {
			t.Fatalf("package-windows-desktop.sh missing %q", want)
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
	if !strings.Contains(source, "cmd := proc.Command(helperPath") || strings.Contains(source, "proc.VisibleCommand(helperPath") {
		t.Fatal("Windows handoff helper should stay hidden while NSIS shows update progress")
	}
	helperData, err := os.ReadFile("cmd/update-helper/main_windows.go")
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
