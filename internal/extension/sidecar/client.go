package sidecar

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"reasonix/internal/extension"
	"reasonix/internal/extension/protocol"
	"reasonix/internal/extension/rpcwire"
	"reasonix/internal/pluginpkg"
	"reasonix/internal/secrets"
)

// Lifecycle budgets.
const (
	// defaultHandshakeTimeout bounds the extension/initialize round trip.
	defaultHandshakeTimeout = 30 * time.Second
	// defaultShutdownRequestTimeout bounds the extension/shutdown request
	// before the bounded process close takes over.
	defaultShutdownRequestTimeout = 5 * time.Second
	// queuedNotifications bounds the ordered notification queue (provider
	// stream chunks). A full queue fails the connection rather than dropping.
	queuedNotifications = 256
	// defaultWriteStallBound caps stalled outbound writes (sidecar not reading).
	defaultWriteStallBound = 10 * time.Second
	// maxInterceptTimeout is the 60s ceiling every sync-intercept budget is
	// clamped to, including manifest overrides.
	maxInterceptTimeout = 60 * time.Second
	// fastInterceptTimeout is the default budget for the latency-sensitive
	// input/tool/permission points.
	fastInterceptTimeout = 5 * time.Second
	// slowInterceptTimeout is the default budget for the session, context,
	// system-prompt, and compaction family (and any point not on the fast
	// path).
	slowInterceptTimeout = 30 * time.Second
)

// UIHandler serves the extension's Extension → Host UI calls. Stage 8 wires
// real frontends; the nil default answers "ui not available".
type UIHandler interface {
	Publish(ctx context.Context, p protocol.UIPublishParams) (protocol.UIPublishResult, error)
	Request(ctx context.Context, p protocol.UIRequestParams) (protocol.UIRequestResult, error)
}

// UIBinder is the optional interface a UIHandler implements to receive
// per-plugin bindings and crash notifications from StartPackages (the stage-8
// UI hub). HandlerFor returns the handler installed on one client's
// connection; ClientCrashed reports that plugin's sidecar dying.
type UIBinder interface {
	UIHandler
	HandlerFor(pluginID string) UIHandler
	ClientCrashed(pluginID string)
}

// unavailableUIHandler is the default UIHandler: every call fails with the
// frozen unknown_method reason so an extension depending on UI degrades
// loudly instead of blocking forever.
type unavailableUIHandler struct{}

func (unavailableUIHandler) Publish(context.Context, protocol.UIPublishParams) (protocol.UIPublishResult, error) {
	return protocol.UIPublishResult{}, &protocol.ProtocolError{Reason: protocol.ErrUnknownMethod, Message: "extension UI is not available on this host"}
}

func (unavailableUIHandler) Request(context.Context, protocol.UIRequestParams) (protocol.UIRequestResult, error) {
	return protocol.UIRequestResult{}, &protocol.ProtocolError{Reason: protocol.ErrUnknownMethod, Message: "extension UI is not available on this host"}
}

// StreamRouter receives the extension's provider stream notifications. The
// stage 7 adapter (internal/extension/providerext) installs the real router
// through SetStreamRouter; the nil default drops with a debug log.
type StreamRouter interface {
	RouteStreamChunk(p protocol.StreamChunkParams)
	RouteStreamEnd(p protocol.StreamEndParams)
}

type dropStreamRouter struct{ pluginID string }

func (r dropStreamRouter) RouteStreamChunk(p protocol.StreamChunkParams) {
	slog.Debug("sidecar: dropping provider stream chunk (no stream router)", "plugin", r.pluginID, "stream", p.StreamID, "seq", p.Seq)
}

func (r dropStreamRouter) RouteStreamEnd(p protocol.StreamEndParams) {
	slog.Debug("sidecar: dropping provider stream end (no stream router)", "plugin", r.pluginID, "stream", p.StreamID)
}

