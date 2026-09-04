// Command reasonix-studio-host runs Studio's kernel behind a loopback socket.
// It is the half of the shell that is not a window: the hub, its panes and its
// event streams over real HTTP, guarded by a boundary this process owns, so a
// renderer living in another process reaches the same surface a browser does.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"reasonix/internal/appupdate"
	"reasonix/internal/boot"
	"reasonix/internal/config"
	"reasonix/internal/event"
	"reasonix/internal/instanceid"
	"reasonix/internal/notify"
	"reasonix/internal/remotehost"
	"reasonix/internal/serve"
	"reasonix/internal/surface"
	"reasonix/internal/traystate"
	"reasonix/internal/update"

	// Kinds register from init, so a binary builds only what it links. Without
	// these every Anthropic model answers "unknown kind" at switch time.
	_ "reasonix/internal/provider/anthropic"
	_ "reasonix/internal/provider/openai"
	_ "reasonix/internal/provider/responses"
)

var version = "dev"

const (
	// credentialBytes is the width of the credential one launch is guarded by.
	credentialBytes = 32
	// handshakeVersion is the shape of the line on stdout. A parent that does
	// not recognise it must refuse the launch rather than read past it.
	handshakeVersion = 1
)

// shellIdentity is what the shell told us about itself, because none of it is
// answerable from here: os.Executable() names this binary, which is a resource
// inside the application rather than the application, and the process holding
// that application open is the one that spawned this.
type shellIdentity struct {
	version string
	exe     string
	pid     int
}

func (s shellIdentity) stated() bool {
	return strings.TrimSpace(s.version) != "" && strings.TrimSpace(s.exe) != "" && s.pid > 0
}

// studioInstall is which build is running and where it lives. A launch that
// named no version gets no install at all, so the version routes refuse by name
// rather than answering for a build nobody claimed. The layout is resolved from
// the executable the shell stated and never from this one: applying an update
// against a guess is how a swap lands on the wrong bundle.
func studioInstall(shell shellIdentity) *update.Install {
	if strings.TrimSpace(shell.version) == "" {
		return nil
	}
	return &update.Install{Version: shell.version, Layout: update.At(shell.exe, update.StudioLine())}
}

// studioUpdateHost is the capability this kernel serves installs through, and
// nil where the shell did not state enough to own one. Nil is the gate: the
// routes are not registered, so a host that cannot name the application it
// would replace has no way to be asked to replace it.
func studioUpdateHost(shell shellIdentity, to io.Writer) appupdate.Capability {
	if !shell.stated() {
		return nil
	}
	// A build not inside a bundle states no application. The version routes
	// still work; the swap is what refuses, by name.
	application, _ := update.ApplicationAt(shell.exe, shell.pid)
	return appupdate.New(appupdate.Options{
		Owner:       &shellOwner{to: to},
		Running:     shell.version,
		Line:        update.StudioLine(),
		Application: application,
	})
}

func main() {
	// Ahead of this host's own flags: a macOS install re-executes this binary
	// to swap the bundle, and that child's argv is the update's. Parsed as this
	// host's, it exits on an undefined flag and the parent reads EOF.
	if handled, code := update.MaybeRunMacHandoff(os.Args[1:]); handled {
		os.Exit(code)
	}
	page := flag.String("page", "", "directory holding the built Studio page")
	identity := flag.Bool("instance-id", false, "print the Studio instance this data home belongs to, and exit")
	// The shell around this process knows which build it is; this process does
	// not. os.Executable() here names the host binary, not the application.
	studioVersion := flag.String("studio-version", "", "the Studio build this host is serving")
	// The application, which is the shell rather than this process: which file
	// it runs as, and which process holds it open while an update waits to
	// replace it.
	studioApp := flag.String("studio-app", "", "the application executable this host runs inside")
	studioAppPID := flag.Int("studio-app-pid", 0, "the process id of that application")
	flag.Parse()
	// Which launches are the same Studio is Reasonix's question, not a shell's:
	// the answer is the canonicalized data home, and a shell asks for it rather
	// than working it out from an environment it does not resolve.
	if *identity {
		fmt.Fprintln(os.Stdout, instanceid.Current())
		return
	}
	shell := shellIdentity{version: *studioVersion, exe: *studioApp, pid: *studioAppPID}
	os.Exit(run(parentLease(os.Stdin), os.Stdout, os.Stderr, *page, shell))
}

