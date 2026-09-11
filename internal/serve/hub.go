package serve

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"

	"reasonix/internal/agent"
	"reasonix/internal/billing"
	"reasonix/internal/boot"
	"strconv"

	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/fileutil"
	"reasonix/internal/provider"
	"reasonix/internal/surface"
	"reasonix/internal/update"
)

// surface is the frontend this hub serves. Every server it adopts and every
// runtime it opens is labelled with it, so the answer lives in one place.
func (h *Hub) surface() surface.Surface { return h.opts.Surface.Or(surface.Serve) }

// decorateSink applies the host's sink wrapper, if it asked for one.
func (h *Hub) decorateSink(sink event.Sink) event.Sink {
	if h.opts.DecorateSink == nil {
		return sink
	}
	return h.opts.DecorateSink(sink)
}

// runtimePrefix is where a hub publishes its runtimes. The frontend builds
// every request under it, so it is part of the wire contract.
const runtimePrefix = "/rt/"

// maxRuntimesDefault caps concurrently driven sessions: each runtime is a full
// assembly — tools, extensions, MCP sidecars — so an unbounded pane count is an
// unbounded process. Where the ceiling belongs depends on the machine, so
// [desktop] max_panes moves it, up to 32.
const maxRuntimesDefault = 8

// maxRuntimes reads the ceiling for this machine. Opening a pane is not a hot
// path, so it is read per call rather than cached into a stale number.
func maxRuntimes() int {
	cfg, err := config.Load()
	if err != nil {
		return maxRuntimesDefault
	}
	return cfg.DesktopMaxPanes(maxRuntimesDefault)
}

// Hub serves several sessions at the same time. Each Runtime is a complete
// Server with its own controller, event stream, title cache and session lease,
// published under /rt/{id}/; the hub owns only what they share — the auth gate,
// the workspace tree, and the rule that one session file gets one runtime.
type Hub struct {
	mu       sync.RWMutex
	runtimes map[string]*Runtime
	order    []string
	seq      int

	opts HubOptions
	auth *authGate
	// What the host decided once and every later pane must inherit: where the
	// setup surface is allowed, and the context its recovery sweep rides.
	setupAddr string
	gcCtx     context.Context
	// titles reads each project's cached session titles for the sidebar tree.
	titles map[string]*titleCache
	// wallets is shared by every pane: opening a conversation builds a runtime,
	// and a per-runtime cache would be cold on exactly the read a switch waits on.
	wallets *billing.Store
	// stance is the Ask/Auto/YOLO posture every pane this hub opens starts in.
	stance *approvalStance
}

// approvalStance is the Ask/Auto/YOLO posture the frontend is in, held by the
// hub rather than read off whichever pane happens to be first. It is one
// setting for the window: a pane opened later starts where the user left the
// others, and closing the pane they set it on does not take it with them.
type approvalStance struct {
	mu   sync.RWMutex
	mode string
}

// set records a posture. A nil stance is a Server outside any hub, which has
// only itself to answer for.
func (a *approvalStance) set(mode string) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.mode = mode
}

