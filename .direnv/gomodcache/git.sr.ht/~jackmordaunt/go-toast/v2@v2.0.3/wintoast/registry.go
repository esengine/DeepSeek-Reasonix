//go:build windows

// This file contains registry manipulation code.
// This logic is orthogonal to, but works in tandem with the COM code; since the
// Windows Runtime uses the registry as it's primary source of state.
package wintoast

import (
	"fmt"
	"path/filepath"
	"sync"

	"golang.org/x/sys/windows/registry"
)

var (
	// allows diffing the new call from the previous so that we can early-out,
	// and avoid touching the registry more than necessary.
	// It also allows empty app data to be supplied to the Notifcation type,
	// without erasing the data that has been set via the global function.
	appData   AppData
	appDataMu sync.Mutex
)

// Overridden in testing.
var (
	writeStringValue = writeStringValueImpl
	setAppDataFunc   = setAppDataImpl
)

var (
	// appKeyRoot is the root path for app metadata.
	appKeyRoot = filepath.Join("SOFTWARE", "Classes", "AppUserModelId")
	// activationKey is the root path to the activation executable.
	activationKey = filepath.Join("SOFTWARE", "Classes", "CLSID", GUID_ImplNotificationActivationCallback.String(), "LocalServer32")
)

// The Windows registry package uses empty string for the "(Default)" key.
const registryDefaultKey string = ""

func setAppDataImpl(data AppData) error {
	if data.AppID == "" {
		return fmt.Errorf("empty app ID")
	}

	appKey := filepath.Join(appKeyRoot, data.AppID)

	if err := writeStringValue(appKey, "DisplayName", data.AppID); err != nil {
		return err
	}

	// CustomActivator teaches Window what COM class to use as the callback when
	// a toast notification is activated.
	if err := writeStringValue(appKey, "CustomActivator", GUID_ImplNotificationActivationCallback.String()); err != nil {
		return err
	}

	if data.IconPath != "" {
		if err := writeStringValue(appKey, "IconUri", data.IconPath); err != nil {
			return err
		}
	}

	if data.IconBackgroundColor != "" {
		if err := writeStringValue(appKey, "IconBackgroundColor", data.IconBackgroundColor); err != nil {
			return err
		}
	}

	if data.ActivationExe != "" {
		if err := writeStringValue(activationKey, registryDefaultKey, data.ActivationExe); err != nil {
			return fmt.Errorf("setting activation executable: %w", err)
		}
	}

	return nil
}

// writeStringValue writes a string value to the path, where name is the subkey and
// value is the literal value.
func writeStringValueImpl(path, name, value string) error {
	if keyExists(path, name) {
		return nil
	}
	key, _, err := registry.CreateKey(registry.CURRENT_USER, path, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("opening registry key: %s: %w", path, err)
	}
	if err := key.SetStringValue(name, value); err != nil {
		return fmt.Errorf("setting string value: (%s) %s=%s: %w", path, name, value, err)
	}
	if err := key.Close(); err != nil {
		return fmt.Errorf("closing key: %s: %w", path, err)
	}
	return nil
}

// keyExists returns true if the key exists.
func keyExists(path, name string) bool {
	key, err := registry.OpenKey(registry.CURRENT_USER, path, registry.READ)
	if err != nil {
		return false
	}
	defer key.Close()
	v, _, err := key.GetStringValue(name)
	if err != nil {
		return false
	}
	return v != ""
}
