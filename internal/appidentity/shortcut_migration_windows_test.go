//go:build windows

package appidentity

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestRepairOwnedShortcutMigratesVersionedElectronAndPreservesProperties(t *testing.T) {
	for _, id := range []string{"Reasonix", ""} {
		for _, customIcon := range []bool{false, true} {
			name := id
			if name == "" {
				name = "no_identity"
			}
			if customIcon {
				name += "_custom_icon"
			}
			t.Run(name, func(t *testing.T) {
				migrationTestCOM(t)
				root := t.TempDir()
				launcher := filepath.Join(root, "reasonix-launcher.exe")
				versionDir := filepath.Join(root, "versions", "v1.20.0")
				target := filepath.Join(versionDir, "app", "Reasonix.exe")
				migrationTestFile(t, launcher)
				migrationTestFile(t, target)
				icon, iconIndex := target, int32(0)
				if customIcon {
					icon = filepath.Join(t.TempDir(), "custom.ico")
					iconIndex = 2
					migrationTestFile(t, icon)
				}
				path := filepath.Join(t.TempDir(), "Reasonix.lnk")
				before := migrationShortcutState{
					target: target, id: id, arguments: `--session "work space"`,
					description: "My Reasonix workspace", workingDirectory: filepath.Dir(target),
					icon: icon, iconIndex: iconIndex, showCmd: 3,
				}
				migrationTestShortcut(t, path, before)

				changed, err := repairOwnedShortcut(path, root)
				if err != nil || !changed {
					t.Fatalf("repair = (%v, %v), want (true, nil)", changed, err)
				}
				got := migrationReadShortcut(t, path)
				if !migrationSamePath(got.target, launcher) || got.id != "io.reasonix.desktop" {
					t.Fatalf("migrated target/id = %q/%q, want %q/io.reasonix.desktop", got.target, got.id, launcher)
				}
				if got.arguments != before.arguments || got.description != before.description || got.showCmd != before.showCmd {
					t.Fatalf("repair changed unrelated properties: got %+v, before %+v", got, before)
				}
				if !migrationSamePath(got.workingDirectory, root) {
					t.Fatalf("working directory = %q, want stable installation root %q", got.workingDirectory, root)
				}
				wantIcon := launcher
				if customIcon {
					wantIcon = icon
				}
				if !migrationSamePath(got.icon, wantIcon) || got.iconIndex != iconIndex {
					t.Fatalf("icon = %q,%d, want %q,%d", got.icon, got.iconIndex, wantIcon, iconIndex)
				}

				after := migrationReadBytes(t, path)
				changed, err = repairOwnedShortcut(path, root)
				if err != nil || changed {
					t.Fatalf("second repair = (%v, %v), want (false, nil)", changed, err)
				}
				if !bytes.Equal(after, migrationReadBytes(t, path)) {
					t.Fatal("second repair changed an already migrated .lnk")
				}
				if err := os.RemoveAll(versionDir); err != nil {
					t.Fatal(err)
				}
				got = migrationReadShortcut(t, path)
				if _, err := os.Stat(got.target); err != nil {
					t.Fatalf("shortcut target disappeared when old version was removed: %v", err)
				}
			})
		}
	}
}

func TestRepairOwnedShortcutLeavesSeparateStudioInstallationsByteIdentical(t *testing.T) {
	for _, test := range []struct{ name, executable, id string }{
		{"legacy_studio", "reasonix-studio.exe", "Reasonix"},
		{"electron_studio", "Reasonix Studio.exe", "io.reasonix.studio"},
	} {
		t.Run(test.name, func(t *testing.T) {
			migrationTestCOM(t)
			root, studioRoot := t.TempDir(), t.TempDir()
			migrationTestFile(t, filepath.Join(root, "reasonix-launcher.exe"))
			target := filepath.Join(studioRoot, test.executable)
			migrationTestFile(t, target)
			path := filepath.Join(t.TempDir(), "Reasonix Studio.lnk")
			migrationTestShortcut(t, path, migrationShortcutState{
				target: target, id: test.id, arguments: "--workspace studio",
				description: "Studio", workingDirectory: studioRoot, icon: target, showCmd: 1,
			})
			migrationAssertUnchanged(t, path, root)
		})
	}
}

