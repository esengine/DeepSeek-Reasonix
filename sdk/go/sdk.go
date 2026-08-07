// Package extension is the Go SDK for Reasonix extension sidecars speaking
// Extension Protocol v1 over stdio. An extension is a separate process: the
// Reasonix host launches it, sends extension/initialize first, drives
// intercepts, events, provider streams, and UI calls, and finally asks it to
// stop with extension/shutdown.
//
// The transport is strict JSON-RPC 2.0 framed as NDJSON (one object per
// line, integer request ids, params as JSON objects, frames capped at
// FrameBytes). The SDK owns the wire, the handshake barrier, and the
// shutdown sequence; the extension implements Handler and, optionally,
// interceptors, a Provider, and UI callbacks via Options. After Initialize
// completes, the SDK may invoke up to 32 callbacks concurrently; extensions
// must synchronize any mutable state shared by those callbacks.
//
// After Serve returns nil from an orderly extension/shutdown the process
// should exit with code 0; the host reaps it by that exit status.
//
// Protocol reference: docs/EXTENSION_PROTOCOL.generated.md and
// internal/extension/protocol/schema.generated.json in the Reasonix
// repository.
package extension

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Public callback types

// Handler is the one mandatory extension hook. Initialize is called once and
// completes before any other callback. Return the sidecar's declaration
// (name, version, subscriptions, replaces, providers, UI actions) — the host
// rejects anything beyond the installed manifest.
type Handler interface {
	Initialize(ctx context.Context, p InitializeParams) (*InitializeResult, error)
}

// InterceptorFunc rules on one intercepted event. payload is the event
// payload as raw JSON; content-ref externalized payloads are rehydrated
// before the call. Return one of Continue, Block, Replace, Allow, or Deny; a
// nil result is Continue. A non-nil error answers the intercept with the
// frozen internal error and the host proceeds with its default behavior.
type InterceptorFunc func(ctx context.Context, event string, payload json.RawMessage) (*InterceptResult, error)

// Provider brokers extension-hosted model providers. The extension holds the
// credentials; only the credential-free DTOs cross the wire.
type Provider interface {
	// Catalog returns the extension's full provider catalog. It may run
	// concurrently with other callbacks.
	Catalog(ctx context.Context) ([]ProviderDescriptor, error)
	// Stream opens one stream and returns its chunk channel. Stream must
	// return promptly; produce chunks in the background. The SDK numbers
	// chunks 1,2,3,… (from the host's SeqBase) and ends the stream with
	// exactly one stream/end: close the channel for a clean end, send an
	// ErrorChunk (or a chunk with Type ChunkError) to fail the stream with
	// end.error, and stop producing when ctx is cancelled (host cancel or
	// shutdown) — the SDK then ends the stream interrupted. Multiple Stream
	// calls may run concurrently.
	Stream(ctx context.Context, req StreamRequest) (<-chan StreamChunk, error)
}

// StreamRequest is one opened provider stream.
type StreamRequest struct {
	StreamID    string
	ProviderRef string
	Model       string
	Effort      string
	Request     ProviderRequest
}

// StreamChunk is one chunk a Provider produces; it is exactly the wire
// ProviderChunk. Build them with TextChunk, ReasoningChunk, UsageChunk,
// DoneChunk, and ErrorChunk.
type StreamChunk = ProviderChunk

// TextChunk is one assistant text delta.
func TextChunk(text string) StreamChunk { return StreamChunk{Type: ChunkText, Text: text} }

// ReasoningChunk is one reasoning delta with its optional signature.
func ReasoningChunk(text, signature string) StreamChunk {
	return StreamChunk{Type: ChunkReasoning, Text: text, Signature: signature}
}

// UsageChunk carries final token accounting.
func UsageChunk(usage ProviderUsage) StreamChunk {
	return StreamChunk{Type: ChunkUsage, Usage: &usage}
}

// DoneChunk marks the logical end of the assistant turn. The stream itself
// ends when the channel closes.
func DoneChunk() StreamChunk { return StreamChunk{Type: ChunkDone} }

// ErrorChunk fails the stream. The SDK ends it with stream/end.error set to
// the chunk's message instead of forwarding the chunk. Keep the message
// generic: it crosses the wire and must never contain credentials, endpoints,
// or response bodies.
func ErrorChunk(message string) StreamChunk {
	if strings.TrimSpace(message) == "" {
		message = frozenErrorSpecs[ErrProviderFailed].Message
	}
	return StreamChunk{Type: ChunkError, Error: &ProviderError{Code: ProviderFailed, Message: message}}
}

// UIHandler carries the extension's UI callbacks. A nil func makes the
// matching method answer unknown_method.
type UIHandler struct {
	// Action runs one handshake-declared action. A non-nil error answers
	// with {accepted:false, message}.
	Action func(ctx context.Context, actionID string, args map[string]string) error
	// Submit consumes one published form surface's values. A non-nil error
	// answers with {accepted:false} and is logged.
	Submit func(ctx context.Context, surfaceID string, values map[string]any) error
}