// ClientOptions configures one sidecar client.
type ClientOptions struct {
	// Package and Installed are the pluginpkg installed-state entry this
	// sidecar launches for. Package.Manifest.Runtime must be non-nil.
	Package   pluginpkg.Package
	Installed pluginpkg.InstalledPlugin
	// Session identifies the session the extension serves.
	Session protocol.SessionContext
	// UI routes host/ui/* calls; nil means "ui not available".
	UI UIHandler
	// Streams routes provider stream notifications; nil drops them.
	Streams StreamRouter
	// OnCrash fires exactly once when a started sidecar's connection ends
	// unexpectedly. Optional.
	OnCrash func(error)
	// UIHostKind declares which host surface family renders extension UI.
	// Empty means headless.
	UIHostKind protocol.UIHostKind
	// HandshakeTimeout bounds extension/initialize; zero uses 30s.
	HandshakeTimeout time.Duration
	// WriteStallBound bounds how long any outbound write may make no progress
	// (the sidecar is alive but has stopped reading stdin) before the
	// connection fails and the process is killed. Zero uses 10s. Without it a
	// wedged reader would hang intercepts, provider/UI calls, and shutdown.
	WriteStallBound time.Duration
}

func (o *ClientOptions) validate() error {
	if o.Package.Manifest.Runtime == nil {
		return fmt.Errorf("sidecar: plugin %q declares no runtime", o.Installed.Name)
	}
	if strings.TrimSpace(o.Installed.Name) == "" {
		return errors.New("sidecar: installed plugin name is required")
	}
	if strings.TrimSpace(o.Session.SessionID) == "" || strings.TrimSpace(o.Session.WorkspaceRoot) == "" {
		return errors.New("sidecar: session context requires a session ID and workspace root")
	}
	return nil
}

type handshakeState uint8

const (
	handshakeNew handshakeState = iota
	handshakeReady
	handshakePoisoned
	handshakeShutdown
)

// Client is one live sidecar connection: the rpcwire transport, the handshake
// state, the content store, and the process handle.
type Client struct {
	pluginID         string
	version          string
	rt               *pluginpkg.RuntimeSpec
	requires         []pluginpkg.CapabilityRef // manifest v2 dependency requirements
	provides         []pluginpkg.CapabilityRef // manifest v2 capability ceiling
	session          protocol.SessionContext
	uiHost           protocol.UIHostKind
	handshakeTimeout time.Duration
	proc             *process
	conn             *rpcwire.Conn
	store            *Store
	ui               UIHandler
	streams          StreamRouter
	streamsMu        sync.RWMutex
	onCrash          func(error)
	initResult       protocol.InitializeResult

	mu       sync.Mutex
	state    handshakeState
	poisoned error

	crashed   atomic.Bool
	crashOnce sync.Once

	shutdownOnce sync.Once
	serveExited  chan struct{}
	seq          atomic.Uint64
}

// StartClient spawns the sidecar and runs the initialize handshake. The host
// sends extension/initialize first; any Extension → Host traffic before the
// handshake completes poisons the connection and fails the start. On any
// failure the process is killed and reaped before StartClient returns.
func StartClient(ctx context.Context, opts ClientOptions) (*Client, error) {
	started := time.Now()
	if err := opts.validate(); err != nil {
		return nil, err
	}
	p, err := startProcess(opts.Package, opts.Installed)
	if err != nil {
		return nil, err
	}
	c := newClient(p, opts)
	go c.supervise()
	if err := c.handshake(ctx); err != nil {
		// The handshake owns the connection until ready: unwind it by killing
		// the tree, then reap and drain the serve loop, all bounded.
		c.proc.kill()
		waitWithBudget(c.proc.wait, closeWaitBudget)
		select {
		case <-c.serveExited:
		case <-time.After(closeWaitBudget):
		}
		return nil, newStartupFailure("handshake", started, p.stderr.String(), err)
	}
	return c, nil
}

