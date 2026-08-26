package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"reasonix/internal/mcpdiag"
	"reasonix/internal/tool"
)

// SessionState is the transport lifecycle state exposed to local diagnostics.
// It intentionally contains no endpoint, project path, or session identifier.
type SessionState string

const (
	SessionStateConnecting   SessionState = "connecting"
	SessionStateListening    SessionState = "listening"
	SessionStateReady        SessionState = "ready"
	SessionStateReconnecting SessionState = "reconnecting"
	SessionStateFailed       SessionState = "failed"
	SessionStateClosed       SessionState = "closed"
)

// SessionErrorKind classifies failures without exposing transport secrets.
type SessionErrorKind string

const (
	SessionErrorNone           SessionErrorKind = ""
	SessionErrorAuthRequired   SessionErrorKind = "auth_required"
	SessionErrorSessionMissing SessionErrorKind = "session_missing"
	SessionErrorStreamClosed   SessionErrorKind = "stream_closed"
	SessionErrorTimeout        SessionErrorKind = "timeout"
	SessionErrorProtocol       SessionErrorKind = "protocol"
	SessionErrorTransport      SessionErrorKind = "transport"
)

type sessionDiagnostics struct {
	ProtocolVersion   string
	State             SessionState
	SessionIDPresent  bool
	ReconnectAttempts int
	LastErrorKind     SessionErrorKind
	LastError         string
}

type sessionDiagnosticsProvider interface {
	sessionDiagnostics() sessionDiagnostics
}

type sdkEndpoint struct {
	transport     mcpsdk.Transport
	close         func()
	startupStderr func() string
}

type managedMCPSession struct {
	generation uint64
	session    *mcpsdk.ClientSession
	endpoint   sdkEndpoint
	protocol   string
}

type sessionBuild struct {
	done    chan struct{}
	session *managedMCPSession
	err     error
}

// sdkSessionTransport is the single connection owner for one configured MCP
// server. The official SDK owns JSON-RPC correlation, cancellation, protocol
// negotiation, Streamable HTTP listening, and graceful protocol close.
// Reasonix owns product timeouts, process isolation, security policy, and
// failure-atomic session replacement.
type sdkSessionTransport struct {
	name string
	spec Spec

	lifeCtx context.Context
	cancel  context.CancelFunc

	progress      progressRouter
	notifications notificationRouter
	oauth         *mcpOAuthClient

	mu                sync.Mutex
	current           *managedMCPSession
	building          *sessionBuild
	nextGeneration    uint64
	closed            bool
	state             SessionState
	reconnectAttempts int
	lastErrorKind     SessionErrorKind
	lastError         string
	autoReconnecting  bool
	reconnectDelays   []time.Duration
	lastStartupStderr string
	endpointFactory   func(context.Context) (sdkEndpoint, error)
	wg                sync.WaitGroup
}

var defaultSessionReconnectDelays = []time.Duration{
	time.Second,
	2 * time.Second,
	5 * time.Second,
	10 * time.Second,
	30 * time.Second,
}

var linkedMCPClientVersion atomic.Pointer[string]

// SetMCPClientVersion supplies the release version injected into an executable.
// Library and development builds fall back to module metadata or "dev".
func SetMCPClientVersion(version string) {
	version = strings.TrimSpace(version)
	if version == "" {
		version = "dev"
	}
	linkedMCPClientVersion.Store(&version)
}