// Options configures Serve. Stdin/Stdout default to os.Stdin/os.Stdout. After
// Initialize, callback fields and Provider methods may be invoked concurrently
// (up to 32 inbound handlers); protect shared mutable maps, slices, counters,
// and clients with synchronization appropriate to the extension.
type Options struct {
	Stdin  io.Reader
	Stdout io.Writer
	// Name and Version fill InitializeResult when the Handler leaves them
	// empty.
	Name    string
	Version string
	// Interceptors maps an event name ("session.start", …) to its ruling
	// func; "*" is the wildcard fallback for events without an exact entry.
	Interceptors map[string]InterceptorFunc
	// Observer receives extension/event notifications. Events are
	// fire-and-forget; the observer cannot change host behavior.
	Observer func(ctx context.Context, event string, payload json.RawMessage)
	// ResourcesChanged receives extension/resources/changed notifications.
	ResourcesChanged func(ctx context.Context, paths []string)
	// Provider serves extension/provider/*; nil answers those methods with
	// unknown_method.
	Provider Provider
	// UI serves extension/ui/action and extension/ui/submit.
	UI UIHandler
	// Shutdown runs on extension/shutdown, bounded by the host's
	// TimeoutMillis. After it returns (or times out) the SDK answers
	// {accepted:true} and closes the transport; the process should then
	// exit(0).
	Shutdown func(ctx context.Context)
	// Logger receives stderr diagnostics (protocol violations, dropped
	// notifications, handler errors). Defaults to a stderr logger.
	Logger *log.Logger
}

// Sentinel errors

// ErrNotReady reports an Extension → Host call made before the handshake
// barrier opened: the sidecar must not send requests or notifications before
// the host's extension/initialized notification.
var ErrNotReady = errors.New("extension: host connection is not initialized (wait for extension/initialized)")

// ErrNoConnection reports a helper call (HostUI methods, ReadContentRef,
// ResolveExternalized) with a context that did not come from an SDK
// callback.
var ErrNoConnection = errors.New("extension: no host connection in context (use the context passed to an SDK callback)")

// ErrUICancelled reports a host prompt the user dismissed. UIRequestResult
// distinguishes dismissal from an empty value set; the SDK surfaces it as
// this sentinel.
var ErrUICancelled = errors.New("extension: the user dismissed the prompt")

// InterceptResult helpers

// Continue lets the event proceed unchanged.
func Continue() *InterceptResult { return &InterceptResult{Decision: DecisionContinue} }

// Block stops the event with a human-readable reason.
func Block(reason string) *InterceptResult {
	return &InterceptResult{Decision: DecisionBlock, Reason: reason}
}

// Replace substitutes the event payload. payload may be a json.RawMessage
// (used verbatim, must be valid JSON) or any marshalable value. The
// replacement travels inline; only the host can mint content refs, so a
// replacement must fit in one frame.
func Replace(payload any) (*InterceptResult, error) {
	var raw json.RawMessage
	switch value := payload.(type) {
	case json.RawMessage:
		raw = value
	case []byte:
		raw = value
	default:
		encoded, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("extension: marshal replacement: %w", err)
		}
		raw = encoded
	}
	if !json.Valid(raw) {
		return nil, errors.New("extension: replacement is not valid JSON")
	}
	return &InterceptResult{Decision: DecisionReplace, Replacement: raw}, nil
}

// Allow grants a permission.decision intercept.
func Allow() *InterceptResult { return &InterceptResult{Decision: DecisionAllow} }

// Deny refuses a permission.decision intercept with a reason.
func Deny(reason string) *InterceptResult {
	return &InterceptResult{Decision: DecisionDeny, Reason: reason}
}

// Serve

type serverState uint8

const (
	stateNew serverState = iota
	// stateHandshake is entered when extension/initialize arrives and held
	// until the host's extension/initialized notification opens the barrier.
	stateHandshake
	stateReady
	stateShutdown
)

type server struct {
	conn    *conn
	handler Handler
	opts    Options
	log     *log.Logger

	mu           sync.Mutex
	state        serverState
	shutdownOnce sync.Once

	streamsMu sync.Mutex
	streams   map[string]*streamHandle
}

type streamHandle struct {
	cancel context.CancelFunc
	done   chan struct{}
}

type serverContextKey struct{}

func serverFrom(ctx context.Context) *server {
	s, _ := ctx.Value(serverContextKey{}).(*server)
	return s
}

// Serve runs the extension sidecar lifecycle on Options.Stdin/Stdout until
// the host closes the transport, asks for shutdown, or fatally violates the
// protocol. It returns nil on a clean end (host EOF or an answered
// extension/shutdown) and a non-nil error otherwise; canceling ctx tears
// everything down and returns the ctx error. After an orderly shutdown the
// process should exit(0).
func Serve(ctx context.Context, h Handler, opts Options) error {
	if h == nil {
		return errors.New("extension: Serve requires a non-nil Handler")
	}
	stdin := opts.Stdin
	if stdin == nil {
		stdin = os.Stdin
	}
	stdout := opts.Stdout
	if stdout == nil {
		stdout = os.Stdout
	}
	logger := opts.Logger
	if logger == nil {
		logger = log.New(os.Stderr, "reasonix-extension: ", log.LstdFlags)
	}
	s := &server{
		handler: h,
		opts:    opts,
		log:     logger,
		state:   stateNew,
		streams: make(map[string]*streamHandle),
	}
	c := newConn(stdin, stdout, logger)
	s.conn = c
	c.beforeRequest = s.gateRequest
	c.beforeNotification = s.gateNotification

	c.reqH[MethodExtensionInitialize] = s.withConnRequest(s.handleInitialize)
	c.reqH[MethodExtensionShutdown] = s.withConnRequest(s.handleShutdown)
	c.reqH[MethodExtensionIntercept] = s.withConnRequest(s.handleIntercept)
	c.reqH[MethodExtensionProviderCatalog] = s.withConnRequest(s.handleProviderCatalog)
	c.reqH[MethodExtensionProviderStreamOpen] = s.withConnRequest(s.handleStreamOpen)
	c.reqH[MethodExtensionProviderStreamCancel] = s.withConnRequest(s.handleStreamCancel)
	c.reqH[MethodExtensionUIAction] = s.withConnRequest(s.handleUIAction)
	c.reqH[MethodExtensionUISubmit] = s.withConnRequest(s.handleUISubmit)
	c.notH[MethodExtensionInitialized] = s.withConnNotification(s.handleInitialized)
	c.notH[MethodExtensionEvent] = s.withConnNotification(s.handleEvent)
	c.notH[MethodExtensionResourcesChanged] = s.withConnNotification(s.handleResourcesChanged)

	return c.serve(ctx)
}

