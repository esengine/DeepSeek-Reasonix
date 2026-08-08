package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func writePortableFixture(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyWindowsPortableVersionedLayout(t *testing.T) {
	verify := filepath.Join("..", "scripts", "verify-windows-portable.sh")
	good := t.TempDir()
	// versioned-v1 root entries
	writePortableFixture(t, good, "reasonix-launcher.exe", "launcher")
	writePortableFixture(t, good, "Reasonix.exe", "launcher")
	writePortableFixture(t, good, "reasonix-cli.exe", "cli")
	ver := filepath.Join(good, "versions", "v1.20.0")
	if err := os.MkdirAll(ver, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"reasonix-desktop.exe", "reasonix-cli.exe", "reasonix-update-helper.exe"} {
		writePortableFixture(t, ver, name, name)
	}
	if err := os.WriteFile(filepath.Join(good, "current.json"), []byte(`{
  "schemaVersion": 1,
  "activeVersion": "v1.20.0",
  "activeDir": "versions/v1.20.0"
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("bash", verify, good).CombinedOutput(); err != nil {
		t.Fatalf("valid versioned portable failed: %v\n%s", err, out)
	}

	// Flat Guard layout must be rejected.
	flat := t.TempDir()
	for _, name := range []string{
		"reasonix-desktop.exe",
		"reasonix-guard.exe",
		"reasonix-update-helper.exe",
		"reasonix-launcher.exe",
		"Reasonix.exe",
		"reasonix-cli.exe",
	} {
		writePortableFixture(t, flat, name, name)
	}
	if out, err := exec.Command("bash", verify, flat).CombinedOutput(); err == nil {
		t.Fatalf("flat portable with guard should fail, output=%s", out)
	}
}

func TestDesktopPackagesPreserveNativePlatformLaunchers(t *testing.T) {
	buildData, err := os.ReadFile("../scripts/desktop-build.sh")
	if err != nil {
		t.Fatal(err)
	}
	build := string(buildData)
	for _, want := range []string{
		`CLINAME="reasonix"`,
		`WINDOWS_CLINAME="reasonix-cli"`,
		`./cmd/reasonix`,
		`./cmd/reasonix-legacy-migrator`,
		`./cmd/reasonix-launcher`,
		`cp "$cli_out" "$app/Contents/MacOS/$CLINAME"`,
		`macOS bundle must not include $GUARDNAME`,
		`[ "$bundle_executable" = "$BINNAME" ]`,
		`Print :CFBundleIconFile`,
		`darwin_icon="$ROOT/desktop/build/darwin/icon.icns"`,
		`cp "$darwin_icon" "$app/Contents/Resources/$bundle_icon"`,
		`cmp -s "$darwin_icon" "$app/Contents/Resources/$bundle_icon"`,
		`Contents/Resources/$bundle_icon`,
		`-H windowsgui`,
		`stamp_windows_executable "$guard_out" "Reasonix Legacy Migrator"`,
		`stamp_windows_executable "$launcher_out" "Reasonix Launcher"`,
		`stamp_windows_executable "build/windows/installer/$UPDATE_HELPER" "Reasonix Update Helper"`,
		`payload_dir="$ROOT/desktop/build/windows/signing-payload"`,
		`cp "build/bin/$BINNAME.exe" "$payload_dir/$BINNAME.exe"`,
		`cp "$launcher_out" "$payload_dir/$LAUNCHERNAME.exe"`,
		`cp "$guard_out" "$payload_dir/$GUARDNAME.exe"`,
		`cp "build/windows/installer/$WINDOWS_CLINAME.exe" "$payload_dir/$WINDOWS_CLINAME.exe"`,
		`cp "build/windows/installer/reasonix-uninstall.exe" "$payload_dir/reasonix-uninstall.exe"`,
		`"$ROOT/scripts/package-windows-desktop.sh" "$arch" "$payload_dir"`,
		`"$BINNAME" "$LAUNCHERNAME" "$GUARDNAME" "$CLINAME"`,
		`Exec=reasonix-launcher`,
	} {
		if !strings.Contains(build, want) {
			t.Errorf("desktop-build.sh missing packaging contract %q", want)
		}
	}
	if strings.Contains(build, `Set :CFBundleExecutable $GUARDNAME`) {
		t.Fatal("macOS package must not replace the native Wails bundle executable with Guard")
	}
	launcherStamp := strings.Index(build, `stamp_windows_executable "$launcher_out" "Reasonix Launcher"`)
	payloadCopy := strings.Index(build, `cp "$launcher_out" "$payload_dir/$LAUNCHERNAME.exe"`)
	if launcherStamp < 0 || payloadCopy < 0 || launcherStamp > payloadCopy {
		t.Fatalf("Windows payload must copy the already-stamped launcher (stamp=%d copy=%d)", launcherStamp, payloadCopy)
	}
	if strings.Contains(build, `"$staging/$CLINAME.exe"`) {
		t.Fatal("Windows package must not collide reasonix.exe with the Reasonix.exe launcher")
	}
	darwinIconCopy := strings.Index(build, `cp "$darwin_icon" "$app/Contents/Resources/$bundle_icon"`)
	developerIDSign := strings.Index(build, `codesign --force --deep --timestamp --options runtime`)
	if darwinIconCopy < 0 || developerIDSign < 0 || darwinIconCopy > developerIDSign {
		t.Fatalf("macOS icon must be replaced before signing (icon=%d sign=%d)", darwinIconCopy, developerIDSign)
	}

	for _, want := range []string{
		`dpkg-deb --field "$deb_path" Package | grep -x 'reasonix-desktop'`,
		`usr/lib/reasonix/reasonix-update-helper`,
		`usr/share/polkit-1/actions/io.reasonix.desktop.update.policy`,
	} {
		if !strings.Contains(build, want) {
			t.Errorf("desktop-build.sh missing Linux deb helper contract %q", want)
		}
	}
	for _, unsafe := range []string{
		`dpkg-deb --field "$deb_path" Package | grep -qx`,
		`dpkg-deb --field "$deb_path" Version | grep -qx`,
		`dpkg-deb --field "$deb_path" Depends | grep -Fq`,
		`dpkg-deb --contents "$deb_path" | grep -Eq`,
	} {
		if strings.Contains(build, unsafe) {
			t.Errorf("desktop-build.sh uses early-exit grep under pipefail: %q", unsafe)
		}
	}

	desktopEntry, err := os.ReadFile("build/linux/reasonix.desktop")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(desktopEntry), "Exec=reasonix-launcher") || strings.Contains(string(desktopEntry), "reasonix-guard") {
		t.Fatal("Linux desktop entry must launch the permanent launcher without Guard")
	}
	nfpmData, err := os.ReadFile("build/linux/nfpm.yaml")
	if err != nil {
		t.Fatal(err)
	}
	nfpm := string(nfpmData)
	if !strings.Contains(nfpm, "dst: /usr/bin/reasonix-launcher") || strings.Contains(nfpm, "dst: /usr/bin/reasonix-guard") {
		t.Fatal("Linux deb must install the permanent launcher and must not persist Guard")
	}
	if !strings.Contains(nfpm, "postinstall: ./build/linux/postinstall.sh") {
		t.Fatal("Linux deb must refresh native desktop icon caches after install and upgrade")
	}
	if !strings.Contains(nfpm, "dst: /usr/share/applications/reasonix.desktop") {
		t.Fatal("Linux deb must install the Reasonix desktop entry")
	}
	postInstall, err := os.ReadFile("build/linux/postinstall.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"gtk-update-icon-cache", "update-desktop-database"} {
		if !strings.Contains(string(postInstall), want) {
			t.Errorf("Linux post-install icon repair missing %q", want)
		}
	}

	windowsData, err := os.ReadFile("build/windows/installer/project.nsi")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(windowsData, []byte{0xef, 0xbb, 0xbf}) {
		t.Fatal("Windows installer script must have a UTF-8 BOM so native makensis accepts localized strings")
	}
	windows := string(windowsData)
	for _, want := range []string{
		`File "/oname=${REASONIX_CLI}" "${REASONIX_CLI}"`,
		`!define REASONIX_UNINST_FINALIZE 'cmd.exe /C copy /Y "%1" "reasonix-uninstall.exe" >NUL'`,
		`!uninstfinalize '${REASONIX_UNINST_FINALIZE}'`,
		`File "/oname=uninstall.exe" "${ARG_REASONIX_SIGNED_UNINSTALLER}"`,
		`StrCpy $R9 "$INSTDIR\versions\.installer-v${INFO_PRODUCTVERSION}-$R8"`,
		`File "/oname=${REASONIX_LAYOUT_INSTALLER}" "${REASONIX_GUARD}"`,
		`nsExec::ExecToLog /OEM`,
		`Reasonix layout activator output:`,
		`--activate-staging "$R9" --no-relaunch`,
		`CreateShortcut "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${REASONIX_LAUNCHER}" "" "$INSTDIR\${REASONIX_LAUNCHER}" 0`,
		`CreateShortCut "$DESKTOP\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${REASONIX_LAUNCHER}" "" "$INSTDIR\${REASONIX_LAUNCHER}" 0`,
		`StrCmp $ReasonixStageMode "1" reasonix_stage_payload`,
		`File "/oname=${REASONIX_GUARD}" "${REASONIX_GUARD}"`,
	} {
		if !strings.Contains(windows, want) {
			t.Errorf("Windows installer missing versioned-layout contract %q", want)
		}
	}
	if strings.Contains(windows, `FileOpen $0 "$INSTDIR\current.json" w`) ||
		strings.Contains(windows, `SetOutPath "$INSTDIR\versions\v${INFO_PRODUCTVERSION}"`) {
		t.Fatal("normal Windows installer must not write the live version or current.json in place")
	}
	if strings.Contains(windows, `ExecWait '"$PLUGINSDIR\${REASONIX_LAYOUT_INSTALLER}"`) {
		t.Fatal("Windows installer must not discard layout activator stdout/stderr")
	}
	if strings.Contains(windows, `CreateShortcut "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${REASONIX_LAUNCHER}" "" "$INSTDIR\versions\v${INFO_PRODUCTVERSION}\${PRODUCT_EXECUTABLE}" 0`) ||
		strings.Contains(windows, `CreateShortCut "$DESKTOP\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${REASONIX_LAUNCHER}" "" "$INSTDIR\versions\v${INFO_PRODUCTVERSION}\${PRODUCT_EXECUTABLE}" 0`) {
		t.Fatal("Windows shortcut icon must not point into a version directory that retention removes")
	}
}