// run serves until the lease ends or the process is signalled. stdout carries
// the handshake first and then one line per act a handover asks of the shell,
// because that pipe is the only channel pointing that way; every log goes to
// logs, which is why no line there can be mistaken for one of these.
func run(lease io.Reader, handshakeTo, logs io.Writer, page string, shell shellIdentity) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if lease != nil {
		// The parent's end of the pipe closing is the parent going away, exit
		// or crash alike. Nothing else says that on all three platforms.
		go func() {
			_, _ = io.Copy(io.Discard, lease)
			stop()
		}()
	}

	served, err := serve.FindPage(page)
	if err != nil {
		fmt.Fprintln(logs, "reasonix-studio-host:", err)
		return 1
	}
	if served == nil {
		fmt.Fprintln(logs, "reasonix-studio-host: no built page found; serving the kernel only")
	}

	hub, err := assemble(ctx, logs, handshakeTo, shell, served)
	if err != nil {
		fmt.Fprintln(logs, "reasonix-studio-host:", err)
		return 1
	}
	// After the socket has drained, never before: a pane torn down under a live
	// request answers it with a half-closed kernel.
	defer hub.Shutdown()

	bound, err := bind(hub.Handler())
	if err != nil {
		fmt.Fprintln(logs, "reasonix-studio-host:", err)
		return 1
	}
	// The credential-writing setup surface opens only on a loopback address,
	// and this is the first host that has one to show it.
	hub.EnableProviderSetupForListener(bound.listener.Addr().String())
	if err := announce(handshakeTo, bound); err != nil {
		fmt.Fprintln(logs, "reasonix-studio-host:", err)
		return 1
	}
	if err := bound.serve(ctx); err != nil {
		fmt.Fprintln(logs, "reasonix-studio-host:", err)
		return 1
	}
	return 0
}

// bound is a hub on a socket: the listener, and the guarded handler that is the
// only way through it.
type bound struct {
	listener net.Listener
	origin   string
	token    string
	handler  http.Handler
}

// bind is the startup order the boundary depends on. The socket comes first
// because nothing can name the origin until the kernel owns a port, and the
// credential is minted here rather than read, so no configuration reaches it.
func bind(next http.Handler) (*bound, error) {
	ln, err := serve.ListenLoopback()
	if err != nil {
		return nil, err
	}
	token, err := launchCredential()
	if err != nil {
		_ = ln.Close()
		return nil, err
	}
	origin := serve.LoopbackOrigin(ln)
	return &bound{
		listener: ln,
		origin:   origin,
		token:    token,
		handler:  serve.NewLoopbackGate(next, serve.LoopbackGateOptions{Token: token, Origin: origin}),
	}, nil
}

// serve runs until ctx ends, then drains. The gate sits outside the hub, so
// nothing here changes what the hub's own auth and CSRF middleware do.
func (b *bound) serve(ctx context.Context) error {
	return serve.RunGracefulHandler(ctx, b.listener, b.handler)
}

