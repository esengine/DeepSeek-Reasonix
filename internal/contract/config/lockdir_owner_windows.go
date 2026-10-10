//go:build windows

package config

import "os"

// lockDirCurrentUID exists so tests build on every platform; Windows has no uid.
var lockDirCurrentUID = os.Getuid

// lockDirForeignOwner is a no-op on Windows, which has no uid ownership check.
func lockDirForeignOwner(os.FileInfo) (int, bool) { return 0, false }