func (a *approvalStance) get() string {
	if a == nil {
		return ""
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.mode
}

// Runtime is one open session: the server driving it, the stream it emits, and
// the workspace it was opened against.
type Runtime struct {
	ID     string
	Root   string
	Server *Server
	Events *Broadcaster

	// Set when a kernel on another machine drives this pane: Server and Events
	// are nil then, and the handler proxies rather than routes.
	remote *remoteBinding

	handler http.Handler
	leases  *control.SessionLeaseKeeper
	stop    context.CancelFunc
}

// HubOptions carries what the embedding host decides for every runtime.
type HubOptions struct {
	Serve config.ServeConfig
	// Grant applies the host's capabilities (folder picking, provider edits) to
	// each runtime, so a pane opened later can do what the first one could.
	Grant func(*Server)
	// OnOpen and OnClose let a host attach its own transport to a runtime — the
	// Wails shell pumps each one's frames onto its bus, keyed by ID.
	OnOpen  func(*Runtime)
	OnClose func(*Runtime)
	// DecorateSink wraps each runtime's event sink, the way Grant applies its
	// capabilities. A window adds system notifications here; a networked server
	// leaves it nil, or they fire on the kernel's machine, not the watcher's.
	DecorateSink func(event.Sink) event.Sink
	// Surface labels the usage records of every session this hub opens or
	// adopts. Unset is Serve; a window sets Desktop, or its turns are filed
	// under a frontend the person never ran.
	Surface surface.Surface
	// Page is the built frontend this hub serves under PagePrefix, or nil to
	// serve the kernel alone. It sits inside the auth gate: a page reachable
	// without the credential is one a networked serve hands to the network.
	Page fs.FS
	// ProviderResolver routes every pane's model roles through a caller-owned
	// catalog. A bootstrapped serve sets the broker's, so a pane opened later
	// reaches the credentials the first one did; nil keeps the local config.
	ProviderResolver provider.Resolver
	// Remote reaches workspaces on other machines. Nil refuses them: a server
	// that dials onward on a request's say-so is someone else's way in.
	Remote RemoteAttacher
	// Asks holds the questions a remote dial stops for. Nil leaves those routes
	// unregistered and the link layer with nobody to ask, which is what makes
	// the strict host-key path the default on a server with no window.
	Asks *AskBroker
	// Tray is the window behind this hub, where there is one. Nil leaves the
	// tray routes unregistered rather than answering for an icon that does not
	// exist — a networked server has no window to put one on.
	Tray TrayHost
	// Install is the build this hub belongs to and where it lives. Only a shell
	// can answer either: inside a bundle os.Executable() names the host binary,
	// not the application. Nil makes the version routes refuse by name.
	Install *update.Install
	// Update is the desktop application this kernel runs inside, where it runs
	// inside one. Nil leaves the update routes unregistered: owning the
	// application is what makes replacing it this process's business.
	Update UpdateHost
}

// OpenRequest asks for a runtime. An empty SessionPath opens a fresh session in
// Root; a path that is already open focuses that runtime rather than binding a
// second writer to one transcript.
type OpenRequest struct {
	Root        string `json:"root"`
	SessionPath string `json:"sessionPath"`
	Model       string `json:"model"`
}

// RuntimeView is what the frontend needs to address and label a pane.
type RuntimeView struct {
	ID          string `json:"id"`
	Base        string `json:"base"`
	Root        string `json:"root"`
	Name        string `json:"name"`
	SessionPath string `json:"sessionPath,omitempty"`
	// Set only on a pane driven over SSH. Its absence is what tells the
	// frontend the pane is this machine's own — the common case stays unmarked.
	Host string `json:"host,omitempty"`
}

// NewHub returns an empty hub. Adopt or Open publishes the first runtime.
func NewHub(opts HubOptions) *Hub {
	return &Hub{
		runtimes: map[string]*Runtime{},
		opts:     opts,
		auth:     newAuthGate(opts.Serve),
		wallets:  &billing.Store{},
		stance:   &approvalStance{},
	}
}

// AuthToken and AuthMode report the gate every runtime shares, so a host still
// prints one token rather than one per pane.
func (h *Hub) AuthToken() string { return h.auth.Token() }

// AuthMode reports the shared gate's mode.
func (h *Hub) AuthMode() string { return h.auth.Mode() }

// Adopt takes over a runtime the host assembled before the hub existed — the
// window's first session — and publishes it under an ID.
func (h *Hub) Adopt(srv *Server, bc *Broadcaster) (*Runtime, error) {
	if srv == nil {
		return nil, nil
	}
	srv.auth = h.auth
	srv.surface = h.surface()
	srv.stance = h.stance
	// The posture the host launched in, which is the one every later pane
	// inherits until someone changes it on the composer.
	h.stance.set(srv.Controller().ToolApprovalMode())
	if h.opts.Grant != nil {
		h.opts.Grant(srv)
	}
	leases, err := h.ownSession(srv)
	if err != nil {
		return nil, err
	}
	rt := &Runtime{ID: h.nextID(), Root: srv.Controller().WorkspaceRoot(), Server: srv, Events: bc, leases: leases}
	h.publish(rt)
	return rt, nil
}

// ownSession makes "a runtime owns the session it writes" true however the
// runtime was born. A host that arranged ownership itself keeps it; anything
// else is given a keeper here — before it holds anything, since a window opens
// with no session and the first turn mints one. SetOnSessionPathChanged carries
// the lease onto that path when it appears.
func (h *Hub) ownSession(srv *Server) (keeper *control.SessionLeaseKeeper, err error) {
	if srv.leases != nil {
		return nil, nil // the host's, and the host's to release
	}
	leases := control.NewSessionLeaseKeeper()
	// A refusal must leave the server as it was found, so the caller can decide
	// on another session and adopt again.
	defer func() {
		if err != nil {
			_ = srv.SetSessionLeases(nil)
			leases.Release()
		}
	}()
	if err = srv.SetSessionLeases(leases); err != nil {
		return nil, err
	}
	if path := strings.TrimSpace(srv.Controller().SessionPath()); path != "" {
		if err = srv.rebindSessionLease(path); err != nil {
			return nil, err
		}
	}
	return leases, nil
}

// Open builds a runtime for req, or returns the one already driving that
// session. The caller gets a runtime it can address immediately.
func (h *Hub) Open(ctx context.Context, req OpenRequest) (*Runtime, error) {
	if rt := h.findSession(req.SessionPath); rt != nil {
		return rt, nil
	}
	limit := maxRuntimes()
	h.mu.RLock()
	full := len(h.order) >= limit
	h.mu.RUnlock()
	if full {
		return nil, refusal(http.StatusConflict, "hub.too_many_panes",
			fmt.Errorf("already driving %d sessions — close one first", limit), map[string]any{"max": limit})
	}
	root, err := h.resolveRoot(req)
	if err != nil {
		return nil, err
	}
	bc := NewBroadcaster()
	paneSink := h.decorateSink(bc)
	built, err := boot.BuildRuntime(ctx, boot.Options{
		Model:         strings.TrimSpace(req.Model),
		WorkspaceRoot: root,
		SessionDir:    SessionDirFor(root),
		Sink:          paneSink,
		Stderr:        os.Stderr,
		StatsSource:   h.surface(),
		BalanceStore:  h.wallets,

		ProviderResolver: h.opts.ProviderResolver,
	})
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", root, err)
	}
	srv := New(built.Controller, bc, h.opts.Serve)
	srv.SetPaneSink(paneSink)
	srv.AdoptRuntime(built)
	srv.auth = h.auth
	srv.surface = h.surface()
	if h.opts.Grant != nil {
		h.opts.Grant(srv)
	}
	srv.stance = h.stance
	// Ask/Auto/YOLO is a posture the user set on the window, not a per-session
	// default, so a pane opened later starts where the others already are.
	if mode := h.stance.get(); mode != "" {
		built.Controller.SetToolApprovalMode(mode)
	}
	// Own the session file for as long as this pane lives, so a second pane —
	// or another process — is refused instead of silently double-writing.
	leases := control.NewSessionLeaseKeeper()
	if err := srv.SetSessionLeases(leases); err != nil {
		built.Controller.Close()
		leases.Release()
		return nil, err
	}
	if path := strings.TrimSpace(req.SessionPath); path != "" {
		if status, err := srv.resumeInto(path); err != nil {
			built.Controller.Close()
			leases.Release()
			return nil, keepRefusalStatus(status, err)
		}
	}
	rt := &Runtime{ID: h.nextID(), Root: root, Server: srv, Events: bc, leases: leases}
	// Before publishing: Close reads rt.stop, and a pane must not be reachable
	// before the fields its teardown depends on are set.
	h.adoptHostDecisions(rt)
	h.publish(rt)
	rememberWorkspace(root)
	return rt, nil
}

