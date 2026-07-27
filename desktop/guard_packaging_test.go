package main

import (
	"fmt"
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

func TestVerifyWindowsPortableRejectsCaseCollisionsAndCLIOverwrite(t *testing.T) {
	verify := filepath.Join("..", "scripts", "verify-windows-portable.sh")
	good := t.TempDir()
	for _, name := range []string{
		"reasonix-desktop.exe",
		"reasonix-guard.exe",
		"reasonix-update-helper.exe",
	} {
		writePortableFixture(t, good, name, name)
	}
	writePortableFixture(t, good, "Reasonix.exe", "launcher")
	writePortableFixture(t, good, "reasonix-launcher.exe", "launcher")
	writePortableFixture(t, good, "reasonix-cli.exe", "cli")
	if out, err := exec.Command("bash", verify, good).CombinedOutput(); err != nil {
		t.Fatalf("valid portable fixture failed: %v\n%s", err, out)
	}

	overwritten := t.TempDir()
	for _, name := range []string{
		"reasonix-desktop.exe",
		"reasonix-guard.exe",
		"reasonix-update-helper.exe",
	} {
		writePortableFixture(t, overwritten, name, name)
	}
	writePortableFixture(t, overwritten, "Reasonix.exe", "cli")
	writePortableFixture(t, overwritten, "reasonix-launcher.exe", "launcher")
	writePortableFixture(t, overwritten, "reasonix-cli.exe", "cli")
	if out, err := exec.Command("bash", verify, overwritten).CombinedOutput(); err == nil || !strings.Contains(string(out), "not the packaged GUI launcher") {
		t.Fatalf("overwritten launcher result = %v, output %q", err, out)
	}

	// A case-sensitive test filesystem can represent the exact source-level
	// mistake that NTFS collapses into one overwritten file. Either filesystem
	// behavior must be rejected by the verifier.
	collision := t.TempDir()
	writePortableFixture(t, collision, "Reasonix.exe", "launcher")
	writePortableFixture(t, collision, "reasonix.exe", "cli")
	entries, err := os.ReadDir(collision)
	if err != nil {
		t.Fatal(err)
	}
	out, verifyErr := exec.Command("bash", verify, collision).CombinedOutput()
	if verifyErr == nil {
		t.Fatal("case-only portable entry names were accepted")
	}
	if len(entries) == 2 && !strings.Contains(string(out), "collide case-insensitively") {
		t.Fatalf("case-collision output = %q", out)
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
		`cp "$guard_out" "$app/Contents/MacOS/$GUARDNAME"`,
		`cp "$cli_out" "$app/Contents/MacOS/$CLINAME"`,
		`[ "$bundle_executable" = "$BINNAME" ]`,
		`Print :CFBundleIconFile`,
		`Contents/Resources/$bundle_icon`,
		`-H windowsgui`,
		`stamp_windows_executable "$guard_out" "Reasonix Guard"`,
		`stamp_windows_executable "$launcher_out" "Reasonix Launcher"`,
		`stamp_windows_executable "build/windows/installer/$UPDATE_HELPER" "Reasonix Update Helper"`,
		`payload_dir="$ROOT/desktop/build/windows/signing-payload"`,
		`cp "build/bin/$BINNAME.exe" "$payload_dir/$BINNAME.exe"`,
		`cp "$launcher_out" "$payload_dir/$LAUNCHERNAME.exe"`,
		`cp "$guard_out" "$payload_dir/$GUARDNAME.exe"`,
		`cp "build/windows/installer/$WINDOWS_CLINAME.exe" "$payload_dir/$WINDOWS_CLINAME.exe"`,
		`cp "build/windows/installer/reasonix-uninstall.exe" "$payload_dir/reasonix-uninstall.exe"`,
		`"$ROOT/scripts/package-windows-desktop.sh" "$arch" "$payload_dir"`,
		`"$BINNAME" "$GUARDNAME" "$CLINAME"`,
	} {
		if !strings.Contains(build, want) {
			t.Errorf("desktop-build.sh missing guard launcher contract %q", want)
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

	workflowData, err := os.ReadFile("../.github/workflows/release-desktop.yml")
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(workflowData)
	for _, platform := range []string{"windows/amd64", "windows/arm64"} {
		if !strings.Contains(workflow, "platform: "+platform) {
			t.Errorf("desktop release matrix missing resource-stamped target %s", platform)
		}
	}

	linuxData, err := os.ReadFile("build/linux/reasonix.desktop")
	if err != nil {
		t.Fatal(err)
	}
	linux := string(linuxData)
	for _, want := range []string{
		"Exec=reasonix-guard launch --detach",
		"Icon=reasonix-desktop",
		"StartupWMClass=reasonix-desktop",
	} {
		if !strings.Contains(linux, want) {
			t.Errorf("Linux desktop entry missing identity contract %q", want)
		}
	}
	nfpmData, err := os.ReadFile("build/linux/nfpm.yaml")
	if err != nil {
		t.Fatal(err)
	}
	nfpm := string(nfpmData)
	for _, size := range []int{16, 24, 32, 48, 64, 128, 256, 512} {
		asset := fmt.Sprintf("build/linux/icons/hicolor/%dx%d/apps/reasonix-desktop.png", size, size)
		if stat, err := os.Stat(asset); err != nil || stat.Size() == 0 {
			t.Errorf("Linux app icon %s is missing or empty", asset)
		}
		destination := fmt.Sprintf("/usr/share/icons/hicolor/%dx%d/apps/reasonix-desktop.png", size, size)
		if !strings.Contains(nfpm, destination) {
			t.Errorf("Linux package does not install %s", destination)
		}
	}
	for _, want := range []string{
		"/usr/bin/reasonix",
		"/usr/lib/reasonix/reasonix-update-helper",
		"/usr/share/polkit-1/actions/io.reasonix.desktop.update.policy",
		"/usr/share/applications/reasonix.desktop",
		"/usr/share/pixmaps/reasonix-desktop.png",
		"/usr/share/icons/hicolor/scalable/apps/reasonix-desktop.svg",
		"pkexec",
	} {
		if !strings.Contains(nfpm, want) {
			t.Errorf("Linux package missing desktop identity asset %q", want)
		}
	}
	if _, err := os.Stat("build/linux/io.reasonix.desktop.update.policy"); err != nil {
		t.Errorf("Polkit policy file missing: %v", err)
	}
	for _, want := range []string{
		`./cmd/update-helper`,
		`reasonix-update-helper`,
		`deb_version=`,
		`dpkg-deb --contents`,
		`dpkg-deb --field`,
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

	windowsData, err := os.ReadFile("build/windows/installer/project.nsi")
	if err != nil {
		t.Fatal(err)
	}
	windows := string(windowsData)
	for _, want := range []string{
		`File "/oname=${REASONIX_CLI}" "${REASONIX_CLI}"`,
		`!uninstfinalize 'cmd.exe /C copy /Y "%1" "reasonix-uninstall.exe" >NUL'`,
		`File "/oname=uninstall.exe" "${ARG_REASONIX_SIGNED_UNINSTALLER}"`,
		`CreateShortcut "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${REASONIX_LAUNCHER}" "launch --detach" "$INSTDIR\${PRODUCT_EXECUTABLE}" 0`,
		`CreateShortCut "$DESKTOP\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${REASONIX_LAUNCHER}" "launch --detach" "$INSTDIR\${PRODUCT_EXECUTABLE}" 0`,
	} {
		if !strings.Contains(windows, want) {
			t.Errorf("Windows installer missing guard shortcut contract %q", want)
		}
	}
}