func newClient(p *process, opts ClientOptions) *Client {
	ui := opts.UI
	if ui == nil {
		ui = unavailableUIHandler{}
	}
	streams := opts.Streams
	if streams == nil {
		streams = dropStreamRouter{pluginID: p.pluginID}
	}
	uiHost := opts.UIHostKind
	if uiHost == "" {
		uiHost = protocol.UIHostHeadless
	}
	handshakeTimeout := opts.HandshakeTimeout
	if handshakeTimeout <= 0 {
		handshakeTimeout = defaultHandshakeTimeout
	}
	stallBound := opts.WriteStallBound
	if stallBound <= 0 {
		stallBound = defaultWriteStallBound
	}
	version := strings.TrimSpace(opts.Installed.Version)
	if version == "" {
		version = strings.TrimSpace(opts.Package.Manifest.Version)
	}
	c := &Client{
		pluginID:         p.pluginID,
		version:          version,
		rt:               opts.Package.Manifest.Runtime,
		requires:         append([]pluginpkg.CapabilityRef(nil), opts.Package.Manifest.Requires...),
		provides:         append([]pluginpkg.CapabilityRef(nil), opts.Package.Manifest.Provides...),
		session:          opts.Session,
		uiHost:           uiHost,
		handshakeTimeout: handshakeTimeout,
		proc:             p,
		store:            NewStore(),
		ui:               ui,
		streams:          streams,
		onCrash:          opts.OnCrash,
		serveExited:      make(chan struct{}),
	}
	c.conn = rpcwire.NewConn(p.stdout, p.stdin, rpcwire.Options{
		Name:                   "extension:" + p.pluginID,
		MaxInboundBytes:        protocol.FrameBytes,
		MaxOutboundBytes:       protocol.FrameBytes,
		StrictJSONRPC:          true,
		MaxQueuedNotifications: queuedNotifications,
		MaxWriteStall:          stallBound,
		BeforeRequest:          c.beforeRequest,
		BeforeNotification:     c.beforeNotification,
	})
	c.conn.Handle(string(protocol.MethodHostContentRead), c.store.ReadHandler)
	c.conn.Handle(string(protocol.MethodHostUIPublish), c.handleUIPublish)
	c.conn.Handle(string(protocol.MethodHostUIRequest), c.handleUIRequest)
	c.conn.HandleNotify(string(protocol.MethodExtensionProviderStreamChunk), c.handleStreamChunk)
	c.conn.HandleNotify(string(protocol.MethodExtensionProviderStreamEnd), c.handleStreamEnd)
	return c
}

// supervise runs the read loop for the life of the connection and turns an
// unexpected end into exactly one crash notification.
func (c *Client) supervise() {
	err := c.conn.Serve(context.Background())
	go c.proc.wait() // reap the zombie promptly; bounded callers never wait on it
	c.mu.Lock()
	orderly := c.state == handshakeShutdown
	started := c.state == handshakeReady
	c.mu.Unlock()
	if !orderly {
		crashErr := err
		if crashErr == nil {
			crashErr = errors.New("extension sidecar exited")
		}
		// An unexpected end can leave the process ALIVE but unreachable — a
		// wedged reader whose pipe writes stalled out, for example. Kill the
		// tree so it never outlives its connection; for a genuinely crashed
		// sidecar the kill is a no-op.
		go c.proc.kill()
		c.crashed.Store(true)
		if started && c.onCrash != nil {
			c.crashOnce.Do(func() { c.onCrash(crashErr) })
		}
	}
	close(c.serveExited)
}

// beforeRequest gates Extension → Host requests on handshake completion,
// running on the read loop so the decision observes wire arrival order. Any
// request before initialized poisons the connection: the sidecar broke the
// protocol's first-rule and cannot be trusted further.
func (c *Client) beforeRequest(method string, _ json.RawMessage) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state != handshakeReady {
		c.poisonLocked(fmt.Errorf("extension %s sent request %q before extension/initialized", c.pluginID, method))
		return protocol.MustProtocolError(protocol.ErrProtocolError).RPCError()
	}
	return nil
}

// beforeNotification applies the same gate to notifications: provider stream
// traffic is only valid once the handshake completed.
func (c *Client) beforeNotification(method string, _ json.RawMessage) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state != handshakeReady {
		c.poisonLocked(fmt.Errorf("extension %s sent notification %q before extension/initialized", c.pluginID, method))
		return protocol.MustProtocolError(protocol.ErrProtocolError).RPCError()
	}
	return nil
}