// withConnRequest injects the server into handler contexts so HostUI,
// ReadContentRef, and ResolveExternalized can reach the transport.
func (s *server) withConnRequest(f requestHandler) requestHandler {
	return func(ctx context.Context, raw json.RawMessage) (any, error) {
		return f(context.WithValue(ctx, serverContextKey{}, s), raw)
	}
}

func (s *server) withConnNotification(f notificationHandler) notificationHandler {
	return func(ctx context.Context, raw json.RawMessage) {
		f(context.WithValue(ctx, serverContextKey{}, s), raw)
	}
}

// Handshake barrier

// gateRequest runs on the read loop before dispatch: the host must open with
// extension/initialize, and until its extension/initialized notification
// arrives only the lifecycle methods are served. Everything else is answered
// with the frozen protocol_error.
func (s *server) gateRequest(method string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch s.state {
	case stateReady:
		return nil
	case stateNew:
		switch method {
		case MethodExtensionInitialize:
			s.state = stateHandshake
			return nil
		case MethodExtensionShutdown:
			return nil
		}
	case stateHandshake:
		if method == MethodExtensionShutdown {
			return nil
		}
	case stateShutdown:
		// fall through to the error below
	}
	return &ProtocolError{
		Reason:  ErrProtocolError,
		Message: fmt.Sprintf("extension protocol violation: host sent request %q before the handshake completed", method),
	}
}

// gateNotification applies the same barrier to notifications; violations are
// dropped (JSON-RPC notifications carry no response).
func (s *server) gateNotification(method string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch s.state {
	case stateReady:
		return nil
	case stateHandshake:
		if method == MethodExtensionInitialized {
			s.state = stateReady
			return nil
		}
	}
	return fmt.Errorf("extension: dropping notification %q before the handshake completed", method)
}

// checkReady gates Extension → Host calls on the opened barrier.
func (s *server) checkReady() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state != stateReady {
		return ErrNotReady
	}
	return nil
}

// Lifecycle handlers

// fatalError marks handler failures that must end the connection after the
// error response is written (a failed handshake leaves nothing to serve).
type fatalError struct{ err error }

func (e *fatalError) Error() string { return e.err.Error() }
func (e *fatalError) Unwrap() error { return e.err }

func (s *server) handleInitialize(ctx context.Context, raw json.RawMessage) (any, error) {
	var p InitializeParams
	if err := strictDecode(raw, &p); err != nil {
		return nil, MustProtocolError(ErrInvalidParams)
	}
	if err := compareProtocolVersion(p.ProtocolID, p.ProtocolVersion); err != nil {
		return nil, &fatalError{err: err}
	}
	result, err := s.handler.Initialize(ctx, p)
	if err != nil {
		s.log.Printf("extension: initialize handler failed: %v", err)
		return nil, &fatalError{err: err}
	}
	if result == nil {
		return nil, &fatalError{err: errors.New("extension: Initialize returned a nil result")}
	}
	result.ProtocolVersion = ProtocolVersion
	if result.Name == "" {
		result.Name = s.opts.Name
	}
	if result.Version == "" {
		result.Version = s.opts.Version
	}
	if strings.TrimSpace(result.Name) == "" || strings.TrimSpace(result.Version) == "" {
		return nil, &fatalError{err: errors.New("extension: initialize result requires a name and version")}
	}
	if result.StateSchemaVersion < 0 {
		return nil, &fatalError{err: errors.New("extension: stateSchemaVersion must be non-negative")}
	}
	return result, nil
}

// compareProtocolVersion mirrors the host's handshake identity check.
func compareProtocolVersion(peerID, peerVersion string) error {
	if peerID != ProtocolID {
		return MustProtocolError(ErrUnsupportedVersion)
	}
	major, err := strconv.Atoi(peerVersion)
	if err != nil {
		return MustProtocolError(ErrProtocolError)
	}
	if major != ProtocolMajor {
		return MustProtocolError(ErrUnsupportedVersion)
	}
	return nil
}

func (s *server) handleInitialized(context.Context, json.RawMessage) {
	// The barrier itself opened in gateNotification, synchronously on the
	// read loop, so no later frame can overtake it.
}

func (s *server) handleShutdown(ctx context.Context, raw json.RawMessage) (any, error) {
	var p ShutdownParams
	if err := strictDecode(raw, &p); err != nil || p.TimeoutMillis < 0 {
		return nil, MustProtocolError(ErrInvalidParams)
	}
	s.shutdownOnce.Do(func() {
		s.mu.Lock()
		s.state = stateShutdown
		s.mu.Unlock()
		if s.opts.Shutdown != nil {
			fnCtx := ctx
			cancel := func() {}
			if p.TimeoutMillis > 0 {
				fnCtx, cancel = context.WithTimeout(ctx, time.Duration(p.TimeoutMillis)*time.Millisecond)
			}
			defer cancel()
			done := make(chan struct{})
			go func() {
				s.opts.Shutdown(fnCtx)
				close(done)
			}()
			select {
			case <-done:
			case <-fnCtx.Done():
				s.log.Printf("extension: shutdown function did not return within %dms", p.TimeoutMillis)
			}
		}
	})
	return deferredResult{
		result: ShutdownResult{Accepted: true},
		after: func() {
			// Orderly close: end in-flight calls, then close the read side so
			// the read loop exits and the host sees EOF when the process
			// exits. Serve returns nil.
			s.conn.shutdown(nil)
			if closer, ok := s.conn.r.(io.Closer); ok {
				_ = closer.Close()
			}
		},
	}, nil
}

