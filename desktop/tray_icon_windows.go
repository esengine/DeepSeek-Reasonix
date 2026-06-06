//go:build windows

package main

import _ "embed"

// Windows systray loads icons through LoadImage(IMAGE_ICON), so it needs ICO
// bytes rather than the PNG app icon used by macOS/Linux.
//
//go:embed build/windows/icon.ico
var trayIconBytes []byte