// poisonLocked records the protocol violation and kills the process so a
// pending handshake unwinds immediately instead of waiting out its timeout.
func (c *Client) poisonLocked(err error) {
	if c.state == handshakePoisoned || c.state == handshakeShutdown {
		return
	}
	c.state = handshakePoisoned
	c.poisoned = err
	go c.proc.kill()
}

// handshake sends extension/initialize (the host's first and only opening
// move), validates the sidecar's declarations against the manifest, and
// finishes with extension/initialized.
func (c *Client) handshake(ctx context.Context) error {
	return c.handshakeWithTimeout(ctx, c.handshakeTimeout)
}

func (c *Client) handshakeWithTimeout(ctx context.Context, timeout time.Duration) error {
	tctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	params := c.initializeParams()
	raw, err := c.conn.Request(tctx, string(protocol.MethodExtensionInitialize), params)
	if err != nil {
		if perr := c.poisonError(); perr != nil {
			return perr
		}
		return mapRequestError(err)
	}
	decoded, err := protocol.DecodeHostRequestResult(protocol.MethodExtensionInitialize, raw)
	if err != nil {
		return &protocol.ProtocolError{Reason: protocol.ErrProtocolError, Message: "invalid initialize result: " + err.Error()}
	}
	result := decoded.(protocol.InitializeResult)
	if err := c.validateHandshakeResult(result); err != nil {
		return err
	}
	c.mu.Lock()
	if c.state == handshakePoisoned {
		poisoned := c.poisoned
		c.mu.Unlock()
		return &protocol.ProtocolError{Reason: protocol.ErrProtocolError, Message: poisoned.Error()}
	}
	c.state = handshakeReady
	c.initResult = result
	c.mu.Unlock()
	if err := c.conn.Notify(string(protocol.MethodExtensionInitialized), protocol.InitializedParams{}); err != nil {
		return err
	}
	return nil
}

func (c *Client) poisonError() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state == handshakePoisoned && c.poisoned != nil {
		return &protocol.ProtocolError{Reason: protocol.ErrProtocolError, Message: c.poisoned.Error()}
	}
	return nil
}

// validateHandshakeResult enforces the declaration contract: the sidecar's
// protocol version must be supported, and every capability it activated must
// be a subset of what its installed manifest declared.
func (c *Client) validateHandshakeResult(result protocol.InitializeResult) error {
	if err := protocol.CompareProtocolVersion(protocol.ProtocolID, result.ProtocolVersion); err != nil {
		return err
	}
	rt := c.rt
	capabilityErr := func(format string, args ...any) error {
		return &protocol.ProtocolError{Reason: protocol.ErrCapabilityNotDeclared, Message: fmt.Sprintf(format, args...)}
	}
	for _, point := range result.Subscriptions {
		if !containsString(rt.Intercepts, point) {
			return capabilityErr("extension %s subscribed to %q which its manifest does not intercept", c.pluginID, point)
		}
	}
	for _, slot := range result.Replaces {
		if !containsString(rt.Replaces, slot) {
			return capabilityErr("extension %s replaced %q which its manifest does not declare", c.pluginID, slot)
		}
	}
	if len(result.Providers) > 0 {
		if !containsString(rt.Capabilities, "providers") {
			return capabilityErr("extension %s declared providers without the providers capability", c.pluginID)
		}
		prefix := "plugin/" + c.pluginID + "/"
		for _, desc := range result.Providers {
			if !strings.HasPrefix(desc.Ref, prefix) {
				return capabilityErr("extension %s declared provider ref %q outside its %q namespace", c.pluginID, desc.Ref, prefix)
			}
		}
	}
	if len(result.UIActions) > 0 && !containsString(rt.Capabilities, "ui") {
		return capabilityErr("extension %s declared UI actions without the ui capability", c.pluginID)
	}
	// Manifest provides is the capability ceiling: handshake must not claim
	// capabilities the package never declared. Declared-but-missing provides
	// stay Unavailable (no forge) — callers read Status via the lifecycle registry.
	if err := validateProvidesCeiling(c.provides, result.Provides); err != nil {
		return &protocol.ProtocolError{Reason: protocol.ErrCapabilityNotDeclared, Message: err.Error()}
	}
	return nil
}