// Intercept and observation

func (s *server) handleIntercept(ctx context.Context, raw json.RawMessage) (any, error) {
	var p InterceptParams
	if err := strictDecode(raw, &p); err != nil {
		return nil, MustProtocolError(ErrInvalidParams)
	}
	if !validInterceptEvent(p.Event) || p.Seq < 1 || p.TimeoutMillis < 0 || !jsonKeyPresent(raw, "payload") {
		return nil, MustProtocolError(ErrInvalidParams)
	}
	payload, err := s.rehydrate(ctx, p.Payload, p.Externalized, "/payload")
	if err != nil {
		return nil, err
	}
	fn := s.opts.Interceptors[string(p.Event)]
	if fn == nil {
		fn = s.opts.Interceptors["*"]
	}
	if fn == nil {
		return Continue(), nil
	}
	if p.TimeoutMillis > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(p.TimeoutMillis)*time.Millisecond)
		defer cancel()
	}
	result, err := fn(ctx, string(p.Event), payload)
	if err != nil {
		// The callback's advertised intercept budget expired. Return the
		// frozen timeout reason rather than racing the host's identical timer
		// with a generic internal error response.
		if errors.Is(err, context.DeadlineExceeded) && errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, MustProtocolError(ErrInterceptTimeout)
		}
		return nil, err
	}
	if result == nil {
		return Continue(), nil
	}
	if !validInterceptDecision(result.Decision) {
		return nil, fmt.Errorf("extension: interceptor for %q returned invalid decision %q", p.Event, result.Decision)
	}
	return result, nil
}

func (s *server) handleEvent(ctx context.Context, raw json.RawMessage) {
	var p EventParams
	if err := strictDecode(raw, &p); err != nil || !validInterceptEvent(p.Event) || !jsonKeyPresent(raw, "payload") {
		s.log.Printf("extension: dropping malformed event notification")
		return
	}
	payload, err := s.rehydrate(ctx, p.Payload, p.Externalized, "/payload")
	if err != nil {
		s.log.Printf("extension: dropping event %q: %v", p.Event, err)
		return
	}
	if s.opts.Observer != nil {
		s.opts.Observer(ctx, string(p.Event), payload)
	}
}

func (s *server) handleResourcesChanged(ctx context.Context, raw json.RawMessage) {
	var p ResourcesChangedParams
	if err := strictDecode(raw, &p); err != nil || p.Paths == nil {
		s.log.Printf("extension: dropping malformed resources/changed notification")
		return
	}
	if s.opts.ResourcesChanged != nil {
		s.opts.ResourcesChanged(ctx, p.Paths)
	}
}

// Provider broker

func (s *server) handleProviderCatalog(ctx context.Context, raw json.RawMessage) (any, error) {
	if s.opts.Provider == nil {
		return nil, MustProtocolError(ErrUnknownMethod)
	}
	if err := strictDecode(raw, &ProviderCatalogParams{}); err != nil {
		return nil, MustProtocolError(ErrInvalidParams)
	}
	providers, err := s.opts.Provider.Catalog(ctx)
	if err != nil {
		return nil, err
	}
	if providers == nil {
		// The wire form requires an array; null fails the host's decoder.
		providers = []ProviderDescriptor{}
	}
	return ProviderCatalogResult{Providers: providers}, nil
}

func (s *server) handleStreamOpen(ctx context.Context, raw json.RawMessage) (any, error) {
	if s.opts.Provider == nil {
		return nil, MustProtocolError(ErrUnknownMethod)
	}
	var p StreamOpenParams
	if err := strictDecode(raw, &p); err != nil {
		return nil, MustProtocolError(ErrInvalidParams)
	}
	if p.SeqBase < 0 {
		return nil, MustProtocolError(ErrInvalidParams)
	}
	if err := p.Validate(); err != nil {
		return nil, MustProtocolError(ErrInvalidParams)
	}
	streamCtx, cancel := context.WithCancel(ctx)
	chunks, err := s.opts.Provider.Stream(streamCtx, StreamRequest{
		StreamID:    p.StreamID,
		ProviderRef: p.ProviderRef,
		Model:       p.Model,
		Effort:      p.Effort,
		Request:     p.Request,
	})
	if err != nil {
		cancel()
		s.log.Printf("extension: provider stream %q failed to open: %v", p.StreamID, err)
		return nil, MustProtocolError(ErrProviderFailed)
	}
	if chunks == nil {
		cancel()
		return nil, errors.New("extension: provider returned a nil chunk channel")
	}
	handle := &streamHandle{cancel: cancel, done: make(chan struct{})}
	s.streamsMu.Lock()
	if _, exists := s.streams[p.StreamID]; exists {
		s.streamsMu.Unlock()
		cancel()
		return nil, &ProtocolError{Reason: ErrProtocolError, Message: "duplicate stream id " + p.StreamID}
	}
	s.streams[p.StreamID] = handle
	s.streamsMu.Unlock()
	return deferredResult{
		result: StreamOpenResult{Accepted: true},
		after:  func() { go s.pumpStream(streamCtx, p.StreamID, p.SeqBase, chunks, handle) },
	}, nil
}

