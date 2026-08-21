//go:build windows

package main

import (
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	shcoreDLL = windows.NewLazySystemDLL("shcore.dll")

	enumDisplayMonitorsProc = user32DLL.NewProc("EnumDisplayMonitors")
	getMonitorInfoWProc     = user32DLL.NewProc("GetMonitorInfoW")
	getDpiForMonitorProc    = shcoreDLL.NewProc("GetDpiForMonitor")
)

type windowsRect struct{ Left, Top, Right, Bottom int32 }

type windowsMonitorInfo struct {
	CBSize    uint32
	RcMonitor windowsRect
	RcWork    windowsRect
	DwFlags   uint32
}

// monitorScreenRects enumerates the current monitors and returns their rects
// converted from physical pixels to Wails logical-pixel space using each
// monitor's effective DPI. Returns nil when enumeration fails so callers fall
// back to the size-only heuristic.
func monitorScreenRects() []screenRect {
	var rects []screenRect
	cb := syscall.NewCallback(func(hMonitor, _ uintptr, _ uintptr, _ uintptr) uintptr {
		var mi windowsMonitorInfo
		mi.CBSize = uint32(unsafe.Sizeof(mi))
		if ok, _, _ := getMonitorInfoWProc.Call(hMonitor, uintptr(unsafe.Pointer(&mi))); ok == 0 {
			return 1 // keep enumerating the remaining monitors
		}
		scale := 1.0
		if err := getDpiForMonitorProc.Find(); err == nil {
			var dpiX, dpiY uint32
			// MDT_EFFECTIVE_DPI = 0; S_OK = 0
			if hr, _, _ := getDpiForMonitorProc.Call(hMonitor, 0,
				uintptr(unsafe.Pointer(&dpiX)), uintptr(unsafe.Pointer(&dpiY))); hr == 0 && dpiX > 0 {
				scale = float64(dpiX) / 96.0
			}
		}
		r := mi.RcMonitor
		rects = append(rects, screenRect{
			X: int(float64(r.Left) / scale),
			Y: int(float64(r.Top) / scale),
			W: int(float64(r.Right-r.Left) / scale),
			H: int(float64(r.Bottom-r.Top) / scale),
		})
		return 1
	})
	if ok, _, _ := enumDisplayMonitorsProc.Call(0, 0, cb, 0); ok == 0 {
		return nil
	}
	return rects
}
