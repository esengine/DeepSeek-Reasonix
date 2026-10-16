package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"reasonix/internal/state/sessionstore"
	"strings"
	"syscall"

	"reasonix/internal/base/i18n"
	"reasonix/internal/base/secrets"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/surface"
	"reasonix/internal/ext/mcpsetup"
	"reasonix/internal/frontend/remotehost"
	"reasonix/internal/frontend/serve"
	"reasonix/internal/frontend/termrender"
	"reasonix/internal/platform/telemetry"
	"reasonix/internal/session/control"
)

// serveHost is what the frontend loop needs from whatever is serving: a single
// server, or a hub driving several sessions at once. Both answer the same way,
// so the command reads the same either way.
type serveHost interface {
	Handler() http.Handler
	AuthMode() string
	AuthToken() string
	RunGraceful(ctx context.Context, addr string) error
	RunGracefulListener(ctx context.Context, ln net.Listener) error
	StartRecoveryGC(ctx context.Context)
	EnableProviderSetupForListener(addr string) bool
	AllowHost(host string)
}

// runServe exposes the controller's HTTP and SSE frontend.
func runServe(args []string, version string) int {
	return runServeWithOptions(args, serveRunOptions{command: "serve", version: version})
}

// runWeb adds local browser lifecycle behavior to the Serve frontend.
func runWeb(args []string, version string) int {
	return runServeWithOptions(args, serveRunOptions{command: "web", openBrowser: true, version: version})
}

type serveRunOptions struct {
	command     string
	version     string
	openBrowser bool
}

type serveFrontendOptions struct {
	command     string
	address     string
	portFile    string
	tokenFile   string
	pidFile     string
	openBrowser bool
	hasSession  bool
	// publicURL is what a phone opens to reach this server, when that is not
	// the bound address: a TLS proxy's origin, say.
	publicURL string
	// brokered is a serve whose provider credentials come from the machine that
	// started it. It holds none of its own, so anything reading them here fails
	// by design rather than because something is wrong.
	brokered bool
	// launchTokenPath names the file holding the launch token, so the
	// terminal shows a path rather than the secret.
	launchTokenPath string
}

type serveFrontendResources struct {
	listener     net.Listener
	displayAddr  string
	artifacts    []string
	registration *webInstanceRegistration
}

func prepareServeFrontend(opts serveFrontendOptions) (_ *serveFrontendResources, err error) {
	resources := &serveFrontendResources{displayAddr: opts.address}
	defer func() {
		if err != nil {
			resources.release(true)
		}
	}()

	if opts.portFile != "" || opts.command == "web" || opts.openBrowser {
		if opts.command == "web" {
			resources.listener, err = listenWebWithPortRetry(opts.address)
		} else {
			resources.listener, err = net.Listen("tcp", opts.address)
		}
		if err != nil {
			return nil, err
		}
		resources.displayAddr = resources.listener.Addr().String()
		requested, selected := requestedPort(opts.address), requestedPort(resources.displayAddr)
		if opts.command == "web" && requested != 0 && requested != selected {
			fmt.Fprintf(os.Stderr, "  port %d is in use; using %d instead\n", requested, selected)
		}
		if opts.portFile != "" {
			if err = writeServeAddrFile(opts.portFile, resources.displayAddr); err != nil {
				return nil, err
			}
			resources.artifacts = append(resources.artifacts, opts.portFile)
		}
	}
	if opts.pidFile != "" {
		if err = writeServePidFile(opts.pidFile); err != nil {
			return nil, err
		}
		resources.artifacts = append(resources.artifacts, opts.pidFile)
	}
	if opts.command == "web" {
		resources.registration, err = registerWebInstance(config.ReasonixHomeDir(), resources.displayAddr)
		if err != nil {
			return nil, err
		}
	}
	return resources, nil
}

func (r *serveFrontendResources) release(closeListener bool) {
	if closeListener && r.listener != nil {
		_ = r.listener.Close()
	}
	if r.registration != nil {
		r.registration.Release()
	}
	for _, path := range r.artifacts {
		_ = os.Remove(path)
	}
}