func (s *server) handleStreamCancel(_ context.Context, raw json.RawMessage) (any, error) {
	var p StreamCancelParams
	if err := strictDecode(raw, &p); err != nil || strings.TrimSpace(p.StreamID) == "" {
		return nil, MustProtocolError(ErrInvalidParams)
	}
	s.streamsMu.Lock()
	handle := s.streams[p.StreamID]
	s.streamsMu.Unlock()
	if handle == nil {
		return StreamCancelResult{Cancelled: false}, nil
	}
	handle.cancel()
	return StreamCancelResult{Cancelled: true}, nil
}

// pumpStream forwards one provider channel onto the wire: chunks become
// stream/chunk notifications with contiguous 1-based seqs (from SeqBase),
// and exactly one stream/end closes the stream — clean on channel close,
// with error on an error chunk, interrupted on cancel. A cancel processed by
// the SDK is never trailed by another chunk.
func (s *server) pumpStream(ctx context.Context, streamID string, seqBase int, chunks <-chan StreamChunk, handle *streamHandle) {
	defer close(handle.done)
	defer func() {
		s.streamsMu.Lock()
		delete(s.streams, streamID)
		s.streamsMu.Unlock()
	}()
	seq := int64(seqBase)
	if seq < 1 {
		seq = 1
	}
	var lastSeq int64
	end := StreamEndParams{StreamID: streamID}
	for {
		// A cancel must never be trailed by one more chunk, so check before
		// every receive and again before every send.
		select {
		case <-ctx.Done():
			end.LastSeq, end.Interrupted = lastSeq, true
			s.sendStreamEnd(&end)
			return
		default:
		}
		select {
		case <-ctx.Done():
			end.LastSeq, end.Interrupted = lastSeq, true
			s.sendStreamEnd(&end)
			return
		case chunk, ok := <-chunks:
			if !ok {
				end.LastSeq = lastSeq
				s.sendStreamEnd(&end)
				return
			}
			if chunk.Type == ChunkError {
				end.LastSeq = lastSeq
				end.Error = frozenErrorSpecs[ErrProviderFailed].Message
				if chunk.Error != nil && strings.TrimSpace(chunk.Error.Message) != "" {
					end.Error = chunk.Error.Message
				}
				s.sendStreamEnd(&end)
				return
			}
			if err := chunk.Validate(); err != nil {
				s.log.Printf("extension: provider stream %q produced an invalid chunk: %v", streamID, err)
				end.LastSeq = lastSeq
				end.Error = "the extension provider produced an invalid chunk"
				s.sendStreamEnd(&end)
				return
			}
			if err := s.conn.notify(MethodExtensionProviderStreamChunk, StreamChunkParams{
				StreamID: streamID, Seq: seq, Chunk: chunk,
			}); err != nil {
				s.log.Printf("extension: provider stream %q could not deliver chunk %d: %v", streamID, seq, err)
				return
			}
			lastSeq = seq
			seq++
		}
	}
}

func (s *server) sendStreamEnd(end *StreamEndParams) {
	if err := s.conn.notify(MethodExtensionProviderStreamEnd, *end); err != nil {
		s.log.Printf("extension: provider stream %q could not deliver stream end: %v", end.StreamID, err)
	}
}

// UI handlers (Host → Extension)

func (s *server) handleUIAction(ctx context.Context, raw json.RawMessage) (any, error) {
	if s.opts.UI.Action == nil {
		return nil, MustProtocolError(ErrUnknownMethod)
	}
	var p UIActionParams
	if err := strictDecode(raw, &p); err != nil || strings.TrimSpace(p.ActionID) == "" || strings.TrimSpace(p.SessionID) == "" {
		return nil, MustProtocolError(ErrInvalidParams)
	}
	if err := s.opts.UI.Action(ctx, p.ActionID, p.Args); err != nil {
		return UIActionResult{Accepted: false, Message: err.Error()}, nil
	}
	return UIActionResult{Accepted: true}, nil
}

func (s *server) handleUISubmit(ctx context.Context, raw json.RawMessage) (any, error) {
	if s.opts.UI.Submit == nil {
		return nil, MustProtocolError(ErrUnknownMethod)
	}
	var p UISubmitParams
	if err := strictDecode(raw, &p); err != nil || strings.TrimSpace(p.SurfaceID) == "" ||
		strings.TrimSpace(p.SessionID) == "" || p.Values == nil {
		return nil, MustProtocolError(ErrInvalidParams)
	}
	if err := s.opts.UI.Submit(ctx, p.SurfaceID, p.Values); err != nil {
		s.log.Printf("extension: UI submit for surface %q failed: %v", p.SurfaceID, err)
		return UISubmitResult{Accepted: false}, nil
	}
	return UISubmitResult{Accepted: true}, nil
}

// HostUI: Extension → Host UI client

// HostUI is the sidecar's client for the host's structured UI surfaces. The
// zero value is ready to use; every method takes the context of an SDK
// callback (interceptor, observer, provider, UI, or shutdown) and fails with
// ErrNoConnection otherwise, and with ErrNotReady before the handshake
// barrier opens. Surfaces are structured-only by design: there is no way to
// send HTML, CSS, JavaScript, or URLs.
type HostUI struct{}

// uiAnswerKey is the field key the host uses for single-field prompts.
const uiAnswerKey = "value"

