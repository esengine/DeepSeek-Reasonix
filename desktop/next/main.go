// Command reasonix-studio is the Wails shell for frontend-next. It differs from
// the existing desktop binary in one way that matters: the UI talks to the
// kernel over internal/serve's HTTP surface instead of Wails bindings, so the
// same build runs in a browser against `reasonix serve`.
package main

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reasonix/internal/i18n"
	goruntime "runtime"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"reasonix/internal/appupdate"
	"reasonix/internal/boot"
	"reasonix/internal/config"
	"reasonix/internal/crashreport"
	"reasonix/internal/event"
	"reasonix/internal/notify"
	"reasonix/internal/remotehost"
	"reasonix/internal/serve"
	"reasonix/internal/surface"
	"reasonix/internal/traystate"
	"reasonix/internal/update"

	// Kinds register from init, so a binary builds only what it links. Without
	// these the shell answers every Anthropic model with "unknown kind" at
	// switch time; openai alone arrived, pulled in transitively by config.
	_ "reasonix/internal/provider/anthropic"
	_ "reasonix/internal/provider/openai"
	_ "reasonix/internal/provider/responses"
)

// Everything the SPA is allowed to route to the kernel. Anything else falls
// through to the static assets, so an unknown path renders index.html rather
// than reaching the controller.
// Must match the name and path SsePort uses when it detects the Wails runtime.
const (
	wailsEventName = "rx:event"
	replayPath     = "/rx-replay"
	// Must match runtimePrefix in internal/serve and the frontend's pane base.
	runtimePrefix = "/rt/"
)

var apiPaths = map[string]bool{
	"/events": true, "/events/replay": true, "/history": true, "/context": true, "/status": true,
	"/sessions": true, "/checkpoints": true, "/branches": true, "/models": true,
	"/submit": true, "/cancel": true, "/approve": true, "/answer": true,
	"/plan": true, "/plan-decision": true, "/preset": true, "/model": true, "/effort": true,
	"/goal": true, "/resume": true, "/compact": true, "/compaction": true, "/new": true,
	"/rewind": true, "/fork": true, "/summarize": true, "/forget": true,
	"/tool-approval-mode": true, "/auto-approve-tools": true, "/bypass": true,
	"/provider-setup": true, "/delete-session": true, "/inbox": true, "/inbox/items": true,
	"/trajectory": true, "/slash": true, "/complete": true,
	"/execution-graph": true, "/adjudications": true,
	"/workspace": true, "/workspaces": true,
	"/mcp": true, "/skills": true, "/capability-scope": true, "/account": true, "/hooks": true,
	"/memory": true, "/network": true, "/shell": true, "/todos": true, "/providers": true,
	"/changes": true, "/attachments": true, "/drop": true, "/roles": true,
	"/themes": true, "/extensions": true, "/plugins": true, "/surfaces": true,
	"/welcome": true, "/appearance": true,
	"/permissions": true, "/sandbox": true, "/storage": true, "/usage": true, "/tray": true, "/asks": true, "/update": true,
	"/balance":        true,
	"/config/problem": true, "/config/repair": true,
}

// A sub-path belongs to the resource it hangs off: /mcp/reconnect is the same
// surface as /mcp. Listing families rather than every leaf is what keeps a new
// endpoint from silently answering with index.html instead of JSON — and
// TestEveryPathTheFrontendCallsIsRouted is what keeps this list honest, because
// the comment alone did not.
var apiPrefixes = []string{"/asks/", "/tray/", "/update/", "/mcp/", "/skills/", "/inbox/", "/account/", "/hooks/", "/memory/", "/network/", "/providers/", "/rewind/", "/extensions/", "/themes/", "/plugins/", "/appearance/", "/storage/", "/changes/", "/context/", "/studio/"}