// readyErr reports whether the client can serve calls right now.
func (c *Client) readyErr() error {
	if c.crashed.Load() {
		return &protocol.ProtocolError{Reason: protocol.ErrProviderInterrupted, Message: "extension sidecar " + c.pluginID + " crashed"}
	}
	c.mu.Lock()
	state := c.state
	c.mu.Unlock()
	switch state {
	case handshakeReady:
		return nil
	case handshakeShutdown:
		return &protocol.ProtocolError{Reason: protocol.ErrProviderInterrupted, Message: "extension sidecar " + c.pluginID + " is shut down"}
	default:
		return &protocol.ProtocolError{Reason: protocol.ErrProtocolError, Message: "extension sidecar " + c.pluginID + " is not initialized"}
	}
}

// PluginID returns the installed plugin package name this client serves.
func (c *Client) PluginID() string { return c.pluginID }

// Required reports whether the plugin's manifest marked its runtime
// required:true — the dispatcher treats such extensions as required-class.
func (c *Client) Required() bool { return c.rt.Required }

// Handshake returns the sidecar's validated initialize result — its declared
// subscriptions, replacements, providers, and UI actions.
func (c *Client) Handshake() protocol.InitializeResult {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.initResult
}

// Store returns the client's content store for externalizing payloads.
func (c *Client) Store() *Store { return c.store }

// Crashed reports whether the connection ended unexpectedly.
func (c *Client) Crashed() bool { return c.crashed.Load() }

// Disconnected returns a channel closed when the connection's serve loop ends
// for any reason — crash, orderly shutdown, or transport failure. Provider
// stream watchers select on it to finish in-flight streams instead of hanging
// on notifications that will never arrive.
func (c *Client) Disconnected() <-chan struct{} { return c.serveExited }

// SetStreamRouter swaps the provider stream router (stage 7). Nil restores
// the drop-with-debug-log default. It is safe to call while notifications are
// in flight; routing for later notifications uses the new router.
func (c *Client) SetStreamRouter(r StreamRouter) {
	if r == nil {
		r = dropStreamRouter{pluginID: c.pluginID}
	}
	c.streamsMu.Lock()
	c.streams = r
	c.streamsMu.Unlock()
}

// streamRouter returns the currently installed router.
func (c *Client) streamRouter() StreamRouter {
	c.streamsMu.RLock()
	defer c.streamsMu.RUnlock()
	return c.streams
}

// Exited reports whether the sidecar process has been reaped.
func (c *Client) Exited() bool {
	select {
	case <-c.proc.waitDone:
		return true
	default:
		return false
	}
}

// TimeoutFor resolves the sync-intercept budget for one point: the manifest's
// per-runtime override clamped to the 60s ceiling, or the point-family
// default (5s for input/tool/permission, 30s for the session, system-prompt,
// context, and compaction family).
func (c *Client) TimeoutFor(point extension.InterceptorPoint) time.Duration {
	if c.rt.TimeoutMillis > 0 {
		timeout := min(time.Duration(c.rt.TimeoutMillis)*time.Millisecond, maxInterceptTimeout)
		return timeout
	}
	switch point {
	case extension.PointInputReceive, extension.PointToolBefore,
		extension.PointToolAfter, extension.PointPermissionDecision:
		return fastInterceptTimeout
	default:
		return slowInterceptTimeout
	}
}