// launchCredential mints what this launch is guarded by. Never read from
// configuration and never persisted: a credential a user can set is one that a
// page which can read their config can present.
func launchCredential() (string, error) {
	buf := make([]byte, credentialBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// handshake is what the parent process needs and nothing else. It goes down the
// pipe the parent already holds; a file or an environment variable would outlive
// the launch and be readable by more than the one process that spawned it.
type handshake struct {
	Version int    `json:"version"`
	Origin  string `json:"origin"`
	Token   string `json:"token"`
}

func announce(w io.Writer, b *bound) error {
	return json.NewEncoder(w).Encode(handshake{Version: handshakeVersion, Origin: b.origin, Token: b.token})
}

// parentLease is the pipe a parent holds open for as long as it wants this host
// alive. Only a pipe is a lease: a terminal or /dev/null would either never end
// or end at once, and neither says anything about a parent.
func parentLease(f *os.File) io.Reader {
	st, err := f.Stat()
	if err != nil || st.Mode()&os.ModeNamedPipe == 0 {
		return nil
	}
	return f
}

// assemble builds the hub this host serves: one pane on the workspace it was
// launched in, carrying the capabilities a local window may exercise.
func assemble(ctx context.Context, logs, handshakeTo io.Writer, shell shellIdentity, page fs.FS) (*serve.Hub, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	// This window is the only client of its kernel, so a system notification
	// reaches the person who asked for it. Every pane gets the same wrapper,
	// not just the one the launch started with.
	notifications := hostNotifications(cfg)
	// One fold behind the status icon: every pane's events on the way through,
	// so the tray surface answers from what the panes did rather than from a
	// count the shell kept for itself.
	tracker := traystate.New(nil)
	decorate := func(sink event.Sink) event.Sink {
		return tracker.Watch(paneKey(sink), notifications(sink))
	}
	bc := serve.NewBroadcaster()
	paneSink := decorate(bc)
	root := boot.ResolveWorkspaceRoot("")
	built, err := boot.BuildRuntime(ctx, boot.Options{
		Version:       version,
		WorkspaceRoot: root,
		SessionDir:    serve.SessionDirFor(root),
		Sink:          paneSink,
		Stderr:        logs,
		StatsSource:   surface.Desktop,
	})
	if err != nil {
		return nil, err
	}
	// A first connect can stop for a host key nobody has seen or a locked key.
	// Both are questions, and the broker is where they live until answered.
	asks := serve.NewAskBroker(nil)
	hubCfg := hostServeConfig(cfg.Serve)
	hub := serve.NewHub(serve.HubOptions{
		Serve:        hubCfg,
		Surface:      surface.Desktop,
		Page:         page,
		Grant:        grantHostCapabilities,
		DecorateSink: decorate,
		Tray:         &studioTray{tracker: tracker},
		Asks:         asks,
		Remote:       remotehost.New(ctx, version, asks),
		OnClose:      func(rt *serve.Runtime) { tracker.Drop(paneKey(rt.Events)) },
		Install:      studioInstall(shell),
		Update:       studioUpdateHost(shell, handshakeTo),
	})
	srv := serve.New(built.Controller, bc, hubCfg)
	srv.SetPaneSink(paneSink)
	srv.AdoptRuntime(built)
	if _, err := hub.Adopt(srv, bc); err != nil {
		hub.Shutdown()
		return nil, err
	}
	hub.StartRecoveryGC(ctx)
	return hub, nil
}

// hostServeConfig is the user's serve settings with their authentication taken
// out. Studio's boundary is the loopback gate, and both gates read one cookie:
// a configured token left in place would have the hub refuse the credential
// this launch minted, on every request. Forwarded headers go with it — the
// boundary decides on the address it was reached at, not on a claim about it.
func hostServeConfig(cfg config.ServeConfig) config.ServeConfig {
	cfg.AuthMode = ""
	cfg.Token = ""
	cfg.PasswordHash = ""
	cfg.BehindProxy = false
	return cfg
}

// hostNotifications is the sink wrapper every runtime gets. Off unless the
// shared [notifications] config asks for it, so the CLI and this window answer
// to one setting rather than each growing its own.
func hostNotifications(cfg *config.Config) func(event.Sink) event.Sink {
	if cfg == nil || !cfg.Notifications.Enabled {
		return func(sink event.Sink) event.Sink { return sink }
	}
	sender := notify.NewPlatformSender()
	return func(sink event.Sink) event.Sink { return notify.NewSink(sink, sender, cfg.Notifications) }
}

// studioTray answers for a shell that can put an icon back whenever the setting
// asks for one: what is set is what is up, so the two never drift the way they
// do on a platform that gives its icon up once per process.
type studioTray struct{ tracker *traystate.Tracker }

func (t *studioTray) IconLive() bool {
	return config.LoadForEdit(config.UserConfigPath()).DesktopTray() != "off"
}

func (t *studioTray) TrayFold() traystate.State { return t.tracker.State() }

// The window reads what it asked for out of the answer, so there is nothing to
// push at it here.
func (t *studioTray) ApplyTrayPrefs(serve.TrayPrefs) {}

// paneKey names a pane by the sink it emits through: the hub hands that to the
// decorator before the pane has an id, and hands the same one back on close.
func paneKey(sink any) string { return fmt.Sprintf("%p", sink) }

// grantHostCapabilities opens what only a local window may do. The single
// client is the person in front of it, so provider keys and the account token
// are local decisions rather than capabilities reachable from a network.
func grantHostCapabilities(srv *serve.Server) {
	srv.AllowWorkspaceSwitch()
	srv.AllowAccountAuth()
	srv.AllowProviderEdit()
}
