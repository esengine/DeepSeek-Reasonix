//go:build !linux

package update

import "fmt"

// InstallDeb exists so callers compile everywhere. Only Debian-family installs
// have a package to upgrade, and every other platform reaches its own apply
// path before asking for one.
func (l Line) InstallDeb(_, _ string, _ func(phase string)) error {
	return fmt.Errorf("update: package installs are Linux-only")
}

// OwnsInstalledPath is false off Linux: with no dpkg there is no package
// ownership to claim, and a portable copy must not read as a managed install.
func (l Line) OwnsInstalledPath(string) bool { return false }