// Close retires a runtime: the conversation is persisted, the assembly torn
// down, and the session file released for another window to open.
func (h *Hub) Close(id string) error {
	h.mu.Lock()
	rt := h.runtimes[id]
	if rt == nil {
		h.mu.Unlock()
		return fmt.Errorf("no runtime %s", id)
	}
	delete(h.runtimes, id)
	h.order = removeString(h.order, id)
	h.mu.Unlock()

	if h.opts.OnClose != nil {
		h.opts.OnClose(rt)
	}
	if rt.stop != nil {
		rt.stop()
	}
	if rt.remote != nil {
		// Retired before the forward that reaches it goes away: releasing first
		// would strand a session lease over there, with nothing left that could
		// reach the kernel holding it.
		rt.closeFarRuntime()
		if rt.remote.release != nil {
			rt.remote.release()
		}
		return nil
	}
	if err := rt.Server.Controller().Snapshot(); err != nil {
		slog.Warn("serve: snapshot before closing runtime", "id", id, "err", err)
	}
	rt.Server.Controller().Close()
	if rt.leases != nil {
		rt.leases.Release()
	}
	return nil
}

// Shutdown closes every runtime, newest first.
func (h *Hub) Shutdown() {
	for _, view := range h.List() {
		if err := h.Close(view.ID); err != nil {
			slog.Warn("serve: close runtime", "id", view.ID, "err", err)
		}
	}
}

