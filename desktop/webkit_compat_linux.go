// Package main provides the Wails desktop shell around the Reasonix kernel.
//
// webkit_compat_linux.go applies WebKit2GTK compatibility workarounds around
// Wails startup and JavaScriptCore initialization.

package main

/*
#cgo linux pkg-config: gtk+-3.0
#cgo !webkit2_41 pkg-config: webkit2gtk-4.0
#cgo webkit2_41 pkg-config: webkit2gtk-4.1

#include <errno.h>
#include <signal.h>
#include <stdio.h>
#include <string.h>

#include <glib.h>

static void reasonix_fix_signal(int signum)
{
	struct sigaction st;

	if (sigaction(signum, NULL, &st) < 0) {
		goto fix_signal_error;
	}
	st.sa_flags |= SA_ONSTACK;
	if (sigaction(signum, &st, NULL) < 0) {
		goto fix_signal_error;
	}
	return;

fix_signal_error:
	fprintf(stderr, "reasonix: error fixing handler for signal %d: %s\n",
		signum, strerror(errno));
}

static void reasonix_install_signal_handlers(void)
{
#if defined(SIGCHLD)
	reasonix_fix_signal(SIGCHLD);
#endif
#if defined(SIGHUP)
	reasonix_fix_signal(SIGHUP);
#endif
#if defined(SIGINT)
	reasonix_fix_signal(SIGINT);
#endif
#if defined(SIGQUIT)
	reasonix_fix_signal(SIGQUIT);
#endif
#if defined(SIGABRT)
	reasonix_fix_signal(SIGABRT);
#endif
#if defined(SIGFPE)
	reasonix_fix_signal(SIGFPE);
#endif
#if defined(SIGTERM)
	reasonix_fix_signal(SIGTERM);
#endif
#if defined(SIGBUS)
	reasonix_fix_signal(SIGBUS);
#endif
#if defined(SIGSEGV)
	reasonix_fix_signal(SIGSEGV);
#endif
	// Do not modify SIGUSR1. JavaScriptCore owns it for conservative GC
	// stack scanning after installing its handler.
#if defined(SIGXCPU)
	reasonix_fix_signal(SIGXCPU);
#endif
#if defined(SIGXFSZ)
	reasonix_fix_signal(SIGXFSZ);
#endif
}

static gboolean reasonix_install_signal_handlers_timeout(gpointer data)
{
	reasonix_install_signal_handlers();
	int *remaining = (int *)data;
	(*remaining)--;
	return *remaining > 0 ? G_SOURCE_CONTINUE : G_SOURCE_REMOVE;
}

static void reasonix_schedule_signal_handler_fix(void)
{
	int *remaining = (int *)g_malloc(sizeof(int));
	*remaining = 100;
	g_timeout_add_full(
		G_PRIORITY_DEFAULT,
		50,
		reasonix_install_signal_handlers_timeout,
		remaining,
		g_free
	);
}
*/
import "C"

import "os"

// init applies Linux WebKit2GTK compatibility workarounds.
//
// WebKit2GTK 2.50.x (shipped by Deepin 23, Arch, and other rolling/bleeding-
// edge distros) can abort while the Wails v2.12 Linux frontend creates or
// starts the webview. One reported crash manifests as:
//
//	GLib-GObject-CRITICAL: g_value_set_boxed: assertion 'G_VALUE_HOLDS_BOXED'
//	SIGABRT: abort  PC=0x... signal arrived during cgo execution
//	runtime.cgocall → _Cfunc_webkit_user_content_manager_new()
//
// JavaScriptCore installs several signal handlers lazily when JavaScript first
// executes. Those handlers can replace the SA_ONSTACK flag required by Go after
// Wails v2.12's one-shot repair has already run. The bounded GLib timer below
// mirrors the Linux repair shipped by Wails v2.13: it restores SA_ONSTACK every
// 50 ms for the first five seconds of WebKit startup, then domReady performs one
// final deterministic repair.
//
// The bounded signal repair is backported from Wails v2.13's verified Linux
// fix. The NVIDIA renderer workaround was reported effective on WebKit2GTK
// 2.50.4 + Go 1.26.5 + NVIDIA GeForce GT 1030 on Deepin 23 (X11).
func init() {
	// Disable the DMA-BUF renderer on NVIDIA GPUs. This is independent of the
	// signal repair and protects WebKit2GTK's accelerated rendering path.
	//
	// WebKit2GTK 2.50.x introduced a new DMA-BUF accelerated compositing
	// path that can crash on NVIDIA proprietary drivers. Disabling it
	// falls back to the stable software OpenGL renderer.
	//
	// This complements nvidia_wayland_linux.go's explicit-sync workaround by
	// guarding all NVIDIA GPUs, in both X11 and Wayland sessions.
	if _, set := os.LookupEnv("WEBKIT_DISABLE_DMABUF_RENDERER"); !set {
		if hasNVIDIAGPU() {
			_ = os.Setenv("WEBKIT_DISABLE_DMABUF_RENDERER", "1")
		}
	}
}

// scheduleWebKitSignalHandlerRepair starts the bounded GLib timer immediately
// before wails.Run. The callback begins executing when Wails enters GTK's main
// loop, which anchors the five-second repair window to WebKit startup instead
// of package initialization.
func scheduleWebKitSignalHandlerRepair() {
	C.reasonix_schedule_signal_handler_fix()
}

// repairWebKitSignalHandlers runs once after the DOM is ready, when JSC has
// installed its lazy signal handlers.
func repairWebKitSignalHandlers() {
	C.reasonix_install_signal_handlers()
}