func TestRepairOwnedShortcutPreservesExplicitForeignIdentityInsideCurrentInstall(t *testing.T) {
	for _, id := range []string{"io.reasonix.studio", "dev.reasonix.desktop", "com.example.custom"} {
		t.Run(id, func(t *testing.T) {
			migrationTestCOM(t)
			root := t.TempDir()
			target := filepath.Join(root, "versions", "v1.20.0", "app", "Reasonix.exe")
			migrationTestFile(t, target)
			migrationTestFile(t, filepath.Join(root, "reasonix-launcher.exe"))
			path := filepath.Join(root, "Reasonix.lnk")
			migrationTestShortcut(t, path, migrationShortcutState{
				target: target, id: id, workingDirectory: filepath.Dir(target), icon: target, showCmd: 1,
			})
			migrationAssertUnchanged(t, path, root)
		})
	}
}

func TestRepairOwnedShortcutRejectsVersionJunctionEscapingCurrentInstall(t *testing.T) {
	for _, executable := range []string{"reasonix-desktop.exe", filepath.Join("app", "Reasonix.exe")} {
		t.Run(executable, func(t *testing.T) {
			migrationTestCOM(t)
			root, externalRoot := t.TempDir(), t.TempDir()
			migrationTestFile(t, filepath.Join(root, "reasonix-launcher.exe"))
			migrationTestFile(t, filepath.Join(externalRoot, executable))
			versions := filepath.Join(root, "versions")
			if err := os.MkdirAll(versions, 0o755); err != nil {
				t.Fatal(err)
			}
			junction := filepath.Join(versions, "v1.20.0")
			if output, err := exec.Command("cmd", "/c", "mklink", "/J", junction, externalRoot).CombinedOutput(); err != nil {
				t.Fatalf("create directory junction: %v: %s", err, output)
			}
			target := filepath.Join(junction, executable)
			path := filepath.Join(root, "Reasonix.lnk")
			migrationTestShortcut(t, path, migrationShortcutState{
				target: target, id: "Reasonix", workingDirectory: filepath.Dir(target), icon: target, showCmd: 1,
			})
			migrationAssertUnchanged(t, path, root)
		})
	}
}

func TestRepairOwnedShortcutRepairsFlatElectronAndPartiallyMigratedLinks(t *testing.T) {
	for _, test := range []struct {
		name, target, icon, id string
	}{
		{"flat Electron", filepath.Join("app", "Reasonix.exe"), filepath.Join("app", "Reasonix.exe"), "Reasonix"},
		{"new identity with version target", filepath.Join("versions", "v1.20.0", "app", "Reasonix.exe"), "reasonix-launcher.exe", "io.reasonix.desktop"},
		{"stable target with version icon", "reasonix-launcher.exe", filepath.Join("versions", "v1.20.0", "app", "Reasonix.exe"), "io.reasonix.desktop"},
	} {
		t.Run(test.name, func(t *testing.T) {
			migrationTestCOM(t)
			root := t.TempDir()
			launcher := filepath.Join(root, "reasonix-launcher.exe")
			target, icon := filepath.Join(root, test.target), filepath.Join(root, test.icon)
			for _, path := range []string{launcher, target, icon, filepath.Join(root, "reasonix-desktop.exe")} {
				migrationTestFile(t, path)
			}
			path := filepath.Join(root, "Reasonix.lnk")
			migrationTestShortcut(t, path, migrationShortcutState{
				target: target, id: test.id, icon: icon,
				workingDirectory: root, arguments: `--session "saved session"`, description: "Saved workspace", showCmd: 3,
			})
			changed, err := repairOwnedShortcut(path, root)
			if err != nil || !changed {
				t.Fatalf("repair = (%v, %v), want (true, nil)", changed, err)
			}
			got := migrationReadShortcut(t, path)
			if !migrationSamePath(got.target, launcher) || !migrationSamePath(got.icon, launcher) || got.id != "io.reasonix.desktop" {
				t.Fatalf("shortcut was not fully migrated: %+v", got)
			}
			if got.arguments != `--session "saved session"` || got.description != "Saved workspace" || got.showCmd != 3 || !migrationSamePath(got.workingDirectory, root) {
				t.Fatalf("repair changed unrelated properties: %+v", got)
			}
			before := migrationReadBytes(t, path)
			changed, err = repairOwnedShortcut(path, root)
			if err != nil || changed || !bytes.Equal(before, migrationReadBytes(t, path)) {
				t.Fatalf("second repair must leave shortcut unchanged: changed=%v err=%v", changed, err)
			}
		})
	}
}