func runServeFrontend(ctrl *control.Controller, srv serveHost, cfg config.ServeConfig, opts serveFrontendOptions) int {
	warnHeldProjectServers(os.Stderr, ctrl)
	resources, err := prepareServeFrontend(opts)
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 1
	}
	defer resources.release(false)
	srv.EnableProviderSetupForListener(resources.displayAddr)
	if public, err := url.Parse(strings.TrimSpace(opts.publicURL)); err == nil && public.Hostname() != "" {
		srv.AllowHost(public.Hostname())
	}
	if srv.AuthMode() == "none" || srv.AuthMode() == "token" {
		if opts.launchTokenPath, err = launchTokenLocation(srv.AuthToken(), opts, resources); err != nil {
			fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
			return 1
		}
	}
	reportServeFrontend(ctrl, srv, cfg, resources.displayAddr, opts)
	// Not for a brokered kernel: its credentials live on the machine that
	// started it, so this fails by design into a log only opened during a
	// fault, where a standing error is the first thing read.
	if !opts.brokered {
		startServeBalanceDiagnostics(ctrl)
	}
	return serveFrontendLoop(ctrl, srv, resources, opts)
}

func reportServeFrontend(ctrl *control.Controller, srv serveHost, cfg config.ServeConfig, address string, opts serveFrontendOptions) {
	fmt.Printf("reasonix %s — %s on http://%s\n", opts.command, ctrl.Label(), address)
	origin := "http://" + address
	if public := strings.TrimRight(strings.TrimSpace(opts.publicURL), "/"); public != "" {
		origin = public
	}
	reportServeAuth(os.Stdout, srv.AuthMode(), origin, opts.launchTokenPath)
	supervised := opts.portFile != "" && opts.tokenFile != ""
	if !supervised && isTTY(os.Stdout) && termrender.ANSIConsoleReady() {
		reportShareQR(os.Stdout, srv.AuthMode(), srv.AuthToken(), address, opts.publicURL)
	}
	if warning := serve.PlainHTTPAuthWarning(cfg, address); warning != "" {
		fmt.Fprintf(os.Stderr, "  %s\n", warning)
	}
}

// reportServeAuth names the file holding the launch token, never the token: a
// sandboxed command can read back a terminal multiplexer's scrollback.
func reportServeAuth(w io.Writer, mode, origin, tokenPath string) {
	switch mode {
	case "token":
		fmt.Fprintln(w, "  auth: token")
		fmt.Fprintf(w, "  share: %s/#token=<token> (token in %s)\n", origin, tokenPath)
	case "password":
		fmt.Fprintf(w, "  auth: password (login at %s/login)\n", origin)
	case "none":
		fmt.Fprintln(w, "  auth: none (changes and approvals still require the launch token)")
		fmt.Fprintf(w, "  approvals: %s/#token=<token> (token in %s)\n", origin, tokenPath)
	}
}

func startServeBalanceDiagnostics(ctrl *control.Controller) {
	go func() {
		switch r := ctrl.Balance(context.Background()); {
		case !r.Configured:
			fmt.Fprintln(os.Stderr, "  balance: not configured (no balance_url for this provider)")
		case r.Err != nil:
			fmt.Fprintf(os.Stderr, "  balance: error — %v\n", r.Err)
		default:
			fmt.Printf("  balance: %s\n", r.Balance.Display())
		}
	}()
}

func serveFrontendLoop(ctrl *control.Controller, srv serveHost, resources *serveFrontendResources, opts serveFrontendOptions) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	srv.StartRecoveryGC(ctx)
	if resources.listener == nil {
		if err := srv.RunGraceful(ctx, opts.address); err != nil {
			fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
			return 1
		}
		return 0
	}
	var serveErr error
	if opts.openBrowser {
		sessionID := ""
		if opts.hasSession {
			sessionID = sessionstore.BranchID(ctrl.SessionPath())
		}
		serveErr = runServeListenerAfterReady(ctx, srv, resources.listener, resources.displayAddr, func() {
			browserURL, err := launchWebBrowser(srv, resources.displayAddr, sessionID)
			if err != nil {
				fmt.Fprintf(os.Stderr, "  browser: could not open %s — %v\n", withoutFragment(browserURL), err)
			} else {
				fmt.Printf("  browser: %s\n", withoutFragment(browserURL))
			}
		})
	} else {
		serveErr = srv.RunGracefulListener(ctx, resources.listener)
	}
	if serveErr != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, serveErr)
		return 1
	}
	return 0
}

