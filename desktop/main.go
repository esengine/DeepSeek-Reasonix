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
	goruntime "runtime"
	"strconv"
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
	_ "reasonix/internal/provider/responses"
	"reasonix/internal/repair"
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

// channel records the build's release line, injected via
// `-X main.channel=preview`. Default "stable" tracks the public release;
// "preview" tracks the opt-in test line. Legacy "canary" builds are treated as
// preview for compatibility.
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
	return channel == "preview" || channel == "canary"
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
	// Detached macOS self-update child: wait for the old PID, hold the shared
	// repair mutation lock, then swap the .app bundle. Must run before Wails.
	if handled, exitCode := maybeRunMacUpdateHandoff(os.Args[1:]); handled {
		os.Exit(exitCode)
	}
	capturePreviousFatalCrash()
	installFatalCrashOutput()

	launch := parseDesktopLaunchArgs(os.Args[1:])

	app := NewApp()
	title := "Reasonix"
	singleInstance := singleInstanceLock(app)
	appMenu := app.createAppMenu()
	dragAndDrop := &options.DragAndDrop{EnableFileDrop: true}
	bindings := []any{app}

	if launch.RemoteWindowTicket != "" {
		// A remote web child window: a second Reasonix process that hosts the
		// SSH Serve page for one remote host. It deliberately skips local
		// runtimes (tabs, tray, heartbeat, providers) and exposes no Wails
		// bindings, local menus, or file drops, so it can never act as a second
		// local app. Its single-instance identity is per owner and host, so one
		// Desktop reuses its window while a restarted Desktop cannot adopt an
		// unregistered survivor from the prior process.
		if launch.RemoteWindowHostKey == "" || !isRemoteWindowOwnerID(launch.RemoteWindowOwnerID) || launch.RemoteWindowParentPID <= 0 {
			println("Error: remote window ticket requires valid host and owner identities")
			return
		}
		app.remoteWindowTicket = launch.RemoteWindowTicket
		app.remoteWindowHostKey = launch.RemoteWindowHostKey
		app.remoteWindowOwnerID = launch.RemoteWindowOwnerID
		app.remoteWindowParentPID = launch.RemoteWindowParentPID
		singleInstance = remoteWindowSingleInstanceLock(app)
		appMenu = nil
		dragAndDrop = &options.DragAndDrop{DisableWebViewDrop: true}
		bindings = nil
	} else {
		// Observe previous run for crash diagnostics only. Startup tracking must
		// never force Safe Mode, disable plugins, or select a previous binary.
		app.previousRun = repair.NewStartupTracker("").ObservePreviousRun()
		capturePendingUpdateHealthIdentity(app)
	}

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

	// Restore saved desktop zoom factor (WebView2 ZoomFactor), or default to 1.0.
	zoomFactor := 1.0
	if zf, ok := loadZoomFactor(); ok && zf > 0 {
		zoomFactor = zf
	}

	// On Linux, cover JavaScriptCore's lazy signal-handler installation window.
	// Other platforms provide a no-op implementation.
	scheduleWebKitSignalHandlerRepair()

	err := wails.Run(&options.App{
		Title:     title,
		Width:     width,
		Height:    height,
		Frameless: goruntime.GOOS == "windows",
		Logger:    newCrashCaptureLogger(app),
		MinWidth:  760,
		MinHeight: 480,
		// Match the dark UI shell so the initial webview background doesn't flash
		// white before CSS loads — particularly visible on WebKitGTK.
		BackgroundColour: &options.RGBA{R: 26, G: 26, B: 46, A: 255},
		AssetServer: &assetserver.Options{
			Assets: assets,
			Middleware: assetserver.ChainMiddleware(
				app.remoteWindowAssetMiddleware(),
				app.jsProfilingMiddleware(),
				app.remoteMarkdownImageMiddleware(),
				app.workspaceMediaMiddleware(),
				app.themeAssetMiddleware(),
			),
		},
		OnStartup:          app.startup,
		OnDomReady:         app.domReady,
		OnBeforeClose:      app.beforeClose,
		OnShutdown:         app.shutdown,
		Bind:               bindings,
		SingleInstanceLock: singleInstance,

		// Start hidden — domReady positions and shows the window after restoring
		// geometry, so the user never sees the default size/position flash.
		StartHidden: true,

		// Native application menu (File > Settings, Edit, Window).
		Menu: appMenu,

		// Native OS file drops: the webview withholds dropped files' paths from the
		// HTML drop event, so the frontend (composer) reads them via runtime.OnFileDrop
		// against the --wails-drop-target element instead.
		DragAndDrop: dragAndDrop,

		// per-platform adaptation (see desktop/README.md for the rationale)
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
			ZoomFactor:           zoomFactor,
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

// desktopLaunchOptions captures legacy argv that old installers/shortcuts may
// still pass. Fields are accepted and ignored so migration never crashes on
// unknown product switches.
type desktopLaunchOptions struct {
	// LegacySafeModeArg is true when --safe-mode was present. v1.20+ ignores it.
	LegacySafeModeArg bool
	// RemoteWindowTicket is the one-shot ticket name for an SSH remote web
	// window child process. The URL and Serve token never appear in argv.
	RemoteWindowTicket string
	// RemoteWindowHostKey is the non-secret per-host digest that derives the
	// child window's single-instance identity and validates the ticket.
	RemoteWindowHostKey string
	// RemoteWindowOwnerID scopes same-host reuse to the primary Desktop process
	// that spawned the child. RemoteWindowParentPID lets the child close when
	// that owner and its loopback SSH tunnel disappear.
	RemoteWindowOwnerID   string
	RemoteWindowParentPID int
}

func parseDesktopLaunchArgs(args []string) desktopLaunchOptions {
	var out desktopLaunchOptions
	for _, arg := range args {
		switch {
		case arg == "--safe-mode" || arg == "-safe-mode":
			out.LegacySafeModeArg = true
		case arg == "launch" || arg == "--detach":
			// Legacy launch tokens from old shortcuts. They produce no behavior.
		case strings.HasPrefix(arg, remoteWindowTicketArgPrefix):
			out.RemoteWindowTicket = strings.TrimPrefix(arg, remoteWindowTicketArgPrefix)
		case strings.HasPrefix(arg, remoteWindowHostArgPrefix):
			out.RemoteWindowHostKey = strings.TrimPrefix(arg, remoteWindowHostArgPrefix)
		case strings.HasPrefix(arg, remoteWindowOwnerArgPrefix):
			out.RemoteWindowOwnerID = strings.TrimPrefix(arg, remoteWindowOwnerArgPrefix)
		case strings.HasPrefix(arg, remoteWindowParentArgPrefix):
			out.RemoteWindowParentPID, _ = strconv.Atoi(strings.TrimPrefix(arg, remoteWindowParentArgPrefix))
		}
	}
	return out
}
