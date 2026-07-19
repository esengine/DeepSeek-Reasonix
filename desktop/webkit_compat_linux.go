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

// JavaScriptCore installs several signal handlers lazily when JavaScript first
// executes. Those handlers can replace the SA_ONSTACK flag required by Go after
// Wails v2.12's one-shot repair has already run. The bounded GLib timer below
// mirrors the Linux repair shipped by Wails v2.13: it restores SA_ONSTACK every
// 50 ms for the first five seconds of WebKit startup, then domReady performs one
// final deterministic repair.
//
// The timer starts dispatching only after Wails enters GTK's main loop. It fixes
// the verified JavaScriptCore signal-handler race, but is not intended to cover
// failures that occur earlier while WebKit constructs the window.

// configureWebKitRendererRecovery applies WebKit's costly DMA-BUF fallback only
// during Safe Mode on NVIDIA systems. Normal launches keep accelerated backing
// store enabled; NVIDIA Wayland sessions continue to use the narrower explicit-
// sync workaround in nvidia_wayland_linux.go. Call this before wails.Run so the
// WebKit process inherits the environment.
func configureWebKitRendererRecovery(safeMode bool) {
	if !safeMode {
		return
	}
	configureWebKitRendererRecoveryForGPU(safeMode, hasNVIDIAGPU())
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