func runServeWithOptions(args []string, opts serveRunOptions) int {
	if opts.command == "" {
		opts.command = "serve"
	}
	fs := flag.NewFlagSet(opts.command, flag.ContinueOnError)
	model := fs.String("model", "", "provider name (default: config default_model)")
	profileFlag := fs.String("profile", "", "deprecated: use --preset (economy|balanced|delivery)")
	presetFlag := fs.String("preset", "balanced", "agent execution setting: light | balanced | delivery")
	maxSteps := fs.Int("max-steps", 0, "one-off max tool-call rounds (0 = automatic)")
	addr := fs.String("addr", "127.0.0.1:8787", "listen address")
	resume := fs.String("resume", "", "resume a saved session file")
	sessionID := new(string)
	if opts.command == "web" {
		sessionID = fs.String("session-id", "", "bind a fresh Web session identity (used by /web handoff)")
	}
	authHelp := "auth mode: none, token, or password (default: config, else a generated token; none still requires the token for changes)"
	if opts.command == "web" {
		authHelp = "auth mode: none, token, or password (default: generated token)"
	}
	auth := fs.String("auth", "", authHelp)
	token := fs.String("token", "", "pre-shared token for auth=token (auto-generated if empty)")
	password := fs.String("password", "", "password for auth=password (use --hash-password to store a hash instead)")
	hashPassword := fs.Bool("hash-password", false, "print a bcrypt hash of --password and exit")
	behindProxy := fs.Bool("behind-proxy", false, "trust X-Forwarded-For / X-Forwarded-Proto headers from a reverse proxy")
	publicURL := fs.String("public-url", "", "the address a phone opens to reach this server, e.g. https://agent.example.com behind a TLS proxy; used for the share link and its QR code")
	portFile := fs.String("port-file", "", "write the actual bound listen address (host:port) to this file after binding")
	tokenFile := fs.String("token-file", "", "read the auth=token pre-shared token from this file (overrides --token; keeps the secret out of argv)")
	pidFile := fs.String("pid-file", "", "write the server process id to this file")
	page := fs.String("page", "", "directory holding a built Studio page, served under /_studio/ (default: look beside the binary)")
	// The project this server works in. Until this flag existed the only way to
	// say so was the process working directory, and on a phone that turned out
	// not to be a way at all: the shell set the child's directory, the kernel
	// resolved its root from `os.Getwd`, and the two disagreed — the sidebar
	// named the shared folder while `write_roots` confined every file tool to
	// the app's own directory, so writes into the folder the person was looking
	// at were refused with `workspace.write_outside_scope`. Naming the root
	// here removes the working directory from the answer, which is what the
	// desktop host already does (`cmd/reasonix-studio-host`, `launchWorkspace`).
	dir := fs.String("dir", "", "project root this server works in: the tool write scope, the folder a new session is filed under, and where relative paths resolve (default: the current directory)")
	broker := registerBrokerFlags(fs)
	openBrowser := fs.Bool("open", opts.openBrowser, "open the Web UI in the default browser")
	noOpen := fs.Bool("no-open", false, "do not open the Web UI in the default browser")
	if code, ok := parseCommandFlags(fs, args); !ok {
		return code
	}
	// Switch into the named project before anything reads the working
	// directory, which is what `--dir` means everywhere else in this CLI. A
	// root that cannot be entered is dropped rather than carried: a
	// `WorkspaceRoot` pointing at a folder that does not open would confine the
	// tools to somewhere unusable, and the person would see every write refused
	// instead of a session in the folder they meant. Dropping it leaves the old
	// answer — the current directory — and says so.
	if entered, err := enterWorkspace(*dir); err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, fmt.Sprintf("%v; continuing in the current directory", err))
		*dir = ""
	} else {
		*dir = entered
	}
	authExplicit := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "auth" {
			authExplicit = true
		}
	})
	if *resume != "" && *sessionID != "" {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, "--resume and --session-id cannot be used together")
		return 2
	}
	if *sessionID != "" {
		if err := validateWebSessionID(*sessionID); err != nil {
			fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
			return 2
		}
	}
	profileRaw := strings.TrimSpace(*profileFlag)
	if profileRaw != "" {
		fmt.Fprintln(os.Stderr, "warning: --profile is deprecated; use --preset light|balanced|delivery")
	} else {
		profileRaw = strings.TrimSpace(*presetFlag)
	}
	profile, err := parseRuntimeProfile(profileRaw)
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 2
	}

	if code, done := printPasswordHash(*hashPassword, *password); done {
		return code
	}

	ctx := context.Background()
	bc := serve.NewBroadcaster()
	cfg, _ := config.Load()

	// Counters for this surface. Enabled() is fail-closed and this frontend is
	// not interactive, so "auto" reports nothing — a headless server counts only
	// where telemetry was turned on deliberately. Wrap is nil-safe.
	reporter := startCLITelemetryReporter(telemetry.Options{
		Mode: cfg.CLITelemetryMode(), Version: opts.version, Surface: surface.Serve,
		HomeDir: config.ReasonixHomeDir(), Proxy: cfg.NetworkProxySpec(),
		Language: cfg.Language,
	})

	// Build serve config, merging CLI flags over config file.
	serveCfg := serveConfigWithCommandDefaults(opts.command, authExplicit, cfg.Serve)
	// Both commands default to a freshly generated token; an explicit --auth
	// always wins, and serve also honours a configured auth_mode.
	if *auth != "" {
		serveCfg.AuthMode = *auth
	}
	if *token != "" {
		serveCfg.Token = *token
	}
	if *tokenFile != "" {
		tok, err := readServeTokenFile(*tokenFile)
		if err != nil {
			fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
			return 1
		}
		serveCfg.Token = tok
		secrets.RegisterHostSecretPath(*tokenFile)
	}
	if *behindProxy {
		serveCfg.BehindProxy = true
	}
	mode, err := serve.NormalizeAuthMode(serveCfg.AuthMode)
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 1
	}
	serveCfg.AuthMode = mode
	if code := applyServePassword(&serveCfg, *password); code != 0 {
		return code
	}

	// Own the active session file for the server's lifetime; the serve
	// handlers that rebind sessions (/resume, /new, /fork) move the lease
	// through the same keeper. Released after the controller closes.
	leases := control.NewSessionLeaseKeeper()
	defer leases.Release()
	var resumeSession *sessionstore.Session
	if *resume != "" {
		if err := leases.Rebind(*resume); err != nil {
			if errors.Is(err, sessionstore.ErrSessionLeaseHeld) {
				fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, control.SessionInUseMessage(err)+"; "+control.SessionLeaseCloseHint)
			} else {
				fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
			}
			return 1
		}
		var err error
		resumeSession, err = loadResumableSession(*resume)
		if err != nil {
			fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
			return 1
		}
	}
	// A bootstrapped serve resolves providers over the tunnel back to the
	// machine that started it, so this host needs no key and no egress of its
	// own. Unset leaves boot on its ordinary config-backed path.
	providerResolver, err := broker.resolver()
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 2
	}
	*model = serveStartModel(*model, *resume, cfg, providerResolver != nil)
	// Keep the browser reachable when the selected provider has no saved key.
	// The loopback-only provider setup surface stores the missing credential and
	// rebuilds this controller in place before the normal web UI is exposed.
	paneSink := reporter.Wrap(bc)
	ctrl, err := setupProfileWithOverrides(ctx, *model, *maxSteps, false, paneSink, profile, cliBuildOverrides{
		NativeSurfaces: true,
		Version:        opts.version, OnSessionRecovered: cliSessionRecoveredHandler(leases),
		ProviderResolver: providerResolver,
		// The project named on the command line, and empty when there was none
		// (or it could not be entered): `boot` then falls back to the working
		// directory exactly as before. Passing it rather than relying on the
		// chdir above is what makes the root a fact the kernel was told instead
		// of one it inferred.
		WorkspaceRoot: *dir,
		// This controller becomes the window's first pane, so it is a window
		// and not a terminal. Without this it was labelled `cli`, and `boot`
		// applies the config's `[desktop] default_tool_approval_mode` only to
		// the surfaces that have a window — so the posture the person chose was
		// written by every save and read by none, and the first pane after a
		// relaunch asked again while every pane opened later obeyed. The label
		// also decides how this frontend's usage is filed.
		StatsSource: surface.Serve,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 1
	}
	defer ctrl.Close()
	SetTaskJobKiller(ctrlKillerAdapter{ctrl})

	if *resume != "" {
		_ = ctrl.Resume(resumeSession, *resume)
	} else if *sessionID != "" {
		freshPath, err := freshWebSessionPath(ctrl.SessionDir(), *sessionID)
		if err != nil {
			fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
			return 1
		}
		ctrl.SetFreshSessionPath(freshPath)
	}
	ctrl.EnsureSessionPath()
	// Fresh sessions take the lease too (defensive: the path is brand new); a
	// resumed path is already held, making this a no-op.
	if err := rebindCLIControllerAuthority(leases, ctrl); err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, control.SessionInUseMessage(err)+"; "+control.SessionLeaseCloseHint)
		return 1
	}

	// A hub, so this frontend drives several sessions at once the way the studio
	// window does. The session this command was started for is the first pane.
	// The door for paired devices is attached here, not only in the desktop host,
	// because a phone is the surface that most needs it: `serve` runs on the
	// device in someone's hand, and its share is how another screen on the same
	// network — a laptop, a tablet — reaches that kernel by scanning a code. The
	// comment on `HubOptions.Share` assumes a networked server with no window; a
	// phone running `serve` is the opposite case, and leaving it nil made the
	// settings screen's device panel answer "not found" on exactly the platform
	// where LAN pairing is the point.
	pageFS := pageOrWarn(*page)
	share := serve.NewDeviceShare(pageFS)
	// The remote panel, and the questions a first SSH connect stops for. Both
	// ride the same decision as the grant above — this listener is the person's
	// own window — so the same test opens them. Without them the settings
	// screen hides its whole "remote" section, which is the one thing a phone
	// bought to drive a server cannot do without.
	asks, remote := remoteFor(*addr, ctx, opts.version)
	hub := serve.NewHub(serve.HubOptions{
		Serve: serveCfg, NativeSurfaces: true, DecorateSink: reporter.Wrap, ProviderResolver: providerResolver,
		Page: pageFS, Share: share, Grant: serveGrant(*addr),
		Asks: asks, Remote: remote,
	})
	share.Attach(hub.Handler())
	adoptFirstPane(hub, ctrl, bc, paneSink, serveCfg, leases)
	return runServeFrontend(ctrl, hub, serveCfg, serveFrontendOptions{
		command: opts.command, address: *addr,
		portFile: *portFile, tokenFile: *tokenFile, pidFile: *pidFile,
		openBrowser: *openBrowser && !*noOpen, brokered: providerResolver != nil,
		hasSession: *resume != "" || *sessionID != "",
		publicURL:  *publicURL,
	})
}