func mcpClientVersion() string {
	if version := linkedMCPClientVersion.Load(); version != nil {
		return *version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}

func newSDKSessionTransport(ctx context.Context, s Spec) (*sdkSessionTransport, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	typeName := canonicalMCPRuntimeTransport(s.Type)
	switch typeName {
	case "stdio":
		if strings.TrimSpace(s.Command) == "" {
			return nil, fmt.Errorf("stdio plugin %q: command is required", s.Name)
		}
	case "streamable-http", "sse":
		if err := validateMCPURL(s.Name, typeName, s.URL); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unknown transport type %q (want stdio|http|sse)", s.Type)
	}

	var oauth *mcpOAuthClient
	var err error
	if typeName == "streamable-http" && !hasExplicitMCPAuth(s) {
		oauth, err = newMCPOAuthClient(s.StateDir, s.OAuthHTTPClient)
		if err != nil {
			return nil, fmt.Errorf("http plugin %q: load OAuth state: %w", s.Name, err)
		}
		if oauth != nil && !sameCanonicalResource(oauth.state.Resource, s.URL) {
			return nil, fmt.Errorf("http plugin %q: stored OAuth token belongs to a different MCP resource; clear authentication and authorize this endpoint", s.Name)
		}
	}

	lifeCtx, cancel := context.WithCancel(ctx)
	return &sdkSessionTransport{
		name:            s.Name,
		spec:            s,
		lifeCtx:         lifeCtx,
		cancel:          cancel,
		oauth:           oauth,
		state:           SessionStateConnecting,
		reconnectDelays: append([]time.Duration(nil), defaultSessionReconnectDelays...),
	}, nil
}

func hasExplicitMCPAuth(s Spec) bool {
	return mcpdiag.HasAuthConfig(s.Headers, s.Env, s.URL)
}

func (t *sdkSessionTransport) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	managed, err := t.acquire(ctx)
	if err != nil {
		return nil, t.sanitizeError(err, nil)
	}
	result, err := invokeSDKMethod(ctx, managed.session, method, params)
	if err == nil {
		t.clearRuntimeError(managed)
		return result, nil
	}

	if errors.Is(err, mcpsdk.ErrSessionMissing) {
		if managed.session.ID() == "" {
			endpointErr := fmt.Errorf("MCP endpoint returned HTTP 404 without an established session: %w", err)
			t.noteRuntimeError(managed, SessionErrorProtocol, endpointErr)
			return nil, t.sanitizeError(endpointErr, managed)
		}
		t.noteRuntimeError(managed, SessionErrorSessionMissing, err)
		t.invalidate(managed)
		replacement, rebuildErr := t.acquire(ctx)
		if rebuildErr != nil {
			return nil, t.sanitizeError(fmt.Errorf("MCP session expired; rebuild failed: %w", rebuildErr), managed)
		}
		result, err = invokeSDKMethod(ctx, replacement.session, method, params)
		if err == nil {
			t.clearRuntimeError(replacement)
			return result, nil
		}
		return nil, t.sanitizeError(err, replacement)
	}

	if isTerminalSDKError(err) || isAmbiguousTransportError(err) || errors.Is(err, context.DeadlineExceeded) {
		kind := SessionErrorStreamClosed
		if errors.Is(err, context.DeadlineExceeded) {
			kind = SessionErrorTimeout
		} else if !isTerminalSDKError(err) {
			kind = SessionErrorTransport
		}
		t.noteRuntimeError(managed, kind, err)
		t.invalidate(managed)
		if safeToReplayMCPMethod(method) {
			replacement, rebuildErr := t.acquire(ctx)
			if rebuildErr != nil {
				return nil, t.sanitizeError(fmt.Errorf("MCP connection closed; rebuild failed: %w", rebuildErr), managed)
			}
			result, err = invokeSDKMethod(ctx, replacement.session, method, params)
			if err == nil {
				t.clearRuntimeError(replacement)
				return result, nil
			}
			return nil, t.sanitizeError(err, replacement)
		}
		t.startAutoReconnect()
		return nil, t.sanitizeError(fmt.Errorf("MCP tool connection closed after dispatch; execution result is unknown and the call was not retried: %w", err), managed)
	}

	kind := classifySessionError(err)
	t.noteRuntimeError(managed, kind, err)
	return nil, t.sanitizeError(err, managed)
}

func (t *sdkSessionTransport) registerProgress(token string, sink tool.ProgressFunc) func() {
	unregister := t.progress.registerProgress(token, sink)
	var once sync.Once
	return func() {
		once.Do(func() {
			// The SDK dispatches notifications independently from the response that
			// completes a call. Keep the token briefly so a progress notification
			// already read from the wire cannot lose a race with the response.
			time.AfterFunc(time.Second, unregister)
		})
	}
}

func (t *sdkSessionTransport) registerNotification(method string, callback func(json.RawMessage)) func() {
	return t.notifications.registerNotification(method, callback)
}

func (t *sdkSessionTransport) acquire(ctx context.Context) (*managedMCPSession, error) {
	for {
		t.mu.Lock()
		if t.closed {
			t.mu.Unlock()
			return nil, mcpsdk.ErrConnectionClosed
		}
		if t.current != nil {
			current := t.current
			t.mu.Unlock()
			return current, nil
		}
		if attempt := t.building; attempt != nil {
			done := attempt.done
			t.mu.Unlock()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-t.lifeCtx.Done():
				return nil, mcpsdk.ErrConnectionClosed
			case <-done:
				if attempt.err != nil {
					return nil, attempt.err
				}
				return attempt.session, nil
			}
		}

		attempt := &sessionBuild{done: make(chan struct{})}
		t.building = attempt
		t.nextGeneration++
		generation := t.nextGeneration
		if generation == 1 {
			t.state = SessionStateConnecting
		} else {
			t.state = SessionStateReconnecting
		}
		t.wg.Add(1)
		t.mu.Unlock()
		go t.runBuild(attempt, generation)
	}
}