type migrationShortcutState struct {
	target, id, arguments, description, workingDirectory, icon string
	iconIndex, showCmd                                         int32
}

func migrationTestCOM(t *testing.T) {
	t.Helper()
	runtime.LockOSThread()
	uninitialize, err := initializeCOM()
	if err != nil {
		runtime.UnlockOSThread()
		t.Fatal(err)
	}
	t.Cleanup(func() { uninitialize(); runtime.UnlockOSThread() })
}

func migrationTestFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func migrationTestShortcut(t *testing.T, path string, state migrationShortcutState) {
	t.Helper()
	createTestShortcut(t, path, state.target)
	s, err := loadShortcut(path, stgmReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	defer s.release()
	for _, field := range []struct {
		method uintptr
		value  string
	}{
		{s.link.VTable.SetArguments, state.arguments},
		{s.link.VTable.SetDescription, state.description},
		{s.link.VTable.SetWorkingDirectory, state.workingDirectory},
	} {
		value, err := windows.UTF16PtrFromString(field.value)
		if err != nil {
			t.Fatal(err)
		}
		hr, _, _ := syscall.SyscallN(field.method, uintptr(unsafe.Pointer(s.link)), uintptr(unsafe.Pointer(value)))
		if err := checkHRESULT("set shortcut fixture property", hr); err != nil {
			t.Fatal(err)
		}
	}
	icon, err := windows.UTF16PtrFromString(state.icon)
	if err != nil {
		t.Fatal(err)
	}
	hr, _, _ := syscall.SyscallN(s.link.VTable.SetIconLocation, uintptr(unsafe.Pointer(s.link)), uintptr(unsafe.Pointer(icon)), uintptr(state.iconIndex))
	if err := checkHRESULT("SetIconLocation", hr); err != nil {
		t.Fatal(err)
	}
	hr, _, _ = syscall.SyscallN(s.link.VTable.SetShowCmd, uintptr(unsafe.Pointer(s.link)), uintptr(state.showCmd))
	if err := checkHRESULT("SetShowCmd", hr); err != nil {
		t.Fatal(err)
	}
	// The existing setter commits and persists all Shell Link properties.
	if err := s.setAppUserModelID(state.id); err != nil {
		t.Fatal(err)
	}
}

func migrationReadShortcut(t *testing.T, path string) migrationShortcutState {
	t.Helper()
	s, err := loadShortcut(path, stgmReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	defer s.release()
	var state migrationShortcutState
	state.target, err = s.targetPath()
	if err != nil {
		t.Fatal(err)
	}
	state.id, err = s.appUserModelID()
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []struct {
		method uintptr
		value  *string
	}{
		{s.link.VTable.GetArguments, &state.arguments},
		{s.link.VTable.GetDescription, &state.description},
		{s.link.VTable.GetWorkingDirectory, &state.workingDirectory},
	} {
		buffer := make([]uint16, windowsPathBuffer)
		hr, _, _ := syscall.SyscallN(field.method, uintptr(unsafe.Pointer(s.link)), uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)))
		if err := checkHRESULT("get shortcut property", hr); err != nil {
			t.Fatal(err)
		}
		*field.value = windows.UTF16ToString(buffer)
	}
	buffer := make([]uint16, windowsPathBuffer)
	hr, _, _ := syscall.SyscallN(s.link.VTable.GetIconLocation, uintptr(unsafe.Pointer(s.link)), uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)), uintptr(unsafe.Pointer(&state.iconIndex)))
	if err := checkHRESULT("GetIconLocation", hr); err != nil {
		t.Fatal(err)
	}
	state.icon = windows.UTF16ToString(buffer)
	hr, _, _ = syscall.SyscallN(s.link.VTable.GetShowCmd, uintptr(unsafe.Pointer(s.link)), uintptr(unsafe.Pointer(&state.showCmd)))
	if err := checkHRESULT("GetShowCmd", hr); err != nil {
		t.Fatal(err)
	}
	return state
}