// remoteFor is the remote panel's two halves — the link layer, and the broker
// a first connect stops at — opened only when this listener is the person's own
// window.
//
// `serve` refuses remote panes by default, and the comment on `HubOptions.Remote`
// gives the reason: "a server that dials onward on a request's say-so is someone
// else's way in." That is right for a machine serving a network. It is wrong for
// a phone running its own kernel on loopback, where the only client is the person
// holding it — and there the refusal costs the feature the app exists for:
// the settings screen reads `/remotes`, gets 501, and takes its entire "remote"
// section out of the table of contents, so there is no SSH entry to find.
//
// The same test as `serveGrant` decides it, for the same reason, and the two
// stay together deliberately: one act of opening the window, not two policies
// that could drift apart.
//
// A nil broker is fine. It only carries this window's providers over to a kernel
// started on the far machine, and one that will not listen leaves those hosts to
// resolve their own credentials, which is how they worked before it existed.
func remoteFor(addr string, ctx context.Context, version string) (*serve.AskBroker, serve.RemoteAttacher) {
	if !loopbackAddr(addr) {
		return nil, nil
	}
	asks := serve.NewAskBroker(nil)
	return asks, remotehost.New(ctx, version, asks)
}

// serveGrant is what this server opens for the person in front of it, chosen by
// where it listens.
//
// `serve` was written for a networked machine, so it granted nothing: adding a
// provider writes an API key into the credential store, and a client reachable
// over a network must not be able to do that. But the same command is also how
// a phone runs its own kernel — loopback, one window, the person holding the
// device — and there the refusal is simply wrong: the setup screen cannot save
// what it was opened to collect, and answers "provider editing is not enabled
// on this server" on the first screen a new install shows.
//
// So the grant follows the listener. Bound to loopback, this process is a
// window and gets what the desktop host gives its window. Bound anywhere else,
// it stays the network service it was designed as and keeps every refusal.
//
// It is safe to decide this from the address rather than from the request
// because the grants are per-request: `hostGrants.at` strips them from anything
// that arrived through a device gate, so a phone that shares its kernel over
// the LAN still cannot let a paired laptop write its keys.
func serveGrant(addr string) func(*serve.Server) {
	if !loopbackAddr(addr) {
		return nil
	}
	return func(srv *serve.Server) {
		srv.AllowWorkspaceSwitch()
		srv.AllowAccountAuth()
		srv.AllowProviderEdit()
	}
}