// PublishStatus publishes or replaces a one-line status surface.
func (HostUI) PublishStatus(ctx context.Context, sessionID string, generation uint64, surfaceID string, p UIStatusPayload) error {
	if strings.TrimSpace(p.Label) == "" {
		return errors.New("extension: status payload requires a label")
	}
	if !validUISeverity(p.Severity) {
		return fmt.Errorf("extension: invalid severity %q", p.Severity)
	}
	return publishSurface(ctx, sessionID, generation, surfaceID, UISurfaceStatus, p)
}

// PublishCard publishes or replaces a rich read-only card surface.
func (HostUI) PublishCard(ctx context.Context, sessionID string, generation uint64, surfaceID string, p UICardPayload) error {
	for i, field := range p.Fields {
		if strings.TrimSpace(field.Key) == "" {
			return fmt.Errorf("extension: card field %d requires a key", i)
		}
	}
	for i, action := range p.Actions {
		if strings.TrimSpace(action.ActionID) == "" || strings.TrimSpace(action.Label) == "" {
			return fmt.Errorf("extension: card action %d requires an actionId and label", i)
		}
	}
	return publishSurface(ctx, sessionID, generation, surfaceID, UISurfaceCard, p)
}

// PublishForm publishes or replaces an editable form surface; submissions
// return through the Options.UI.Submit callback.
func (HostUI) PublishForm(ctx context.Context, sessionID string, generation uint64, surfaceID string, p UIFormPayload) error {
	if err := validateFormPayload(p); err != nil {
		return err
	}
	return publishSurface(ctx, sessionID, generation, surfaceID, UISurfaceForm, p)
}

// PublishNotification publishes a transient toast-style message.
func (HostUI) PublishNotification(ctx context.Context, sessionID string, generation uint64, surfaceID string, p UINotificationPayload) error {
	if strings.TrimSpace(p.Title) == "" {
		return errors.New("extension: notification payload requires a title")
	}
	if !validUISeverity(p.Severity) {
		return fmt.Errorf("extension: invalid severity %q", p.Severity)
	}
	return publishSurface(ctx, sessionID, generation, surfaceID, UISurfaceNotification, p)
}

func publishSurface(ctx context.Context, sessionID string, generation uint64, surfaceID string, kind UISurfaceKind, payload any) error {
	s := serverFrom(ctx)
	if s == nil {
		return ErrNoConnection
	}
	if strings.TrimSpace(surfaceID) == "" || strings.TrimSpace(sessionID) == "" {
		return errors.New("extension: surfaceId and sessionId are required")
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("extension: marshal %s payload: %w", kind, err)
	}
	resultRaw, err := s.callHost(ctx, MethodHostUIPublish, UIPublishParams{
		SurfaceID: surfaceID, SessionID: sessionID, Generation: generation, Kind: kind, Payload: raw,
	})
	if err != nil {
		return err
	}
	var result UIPublishResult
	if err := strictDecode(resultRaw, &result); err != nil {
		return &ProtocolError{Reason: ErrProtocolError, Message: "invalid host/ui/publish result"}
	}
	if !result.Accepted {
		return fmt.Errorf("extension: host rejected the %s surface %q", kind, surfaceID)
	}
	return nil
}

// InputPrompt configures RequestInput.
type InputPrompt struct {
	Title    string
	Message  string
	Label    string
	Default  string
	Required bool
}

// SelectPrompt configures RequestSelect.
type SelectPrompt struct {
	Title    string
	Message  string
	Label    string
	Options  []string
	Default  string
	Required bool
}

// MultiSelectPrompt configures RequestMultiSelect.
type MultiSelectPrompt struct {
	Title    string
	Message  string
	Label    string
	Options  []string
	Required bool
}

// RequestConfirm blocks on a yes/no prompt; the bool is the user's answer.
// A dismissed prompt returns ErrUICancelled.
func (h HostUI) RequestConfirm(ctx context.Context, sessionID string, generation uint64, surfaceID, message string) (bool, error) {
	form := UIFormPayload{
		Message: message,
		Fields:  []UIFormField{{Key: uiAnswerKey, Label: message, Kind: UIFieldConfirm}},
	}
	values, err := h.requestPrompt(ctx, sessionID, generation, surfaceID, UIRequestConfirm, form)
	if err != nil {
		return false, err
	}
	answer, _ := values[uiAnswerKey].(bool)
	return answer, nil
}

// RequestInput blocks on a free-text prompt and returns the entered text.
func (h HostUI) RequestInput(ctx context.Context, sessionID string, generation uint64, surfaceID string, p InputPrompt) (string, error) {
	field := UIFormField{Key: uiAnswerKey, Label: p.Label, Kind: UIFieldInput, Required: p.Required}
	if p.Default != "" {
		field.Default = p.Default
	}
	values, err := h.requestPrompt(ctx, sessionID, generation, surfaceID, UIRequestInput, UIFormPayload{
		Title: p.Title, Message: p.Message, Fields: []UIFormField{field},
	})
	if err != nil {
		return "", err
	}
	answer, _ := values[uiAnswerKey].(string)
	return answer, nil
}