func (t *sdkSessionTransport) runBuild(attempt *sessionBuild, generation uint64) {
	defer t.wg.Done()
	buildCtx, cancel := context.WithTimeout(t.lifeCtx, t.spec.startupTimeout())
	managed, buildErr := t.build(buildCtx, generation)
	cancel()

	t.mu.Lock()
	if t.closed && managed != nil {
		t.mu.Unlock()
		closeManagedSession(managed)
		t.mu.Lock()
		managed = nil
		buildErr = mcpsdk.ErrConnectionClosed
	}
	if buildErr == nil {
		t.current = managed
		t.state = SessionStateReady
		t.reconnectAttempts = 0
		t.lastErrorKind = SessionErrorNone
		t.lastError = ""
	} else {
		t.state = SessionStateFailed
		t.lastErrorKind = classifySessionError(buildErr)
		t.lastError = t.safeErrorText(buildErr, "")
	}
	attempt.session = managed
	attempt.err = buildErr
	if t.building == attempt {
		t.building = nil
	}
	close(attempt.done)
	t.mu.Unlock()

	if managed != nil {
		t.watch(managed)
	}
}

func (t *sdkSessionTransport) build(ctx context.Context, generation uint64) (*managedMCPSession, error) {
	// Connect uses its context for the connection lifetime, not only for the
	// handshake. Give the connection a session-scoped context and let the bounded
	// build context cancel it only while Connect is still in flight.
	sessionCtx, cancelSession := context.WithCancel(t.lifeCtx)
	stopBuildCancel := context.AfterFunc(ctx, cancelSession)
	endpoint, err := t.newEndpoint(sessionCtx)
	if err != nil {
		stopBuildCancel()
		cancelSession()
		return nil, err
	}
	closeEndpoint := endpoint.close
	var closeOnce sync.Once
	endpoint.close = func() {
		closeOnce.Do(func() {
			cancelSession()
			if closeEndpoint != nil {
				closeEndpoint()
			}
		})
	}

	capabilities := &mcpsdk.ClientCapabilities{}
	if len(mcpRoots(t.spec.WorkspaceRoot)) > 0 {
		//nolint:staticcheck // Legacy MCP servers still require roots during the SDK deprecation window.
		capabilities.RootsV2 = &mcpsdk.RootCapabilities{ListChanged: false}
	}
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "reasonix", Version: mcpClientVersion()}, &mcpsdk.ClientOptions{
		Capabilities:   capabilities,
		MultiRoundTrip: &mcpsdk.MultiRoundTripOptions{Disabled: true},
		ToolListChangedHandler: func(_ context.Context, req *mcpsdk.ToolListChangedRequest) {
			t.dispatchSDKNotification(generation, "notifications/tools/list_changed", req.Params)
		},
		PromptListChangedHandler: func(_ context.Context, req *mcpsdk.PromptListChangedRequest) {
			t.dispatchSDKNotification(generation, "notifications/prompts/list_changed", req.Params)
		},
		ResourceListChangedHandler: func(_ context.Context, req *mcpsdk.ResourceListChangedRequest) {
			t.dispatchSDKNotification(generation, "notifications/resources/list_changed", req.Params)
		},
		ProgressNotificationHandler: func(_ context.Context, req *mcpsdk.ProgressNotificationClientRequest) {
			t.dispatchSDKProgress(generation, req.Params)
		},
	})
	for _, root := range mcpRoots(t.spec.WorkspaceRoot) {
		//nolint:staticcheck // Preserve the existing workspace-root contract for legacy MCP servers.
		client.AddRoots(&mcpsdk.Root{URI: root.URI, Name: root.Name})
	}

	t.setStateIfBuilding(generation, SessionStateListening)
	session, err := client.Connect(sessionCtx, endpoint.transport, nil)
	if err != nil {
		stopBuildCancel()
		endpoint.close()
		stderr := ""
		if endpoint.startupStderr != nil {
			stderr = endpoint.startupStderr()
		}
		if stderr != "" {
			t.mu.Lock()
			t.lastStartupStderr = stderr
			t.mu.Unlock()
		}
		return nil, err
	}
	if !stopBuildCancel() || ctx.Err() != nil {
		_ = session.Close()
		endpoint.close()
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return nil, mcpsdk.ErrConnectionClosed
	}
	protocol := ""
	if result := session.InitializeResult(); result != nil {
		protocol = result.ProtocolVersion
	}
	return &managedMCPSession{
		generation: generation,
		session:    session,
		endpoint:   endpoint,
		protocol:   protocol,
	}, nil
}