// Get returns a runtime by ID, or nil.
func (h *Hub) Get(id string) *Runtime {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.runtimes[id]
}

// List returns the open runtimes in the order they were opened.
func (h *Hub) List() []RuntimeView {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]RuntimeView, 0, len(h.order))
	for _, id := range h.order {
		if rt := h.runtimes[id]; rt != nil {
			out = append(out, rt.view())
		}
	}
	return out
}

// Local reports whether this pane's kernel runs in this process. A remote one
// has no Server, Events or lease here — they belong to the machine it proxies
// to, which also already made every decision a host would apply to a pane.
func (rt *Runtime) Local() bool { return rt.remote == nil }

// localRuntimes returns the panes this process drives itself. Host decisions —
// the approval posture, the setup surface, the recovery sweep — reach those
// only: the rest are another kernel's, and reaching into one would nil-panic.
func (h *Hub) localRuntimes() []*Runtime {
	out := make([]*Runtime, 0, len(h.order))
	for _, rt := range h.Runtimes() {
		if rt.Local() {
			out = append(out, rt)
		}
	}
	return out
}

// Runtimes returns the live runtimes, for a host that needs more than the view.
func (h *Hub) Runtimes() []*Runtime {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]*Runtime, 0, len(h.order))
	for _, id := range h.order {
		if rt := h.runtimes[id]; rt != nil {
			out = append(out, rt)
		}
	}
	return out
}

func (rt *Runtime) view() RuntimeView {
	if rt.remote != nil {
		return rt.remoteView()
	}
	ctrl := rt.Server.Controller()
	return RuntimeView{
		ID:          rt.ID,
		Base:        runtimePrefix + rt.ID,
		Root:        ctrl.WorkspaceRoot(),
		Name:        fileutil.RootName(ctrl.WorkspaceRoot()),
		SessionPath: ctrl.SessionPath(),
	}
}

// Handler routes hub endpoints and mounts every runtime under /rt/{id}/. Auth,
// CSRF and logging wrap the lot once, which is why runtimes are mounted as
// bare muxes.
func (h *Hub) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /runtimes", h.listRuntimes)
	mux.HandleFunc("POST /runtimes", h.openRuntime)
	mux.HandleFunc("POST /runtimes/{id}/close", h.closeRuntime)
	mux.HandleFunc("GET /remotes", h.listRemoteHosts)
	mux.HandleFunc("POST /remotes", h.saveRemoteHost)
	mux.HandleFunc("GET /remotes/candidates", h.remoteCandidates)
	mux.HandleFunc("GET /remotes/{host}/tree", h.remoteTree)
	mux.HandleFunc("GET /remotes/{host}/dirs", h.remoteDirs)
	mux.HandleFunc("GET /remotes/{host}/probe", h.remoteProbe)
	mux.HandleFunc("POST /remotes/{host}/workspaces", h.addRemoteWorkspace)
	mux.HandleFunc("POST /remotes/{host}/workspaces/remove", h.removeRemoteWorkspace)
	mux.HandleFunc("POST /remotes/remove", h.removeRemoteHost)
	mux.HandleFunc("POST /remotes/open", h.openRemoteRuntime)
	h.registerTreeRoutes(mux)
	h.registerTrayRoutes(mux)
	h.registerStudioVersionRoutes(mux)
	h.registerUpdateRoutes(mux)
	h.registerAskRoutes(mux)
	mux.HandleFunc(runtimePrefix+"{id}/", h.routeRuntime)
	mux.HandleFunc("/", h.routeDefault)
	return logMiddleware(h.auth.middleware(withPage(csrfGuard(mux), h.opts.Page)))
}

// The ceiling rides the list rather than a second endpoint: a client that
// hardcoded it would grey out its control at the wrong count.
func (h *Hub) listRuntimes(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("X-Panes-Max", strconv.Itoa(maxRuntimes()))
	writeJSON(w, h.List())
}

