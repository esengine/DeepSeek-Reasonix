// Command reasonix-desktop is the Wails shell around the Reasonix kernel: a native
// window hosting a webview frontend, with the Go-side control.Controller bound
// directly to the UI (no HTTP hop — bindings in, runtime events out). It lives in
// a nested module (reasonix/desktop) so the CGO/WebKit desktop build never touches
// the CLI's CGO_ENABLED=0 single-static-binary guarantee, while still importing
// the same internal/* kernel.
package main

import (
	"embed"
	"os"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	"github.com/wailsapp/wails/v2/pkg/options/windows"

	// Blank imports wire compile-time built-ins into their registries, exactly as
	// cmd/reasonix does — boot.Build resolves providers/tools from these registries.
	_ "reasonix/internal/provider/anthropic"
	_ "reasonix/internal/provider/openai"
	_ "reasonix/internal/tool/builtin"
)

// assets embeds the built frontend. `all:` so dotfiles (e.g. the dist .gitkeep
// that keeps this directive compilable before the first `pnpm build`) are
// included. A real run requires `pnpm build` (or `wails build`) to populate dist.
//
//go:embed all:frontend/dist
var assets embed.FS

// version is injected at build time via `wails build -ldflags "-X main.version=..."`,
// mirroring cmd/reasonix/main.go. The auto-updater reads it (App.Version) to compare
// against the published manifest; an un-injected dev build stays "dev" and never
// prompts to update.
var version = "dev"

// channel selects which updater pointer this build polls, injected via
// `-X main.channel=canary`. Default "stable" tracks the public release; "canary"
// tracks the opt-in pre-release line and never crosses over to stable.
var channel = "stable"

// macSelfUpdate is injected as "true" only for Developer ID signed + notarized
// macOS release builds. Local/ad-hoc macOS builds keep the manual download path.
var macSelfUpdate = "false"

const (
	disableWebview2GPUEnv  = "REASONIX_DESKTOP_DISABLE_WEBVIEW2_GPU"
	linuxDRIRenderNodeGlob = "/dev/dri/renderD*"
)

func macSelfUpdateAllowed() bool {
	switch strings.ToLower(strings.TrimSpace(macSelfUpdate)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func windowsWebview2GPUDisabled() bool {
	if raw, ok := os.LookupEnv(disableWebview2GPUEnv); ok {
		switch strings.ToLower(strings.TrimSpace(raw)) {
		case "1", "true", "yes", "on":
			return true
		case "0", "false", "no", "off", "":
			return false
		}
	}
	return channel == "canary"
}

func linuxWebviewGpuPolicy(pattern string) linux.WebviewGpuPolicy {
	matches, err := filepath.Glob(pattern)
	if err == nil {
		for _, path := range matches {
			f, err := os.OpenFile(path, os.O_RDWR, 0)
			if err == nil {
				_ = f.Close()
				return linux.WebviewGpuPolicyOnDemand
			}
		}
	}
	return linux.WebviewGpuPolicyNever
}

func main() {
	app := NewApp()

	// Restore saved window size, or fall back to the default.
	width, height := 1240, 720
	if saved, ok := loadWindowState(); ok {
		if saved.Width > 0 {
			width = saved.Width
		}
		if saved.Height > 0 {
			height = saved.Height
		}
	}

	err := wails.Run(&options.App{
		Title:     "Reasonix",
		Width:     width,
		Height:    height,
		MinWidth:  760,
		MinHeight: 480,
		// Match the dark UI shell so the initial webview background doesn't flash
		// white before CSS loads — particularly visible on WebKitGTK.
		BackgroundColour:   &options.RGBA{R: 26, G: 26, B: 46, A: 255},
		AssetServer:        &assetserver.Options{Assets: assets, Middleware: app.workspaceMediaMiddleware()},
		OnStartup:          app.startup,
		OnDomReady:         app.domReady,
		OnBeforeClose:      app.beforeClose,
		OnShutdown:         app.shutdown,
		Bind:               []any{app},
		SingleInstanceLock: singleInstanceLock(app),

		// Start hidden — domReady positions and shows the window after restoring
		// geometry, so the user never sees the default size/position flash.
		StartHidden: true,

		// Native application menu (File > Settings, Edit, Window).
		Menu: app.createAppMenu(),

		// Native OS file drops: the webview withholds dropped files' paths from the
		// HTML drop event, so the frontend (composer) reads them via runtime.OnFileDrop
		// against the --wails-drop-target element instead.
		DragAndDrop: &options.DragAndDrop{EnableFileDrop: true},

		// --- per-platform adaptation (see desktop/README.md for the rationale) ---
		Mac: &mac.Options{
			// Inset traffic-lights over a frameless-feeling header; the frontend
			// leaves a drag region at the top (CSS --wails-draggable).
			TitleBar: mac.TitleBarHiddenInset(),
			// Follow the OS appearance so the title bar matches light/dark system
			// preference instead of being locked to dark.
			Appearance: mac.DefaultAppearance,
		},
		Windows: &windows.Options{
			// Follow the OS theme so the title bar matches light/dark system
			// preference instead of being locked to dark.
			Theme:                windows.SystemDefault,
			WebviewGpuIsDisabled: windowsWebview2GPUDisabled(),
		},
		Linux: &linux.Options{
			ProgramName: "Reasonix",
			// WebKitGTK GPU compositing is inconsistent across distros/drivers and
			// is the one real cross-platform rough edge for a Go+webview stack:
			// "always" can yield blank or flickering webviews on some setups, so
			// we let the webview decide on demand when a render node is usable, and
			// disable acceleration when remote/software-rendered sessions cannot
			// access /dev/dri.
			WebviewGpuPolicy: linuxWebviewGpuPolicy(linuxDRIRenderNodeGlob),
		},
	})
	if err != nil {
		println("Error:", err.Error())
	}
}
