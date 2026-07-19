package main

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestDesktopPackagesUseGuardAsDefaultLauncher(t *testing.T) {
	buildData, err := os.ReadFile("../scripts/desktop-build.sh")
	if err != nil {
		t.Fatal(err)
	}
	build := string(buildData)
	for _, want := range []string{
		`CLINAME="reasonix"`,
		`./cmd/reasonix`,
		`cp "$guard_out" "$app/Contents/MacOS/$GUARDNAME"`,
		`cp "$cli_out" "$app/Contents/MacOS/$CLINAME"`,
		`Set :CFBundleExecutable $GUARDNAME`,
		`Print :CFBundleIconFile`,
		`Contents/Resources/$bundle_icon`,
		`cp "$portable" "$staging/$BINNAME.exe"`,
		`-H windowsgui`,
		`stamp_windows_executable "$guard_out" "Reasonix Guard"`,
		`stamp_windows_executable "$launcher_out" "Reasonix Launcher"`,
		`stamp_windows_executable "build/windows/installer/$UPDATE_HELPER" "Reasonix Update Helper"`,
		`cp "$launcher_out" "$staging/${APPNAME}.exe"`,
		`cp "$guard_out" "$staging/$GUARDNAME.exe"`,
		`cp "build/windows/installer/$CLINAME.exe" "$staging/$CLINAME.exe"`,
		`"$BINNAME" "$GUARDNAME" "$CLINAME"`,
	} {
		if !strings.Contains(build, want) {
			t.Errorf("desktop-build.sh missing guard launcher contract %q", want)
		}
	}
	launcherStamp := strings.Index(build, `stamp_windows_executable "$launcher_out" "Reasonix Launcher"`)
	portableCopy := strings.Index(build, `cp "$launcher_out" "$staging/${APPNAME}.exe"`)
	if launcherStamp < 0 || portableCopy < 0 || launcherStamp > portableCopy {
		t.Fatalf("portable Reasonix.exe must copy the already-stamped launcher (stamp=%d copy=%d)", launcherStamp, portableCopy)
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
		"/usr/share/applications/reasonix.desktop",
		"/usr/share/pixmaps/reasonix-desktop.png",
		"/usr/share/icons/hicolor/scalable/apps/reasonix-desktop.svg",
	} {
		if !strings.Contains(nfpm, want) {
			t.Errorf("Linux package missing desktop identity asset %q", want)
		}
	}

	windowsData, err := os.ReadFile("build/windows/installer/project.nsi")
	if err != nil {
		t.Fatal(err)
	}
	windows := string(windowsData)
	for _, want := range []string{
		`File "/oname=${REASONIX_CLI}" "${REASONIX_CLI}"`,
		`CreateShortcut "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${REASONIX_LAUNCHER}" "launch --detach" "$INSTDIR\${PRODUCT_EXECUTABLE}" 0`,
		`CreateShortCut "$DESKTOP\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${REASONIX_LAUNCHER}" "launch --detach" "$INSTDIR\${PRODUCT_EXECUTABLE}" 0`,
	} {
		if !strings.Contains(windows, want) {
			t.Errorf("Windows installer missing guard shortcut contract %q", want)
		}
	}
}
