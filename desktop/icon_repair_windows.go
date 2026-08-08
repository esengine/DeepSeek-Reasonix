//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"unsafe"

	"github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
	"golang.org/x/sys/windows"

	"reasonix/internal/appidentity"
	"reasonix/internal/installlayout"
)

var (
	windowsKnownFolderPath      = windows.KnownFolderPath
	windowsRepairShortcut       = repairWindowsShortcut
	windowsNotifyShortcutChange = notifyWindowsShortcutChanged
)

func repairDesktopIconIntegration() error {
	executable, err := os.Executable()
	if err != nil {
		return nil
	}
	installRoot, err := installlayout.ResolveInstallRoot(executable)
	if err != nil || installRoot == "" {
		return nil
	}
	launcher := filepath.Join(installRoot, "reasonix-launcher.exe")
	if info, err := os.Lstat(launcher); err != nil || !info.Mode().IsRegular() {
		return nil
	}
	paths, err := reasonixWindowsShortcutPaths()
	if err != nil {
		return err
	}
	return errors.Join(
		repairExistingWindowsShortcuts(paths, launcher, windowsRepairShortcut),
		appidentity.RepairOwnedShortcuts(installRoot),
	)
}

func reasonixWindowsShortcutPaths() ([]string, error) {
	desktop, desktopErr := windowsKnownFolderPath(windows.FOLDERID_Desktop, windows.KF_FLAG_DEFAULT)
	programs, programsErr := windowsKnownFolderPath(windows.FOLDERID_Programs, windows.KF_FLAG_DEFAULT)
	if desktopErr != nil || programsErr != nil {
		return nil, errors.Join(desktopErr, programsErr)
	}
	return []string{
		filepath.Join(desktop, "Reasonix.lnk"),
		filepath.Join(programs, "Reasonix.lnk"),
	}, nil
}

func repairExistingWindowsShortcuts(paths []string, launcher string, repair func(string, string) (bool, error)) error {
	var repairErr error
	for _, path := range paths {
		info, err := os.Lstat(path)
		if err != nil {
			if !os.IsNotExist(err) {
				repairErr = errors.Join(repairErr, err)
			}
			continue
		}
		if !info.Mode().IsRegular() {
			continue
		}
		repaired, err := repair(path, launcher)
		if err != nil {
			repairErr = errors.Join(repairErr, fmt.Errorf("%s: %w", path, err))
			continue
		}
		if repaired {
			windowsNotifyShortcutChange(path)
		}
	}
	return repairErr
}

func repairWindowsShortcut(shortcutPath, launcher string) (bool, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := ole.CoInitializeEx(0, ole.COINIT_APARTMENTTHREADED); err != nil {
		return false, err
	}
	defer ole.CoUninitialize()

	unknown, err := oleutil.CreateObject("WScript.Shell")
	if err != nil {
		return false, err
	}
	defer unknown.Release()
	shell, err := unknown.QueryInterface(ole.IID_IDispatch)
	if err != nil {
		return false, err
	}
	defer shell.Release()
	created, err := oleutil.CallMethod(shell, "CreateShortcut", shortcutPath)
	if err != nil {
		return false, err
	}
	shortcut := created.ToIDispatch()
	if shortcut == nil {
		_ = created.Clear()
		return false, fmt.Errorf("WScript.Shell returned no shortcut object")
	}
	defer shortcut.Release()

	targetValue, err := oleutil.GetProperty(shortcut, "TargetPath")
	if err != nil {
		return false, fmt.Errorf("read TargetPath: %w", err)
	}
	target := targetValue.ToString()
	_ = targetValue.Clear()
	if !reasonixWindowsShortcutTarget(target, launcher) {
		return false, nil
	}
	iconValue, err := oleutil.GetProperty(shortcut, "IconLocation")
	if err != nil {
		return false, fmt.Errorf("read IconLocation: %w", err)
	}
	iconLocation := iconValue.ToString()
	_ = iconValue.Clear()
	repointTarget, fixIcon := repairWindowsShortcutPlan(target, iconLocation, launcher)
	if !repointTarget && !fixIcon {
		return false, nil
	}
	if repointTarget {
		// A version-scoped target points into versions/<v>/reasonix-desktop.exe,
		// which the updater deletes when it switches or prunes versions. Repoint
		// it at the stable launcher so the shortcut survives updates.
		result, err := oleutil.PutProperty(shortcut, "TargetPath", launcher)
		if result != nil {
			_ = result.Clear()
		}
		if err != nil {
			return false, fmt.Errorf("set TargetPath: %w", err)
		}
		result, err = oleutil.PutProperty(shortcut, "WorkingDirectory", filepath.Dir(launcher))
		if result != nil {
			_ = result.Clear()
		}
		if err != nil {
			return false, fmt.Errorf("set WorkingDirectory: %w", err)
		}
	}
	if fixIcon {
		result, err := oleutil.PutProperty(shortcut, "IconLocation", launcher+",0")
		if result != nil {
			_ = result.Clear()
		}
		if err != nil {
			return false, fmt.Errorf("set IconLocation: %w", err)
		}
	}
	result, err := oleutil.CallMethod(shortcut, "Save")
	if result != nil {
		_ = result.Clear()
	}
	return err == nil, err
}