// loopbackAddr reports whether a listen address is this machine only. An empty
// host is every interface, which is not this machine only; a named host is
// resolved, so "localhost" counts and a LAN address does not.
func loopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil {
		// Not host:port — the kernel accepts a bare port, and a bare port on
		// every interface is not a window.
		return false
	}
	if host == "" {
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return strings.EqualFold(host, "localhost")
}

// applyServePassword settles password auth before anything is served: a
// --password is hashed at startup so the config never holds plaintext, and
// overrides a configured hash; password mode with neither refuses to start.
func applyServePassword(serveCfg *config.ServeConfig, password string) int {
	if password != "" && serveCfg.AuthMode == "password" {
		h, err := serve.HashPassword(password)
		if err != nil {
			fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, "failed to hash password:", err)
			return 1
		}
		serveCfg.PasswordHash = h
	}
	if serveCfg.AuthMode == "password" && strings.TrimSpace(serveCfg.PasswordHash) == "" {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, "auth mode password requires --password or serve.password_hash")
		return 1
	}
	return 0
}

// pageOrWarn resolves the built Studio page this serve mounts under
// serve.PagePrefix. A --page holding none is said out loud and carried on from:
// unlike the window, this kernel has a page of its own to fall back to.
func pageOrWarn(dir string) fs.FS {
	page, err := serve.FindPage(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: %v\n", err)
	}
	return page
}