func (t *sdkSessionTransport) setStateIfBuilding(generation uint64, state SessionState) {
	t.mu.Lock()
	if !t.closed && t.current == nil && t.nextGeneration == generation {
		t.state = state
	}
	t.mu.Unlock()
}

func (t *sdkSessionTransport) dispatchSDKNotification(generation uint64, method string, params any) {
	if !t.generationActive(generation) {
		return
	}
	payload, err := json.Marshal(params)
	if err != nil {
		return
	}
	t.notifications.dispatchNotification(method, payload)
}

func (t *sdkSessionTransport) dispatchSDKProgress(generation uint64, params any) {
	if !t.generationActive(generation) {
		return
	}
	payload, err := json.Marshal(params)
	if err != nil {
		return
	}
	t.progress.dispatchProgress(payload)
}

func (t *sdkSessionTransport) generationActive(generation uint64) bool {
	t.mu.Lock()
	current := t.current
	valid := !t.closed && (current == nil && t.nextGeneration == generation || current != nil && current.generation == generation)
	t.mu.Unlock()
	return valid
}

func (t *sdkSessionTransport) watch(managed *managedMCPSession) {
	t.wg.Go(func() {
		t.handleSessionEnd(managed, managed.session.Wait())
	})
}

func (t *sdkSessionTransport) handleSessionEnd(managed *managedMCPSession, err error) {
	t.mu.Lock()
	if t.closed || t.current != managed {
		t.mu.Unlock()
		return
	}
	t.current = nil
	if errors.Is(err, mcpsdk.ErrSessionMissing) && managed.session.ID() == "" {
		t.state = SessionStateFailed
		t.lastErrorKind = SessionErrorProtocol
		t.lastError = t.safeErrorText(fmt.Errorf("MCP endpoint returned HTTP 404 without an established session: %w", err), "")
		t.mu.Unlock()
		if managed.endpoint.close != nil {
			managed.endpoint.close()
		}
		return
	}
	t.state = SessionStateReconnecting
	t.lastErrorKind = SessionErrorStreamClosed
	t.lastError = t.safeErrorText(err, managed.session.ID())
	t.mu.Unlock()
	if managed.endpoint.close != nil {
		managed.endpoint.close()
	}
	t.startAutoReconnect()
}

func (t *sdkSessionTransport) invalidate(managed *managedMCPSession) {
	if managed == nil {
		return
	}
	t.mu.Lock()
	if t.current != managed {
		t.mu.Unlock()
		return
	}
	t.current = nil
	t.state = SessionStateReconnecting
	t.mu.Unlock()
	closeManagedSession(managed)
}

func (t *sdkSessionTransport) startAutoReconnect() {
	t.mu.Lock()
	if t.closed || t.autoReconnecting || t.current != nil {
		t.mu.Unlock()
		return
	}
	t.autoReconnecting = true
	delays := append([]time.Duration(nil), t.reconnectDelays...)
	t.wg.Add(1)
	t.mu.Unlock()

	go func() {
		defer t.wg.Done()
		defer func() {
			t.mu.Lock()
			t.autoReconnecting = false
			t.mu.Unlock()
		}()
		for index, delay := range delays {
			if err := sleepContext(t.lifeCtx, delay); err != nil {
				return
			}
			t.mu.Lock()
			if t.closed || t.current != nil {
				t.mu.Unlock()
				return
			}
			t.reconnectAttempts = index + 1
			t.state = SessionStateReconnecting
			t.mu.Unlock()

			attemptCtx, cancel := context.WithTimeout(t.lifeCtx, t.spec.startupTimeout())
			_, err := t.acquire(attemptCtx)
			cancel()
			if err == nil {
				return
			}
		}
		t.mu.Lock()
		if !t.closed && t.current == nil {
			t.state = SessionStateFailed
		}
		t.mu.Unlock()
	}()
}

func (t *sdkSessionTransport) noteRuntimeError(managed *managedMCPSession, kind SessionErrorKind, err error) {
	t.mu.Lock()
	if !t.closed && (managed == nil || t.current == managed) {
		t.lastErrorKind = kind
		sessionID := ""
		if managed != nil {
			sessionID = managed.session.ID()
		}
		t.lastError = t.safeErrorText(err, sessionID)
	}
	t.mu.Unlock()
}

