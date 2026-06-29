package main

import "os"

// init applies the NVIDIA/Wayland WebKit workaround before the Wails runtime
// initializes WebKitGTK. On KDE Plasma Wayland with NVIDIA GPUs the webview
// crashes due to an upstream WebKit explicit-sync bug (WebKit bugs #280210 and
// #317089). The underlying interaction is between WebKit's ANGLE library and
// NVIDIA's egl-wayland: ANGLE advertises explicit-sync (wp_linux_drm_syncobj)
// but fails to set an acquire point before committing the buffer, which violates
// the Wayland protocol and causes the compositor to disconnect the client.
//
// Setting __NV_DISABLE_EXPLICIT_SYNC=1 is the official NVIDIA EGL API to
// disable the explicit-sync protocol path. It preserves GPU acceleration
// (unlike WEBKIT_DISABLE_DMABUF_RENDERER=1 which disables DMA-BUF entirely
// and severely degrades performance) and keeps the native Wayland session
// (unlike GDK_BACKEND=x11 which falls back to XWayland).
//
// The fix only applies when all three conditions are true:
//  1. Wayland session (WAYLAND_DISPLAY set or XDG_SESSION_TYPE=wayland)
//  2. NVIDIA GPU present (/sys/module/nvidia exists)
//  3. User has not already explicitly set __NV_DISABLE_EXPLICIT_SYNC
func init() {
	// Only apply under Wayland — the explicit-sync bug is Wayland-specific.
	if os.Getenv("WAYLAND_DISPLAY") == "" && os.Getenv("XDG_SESSION_TYPE") != "wayland" {
		return
	}
	// Only apply when an NVIDIA GPU is present — the env var is NVIDIA-specific.
	if !hasNVIDIAGPU() {
		return
	}
	// Respect an explicit user choice so the workaround can be opted out of.
	if _, ok := os.LookupEnv("__NV_DISABLE_EXPLICIT_SYNC"); ok {
		return
	}
	os.Setenv("__NV_DISABLE_EXPLICIT_SYNC", "1")
}

// hasNVIDIAGPU checks whether the NVIDIA kernel module is loaded by looking for
// /sys/module/nvidia. This is the most reliable detection method that works
// across all Linux distributions and doesn't require external tools.
func hasNVIDIAGPU() bool {
	_, err := os.Stat("/sys/module/nvidia")
	return err == nil
}