// splitRuntimePath separates a pane's address from the route it is asking for:
// /rt/r2/status is runtime r2 asking for /status. An unprefixed path belongs to
// no pane in particular and reaches the hub's first runtime.
func splitRuntimePath(p string) (id, path string) {
	if !strings.HasPrefix(p, runtimePrefix) {
		return "", p
	}
	rest := strings.TrimPrefix(p, runtimePrefix)
	id, path, found := strings.Cut(rest, "/")
	if !found {
		return id, "/"
	}
	return id, "/" + path
}

// isHubPath covers the routes that belong to the hub rather than to any one
// runtime: the pane list and the workspace tree the sidebar reads.
func isHubPath(p string) bool {
	p = strings.TrimSuffix(p, "/")
	return p == "/runtimes" || strings.HasPrefix(p, "/runtimes/") ||
		p == "/tree" || strings.HasPrefix(p, "/tree/") ||
		p == "/remotes" || strings.HasPrefix(p, "/remotes/")
}

func isAPIPath(p string) bool {
	p = strings.TrimSuffix(p, "/")
	if apiPaths[p] {
		return true
	}
	for _, prefix := range apiPrefixes {
		if strings.HasPrefix(p+"/", prefix) && p+"/" != prefix {
			return true
		}
	}
	return false
}

func main() {
	// A macOS install re-executes this binary to swap the bundle after the old
	// process exits. That child must never reach run(), or the swap would race a
	// second Studio booting on top of the directory it is replacing.
	if handled, code := update.MaybeRunMacHandoff(os.Args[1:]); handled {
		os.Exit(code)
	}
	os.Exit(start())
}

// start owns the two things that have to outlive run: the log stderr now points
// at, and the recover that turns a Go panic into a report. A fatal signal
// reaches neither — it reaches the log, which is why the log is here.
func start() int {
	home := config.ReasonixHomeDir()
	logs, flush, err := openCrashLog(home)
	defer flush()
	if err != nil {
		fmt.Fprintln(logs, "reasonix-next: crash log:", err)
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			_ = crashreport.CaptureStudioPanic(home, version, recovered, debug.Stack())
			// The re-panic below ends the process, and its trace is written to
			// the descriptor this flush is about to stop holding open.
			flush()
			panic(recovered)
		}
	}()
	if err := run(logs); err != nil {
		fmt.Fprintln(logs, "reasonix-next:", err)
		return 1
	}
	return 0
}