// Intercept makes the blocking extension/intercept call. A late answer maps
// to the frozen intercept_timeout error; a crashed or closed sidecar fails
// fast with the provider_interrupted family instead of waiting. A payload
// above protocol.ExternalizeFieldBytes moves into this connection's content
// store and travels as a content-ref envelope; an externalized replacement in
// the answer is paged back and verified before the caller's strict decode.
func (c *Client) Intercept(ctx context.Context, event protocol.InterceptEvent, payload json.RawMessage, timeout time.Duration) (protocol.InterceptResult, error) {
	if err := c.readyErr(); err != nil {
		return protocol.InterceptResult{}, err
	}
	if timeout <= 0 {
		timeout = c.TimeoutFor(extension.InterceptorPoint(event))
	}
	tctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	params := protocol.InterceptParams{
		Event:         event,
		Seq:           c.seq.Add(1),
		Payload:       payload,
		TimeoutMillis: int(timeout.Milliseconds()),
	}
	if err := c.externalizeInterceptParams(&params); err != nil {
		return protocol.InterceptResult{}, err
	}
	raw, err := c.conn.Request(tctx, string(protocol.MethodExtensionIntercept), params)
	if err != nil {
		if errors.Is(tctx.Err(), context.DeadlineExceeded) && ctx.Err() == nil {
			return protocol.InterceptResult{}, &protocol.ProtocolError{
				Reason:  protocol.ErrInterceptTimeout,
				Message: fmt.Sprintf("extension %s did not answer %s within %s", c.pluginID, event, timeout),
			}
		}
		return protocol.InterceptResult{}, mapRequestError(err)
	}
	decoded, err := protocol.DecodeHostRequestResult(protocol.MethodExtensionIntercept, raw)
	if err != nil {
		return protocol.InterceptResult{}, &protocol.ProtocolError{Reason: protocol.ErrProtocolError, Message: "invalid intercept result: " + err.Error()}
	}
	result := decoded.(protocol.InterceptResult)
	if err := c.resolveExternalizedReplacement(&result); err != nil {
		return protocol.InterceptResult{}, err
	}
	return result, nil
}

// TryNotifyEvent non-blockingly enqueues the fire-and-forget extension/event
// observation. The payload follows the same content-ref rule as
// extension/intercept. Queue saturation drops the observation instead of
// propagating sidecar backpressure into the Agent hot path.
func (c *Client) TryNotifyEvent(event protocol.InterceptEvent, payload json.RawMessage) error {
	if err := c.readyErr(); err != nil {
		return err
	}
	params := protocol.EventParams{Event: event, Payload: payload}
	if err := c.externalizeEventParams(&params); err != nil {
		return err
	}
	return c.conn.TryNotify(string(protocol.MethodExtensionEvent), params)
}

// NotifyEvent is the compatibility spelling for direct callers. Its delivery
// semantics are the same non-blocking enqueue as TryNotifyEvent.
func (c *Client) NotifyEvent(event protocol.InterceptEvent, payload json.RawMessage) error {
	return c.TryNotifyEvent(event, payload)
}

// NotifyResourcesChanged sends extension/resources/changed.
func (c *Client) NotifyResourcesChanged(paths []string) error {
	if err := c.readyErr(); err != nil {
		return err
	}
	return c.conn.Notify(string(protocol.MethodExtensionResourcesChanged), protocol.ResourcesChangedParams{Paths: paths})
}

// UIAction invokes one handshake-declared UI action on the sidecar (stage 8).
// The host UI hub routes /<plugin>:<action> invocations here. A crashed or
// shut-down sidecar fails fast with the provider_interrupted reason.
func (c *Client) UIAction(ctx context.Context, params protocol.UIActionParams) (protocol.UIActionResult, error) {
	if err := c.readyErr(); err != nil {
		return protocol.UIActionResult{}, err
	}
	raw, err := c.conn.Request(ctx, string(protocol.MethodExtensionUIAction), params)
	if err != nil {
		return protocol.UIActionResult{}, mapRequestError(err)
	}
	decoded, err := protocol.DecodeHostRequestResult(protocol.MethodExtensionUIAction, raw)
	if err != nil {
		return protocol.UIActionResult{}, &protocol.ProtocolError{Reason: protocol.ErrProtocolError, Message: "invalid UI action result: " + err.Error()}
	}
	return decoded.(protocol.UIActionResult), nil
}