// RequestSelect blocks on a single-choice prompt and returns the picked
// option.
func (h HostUI) RequestSelect(ctx context.Context, sessionID string, generation uint64, surfaceID string, p SelectPrompt) (string, error) {
	if len(p.Options) == 0 {
		return "", errors.New("extension: select prompt requires options")
	}
	field := UIFormField{Key: uiAnswerKey, Label: p.Label, Kind: UIFieldSelect, Options: p.Options, Required: p.Required}
	if p.Default != "" {
		field.Default = p.Default
	}
	values, err := h.requestPrompt(ctx, sessionID, generation, surfaceID, UIRequestSelect, UIFormPayload{
		Title: p.Title, Message: p.Message, Fields: []UIFormField{field},
	})
	if err != nil {
		return "", err
	}
	answer, _ := values[uiAnswerKey].(string)
	return answer, nil
}

// RequestMultiSelect blocks on a multi-choice prompt and returns the picked
// options.
func (h HostUI) RequestMultiSelect(ctx context.Context, sessionID string, generation uint64, surfaceID string, p MultiSelectPrompt) ([]string, error) {
	if len(p.Options) == 0 {
		return nil, errors.New("extension: multiselect prompt requires options")
	}
	field := UIFormField{Key: uiAnswerKey, Label: p.Label, Kind: UIFieldMultiselect, Options: p.Options, Required: p.Required}
	values, err := h.requestPrompt(ctx, sessionID, generation, surfaceID, UIRequestMultiselect, UIFormPayload{
		Title: p.Title, Message: p.Message, Fields: []UIFormField{field},
	})
	if err != nil {
		return nil, err
	}
	switch answer := values[uiAnswerKey].(type) {
	case []string:
		return answer, nil
	case []any:
		out := make([]string, 0, len(answer))
		for _, item := range answer {
			text, ok := item.(string)
			if !ok {
				return nil, &ProtocolError{Reason: ErrProtocolError, Message: "host/ui/request multiselect answer is not a string list"}
			}
			out = append(out, text)
		}
		return out, nil
	case nil:
		return []string{}, nil
	default:
		return nil, &ProtocolError{Reason: ErrProtocolError, Message: "host/ui/request multiselect answer is not a string list"}
	}
}

// RequestForm blocks on a fully custom form prompt and returns all values
// keyed by field key. It is the structured escape hatch behind the typed
// prompt helpers.
func (h HostUI) RequestForm(ctx context.Context, sessionID string, generation uint64, surfaceID string, form UIFormPayload) (map[string]any, error) {
	if err := validateFormPayload(form); err != nil {
		return nil, err
	}
	return h.requestPrompt(ctx, sessionID, generation, surfaceID, UIRequestInput, form)
}

func (h HostUI) requestPrompt(ctx context.Context, sessionID string, generation uint64, surfaceID string, kind UIRequestKind, form UIFormPayload) (map[string]any, error) {
	s := serverFrom(ctx)
	if s == nil {
		return nil, ErrNoConnection
	}
	if strings.TrimSpace(surfaceID) == "" || strings.TrimSpace(sessionID) == "" {
		return nil, errors.New("extension: surfaceId and sessionId are required")
	}
	raw, err := json.Marshal(form)
	if err != nil {
		return nil, fmt.Errorf("extension: marshal %s payload: %w", kind, err)
	}
	resultRaw, err := s.callHost(ctx, MethodHostUIRequest, UIRequestParams{
		SurfaceID: surfaceID, SessionID: sessionID, Generation: generation, Kind: kind, Payload: raw,
	})
	if err != nil {
		return nil, err
	}
	var result UIRequestResult
	if err := strictDecode(resultRaw, &result); err != nil {
		return nil, &ProtocolError{Reason: ErrProtocolError, Message: "invalid host/ui/request result"}
	}
	if result.Cancelled {
		return nil, ErrUICancelled
	}
	return result.Values, nil
}

func validateFormPayload(p UIFormPayload) error {
	if p.Fields == nil {
		return errors.New("extension: form payload requires a fields array (possibly empty)")
	}
	for i, field := range p.Fields {
		if strings.TrimSpace(field.Key) == "" {
			return fmt.Errorf("extension: form field %d requires a key", i)
		}
		if !validUIFieldKind(field.Kind) {
			return fmt.Errorf("extension: form field %q has invalid kind %q", field.Key, field.Kind)
		}
	}
	return nil
}

// Content refs (Extension → Host)

// ReadContentRef pages one whole content ref back from the host in
// ContentRefChunkBytes chunks, verifies the reassembled byte count and
// SHA-256 against the host's own report, and fails on any inconsistency. An
// expired or unknown ref returns a *ProtocolError with Reason
// ErrContentRefExpired.
func ReadContentRef(ctx context.Context, ref string) ([]byte, error) {
	s := serverFrom(ctx)
	if s == nil {
		return nil, ErrNoConnection
	}
	if strings.TrimSpace(ref) == "" {
		return nil, errors.New("extension: content ref is required")
	}
	var out []byte
	var offset int64
	for {
		raw, err := s.callHost(ctx, MethodHostContentRead, ContentReadParams{ContentRef: ref, Offset: offset})
		if err != nil {
			return nil, err
		}
		var result ContentReadResult
		if err := strictDecode(raw, &result); err != nil {
			return nil, &ProtocolError{Reason: ErrProtocolError, Message: "invalid host/content/read result"}
		}
		if result.ContentRef != ref || result.Offset != offset {
			return nil, &ProtocolError{Reason: ErrProtocolError, Message: "host/content/read answered a different ref or offset"}
		}
		if result.Encoding != ContentUTF8 {
			return nil, &ProtocolError{Reason: ErrProtocolError, Message: "host/content/read answered with an unknown encoding"}
		}
		if result.TotalBytes > ContentRefObjectBytes {
			return nil, &ProtocolError{Reason: ErrFrameTooLarge, Message: fmt.Sprintf(
				"content ref is %d bytes, above the %d byte object cap", result.TotalBytes, ContentRefObjectBytes)}
		}
		chunk, err := base64.StdEncoding.DecodeString(result.DataBase64)
		if err != nil {
			return nil, &ProtocolError{Reason: ErrProtocolError, Message: "host/content/read returned invalid base64"}
		}
		if len(chunk) > ContentRefChunkBytes {
			return nil, &ProtocolError{Reason: ErrProtocolError, Message: "host/content/read returned an oversized chunk"}
		}
		out = append(out, chunk...)
		if result.NextOffset == nil {
			if int64(len(out)) != result.TotalBytes {
				return nil, &ProtocolError{Reason: ErrProtocolError, Message: fmt.Sprintf(
					"content ref reassembled to %d bytes, host reported %d", len(out), result.TotalBytes)}
			}
			sum := sha256.Sum256(out)
			if !strings.EqualFold(hex.EncodeToString(sum[:]), result.SHA256) {
				return nil, &ProtocolError{Reason: ErrProtocolError, Message: "content ref SHA-256 mismatch"}
			}
			return out, nil
		}
		if *result.NextOffset <= offset {
			return nil, &ProtocolError{Reason: ErrProtocolError, Message: "host/content/read made no progress"}
		}
		offset = *result.NextOffset
	}
}