func run(logs io.Writer) error {
	ctx := context.Background()
	// Before the first window: Windows reads this when the taskbar button is
	// created, and a notification sent under an unclaimed identity is dropped.
	applyAppIdentity(logs)

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	// This window is the only client of its kernel, so a system notification
	// reaches the person who asked for it. A networked server leaves this off.
	notifySink := windowNotifications(cfg)
	// One fold behind the status icon: every pane's events on the way through,
	// plus what they leave running. Built before the first runtime so that
	// pane's sink is decorated with the rest.
	tracker := traystate.New(nil)
	count := windowTelemetry(cfg)
	watch := func(sink event.Sink) event.Sink { return count(tracker.Watch(paneKey(sink), notifySink(sink))) }

	bc := serve.NewBroadcaster()
	paneSink := watch(bc)
	// A window opens where it was left, not where its shortcut happened to point.
	root := boot.ResolveWorkspaceRoot(lastWorkspace())
	built, err := boot.BuildRuntime(ctx, boot.Options{
		Version:       version,
		WorkspaceRoot: root,
		SessionDir:    serve.SessionDirFor(root),
		Sink:          paneSink,
		Stderr:        logs,
	})
	if err != nil {
		return err
	}
	ctrl := built.Controller
	// No EnsureSessionPath here: minting the file at launch left one empty
	// transcript behind every time the window opened. The first turn creates
	// it, and the inbox ensures its own path when it enqueues.

	// Ask/Auto/YOLO is a posture the user set on the composer, not a per-launch
	// default — the old shell has read this since it had a picker.
	ctrl.SetToolApprovalMode(cfg.DesktopDefaultToolApprovalMode())
	// Native panels the shell opens are outside the webview, so the frontend's
	// catalogue cannot reach them. They follow the desktop interface language, a
	// separate setting from the kernel's — hence a catalogue read, not the active one.
	shell := &App{pumps: map[string]context.CancelFunc{}, say: i18n.CatalogFor(cfg.DesktopLanguage()), tracker: tracker}
	// A first connect can stop for a host key nobody has seen or a locked key.
	// Both are questions, and the broker is where they live until answered.
	asks := serve.NewAskBroker(nil)
	// What this shell is and where it lives; the kernel resolves neither for a
	// process that is not the application.
	install := shell.install()
	// This shell is its own application, so it states itself. A dev copy not
	// running from a bundle states nothing and still starts: what refuses then
	// is the swap, by name, rather than a launch with nothing to swap.
	application, _ := update.LocalApplication(install.Layout)
	// Read before the hub that serves it: an update can only be acknowledged by
	// the launch that booted from it, and the transaction on disk right now is
	// the only one that is.
	shell.updateHost = appupdate.New(appupdate.Options{
		Owner:       shell,
		Running:     version,
		Line:        update.StudioLine(),
		Application: application,
	})
	// One hub, several panes: each session gets its own runtime, so a second
	// conversation runs beside the first instead of rebuilding it.
	hub := serve.NewHub(serve.HubOptions{
		Serve:        cfg.Serve,
		Surface:      surface.Desktop,
		Grant:        grantWindowCapabilities,
		DecorateSink: watch,
		OnOpen:       shell.startPump,
		OnClose: func(rt *serve.Runtime) {
			shell.stopPump(rt)
			tracker.Drop(paneKey(rt.Events))
		},
		Remote:  remotehost.New(ctx, version, asks),
		Asks:    asks,
		Tray:    shell,
		Install: &install,
		Update:  shell.updateHost,
	})
	shell.hub = hub
	srv := serve.New(ctrl, bc, cfg.Serve)
	srv.SetPaneSink(paneSink)
	srv.AdoptRuntime(built)
	// Without this a past save-conflict loop's forks stay on disk forever: the
	// CLI and the old shell each sweep them, this window is the third host.
	srv.StartRecoveryGC(ctx)
	if err := adoptWindowPane(hub, srv, bc, ctrl); err != nil {
		return err
	}
	api := hub.Handler()
	defer hub.Shutdown()

	assets, err := frontendAssets()
	if err != nil {
		return err
	}
	return shell.runWindow(api, assets, cfg, tracker)
}

