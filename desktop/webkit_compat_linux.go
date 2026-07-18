// Package main provides the Wails desktop shell around the Reasonix kernel.
//
// webkit_compat_linux.go applies WebKit2GTK compatibility workarounds before
// the Wails runtime initialises. It runs as a Go package init() so the fixes
// are in place before wails.Run() and any cgo/WebKit calls.

package main

import (
	"os"

	goruntime "runtime"
)

// init applies Linux WebKit2GTK compatibility workarounds.
//
// WebKit2GTK 2.50.x (shipped by Deepin 23, Arch, and other rolling/bleeding-
// edge distros) changed internal GObject type registration in ways that can
// cause a SIGABRT inside webkit_user_content_manager_new() when the Wails
// v2.12.0 linux frontend creates the webview. The crash manifests as:
//
//   GLib-GObject-CRITICAL: g_value_set_boxed: assertion 'G_VALUE_HOLDS_BOXED'
//   SIGABRT: abort  PC=0x... signal arrived during cgo execution
//   runtime.cgocall → _Cfunc_webkit_user_content_manager_new()
//
// The root cause is a conflict between the Go runtime and JavaScriptCore over
// the SIGUSR1 signal (signal 10). Go uses SIGUSR1 for thread management;
// JavaScriptCore uses it for conservative GC stack scanning. When Go's signal
// handler is installed first, WebKit's attempt to register its own handler is
// overridden, which can corrupt the GObject type system during webview setup.
//
// These workarounds have been verified on WebKit2GTK 2.50.4 + Go 1.26.5 +
// NVIDIA GeForce GT 1030 (driver 580.119.02) on Deepin 23 (X11).
func init() {
	// Only apply on Linux.
	if goruntime.GOOS != "linux" {
		return
	}

	// --- 1. Relocate JavaScriptCore GC signal away from SIGUSR1 -----------
	//
	// Go's runtime uses SIGUSR1 (signal 10) for scheduling. JavaScriptCore
	// also wants SIGUSR1 for GC thread suspension. Setting JSC_SIGNAL_FOR_GC
	// tells WebKit to use a different signal (SIGUSR2 = 12) instead.
	//
	// Without this, the "Overriding existing handler for signal 10" warning
	// appears and JSC's GC synchronisation breaks, leading to the SIGABRT.
	// See also: https://trac.webkit.org/changeset/281854
	if _, set := os.LookupEnv("JSC_SIGNAL_FOR_GC"); !set {
		_ = os.Setenv("JSC_SIGNAL_FOR_GC", "12")
	}

	// --- 2. Disable DMA-BUF renderer on NVIDIA GPUs ----------------------
	//
	// WebKit2GTK 2.50.x introduced a new DMA-BUF accelerated compositing
	// path that can crash on NVIDIA proprietary drivers. Disabling it
	// falls back to the stable software OpenGL renderer.
	//
	// nvidia_wayland_linux.go's init() (filename order: "n" < "w") runs
	// before this one and already checks for NVIDIA + Wayland. We add a
	// complementary DMA-BUF guard for all NVIDIA GPUs (both X11 and
	// Wayland sessions).
	if _, set := os.LookupEnv("WEBKIT_DISABLE_DMABUF_RENDERER"); !set {
		if hasNVIDIAGPU() {
			_ = os.Setenv("WEBKIT_DISABLE_DMABUF_RENDERER", "1")
		}
	}
}

// hasNVIDIAGPU checks whether the NVIDIA kernel module is loaded.
// Defined locally (rather than calling into nvidia_wayland_linux.go) so each
// compat file is independently verifiable by Go tooling. The implementation
// is identical.
func hasNVIDIAGPU() bool {
	_, err := os.Stat("/sys/module/nvidia")
	return err == nil
}