// ResolveExternalized rehydrates one owner document's externalizable field.
// raw is the field's inline value and externalized the owner's envelope, at
// the schema-registered JSON pointer ("/payload" for intercept and event
// params, "/replacement" for intercept results). With an empty envelope the
// inline value passes through; otherwise the envelope must hold exactly the
// pointer's descriptor, and the ref is paged back and verified against the
// descriptor's byte count and SHA-256 before it is returned. An inline value
// alongside an envelope, a wrong pointer, or unverifiable content is a
// protocol error — never decode bytes the peer did not prove.
//
// Intercept and event payloads are resolved automatically before the
// interceptor/observer runs; this helper remains for manual use.
func ResolveExternalized(ctx context.Context, raw json.RawMessage, externalized []ExternalizedField, pointer string) (json.RawMessage, error) {
	if serverFrom(ctx) == nil {
		return nil, ErrNoConnection
	}
	return resolveExternalized(ctx, raw, externalized, pointer)
}

func (s *server) rehydrate(ctx context.Context, raw json.RawMessage, externalized []ExternalizedField, pointer string) (json.RawMessage, error) {
	return resolveExternalized(ctx, raw, externalized, pointer)
}

func resolveExternalized(ctx context.Context, raw json.RawMessage, externalized []ExternalizedField, pointer string) (json.RawMessage, error) {
	if len(externalized) == 0 {
		return raw, nil
	}
	if inline := bytes.TrimSpace(raw); len(inline) > 0 && !bytes.Equal(inline, []byte("null")) {
		return nil, &ProtocolError{Reason: ErrProtocolError, Message: "document carries both an inline value and an externalized envelope"}
	}
	if len(externalized) != 1 || externalized[0].JSONPointer != pointer {
		return nil, &ProtocolError{Reason: ErrProtocolError, Message: fmt.Sprintf(
			"externalized envelope must hold exactly the %s descriptor", pointer)}
	}
	descriptor := externalized[0]
	if descriptor.TotalBytes > ContentRefObjectBytes {
		return nil, &ProtocolError{Reason: ErrFrameTooLarge, Message: fmt.Sprintf(
			"externalized value is %d bytes, above the %d byte object cap", descriptor.TotalBytes, ContentRefObjectBytes)}
	}
	data, err := ReadContentRef(ctx, descriptor.ContentRef)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) != descriptor.TotalBytes {
		return nil, &ProtocolError{Reason: ErrProtocolError, Message: fmt.Sprintf(
			"externalized value reassembled to %d bytes, want %d", len(data), descriptor.TotalBytes)}
	}
	sum := sha256.Sum256(data)
	if !strings.EqualFold(hex.EncodeToString(sum[:]), descriptor.SHA256) {
		return nil, &ProtocolError{Reason: ErrProtocolError, Message: "externalized value SHA-256 mismatch"}
	}
	return data, nil
}

// shared helpers

// callHost issues one Extension → Host request behind the handshake barrier
// and maps a structured wire error back to a *ProtocolError.
func (s *server) callHost(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if err := s.checkReady(); err != nil {
		return nil, err
	}
	raw, err := s.conn.call(ctx, method, params)
	if err != nil {
		return nil, mapCallError(err)
	}
	return raw, nil
}

// mapCallError converts a peer's JSON-RPC error into a *ProtocolError when it
// carries a frozen reason.
func mapCallError(err error) error {
	var respErr *ResponseError
	if errors.As(err, &respErr) {
		var data ProtocolErrorData
		if len(respErr.Data) > 0 && json.Unmarshal(respErr.Data, &data) == nil && data.Validate() == nil {
			return &ProtocolError{Reason: data.Reason, Message: respErr.Message}
		}
	}
	return err
}

// strictDecode decodes one params/result document rejecting unknown fields
// and trailing JSON, mirroring the host's strict decoder envelope rules.
func strictDecode(raw json.RawMessage, v any) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		raw = json.RawMessage(`{}`)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(v); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("trailing JSON")
	}
	return nil
}

// jsonKeyPresent reports whether raw is an object containing key, for
// required-but-nullable fields such as the externalizable payload.
func jsonKeyPresent(raw json.RawMessage, key string) bool {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		return false
	}
	_, ok := object[key]
	return ok
}