// runWindow is everything from here on: the window, and the two hooks that own
// the status icon's life. Split from the assembly above because they fail for
// different reasons and read as one long function otherwise.
func (a *App) runWindow(api http.Handler, assets fs.FS, cfg *config.Config, tracker *traystate.Tracker) error {
	shell, hub := a, a.hub
	return wails.Run(&options.App{
		Title:  "Reasonix Studio",
		Width:  1440,
		Height: 900,
		// The top row is the title bar — project, goal, preset — so a native one
		// above it costs a whole row to say the app's own name. Same treatment
		// the existing shell uses; Linux keeps its frame, WebKitGTK has no inset.
		Frameless: goruntime.GOOS == "windows",
		// Shown by fitWindow once it has been measured against the screen it
		// landed on. Sizing a visible window makes the correction a flicker.
		StartHidden: true,
		Mac:         &mac.Options{TitleBar: mac.TitleBarHiddenInset(), Appearance: mac.DefaultAppearance},
		Windows:     &windows.Options{Theme: windows.SystemDefault},
		MinWidth:    760,
		MinHeight:   480,
		Menu:        appMenu(),
		// Double-clicking the launcher again focuses this window instead of
		// starting a second one over the same sessions; see single_instance.go.
		SingleInstanceLock: singleInstanceLock(shell),
		// Production builds ship without one, so the window had no copy, paste,
		// or select-all on right-click — in a text editor that reads as broken.
		EnableDefaultContextMenu: true,
		Bind:                     []any{shell},
		DragAndDrop:              dragAndDrop(),
		OnStartup: func(ctx context.Context) {
			shell.mu.Lock()
			shell.ctx = ctx
			shell.mu.Unlock()
			applyDockIcon()
			// Before anything is drawn: a window that does not fit takes its own
			// title bar off-screen on Windows, and with it the way to move it.
			fitWindow(ctx)
			// Panes opened before the window came up have no pump yet; from here
			// on the hub's OnOpen starts one as each is published.
			for _, rt := range hub.Runtimes() {
				shell.startPump(rt)
			}
			// No icon, no backgrounding: a window that vanished with no
			// way back to it would be the worst of both.
			if icon := startTray(shell, shell.say, tracker, cfg); icon != nil {
				shell.adoptIcon(icon)
				shell.background.Store(cfg.DesktopClosesToBackground())
			}
		},
		AssetServer: &assetserver.Options{
			Assets: assets,
			// In-process HTTP: no port, no CORS, no second transport to keep in
			// sync with the browser build.
			Middleware: hubMiddleware(shell, hub, api),
		},
		// A loaded renderer is the first point this build has shown it can do
		// anything; the window itself came up in OnStartup.
		OnDomReady: shell.acknowledgeUpdateHealth,
		// The window is held open across the shutdown rather than vanishing into
		// one; see closing.go. OnShutdown stays as the backstop for exits that
		// never pass the close button (a signal, a quit from the dock menu).
		OnBeforeClose: shell.beginClose,
		OnShutdown: func(context.Context) {
			shell.closeIcon()
			hub.Shutdown()
			closeProcessCatalogs()
		},
	})
}