func (h *Hub) openRuntime(w http.ResponseWriter, r *http.Request) {
	var req OpenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badBody(w)
		return
	}
	rt, err := h.Open(r.Context(), req)
	if err != nil {
		// Every refusal Open produces carries its own status; what is left is
		// a pane the kernel could not build, which is ours and not a clash.
		// 409 as the catch-all dressed a missing transcript as a held one.
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, rt.view())
}

func (h *Hub) closeRuntime(w http.ResponseWriter, r *http.Request) {
	if err := h.Close(r.PathValue("id")); err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Hub) routeRuntime(w http.ResponseWriter, r *http.Request) {
	rt := h.Get(r.PathValue("id"))
	if rt == nil {
		notFound(w, "runtime", r.PathValue("rt"))
		return
	}
	rt.handler.ServeHTTP(w, r)
}

// routeDefault keeps an unprefixed client working — a browser opened straight
// at the port, or an older frontend — by serving the first runtime.
func (h *Hub) routeDefault(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	var rt *Runtime
	if len(h.order) > 0 {
		rt = h.runtimes[h.order[0]]
	}
	h.mu.RUnlock()
	if rt == nil {
		refuse(w, http.StatusServiceUnavailable, "hub.no_runtime_open", "no runtime is open", nil)
		return
	}
	rt.Server.routes().ServeHTTP(w, r)
}

// publish registers a runtime and freezes the handler that serves it. The
// prefix is stripped here so the runtime's own routes stay unprefixed.
func (h *Hub) publish(rt *Runtime) {
	if rt.remote != nil {
		rt.handler = http.StripPrefix(runtimePrefix+rt.ID, remoteProxy(rt.remote.ep))
	} else {
		// A local runtime a Hub publishes is driven by an interactive frontend,
		// so that binding is completed here, not at whichever entry point made
		// it — Adopt takes what the host built, and this is the half it cannot.
		rt.Server.Controller().EnableInteractiveApproval()
		rt.handler = http.StripPrefix(runtimePrefix+rt.ID, rt.Server.routes())
	}
	h.mu.Lock()
	h.runtimes[rt.ID] = rt
	h.order = append(h.order, rt.ID)
	h.mu.Unlock()
	if h.opts.OnOpen != nil {
		h.opts.OnOpen(rt)
	}
}

// findSession returns the runtime already driving path. Session paths reach us
// spelled more than one way, so compare them canonically — binding a second
// writer to one transcript is what forks a recovery branch on every save.
func (h *Hub) findSession(path string) *Runtime {
	path = agent.CanonicalSessionPath(strings.TrimSpace(path))
	if path == "" {
		return nil
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, id := range h.order {
		rt := h.runtimes[id]
		// A remote transcript names a file on another machine: it cannot
		// collide with a local one, and deduplicating it is that kernel's job.
		if rt != nil && rt.remote == nil && agent.CanonicalSessionPath(rt.Server.Controller().SessionPath()) == path {
			return rt
		}
	}
	return nil
}

// resolveRoot decides which folder a new runtime opens against: the one asked
// for, the one that owns the session being opened, or the first runtime's.
func (h *Hub) resolveRoot(req OpenRequest) (string, error) {
	if root := strings.TrimSpace(req.Root); root != "" {
		return resolveWorkspaceDir(root)
	}
	if path := strings.TrimSpace(req.SessionPath); path != "" {
		if root := workspaceRootForSession(path); root != "" {
			return root, nil
		}
	}
	h.mu.RLock()
	first := ""
	if len(h.order) > 0 {
		if rt := h.runtimes[h.order[0]]; rt != nil {
			first = rt.Server.Controller().WorkspaceRoot()
		}
	}
	h.mu.RUnlock()
	if first != "" {
		return first, nil
	}
	// Closing the last pane leaves nothing to infer from, and "open a session"
	// is exactly what someone does next. Fall back to the remembered list, the
	// same answer the window uses when it launches with no pane at all.
	for _, dir := range Workspaces() {
		if st, err := os.Stat(dir); err == nil && st.IsDir() {
			return dir, nil
		}
	}
	return "", errors.New("no workspace to open in — add a folder first")
}

func (h *Hub) nextID() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.seq++
	return fmt.Sprintf("r%d", h.seq)
}

func removeString(values []string, drop string) []string {
	out := values[:0]
	for _, v := range values {
		if v != drop {
			out = append(out, v)
		}
	}
	return out
}
