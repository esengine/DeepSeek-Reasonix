//go:build darwin && cgo

package main

/*
#cgo LDFLAGS: -framework Foundation
#include <stdlib.h>

int reasonix_repair_alias(const char *alias_path, const char *current_app_path,
                          int allow_broken, char **error_message);
int reasonix_write_alias(const char *target_path, const char *alias_path,
                         char **error_message);
int reasonix_resolve_alias(const char *alias_path, char **target_path,
                           char **error_message);
*/
import "C"

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unsafe"
)

func repairDesktopIconIntegration() error {
	currentApp, err := currentMacAppBundle()
	if err != nil {
		// Developer binaries and tests are not installed app bundles.
		return nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	return repairMacDesktopAliases(filepath.Join(home, "Desktop"), currentApp)
}

func repairMacDesktopAliases(desktopDir, currentApp string) error {
	entries, err := os.ReadDir(desktopDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var repairErr error
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		path := filepath.Join(desktopDir, entry.Name())
		allowBroken := reasonixExactAliasName(entry.Name())
		repaired, err := repairMacAlias(path, currentApp, allowBroken)
		if err != nil {
			repairErr = errors.Join(repairErr, fmt.Errorf("%s: %w", entry.Name(), err))
			continue
		}
		_ = repaired // Native code returns false for ordinary desktop files.
	}
	return repairErr
}

func reasonixExactAliasName(name string) bool {
	name = strings.TrimSpace(name)
	return strings.EqualFold(name, "Reasonix") || strings.EqualFold(name, "Reasonix.app")
}

func repairMacAlias(aliasPath, currentApp string, allowBroken bool) (bool, error) {
	aliasCString := C.CString(aliasPath)
	appCString := C.CString(currentApp)
	defer C.free(unsafe.Pointer(aliasCString))
	defer C.free(unsafe.Pointer(appCString))
	var nativeErr *C.char
	allow := C.int(0)
	if allowBroken {
		allow = 1
	}
	result := C.reasonix_repair_alias(aliasCString, appCString, allow, &nativeErr)
	if nativeErr != nil {
		defer C.free(unsafe.Pointer(nativeErr))
	}
	if result < 0 {
		if nativeErr == nil {
			return false, fmt.Errorf("native alias repair failed")
		}
		return false, fmt.Errorf("%s", C.GoString(nativeErr))
	}
	return result == 1, nil
}

func writeMacAlias(targetPath, aliasPath string) error {
	targetCString := C.CString(targetPath)
	aliasCString := C.CString(aliasPath)
	defer C.free(unsafe.Pointer(targetCString))
	defer C.free(unsafe.Pointer(aliasCString))
	var nativeErr *C.char
	result := C.reasonix_write_alias(targetCString, aliasCString, &nativeErr)
	if nativeErr != nil {
		defer C.free(unsafe.Pointer(nativeErr))
	}
	if result != 0 {
		if nativeErr == nil {
			return fmt.Errorf("native alias write failed")
		}
		return fmt.Errorf("%s", C.GoString(nativeErr))
	}
	return nil
}

func resolveMacAlias(aliasPath string) (string, error) {
	aliasCString := C.CString(aliasPath)
	defer C.free(unsafe.Pointer(aliasCString))
	var target, nativeErr *C.char
	result := C.reasonix_resolve_alias(aliasCString, &target, &nativeErr)
	if target != nil {
		defer C.free(unsafe.Pointer(target))
	}
	if nativeErr != nil {
		defer C.free(unsafe.Pointer(nativeErr))
	}
	if result != 0 {
		if nativeErr == nil {
			return "", fmt.Errorf("native alias resolution failed")
		}
		return "", fmt.Errorf("%s", C.GoString(nativeErr))
	}
	return C.GoString(target), nil
}
