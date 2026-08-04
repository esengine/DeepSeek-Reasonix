//go:build darwin && !cgo

package main

// The Wails macOS build always uses cgo. Keep source-only/cross checks working
// without attempting to mutate Finder aliases when Foundation is unavailable.
func repairDesktopIconIntegration() error { return nil }
