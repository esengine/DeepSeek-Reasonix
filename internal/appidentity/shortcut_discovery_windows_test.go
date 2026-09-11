//go:build windows

package appidentity

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestRepairOwnedShortcutsDiscoversPublicAndProductProgramFolders(t *testing.T) {
	migrationTestCOM(t)
	root := t.TempDir()
	launcher := filepath.Join(root, "reasonix-launcher.exe")
	migrationTestFile(t, launcher)
	studioTarget := filepath.Join(t.TempDir(), "reasonix-studio.exe")
	migrationTestFile(t, studioTarget)

	// Every known-folder request is confined to this temporary tree. Unknown
	// requests fail instead of falling back to the machine's real shell folders.
	shellRoot := t.TempDir()
	folders := map[windows.KNOWNFOLDERID]string{
		*windows.FOLDERID_Desktop:        filepath.Join(shellRoot, "Desktop"),
		*windows.FOLDERID_PublicDesktop:  filepath.Join(shellRoot, "PublicDesktop"),
		*windows.FOLDERID_Programs:       filepath.Join(shellRoot, "Programs"),
		*windows.FOLDERID_CommonPrograms: filepath.Join(shellRoot, "CommonPrograms"),
		*windows.FOLDERID_RoamingAppData: filepath.Join(shellRoot, "Roaming"),
	}
	originalKnownFolderPath := knownFolderPath
	knownFolderPath = func(id *windows.KNOWNFOLDERID, _ uint32) (string, error) {
		if id != nil {
			if path, ok := folders[*id]; ok {
				return path, nil
			}
		}
		return "", fmt.Errorf("unexpected known-folder request in isolated test")
	}
	t.Cleanup(func() { knownFolderPath = originalKnownFolderPath })

	locations := []string{
		folders[*windows.FOLDERID_Desktop],
		folders[*windows.FOLDERID_PublicDesktop],
		folders[*windows.FOLDERID_Programs],
		folders[*windows.FOLDERID_CommonPrograms],
		filepath.Join(folders[*windows.FOLDERID_Programs], "Reasonix"),
		filepath.Join(folders[*windows.FOLDERID_CommonPrograms], "Reasonix"),
		filepath.Join(folders[*windows.FOLDERID_RoamingAppData], "Microsoft", "Internet Explorer", "Quick Launch", "User Pinned", "TaskBar"),
	}
	var ownedPaths []string
	studioBefore := make(map[string][]byte)
	for _, location := range locations {
		if err := os.MkdirAll(location, 0o755); err != nil {
			t.Fatal(err)
		}
		owned := filepath.Join(location, "Reasonix.lnk")
		migrationTestShortcut(t, owned, migrationShortcutState{
			target: launcher, id: "Reasonix", icon: launcher, workingDirectory: root, showCmd: 1,
		})
		ownedPaths = append(ownedPaths, owned)
		studio := filepath.Join(location, "Reasonix Studio.lnk")
		migrationTestShortcut(t, studio, migrationShortcutState{
			target: studioTarget, id: "Reasonix", icon: studioTarget, workingDirectory: filepath.Dir(studioTarget), showCmd: 1,
		})
		studioBefore[studio] = migrationReadBytes(t, studio)
	}

	candidates, err := shortcutCandidates(root)
	if err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]bool)
	for _, path := range candidates {
		seen[strings.ToLower(filepath.Clean(path))] = true
	}
	for _, path := range ownedPaths {
		if !seen[strings.ToLower(filepath.Clean(path))] {
			t.Errorf("owned shortcut was not discovered: %s", path)
		}
	}
	if err := RepairOwnedShortcuts(root); err != nil {
		t.Fatal(err)
	}
	for _, path := range ownedPaths {
		got := migrationReadShortcut(t, path)
		if got.id != "io.reasonix.desktop" || !migrationSamePath(got.target, launcher) {
			t.Errorf("discovered shortcut was not repaired: %s: %+v", path, got)
		}
	}
	for path, before := range studioBefore {
		if !bytes.Equal(before, migrationReadBytes(t, path)) {
			t.Errorf("discovery repair changed independently installed Studio shortcut: %s", path)
		}
	}
}
