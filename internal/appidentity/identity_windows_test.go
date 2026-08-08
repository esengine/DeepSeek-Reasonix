//go:build windows

package appidentity

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestOwnedShortcutTargetCoversStableAndVersionedEntries(t *testing.T) {
	root := filepath.Join(`C:\Program Files`, "Reasonix")
	tests := []struct {
		target string
		want   bool
	}{
		{filepath.Join(root, "reasonix-launcher.exe"), true},
		{filepath.Join(root, "Reasonix.exe"), true},
		{filepath.Join(root, "reasonix-desktop.exe"), true},
		{filepath.Join(root, "versions", "v1.20.0", "reasonix-desktop.exe"), true},
		{filepath.Join(root, "versions", "v1.20.0", "reasonix-cli.exe"), false},
		{filepath.Join(`D:\Apps`, "Reasonix", "reasonix-launcher.exe"), false},
	}
	for _, test := range tests {
		if got := ownedShortcutTarget(test.target, root); got != test.want {
			t.Errorf("ownedShortcutTarget(%q, %q) = %v, want %v", test.target, root, got, test.want)
		}
	}
}

func TestReasonixShortcutName(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"Reasonix.lnk", true},
		{"reasonix launcher.LNK", true},
		{"Reasonix (2).lnk", true},
		{"Other.lnk", false},
		{"Reasonix.exe", false},
	}
	for _, test := range tests {
		if got := reasonixShortcutName(test.name); got != test.want {
			t.Errorf("reasonixShortcutName(%q) = %v, want %v", test.name, got, test.want)
		}
	}
}

func TestOwnedShortcutTargetAcceptsVersionedDesktopThroughJunction(t *testing.T) {
	root := t.TempDir()
	versionDir := filepath.Join(root, "versions", "v1.20.0")
	if err := os.MkdirAll(versionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(versionDir, "reasonix-desktop.exe"), []byte("desktop"), 0o600); err != nil {
		t.Fatal(err)
	}
	junction := filepath.Join(t.TempDir(), "current")
	if output, err := exec.Command("cmd", "/c", "mklink", "/J", junction, root).CombinedOutput(); err != nil {
		t.Fatalf("create directory junction: %v: %s", err, output)
	}
	target := filepath.Join(junction, "versions", "v1.20.0", "reasonix-desktop.exe")
	if !ownedShortcutTarget(target, root) {
		t.Fatalf("junction target %q was not recognised under %q", target, root)
	}
}

func TestRepairOwnedShortcutPersistsAppUserModelID(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "Reasonix.exe")
	if err := os.WriteFile(target, []byte("launcher"), 0o600); err != nil {
		t.Fatal(err)
	}
	shortcutPath := filepath.Join(root, "Reasonix.lnk")

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	uninitialize, err := initializeCOM()
	if err != nil {
		t.Fatal(err)
	}
	defer uninitialize()
	createTestShortcut(t, shortcutPath, target)

	changed, err := repairOwnedShortcut(shortcutPath, root)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("first repair did not report a change")
	}
	shortcut, err := loadShortcut(shortcutPath, stgmReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	got, err := shortcut.appUserModelID()
	shortcut.release()
	if err != nil {
		t.Fatal(err)
	}
	if got != AppUserModelID {
		t.Fatalf("shortcut AppUserModelID = %q, want %q", got, AppUserModelID)
	}
	changed, err = repairOwnedShortcut(shortcutPath, root)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("second repair rewrote an already healthy shortcut")
	}
}

func TestRepairOwnedShortcutLeavesSeparateReasonix053InstallUntouched(t *testing.T) {
	currentRoot := t.TempDir()
	legacyRoot := t.TempDir()
	legacyTarget := filepath.Join(legacyRoot, "reasonix-desktop.exe")
	if err := os.WriteFile(legacyTarget, []byte("legacy tauri desktop"), 0o600); err != nil {
		t.Fatal(err)
	}
	shortcutPath := filepath.Join(t.TempDir(), "Reasonix.lnk")

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	uninitialize, err := initializeCOM()
	if err != nil {
		t.Fatal(err)
	}
	defer uninitialize()
	createTestShortcut(t, shortcutPath, legacyTarget)

	shortcut, err := loadShortcut(shortcutPath, stgmReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	if err := shortcut.setAppUserModelID(legacyTauriAppUserModelID); err != nil {
		shortcut.release()
		t.Fatal(err)
	}
	shortcut.release()

	changed, err := repairOwnedShortcut(shortcutPath, currentRoot)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("current install rewrote a shortcut owned by a separate Reasonix 0.53 installation")
	}

	shortcut, err = loadShortcut(shortcutPath, stgmReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	gotTarget, targetErr := shortcut.targetPath()
	gotID, idErr := shortcut.appUserModelID()
	shortcut.release()
	if targetErr != nil {
		t.Fatal(targetErr)
	}
	if idErr != nil {
		t.Fatal(idErr)
	}
	if !sameWindowsPathOrFile(gotTarget, legacyTarget) {
		t.Fatalf("legacy shortcut target = %q, want %q", gotTarget, legacyTarget)
	}
	if gotID != legacyTauriAppUserModelID {
		t.Fatalf("legacy shortcut AppUserModelID = %q, want %q", gotID, legacyTauriAppUserModelID)
	}
}

func createTestShortcut(t *testing.T, shortcutPath, target string) {
	t.Helper()
	var link *shellLinkW
	hr, _, _ := procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidShellLink)),
		0,
		clsctxInprocServer,
		uintptr(unsafe.Pointer(&iidIShellLinkW)),
		uintptr(unsafe.Pointer(&link)),
	)
	if err := checkHRESULT("CoCreateInstance(CLSID_ShellLink)", hr); err != nil {
		t.Fatal(err)
	}
	defer releaseInterface(unsafe.Pointer(link))
	targetPtr, err := windows.UTF16PtrFromString(target)
	if err != nil {
		t.Fatal(err)
	}
	hr, _, _ = syscall.SyscallN(
		link.VTable.SetPath,
		uintptr(unsafe.Pointer(link)),
		uintptr(unsafe.Pointer(targetPtr)),
	)
	if err := checkHRESULT("IShellLinkW.SetPath", hr); err != nil {
		t.Fatal(err)
	}
	var persist *persistFile
	if err := queryInterface(unsafe.Pointer(link), &iidIPersistFile, unsafe.Pointer(&persist)); err != nil {
		t.Fatal(err)
	}
	defer releaseInterface(unsafe.Pointer(persist))
	shortcutPtr, err := windows.UTF16PtrFromString(shortcutPath)
	if err != nil {
		t.Fatal(err)
	}
	hr, _, _ = syscall.SyscallN(
		persist.VTable.Save,
		uintptr(unsafe.Pointer(persist)),
		uintptr(unsafe.Pointer(shortcutPtr)),
		1,
	)
	if err := checkHRESULT("IPersistFile.Save", hr); err != nil {
		t.Fatal(err)
	}
}
