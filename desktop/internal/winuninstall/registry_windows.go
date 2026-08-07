//go:build windows

package winuninstall

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const (
	uninstallRegistryBase     = `Software\Microsoft\Windows\CurrentVersion\Uninstall\`
	legacyProductRegistryPath = `Software\reasonix\Reasonix`
)

// Reconcile refreshes the current Wails per-user registration after a
// successful version activation. It intentionally refuses legacy-only and
// portable trees; those require the full signed installer to establish a
// trustworthy uninstaller first.
func Reconcile(installRoot, version string) (bool, error) {
	installRoot = filepath.Clean(strings.TrimSpace(installRoot))
	if installRoot == "" || installRoot == "." || !filepath.IsAbs(installRoot) {
		return false, fmt.Errorf("reconcile Windows uninstall registration: invalid install root")
	}
	uninstaller := filepath.Join(installRoot, "uninstall.exe")
	info, err := os.Lstat(uninstaller)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return false, nil
	}

	current, err := readRegistration(CurrentKeyName)
	if err != nil {
		return false, err
	}
	legacy, err := readRegistration(LegacyKeyName)
	if err != nil {
		return false, err
	}
	plan, err := Plan(current, legacy, installRoot, version, true)
	if err != nil || !plan.Managed {
		return false, err
	}
	if err := writeRegistration(CurrentKeyName, plan.Desired); err != nil {
		return false, err
	}
	written, err := readRegistration(CurrentKeyName)
	if err != nil {
		return false, err
	}
	if !sameRegistration(written, &plan.Desired) {
		return false, fmt.Errorf("reconcile Windows uninstall registration: registry verification failed")
	}
	// Tauri 0.53 stored its install root separately from the Add/Remove
	// Programs entry and restores it before every later install. Remove only a
	// same-root default value so an old installer cannot overwrite the current
	// uninstaller; named values and separate legacy installations remain intact.
	if err := deleteInstallLocationIfOwned(legacyProductRegistryPath, installRoot); err != nil {
		return false, err
	}
	if plan.DeleteLegacy {
		if err := registry.DeleteKey(registry.CURRENT_USER, uninstallRegistryBase+LegacyKeyName); err != nil && !errors.Is(err, registry.ErrNotExist) {
			return false, err
		}
	}
	return true, nil
}

func deleteInstallLocationIfOwned(keyPath, installRoot string) error {
	key, err := registry.OpenKey(registry.CURRENT_USER, keyPath, registry.QUERY_VALUE|registry.SET_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer key.Close()

	location, _, err := key.GetStringValue("")
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !sameWindowsPath(location, installRoot) {
		return nil
	}
	if err := key.DeleteValue(""); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return err
	}
	return nil
}

func readRegistration(name string) (*Registration, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, uninstallRegistryBase+name, registry.QUERY_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer key.Close()
	read := func(value string) string {
		got, _, valueErr := key.GetStringValue(value)
		if valueErr != nil {
			return ""
		}
		return got
	}
	return &Registration{
		DisplayName:          read("DisplayName"),
		DisplayVersion:       read("DisplayVersion"),
		Publisher:            read("Publisher"),
		InstallLocation:      read("InstallLocation"),
		DisplayIcon:          read("DisplayIcon"),
		UninstallString:      read("UninstallString"),
		QuietUninstallString: read("QuietUninstallString"),
	}, nil
}

func writeRegistration(name string, value Registration) error {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, uninstallRegistryBase+name, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	for name, value := range map[string]string{
		"DisplayName":          value.DisplayName,
		"DisplayVersion":       value.DisplayVersion,
		"Publisher":            value.Publisher,
		"InstallLocation":      value.InstallLocation,
		"DisplayIcon":          value.DisplayIcon,
		"UninstallString":      value.UninstallString,
		"QuietUninstallString": value.QuietUninstallString,
	} {
		if err := key.SetStringValue(name, value); err != nil {
			return err
		}
	}
	if err := key.SetDWordValue("NoModify", 1); err != nil {
		return err
	}
	return key.SetDWordValue("NoRepair", 1)
}

func sameRegistration(left, right *Registration) bool {
	if left == nil || right == nil {
		return left == right
	}
	return left.DisplayName == right.DisplayName &&
		left.DisplayVersion == right.DisplayVersion &&
		left.Publisher == right.Publisher &&
		sameWindowsPath(left.InstallLocation, right.InstallLocation) &&
		sameWindowsPath(left.DisplayIcon, right.DisplayIcon) &&
		sameWindowsPath(uninstallExecutable(left.UninstallString), uninstallExecutable(right.UninstallString)) &&
		strings.EqualFold(strings.TrimSpace(left.QuietUninstallString), strings.TrimSpace(right.QuietUninstallString))
}
