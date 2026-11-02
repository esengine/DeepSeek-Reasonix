// Package testscope holds the user-state home explicitly installed by a test harness.
package testscope

import "sync/atomic"

var userHome atomic.Pointer[string]

// Home returns the installed user-state home, or empty outside a test scope.
func Home() string {
	if home := userHome.Load(); home != nil {
		return *home
	}
	return ""
}

// InstallHome installs a test harness's user-state home and returns its restore.
// Restore nested scopes in reverse installation order.
func InstallHome(home string) func() {
	previous := userHome.Swap(&home)
	return func() { userHome.Store(previous) }
}
