//go:build windows

package appidentity

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestShortcutMigrationAcceptsShortAndLongPathsForTheSameFile(t *testing.T) {
	migrationTestCOM(t)
	root := filepath.Join(t.TempDir(), "Reasonix identity validation")
	launcher := filepath.Join(root, "reasonix-launcher.exe")
	target := filepath.Join(root, "versions", "v1.38.6", "app", "Reasonix.exe")
	migrationTestFile(t, launcher)
	migrationTestFile(t, target)
	rootPtr, err := windows.UTF16PtrFromString(root)
	if err != nil {
		t.Fatal(err)
	}
	buffer := make([]uint16, windowsPathBuffer)
	n, err := windows.GetShortPathName(rootPtr, &buffer[0], uint32(len(buffer)))
	if err != nil || n >= uint32(len(buffer)) {
		t.Fatalf("short path: length=%d err=%v", n, err)
	}
	shortRoot := windows.UTF16ToString(buffer[:n])
	if strings.EqualFold(shortRoot, root) {
		t.Skip("the test volume does not create DOS short names")
	}
	path := filepath.Join(root, "Reasonix.lnk")
	migrationTestShortcut(t, path, migrationShortcutState{
		target: filepath.Join(shortRoot, "versions", "v1.38.6", "app", "Reasonix.exe"),
		id:     "Reasonix", workingDirectory: shortRoot, showCmd: 1,
	})
	changed, err := repairOwnedShortcut(path, shortRoot)
	if err != nil || !changed {
		t.Fatalf("short-root repair = %t, %v", changed, err)
	}
	got := migrationReadShortcut(t, path)
	if !migrationSamePath(got.target, launcher) || got.id != AppUserModelID {
		t.Fatalf("short-root repair returned %+v", got)
	}
	other := filepath.Join(t.TempDir(), "reasonix-launcher.exe")
	if err := os.WriteFile(other, []byte("another installation"), 0o600); err != nil {
		t.Fatal(err)
	}
	if migrationSamePath(got.target, other) {
		t.Fatal("different files with the same executable name must not compare equal")
	}
}
