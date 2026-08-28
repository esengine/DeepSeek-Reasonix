//go:build !windows && cgo

package main

import _ "embed"

//go:embed build/appicon.png
var trayIconBytes []byte