func (t *sdkSessionTransport) clearRuntimeError(managed *managedMCPSession) {
	t.mu.Lock()
	if !t.closed && t.current == managed {
		t.lastErrorKind = SessionErrorNone
		t.lastError = ""
		t.state = SessionStateReady
	}
	t.mu.Unlock()
}

func (t *sdkSessionTransport) sessionDiagnostics() sessionDiagnostics {
	t.mu.Lock()
	defer t.mu.Unlock()
	d := sessionDiagnostics{
		State:             t.state,
		ReconnectAttempts: t.reconnectAttempts,
		LastErrorKind:     t.lastErrorKind,
		LastError:         t.lastError,
	}
	if t.current != nil {
		d.ProtocolVersion = t.current.protocol
		d.SessionIDPresent = t.current.session.ID() != ""
	}
	return d
}

func (t *sdkSessionTransport) startupStderr() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.current != nil && t.current.endpoint.startupStderr != nil {
		return redactMCPConfigValues(t.current.endpoint.startupStderr(), t.spec)
	}
	return redactMCPConfigValues(t.lastStartupStderr, t.spec)
}

func (t *sdkSessionTransport) close() {
	if t == nil {
		return
	}
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return
	}
	t.closed = true
	t.state = SessionStateClosed
	current := t.current
	t.current = nil
	t.mu.Unlock()

	t.cancel()
	t.progress.clear()
	closeManagedSession(current)
	waitWithBudget(t.wg.Wait, closeWaitBudget)
}

func closeManagedSession(managed *managedMCPSession) {
	if managed == nil {
		return
	}
	done := make(chan struct{})
	go func() {
		_ = managed.session.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		if managed.endpoint.close != nil {
			managed.endpoint.close()
		}
		select {
		case <-done:
		case <-time.After(gracefulCloseWaitBudget):
		}
	}
	if managed.endpoint.close != nil {
		managed.endpoint.close()
	}
}

func invokeSDKMethod(ctx context.Context, session *mcpsdk.ClientSession, method string, params any) (json.RawMessage, error) {
	marshal := func(value any, err error) (json.RawMessage, error) {
		if err != nil {
			return nil, err
		}
		data, err := json.Marshal(value)
		return json.RawMessage(data), err
	}
	decode := func(target any) error {
		data, err := json.Marshal(params)
		if err != nil {
			return err
		}
		return json.Unmarshal(data, target)
	}

	switch method {
	case "initialize":
		return marshal(session.InitializeResult(), nil)
	case "ping":
		return marshal(map[string]any{}, session.Ping(ctx, nil))
	case "tools/list":
		items := make([]*mcpsdk.Tool, 0)
		for item, err := range session.Tools(ctx, nil) {
			if err != nil {
				return nil, err
			}
			items = append(items, item)
		}
		return marshal(map[string]any{"tools": items}, nil)
	case "tools/call":
		var typed mcpsdk.CallToolParams
		if err := decode(&typed); err != nil {
			return nil, err
		}
		return marshal(session.CallTool(ctx, &typed))
	case "prompts/list":
		items := make([]*mcpsdk.Prompt, 0)
		for item, err := range session.Prompts(ctx, nil) {
			if err != nil {
				return nil, err
			}
			items = append(items, item)
		}
		return marshal(map[string]any{"prompts": items}, nil)
	case "prompts/get":
		var typed mcpsdk.GetPromptParams
		if err := decode(&typed); err != nil {
			return nil, err
		}
		return marshal(session.GetPrompt(ctx, &typed))
	case "resources/list":
		items := make([]*mcpsdk.Resource, 0)
		for item, err := range session.Resources(ctx, nil) {
			if err != nil {
				return nil, err
			}
			items = append(items, item)
		}
		return marshal(map[string]any{"resources": items}, nil)
	case "resources/read":
		var typed mcpsdk.ReadResourceParams
		if err := decode(&typed); err != nil {
			return nil, err
		}
		return marshal(session.ReadResource(ctx, &typed))
	default:
		return nil, fmt.Errorf("unsupported MCP method %q", method)
	}
}

func safeToReplayMCPMethod(method string) bool {
	switch method {
	case "initialize", "ping", "tools/list", "prompts/list", "prompts/get", "resources/list", "resources/read":
		return true
	default:
		return false
	}
}