// UISubmit delivers a form surface's values back to the sidecar (stage 8).
// The host UI hub routes submissions here.
func (c *Client) UISubmit(ctx context.Context, params protocol.UISubmitParams) (protocol.UISubmitResult, error) {
	if err := c.readyErr(); err != nil {
		return protocol.UISubmitResult{}, err
	}
	raw, err := c.conn.Request(ctx, string(protocol.MethodExtensionUISubmit), params)
	if err != nil {
		return protocol.UISubmitResult{}, mapRequestError(err)
	}
	decoded, err := protocol.DecodeHostRequestResult(protocol.MethodExtensionUISubmit, raw)
	if err != nil {
		return protocol.UISubmitResult{}, &protocol.ProtocolError{Reason: protocol.ErrProtocolError, Message: "invalid UI submit result: " + err.Error()}
	}
	return decoded.(protocol.UISubmitResult), nil
}

// ProviderCatalog fetches the sidecar's extension-hosted provider catalog
// (stage 7). The result carries no credentials — the sidecar's refs,
// descriptors, and declared capabilities only.
func (c *Client) ProviderCatalog(ctx context.Context) ([]protocol.ProviderDescriptor, error) {
	if err := c.readyErr(); err != nil {
		return nil, err
	}
	raw, err := c.conn.Request(ctx, string(protocol.MethodExtensionProviderCatalog), protocol.ProviderCatalogParams{})
	if err != nil {
		return nil, mapRequestError(err)
	}
	decoded, err := protocol.DecodeHostRequestResult(protocol.MethodExtensionProviderCatalog, raw)
	if err != nil {
		return nil, &protocol.ProtocolError{Reason: protocol.ErrProtocolError, Message: "invalid provider catalog result: " + err.Error()}
	}
	return decoded.(protocol.ProviderCatalogResult).Providers, nil
}

// ProviderStreamOpen asks the sidecar to start one provider stream (stage 7).
// Accepted streams deliver chunks as extension/provider/stream/chunk
// notifications routed to the installed StreamRouter and exactly one
// stream/end. A crashed or shut-down sidecar fails fast with the
// provider_interrupted reason.
func (c *Client) ProviderStreamOpen(ctx context.Context, params protocol.StreamOpenParams) (protocol.StreamOpenResult, error) {
	if err := c.readyErr(); err != nil {
		return protocol.StreamOpenResult{}, err
	}
	raw, err := c.conn.Request(ctx, string(protocol.MethodExtensionProviderStreamOpen), params)
	if err != nil {
		return protocol.StreamOpenResult{}, mapRequestError(err)
	}
	decoded, err := protocol.DecodeHostRequestResult(protocol.MethodExtensionProviderStreamOpen, raw)
	if err != nil {
		return protocol.StreamOpenResult{}, &protocol.ProtocolError{Reason: protocol.ErrProtocolError, Message: "invalid stream open result: " + err.Error()}
	}
	return decoded.(protocol.StreamOpenResult), nil
}

// ProviderStreamCancel cancels one in-flight provider stream, best effort: a
// wedged or dead sidecar simply never answers inside the bounded budget.
func (c *Client) ProviderStreamCancel(streamID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _ = c.conn.Request(ctx, string(protocol.MethodExtensionProviderStreamCancel), protocol.StreamCancelParams{StreamID: streamID})
}

// Shutdown stops the sidecar with the bounded sequence: extension/shutdown
// (bounded by timeout), close stdin, a 750ms EOF grace, a process-tree kill,
// and a 5s reap. It is idempotent; later calls return immediately.
func (c *Client) Shutdown(_ context.Context, timeout time.Duration) error {
	c.shutdownOnce.Do(func() {
		c.mu.Lock()
		wasReady := c.state == handshakeReady
		if c.state != handshakePoisoned {
			c.state = handshakeShutdown
		}
		c.mu.Unlock()
		if timeout <= 0 {
			timeout = defaultShutdownRequestTimeout
		}
		if wasReady && !c.crashed.Load() && c.conn != nil {
			tctx, cancel := context.WithTimeout(context.Background(), timeout)
			_, _ = c.conn.Request(tctx, string(protocol.MethodExtensionShutdown), protocol.ShutdownParams{
				TimeoutMillis: int(timeout.Milliseconds()),
			})
			cancel()
		}
		if c.proc != nil {
			c.proc.close()
		}
		if c.serveExited != nil {
			select {
			case <-c.serveExited:
			case <-time.After(closeWaitBudget):
			}
		}
	})
	return nil
}