func hubMiddleware(shell *App, hub *serve.Hub, api http.Handler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id, path := splitRuntimePath(r.URL.Path)
			// The bus has no reconnect handshake, so a reloaded page asks for the
			// replay resubscribing to /events would have given it.
			if path == replayPath {
				// A remote pane has no local controller to ask: its kernel replays
				// on subscribe, so restarting the pump is the same handover.
				if rt := hub.Get(id); rt != nil && rt.Local() {
					rt.Server.Controller().ReplayPendingPromptsWith(func() event.Sink { return rt.Events })
				} else if rt != nil {
					shell.restartPump(rt)
				}
				w.WriteHeader(http.StatusNoContent)
				return
			}
			if isAPIPath(path) || isHubPath(path) {
				api.ServeHTTP(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// appMenu is what makes ⌘C work: a WKWebView takes its editing shortcuts from
// the application's Edit menu, and a window without one has no copy, paste, or
// undo at all. macOS only — elsewhere the same bar renders as a stray
// in-window strip, and those platforms bind the shortcuts themselves.
func appMenu() *menu.Menu {
	if goruntime.GOOS != "darwin" {
		return nil
	}
	m := menu.NewMenu()
	m.Append(menu.AppMenu())
	m.Append(menu.EditMenu())
	m.Append(menu.WindowMenu())
	return m
}

// App is the one thing the SPA cannot do over HTTP: open the platform's folder
// picker. Everything else it needs is a route on the embedded hub. It also owns
// the per-runtime event pumps, because the bus is the window's, not a server's.
type App struct {
	hub *serve.Hub
	ctx context.Context
	// Text for the native panels this shell opens. Read once at launch from the
	// desktop interface language; the kernel's own catalogue is a different
	// setting and must not be swapped out for this.
	say i18n.Messages

	// background is what the close button does: hide where an icon can bring the
	// window back, quit where it cannot. Runtime state rather than a config read
	// per close, because the tray toggles it and the answer must be immediate.
	background atomic.Bool

	// What the panes add up to, folded as they emit. The icon paints it and the
	// tray surface answers with it.
	tracker *traystate.Tracker

	mu    sync.Mutex
	pumps map[string]context.CancelFunc
	// The status icon this launch got, if the platform gave one. It comes down
	// only at shutdown — see tray_prefs.go.
	icon *tray
	// See closing.go: the window outlives the close button by however long the
	// session takes to reach disk.
	closing closeState

	// The update this launch booted from, if it booted from one. Captured
	// before the window runs and read from the readiness hook, so it is not
	// mutable state the mutex above has anything to say about.
	updateHost serve.UpdateHost
}

// windowNotifications returns the sink wrapper every runtime in this window
// gets. Off by default and per the shared [notifications] config, so the CLI
// and the window answer to one setting rather than each growing its own.
func windowNotifications(cfg *config.Config) func(event.Sink) event.Sink {
	if cfg == nil || !cfg.Notifications.Enabled {
		return func(sink event.Sink) event.Sink { return sink }
	}
	sender := notify.NewPlatformSender()
	return func(sink event.Sink) event.Sink {
		return notify.NewSink(sink, sender, cfg.Notifications)
	}
}

// grantWindowCapabilities opens up what only a local window may do. The single
// client is the person in front of it, so the folder picker, the account token
// and provider keys are local dialogs rather than remote capabilities — and
// every pane gets them, not just the one the window started with.
func grantWindowCapabilities(srv *serve.Server) {
	srv.AllowWorkspaceSwitch()
	srv.AllowAccountAuth()
	srv.AllowProviderEdit()
	// Without this the first-run connection step never appears: /provider-setup
	// answers 404 while it is off, the window reads that as "nothing to set up",
	// and a machine with no key lands in the composer where every turn fails.
	srv.EnableProviderSetup()
}

// startPump forwards one runtime's frames onto the Wails bus under its own
// event name. Before OnStartup there is no context to emit into; the shell
// pumps whatever the hub already holds once the window comes up.
func (a *App) startPump(rt *serve.Runtime) {
	if rt == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.ctx == nil || a.pumps[rt.ID] != nil {
		return
	}
	ctx, cancel := context.WithCancel(a.ctx)
	a.pumps[rt.ID] = cancel
	if rt.Local() {
		go pumpEvents(ctx, rt)
		return
	}
	go pumpRemoteEvents(ctx, rt)
}

// restartPump re-establishes a pane's stream. For a remote one this is also how
// a reload replays: the far kernel sends what is pending when it is subscribed.
func (a *App) restartPump(rt *serve.Runtime) {
	a.stopPump(rt)
	a.startPump(rt)
}

func (a *App) stopPump(rt *serve.Runtime) {
	if rt == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if cancel := a.pumps[rt.ID]; cancel != nil {
		cancel()
		delete(a.pumps, rt.ID)
	}
}

// currentRoot is the folder the picker should open at: whichever pane the hub
// lists first. A window with no pane left has nothing to anchor to.
func (a *App) currentRoot() string {
	for _, rt := range a.hub.Runtimes() {
		if !rt.Local() {
			// A remote path names a folder on another machine — opening a picker
			// there would resolve it against this one's filesystem.
			continue
		}
		if root := rt.Server.Controller().WorkspaceRoot(); root != "" {
			return root
		}
	}
	return ""
}

// PickWorkspace returns the folder the user chose, or "" if they cancelled.
// Choosing does not switch anything — the SPA posts the path back to
// /workspace, so the browser build reaches the same code by typing one.
func (a *App) PickWorkspace() (string, error) {
	if a.ctx == nil {
		return "", nil
	}
	// The panel can make a folder — that is what CanCreateDirectories is for —
	// but nothing said so, and a title reading "open" describes picking one that
	// exists. Users concluded the app could only open projects, never start one.
	opts := runtime.OpenDialogOptions{Title: a.say.PickWorkspaceTitle, CanCreateDirectories: true}
	// Wails refuses to open the panel at all when this points at nothing, and
	// answers with an error instead — a workspace that has since been moved
	// would take the picker down with it.
	if root := a.currentRoot(); root != "" {
		if st, err := os.Stat(root); err == nil && st.IsDir() {
			opts.DefaultDirectory = root
		}
	}
	dir, err := runtime.OpenDirectoryDialog(a.ctx, opts)
	if err != nil {
		slog.Warn("studio: folder picker", "err", err)
		return "", err
	}
	return dir, nil
}

// OpenExternal hands a link to the platform browser. A WKWebView answers a
// target="_blank" click with nothing at all — Wails binds no delegate for it —
// so every link in the window is dead until something routes it out. http(s)
// only: these come from model output, which may not reach the OS opener with a
// scheme of its choosing.
func (a *App) OpenExternal(rawURL string) {
	if a.ctx == nil {
		return
	}
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		slog.Warn("studio: refused to open a link", "url", rawURL)
		return
	}
	runtime.BrowserOpenURL(a.ctx, u.String())
}

// lastWorkspace is the folder this shell was driving when it last closed, or ""
// to let boot resolve one from the process working directory.
func lastWorkspace() string {
	for _, dir := range serve.Workspaces() {
		if st, err := os.Stat(dir); err == nil && st.IsDir() {
			return dir
		}
	}
	return ""
}

// The asset server buffers a response until its handler returns, so GET /events
// never delivers a byte inside the shell — the SSE handler is still streaming
// when the page gives up. Wails' own bus is the one channel that does push, so
// the shell forwards the same broadcaster frames the browser reads over SSE.
// The payload is byte-identical either way; only the transport differs.
func pumpEvents(ctx context.Context, rt *serve.Runtime) {
	var ch <-chan serve.Frame
	var unsubscribe func()
	// The same handoff GET /events performs: subscribe and replay as one step,
	// so a prompt already waiting for an answer survives the handover.
	rt.Server.Controller().ReplayPendingPromptsWith(func() event.Sink {
		ch, unsubscribe = rt.Events.Subscribe()
		return event.FuncSink(func(e event.Event) { rt.Events.EmitTo(ch, e) })
	})
	defer unsubscribe()
	name := runtimeEventName(rt.ID)
	// The bus has nothing resembling a connection, so a page that missed a frame
	// has no reconnect to notice it by. Same watermark the SSE keepalive sends,
	// for the same reason: a loss at the end of a turn is otherwise invisible.
	watermark := time.NewTicker(serve.SSEWatermarkInterval)
	defer watermark.Stop()
	for {
		select {
		case f, ok := <-ch:
			if !ok {
				return
			}
			runtime.EventsEmit(ctx, name, string(f.Data))
		case <-watermark.C:
			runtime.EventsEmit(ctx, name, fmt.Sprintf(`{"kind":"stream_watermark","seq":%d}`, rt.Events.Watermark()))
		case <-ctx.Done():
			return
		}
	}
}

// runtimeEventName is the bus channel one pane listens on. Panes run at the
// same time, so a single channel would interleave two conversations into both.
func runtimeEventName(id string) string { return wailsEventName + ":" + id }

func frontendAssets() (fs.FS, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	for _, dir := range []string{
		filepath.Join(filepath.Dir(exe), "frontend-next", "dist"),
		filepath.Join("..", "frontend-next", "dist"),
		filepath.Join("frontend-next", "dist"),
	} {
		if st, err := os.Stat(filepath.Join(dir, "index.html")); err == nil && !st.IsDir() {
			return os.DirFS(dir), nil
		}
	}
	return nil, fmt.Errorf("frontend-next/dist not found: run `pnpm build` in desktop/frontend-next first")
}

// paneKey names a pane by the broadcaster it emits through: the hub hands that
// to the sink decorator before the pane has an id of its own, and hands the
// same one back as rt.Events when the pane closes.
func paneKey(sink any) string { return fmt.Sprintf("%p", sink) }