func reasonixWindowsShortcutTarget(target, launcher string) bool {
	target = filepath.Clean(strings.TrimSpace(target))
	launcher = filepath.Clean(strings.TrimSpace(launcher))
	if target == "." || launcher == "." {
		return false
	}
	root := filepath.Dir(launcher)
	for _, owned := range []string{
		launcher,
		filepath.Join(root, "Reasonix.exe"),
		filepath.Join(root, "reasonix-desktop.exe"),
	} {
		if strings.EqualFold(target, owned) {
			return true
		}
	}
	return reasonixWindowsVersionedTarget(target, launcher)
}

// reasonixWindowsVersionedTarget reports whether target points into this
// install's versions/<version>/reasonix-desktop.exe, a version-scoped path
// that the updater deletes when it switches or prunes versions. Such targets
// dangle after an update, so repair must repoint them at the stable launcher.
func reasonixWindowsVersionedTarget(target, launcher string) bool {
	root := filepath.Dir(filepath.Clean(strings.TrimSpace(launcher)))
	rel, err := filepath.Rel(root, filepath.Clean(strings.TrimSpace(target)))
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	parts := strings.Split(rel, string(filepath.Separator))
	return len(parts) == 3 && strings.EqualFold(parts[0], "versions") &&
		strings.EqualFold(parts[2], "reasonix-desktop.exe")
}

// repairWindowsShortcutPlan decides which owned-shortcut properties need
// rewriting. repointTarget is true when TargetPath points into a versioned
// directory the updater can delete; fixIcon is true when IconLocation points
// at the versioned desktop binary instead of the stable launcher.
func repairWindowsShortcutPlan(target, iconLocation, launcher string) (repointTarget, fixIcon bool) {
	return reasonixWindowsVersionedTarget(target, launcher), reasonixWindowsStaleIcon(iconLocation, launcher)
}

func reasonixWindowsStaleIcon(iconLocation, launcher string) bool {
	iconPath := strings.TrimSpace(iconLocation)
	if comma := strings.LastIndex(iconPath, ","); comma >= 0 {
		if _, err := strconv.Atoi(strings.TrimSpace(iconPath[comma+1:])); err == nil {
			iconPath = iconPath[:comma]
		}
	}
	iconPath = strings.Trim(strings.TrimSpace(iconPath), `"`)
	if iconPath == "" {
		return false
	}
	root := filepath.Dir(filepath.Clean(strings.TrimSpace(launcher)))
	rel, err := filepath.Rel(root, filepath.Clean(iconPath))
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	parts := strings.Split(rel, string(filepath.Separator))
	return len(parts) == 3 && strings.EqualFold(parts[0], "versions") &&
		strings.EqualFold(parts[2], "reasonix-desktop.exe")
}

func notifyWindowsShortcutChanged(path string) {
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return
	}
	// SHCNE_UPDATEITEM + SHCNF_PATHW asks Explorer to invalidate only this
	// shortcut instead of flushing the entire association cache.
	const (
		shcneUpdateItem = 0x00002000
		shcnfPathW      = 0x0005
	)
	proc := windows.NewLazySystemDLL("shell32.dll").NewProc("SHChangeNotify")
	_, _, _ = proc.Call(shcneUpdateItem, shcnfPathW, uintptr(unsafe.Pointer(pathPtr)), 0)
}
