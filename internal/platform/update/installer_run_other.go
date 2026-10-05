//go:build !windows

package update

import "fmt"

// RunInstaller exists so callers compile everywhere. Only Windows updates by
// handing a downloaded installer control of the install; every other platform
// reaches its own apply path before asking for one.
func (l Line) RunInstaller(string) error {
	return fmt.Errorf("update: installer handoff is Windows-only")
}
