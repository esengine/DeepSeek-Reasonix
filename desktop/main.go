// Command reasonix-desktop is the Reasonix desktop service: the Go-side
// control.Controller and platform integrations, driven by the Electron shell
// over the desktop host protocol (--host-rpc on stdin/stdout). A plain launch
// bootstraps the Electron shell installed beside this binary (app/) and exits;
// the shell then restarts this binary as its --host-rpc service. It lives in a
// nested module (reasonix/desktop) so the desktop build never touches the
// CLI's CGO_ENABLED=0 single-static-binary guarantee, while still importing
// the same internal/* kernel.
package main

import (
	"fmt"
	"os"
	"strings"

	// Blank imports wire compile-time built-ins into their registries, exactly as
	// cmd/reasonix does — boot.Build resolves providers/tools from these registries.
	_ "reasonix/internal/provider/anthropic"
	_ "reasonix/internal/provider/openai"
	_ "reasonix/internal/provider/responses"
	_ "reasonix/internal/tool/builtin"
)

// version is injected at build time via `-ldflags "-X main.version=..."`,
// mirroring cmd/reasonix/main.go. The auto-updater reads it (App.Version) to compare
// against the published manifest; an un-injected dev build stays "dev" and never
// prompts to update.
var version = "dev"

// channel records the build's release line, injected via
// `-X main.channel=preview`. Default "stable" tracks the public release;
// "preview" tracks the opt-in test line. Legacy "canary" builds are treated as
// preview for compatibility.
var channel = "stable"

// macSelfUpdate is injected as "true" only for Developer ID signed + notarized
// macOS release builds. Local/ad-hoc macOS builds keep the manual download path.
var macSelfUpdate = "false"

func macSelfUpdateAllowed() bool {
	switch strings.ToLower(strings.TrimSpace(macSelfUpdate)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func main() {
	// The detached macOS self-update child must run before any shell starts.
	if handled, exitCode := maybeRunMacUpdateHandoff(os.Args[1:]); handled {
		os.Exit(exitCode)
	}
	capturePreviousFatalCrash()
	installFatalCrashOutput()
	exitIfHostLaunchMode(os.Args[1:])
	if maybeRelaunchIfSuperseded() {
		return
	}
	exitIfShellBootstrapped(os.Args[1:])

	fmt.Fprintln(os.Stderr, "reasonix-desktop: no Electron desktop shell (app/) is installed beside this binary; reinstall Reasonix or run the packaged desktop app")
	os.Exit(1)
}