func migrationReadBytes(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func migrationAssertUnchanged(t *testing.T, path, root string) {
	t.Helper()
	before := migrationReadBytes(t, path)
	changed, err := repairOwnedShortcut(path, root)
	if err != nil || changed {
		t.Fatalf("foreign shortcut repair = (%v, %v), want (false, nil)", changed, err)
	}
	if !bytes.Equal(before, migrationReadBytes(t, path)) {
		t.Fatal("foreign shortcut bytes changed")
	}
}

func migrationSamePath(left, right string) bool {
	if strings.EqualFold(filepath.Clean(left), filepath.Clean(right)) {
		return true
	}
	leftInfo, leftErr := os.Stat(left)
	rightInfo, rightErr := os.Stat(right)
	return leftErr == nil && rightErr == nil && os.SameFile(leftInfo, rightInfo)
}

func TestRepairShortcutsLeavesReadOnlyShortcutByteIdentical(t *testing.T) {
	migrationTestCOM(t)
	root := t.TempDir()
	launcher := filepath.Join(root, "reasonix-launcher.exe")
	target := filepath.Join(root, "versions", "v1.20.0", "app", "Reasonix.exe")
	migrationTestFile(t, launcher)
	migrationTestFile(t, target)
	path := filepath.Join(root, "Reasonix.lnk")
	migrationTestShortcut(t, path, migrationShortcutState{
		target: target, id: "Reasonix", icon: target, workingDirectory: filepath.Dir(target), showCmd: 1,
	})
	before := migrationReadBytes(t, path)
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	attributes, err := windows.GetFileAttributes(pathPtr)
	if err != nil {
		t.Fatal(err)
	}
	// Registered after TempDir so the read-only flag is removed before its cleanup.
	t.Cleanup(func() {
		if err := windows.SetFileAttributes(pathPtr, attributes); err != nil {
			t.Errorf("restore shortcut file attributes: %v", err)
		}
	})
	if err := windows.SetFileAttributes(pathPtr, attributes|windows.FILE_ATTRIBUTE_READONLY); err != nil {
		t.Fatal(err)
	}
	if err := RepairShortcuts(root, []string{path}); err == nil {
		t.Fatal("repair of a read-only shortcut must return an error")
	}
	if !bytes.Equal(before, migrationReadBytes(t, path)) {
		t.Fatal("failed repair changed the read-only shortcut bytes")
	}
}

func TestRepairShortcutsValidatesAllPathsBeforeWritingAnyShortcut(t *testing.T) {
	migrationTestCOM(t)
	root := t.TempDir()
	launcher := filepath.Join(root, "reasonix-launcher.exe")
	target := filepath.Join(root, "versions", "v1.20.0", "app", "Reasonix.exe")
	migrationTestFile(t, launcher)
	migrationTestFile(t, target)
	path := filepath.Join(root, "Reasonix.lnk")
	migrationTestShortcut(t, path, migrationShortcutState{
		target: target, id: "Reasonix", icon: target, workingDirectory: filepath.Dir(target), showCmd: 1,
	})
	before := migrationReadBytes(t, path)
	if err := RepairShortcuts(root, []string{path, "relative.lnk"}); err == nil {
		t.Fatal("repair must reject the later relative path")
	}
	if !bytes.Equal(before, migrationReadBytes(t, path)) {
		t.Fatal("repair wrote the first shortcut before validating the full path list")
	}
}