// Close shuts the sidecar down with default budgets.
func (c *Client) Close() error {
	return c.Shutdown(context.Background(), defaultShutdownRequestTimeout)
}

func (c *Client) handleUIPublish(ctx context.Context, raw json.RawMessage) (any, error) {
	decoded, err := protocol.DecodeExtensionRequestParams(protocol.MethodHostUIPublish, raw)
	if err != nil {
		return nil, protocol.MustProtocolError(protocol.ErrInvalidParams).RPCError()
	}
	result, err := c.ui.Publish(ctx, decoded.(protocol.UIPublishParams))
	if err != nil {
		return nil, mapHandlerError(err)
	}
	return result, nil
}

func (c *Client) handleUIRequest(ctx context.Context, raw json.RawMessage) (any, error) {
	decoded, err := protocol.DecodeExtensionRequestParams(protocol.MethodHostUIRequest, raw)
	if err != nil {
		return nil, protocol.MustProtocolError(protocol.ErrInvalidParams).RPCError()
	}
	result, err := c.ui.Request(ctx, decoded.(protocol.UIRequestParams))
	if err != nil {
		return nil, mapHandlerError(err)
	}
	return result, nil
}

func (c *Client) handleStreamChunk(_ context.Context, raw json.RawMessage) {
	decoded, err := protocol.DecodeExtensionNotificationParams(protocol.MethodExtensionProviderStreamChunk, raw)
	if err != nil {
		slog.Debug("sidecar: dropping malformed stream chunk", "plugin", c.pluginID, "err", err)
		return
	}
	c.streamRouter().RouteStreamChunk(decoded.(protocol.StreamChunkParams))
}

func (c *Client) handleStreamEnd(_ context.Context, raw json.RawMessage) {
	decoded, err := protocol.DecodeExtensionNotificationParams(protocol.MethodExtensionProviderStreamEnd, raw)
	if err != nil {
		slog.Debug("sidecar: dropping malformed stream end", "plugin", c.pluginID, "err", err)
		return
	}
	c.streamRouter().RouteStreamEnd(decoded.(protocol.StreamEndParams))
}

// mapHandlerError converts a UIHandler failure into a wire-safe error.
func mapHandlerError(err error) error {
	var protocolErr *protocol.ProtocolError
	if errors.As(err, &protocolErr) {
		return protocolErr.RPCError()
	}
	var rpcErr *rpcwire.RPCError
	if errors.As(err, &rpcErr) {
		return rpcErr
	}
	return protocol.MustProtocolError(protocol.ErrInternal).RPCError()
}

// mapRequestError converts a failed outbound call: peer protocol errors keep
// their frozen reason; transport endings map to the crash/shutdown family.
func mapRequestError(err error) error {
	var respErr *rpcwire.ResponseError
	if errors.As(err, &respErr) {
		message := secrets.RedactCredentials(respErr.Message)
		var data protocol.ProtocolErrorData
		if len(respErr.Data) > 0 && json.Unmarshal(respErr.Data, &data) == nil && data.Validate() == nil {
			return &protocol.ProtocolError{Reason: data.Reason, Message: message}
		}
		// Invalid or absent protocol data still came from the untrusted peer.
		// Preserve the transport code for diagnostics, but never let its message
		// bypass the host's credential-redaction boundary.
		return &rpcwire.ResponseError{Code: respErr.Code, Message: message, Data: append(json.RawMessage(nil), respErr.Data...)}
	}
	return err
}

func containsString(items []string, value string) bool {
	return slices.Contains(items, value)
}
