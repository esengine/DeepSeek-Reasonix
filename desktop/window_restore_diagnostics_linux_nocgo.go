//go:build linux && !cgo

package main

func windowRestoreDiagnosticsSupported() bool { return false }
func windowRestoreOwnerAlive(int) bool        { return false }
func windowRestoreConfirmed() bool            { return false }
func showWindowRestoreFailure(*App)           {}
