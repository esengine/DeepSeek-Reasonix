// Command reasonix-desktop is the Wails shell around the Reasonix kernel: a native
// window hosting a webview frontend, with the Go-side control.Controller bound
// directly to the UI (no HTTP hop — bindings in, runtime events out). It lives in
// a nested module (reasonix/desktop) so the CGO/WebKit desktop build never touches
// the CLI's CGO_ENABLED=0 single-static-binary guarantee, while still importing
// the same internal/* kernel.
package main

import (
	"embed"

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

func main() {
	app := NewApp()

	err := wails.Run(&options.App{
		Title:     "Reasonix",
		Width:     1240,
		Height:    720,
		MinWidth:  760,
		MinHeight: 480,
		// Match the dark UI shell so first paint (before CSS loads) doesn't flash
		// white — particularly visible on WebKitGTK. Uses the dark theme's --bg
		// colour (#1a1a2e = RGB 26,26,46).
		BackgroundColour: &options.RGBA{R: 26, G: 26, B: 46, A: 255},
		AssetServer:      &assetserver.Options{Assets: assets},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		Bind:             []any{app},

		// --- per-platform adaptation (see desktop/README.md for the rationale) ---
		Mac: &mac.Options{
			// Inset traffic-lights over a frameless-feeling header; the frontend
			// leaves a drag region at the top (CSS --wails-draggable).
			TitleBar:   mac.TitleBarHiddenInset(),
			Appearance: mac.NSAppearanceNameDarkAqua,
		},
		Windows: &windows.Options{
			// Paint the OS title bar with the app's dark shell colour instead of the
			// default light caption that clashed against the dark UI on light-mode
			// Windows (#2793). Both modes use the shell colour so it always blends.
			Theme: windows.Dark,
			CustomTheme: &windows.ThemeSettings{
				DarkModeTitleBar:   windows.RGB(26, 26, 46),
				DarkModeTitleText:  windows.RGB(228, 228, 238),
				DarkModeBorder:     windows.RGB(26, 26, 46),
				LightModeTitleBar:  windows.RGB(26, 26, 46),
				LightModeTitleText: windows.RGB(228, 228, 238),
				LightModeBorder:    windows.RGB(26, 26, 46),
			},
		},
		Linux: &linux.Options{
			ProgramName: "Reasonix",
			// WebKitGTK GPU compositing is inconsistent across distros/drivers and
			// is the one real cross-platform rough edge for a Go+webview stack:
			// "always" can yield blank or flickering webviews on some setups, so
			// we let the webview decide on demand. Users still hitting artifacts
			// can fall back to WEBKIT_DISABLE_COMPOSITING_MODE=1 (see README).
			WebviewGpuPolicy: linux.WebviewGpuPolicyOnDemand,
		},
	})
	if err != nil {
		println("Error:", err.Error())
	}
}