// adoptFirstPane publishes the session this command was started for as the
// hub's first pane. paneSink is what its controller emits into: the hub
// decorates the panes it opens itself, and this one was built before the hub
// existed, so it hands its own decoration over or loses it on the first
// rebuild. Already leased above, so adopting cannot be refused.
func adoptFirstPane(hub *serve.Hub, ctrl *control.Controller, bc *serve.Broadcaster, paneSink event.Sink, serveCfg config.ServeConfig, leases *control.SessionLeaseKeeper) {
	srv := serve.New(ctrl, bc, serveCfg)
	srv.SetPaneSink(paneSink)
	_ = srv.SetSessionLeases(leases) // same live keeper was bound above
	_, _ = hub.Adopt(srv, bc)
}

// warnHeldProjectServers says on stderr which project servers this serve holds
// off and how to let one run: a headless host has no settings screen to show it.
func warnHeldProjectServers(w io.Writer, ctrl *control.Controller) {
	if ctrl == nil {
		return
	}
	for _, p := range ctrl.PendingMCPApprovals() {
		name := mcpsetup.DisplayName(p.Name)
		fmt.Fprintf(w, "warning: MCP server %q: %s; run `reasonix mcp enable %s` to approve the command shown. command: %s\n",
			name, p.Reason.Text(), name, p.Launch)
	}
}
