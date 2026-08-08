package extension

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestHandshakeHappyPath verifies the initialize exchange echoes the
// sidecar's declaration and the barrier opens on extension/initialized.
func TestHandshakeHappyPath(t *testing.T) {
	handler := basicHandler()
	handler.result.Subscriptions = []string{"tool.before", "session.start"}
	handler.result.Replaces = []string{"model"}
	handler.result.UIActions = []UIActionDecl{{ActionID: "open", Label: "Open"}}
	var seen InitializeParams
	handler.seen = &seen
	host, _ := startFakeHost(t, handler, Options{})
	result := host.handshake(t)
	if result.ProtocolVersion != ProtocolVersion {
		t.Fatalf("protocolVersion = %q, want %q", result.ProtocolVersion, ProtocolVersion)
	}
	if result.Name != "test-ext" || result.Version != "0.1.0" {
		t.Fatalf("identity = %q/%q, want test-ext/0.1.0", result.Name, result.Version)
	}
	if len(result.Subscriptions) != 2 || result.Subscriptions[0] != "tool.before" {
		t.Fatalf("subscriptions = %v", result.Subscriptions)
	}
	if len(result.UIActions) != 1 || result.UIActions[0].ActionID != "open" {
		t.Fatalf("uiActions = %v", result.UIActions)
	}
	if seen.Session.SessionID != "sess-1" || seen.Session.Generation != 7 || seen.Session.WorkspaceRoot != "/repo" {
		t.Fatalf("session context = %+v", seen.Session)
	}
	if !seen.Capabilities.ContentRefs || seen.Capabilities.UIHost != UIHostHeadless {
		t.Fatalf("host capabilities = %+v", seen.Capabilities)
	}
	if seen.Manifest.Capabilities == nil {
		t.Fatalf("manifest expectation = %+v", seen.Manifest)
	}
}

// TestHostRequestBeforeInitializeRejected sends a non-initialize request
// first: it must be answered with the frozen protocol_error.
func TestHostRequestBeforeInitializeRejected(t *testing.T) {
	host, _ := startFakeHost(t, basicHandler(), Options{})
	resp := host.request(MethodExtensionIntercept, InterceptParams{
		Event: EventToolBefore, Seq: 1, Payload: json.RawMessage(`{}`),
	})
	if resp.Err == nil {
		t.Fatal("expected a protocol error for a request before initialize")
	}
	if resp.Err.Code != CodeInvalidRequest {
		t.Fatalf("code = %d, want %d", resp.Err.Code, CodeInvalidRequest)
	}
	data, ok := resp.Err.Data.(ProtocolErrorData)
	if !ok || data.Reason != ErrProtocolError {
		t.Fatalf("error data = %+v, want reason protocol_error", resp.Err.Data)
	}
}

// TestHostRequestBeforeInitializedRejected covers the barrier window: the
// handshake answer is out but extension/initialized has not arrived, so
// intercepts are still refused.
func TestHostRequestBeforeInitializedRejected(t *testing.T) {
	host, _ := startFakeHost(t, basicHandler(), Options{})
	resp := host.request(MethodExtensionInitialize, InitializeParams{
		ProtocolVersion: ProtocolVersion, ProtocolID: ProtocolID,
		Session:      SessionContext{SessionID: "s", WorkspaceRoot: "/r"},
		Capabilities: HostCapabilities{ProtocolVersion: ProtocolVersion},
	})
	if resp.Err != nil {
		t.Fatalf("initialize failed: %+v", resp.Err)
	}
	resp = host.request(MethodExtensionIntercept, InterceptParams{
		Event: EventToolBefore, Seq: 1, Payload: json.RawMessage(`{}`),
	})
	if resp.Err == nil || resp.Err.Code != CodeInvalidRequest {
		t.Fatalf("expected protocol_error before initialized, got %+v", resp.Err)
	}
	// Notifications before the barrier are dropped silently (no response is
	// possible); the connection must survive and the barrier must still open.
	host.notify(MethodExtensionEvent, EventParams{Event: EventSessionStart, Payload: json.RawMessage(`{}`)})
	host.notify(MethodExtensionInitialized, InitializedParams{})
	resp = host.request(MethodExtensionIntercept, InterceptParams{
		Event: EventToolBefore, Seq: 1, Payload: json.RawMessage(`{}`),
	})
	if resp.Err != nil {
		t.Fatalf("intercept after initialized failed: %+v", resp.Err)
	}
}

// TestOutboundCallBeforeInitializedFails checks the sidecar side of the
// barrier: Extension → Host calls from the Initialize handler return
// ErrNotReady.
func TestOutboundCallBeforeInitializedFails(t *testing.T) {
	handler := &testHandler{}
	var readyErr error
	handlerWithProbe := HandlerFunc(func(ctx context.Context, p InitializeParams) (*InitializeResult, error) {
		ui := HostUI{}
		readyErr = ui.PublishNotification(ctx, p.Session.SessionID, p.Session.Generation, "probe",
			UINotificationPayload{Title: "hi"})
		return &InitializeResult{Name: "probe", Version: "1"}, nil
	})
	handler.result = &InitializeResult{Name: "x", Version: "1"}
	host, _ := startFakeHost(t, handlerWithProbe, Options{})
	host.handshake(t)
	if !errors.Is(readyErr, ErrNotReady) {
		t.Fatalf("outbound call before initialized = %v, want ErrNotReady", readyErr)
	}
}

// TestInitializeVersionMismatch answers unsupported_version and ends Serve.
func TestInitializeVersionMismatch(t *testing.T) {
	host, serveDone := startFakeHost(t, basicHandler(), Options{})
	resp := host.request(MethodExtensionInitialize, InitializeParams{
		ProtocolVersion: "1", ProtocolID: ProtocolID,
		Session:      SessionContext{SessionID: "s", WorkspaceRoot: "/r"},
		Capabilities: HostCapabilities{ProtocolVersion: ProtocolVersion},
	})
	if resp.Err == nil {
		t.Fatal("expected unsupported_version")
	}
	data, _ := resp.Err.Data.(ProtocolErrorData)
	if data.Reason != ErrUnsupportedVersion {
		t.Fatalf("reason = %q, want unsupported_version", data.Reason)
	}
	err, ok := serveDone.wait(5 * time.Second)
	if !ok {
		t.Fatal("Serve did not end after a failed handshake")
	}
	if err == nil {
		t.Fatal("Serve returned nil after a failed handshake")
	}
}

// TestUnknownMethod verifies the frozen unknown_method answer.
func TestUnknownMethod(t *testing.T) {
	host, _ := startFakeHost(t, basicHandler(), Options{})
	host.handshake(t)
	resp := host.request("extension/bogus", struct{}{})
	if resp.Err == nil || resp.Err.Code != CodeMethodNotFound {
		t.Fatalf("expected -32601, got %+v", resp.Err)
	}
	data, _ := resp.Err.Data.(ProtocolErrorData)
	if data.Reason != ErrUnknownMethod {
		t.Fatalf("reason = %q, want unknown_method", data.Reason)
	}
}

// TestOversizedInboundFrame kills the connection with a frame error.
func TestOversizedInboundFrame(t *testing.T) {
	host, serveDone := startFakeHost(t, basicHandler(), Options{})
	big := make([]byte, FrameBytes+16)
	for i := range big {
		big[i] = ' '
	}
	copy(big, []byte(`{"jsonrpc":"2.0","id":1,"method":"extension/initialize","params":{}}`))
	host.writeRaw(big)
	err, ok := serveDone.wait(5 * time.Second)
	if !ok {
		t.Fatal("Serve did not end on an oversized frame")
	}
	var tooLarge *FrameTooLargeError
	if !errors.As(err, &tooLarge) {
		t.Fatalf("Serve error = %v, want FrameTooLargeError", err)
	}
}

// TestStrictFrameViolations checks envelope rejection: wrong jsonrpc
// version, string ids, and non-object params all answer -32600, and the
// connection survives to complete the handshake afterwards.
func TestStrictFrameViolations(t *testing.T) {
	host, _ := startFakeHost(t, basicHandler(), Options{})
	cases := []struct {
		name   string
		frame  string
		wantID string // expected id of the rejection response
	}{
		{"wrong jsonrpc", `{"jsonrpc":"1.0","id":1,"method":"extension/initialize","params":{}}`, "1"},
		{"missing jsonrpc", `{"id":2,"method":"extension/initialize","params":{}}`, "2"},
		{"string id", `{"jsonrpc":"2.0","id":"abc","method":"extension/initialize","params":{}}`, "null"},
		{"fractional id", `{"jsonrpc":"2.0","id":1.5,"method":"extension/initialize","params":{}}`, "null"},
		{"array params", `{"jsonrpc":"2.0","id":3,"method":"extension/initialize","params":[]}`, "3"},
		{"scalar params", `{"jsonrpc":"2.0","id":4,"method":"extension/initialize","params":42}`, "4"},
		{"result and method", `{"jsonrpc":"2.0","id":5,"method":"extension/initialize","result":{}}`, "5"},
	}
	for _, tc := range cases {
		host.writeRaw([]byte(tc.frame))
		stray := host.nextStray()
		if stray.Error == nil {
			t.Fatalf("%s: frame %s: expected an error response, got a result", tc.name, tc.frame)
		}
		if stray.Error.Code != CodeInvalidRequest {
			t.Fatalf("%s: code = %d, want %d", tc.name, stray.Error.Code, CodeInvalidRequest)
		}
		if string(stray.ID) != tc.wantID {
			t.Fatalf("%s: response id = %s, want %s", tc.name, stray.ID, tc.wantID)
		}
	}
	// The connection survived every rejection.
	host.handshake(t)
}

// TestInterceptRouting exercises exact and wildcard routing plus the default
// continue answer.
func TestInterceptRouting(t *testing.T) {
	var calls atomic.Int64
	interceptors := map[string]InterceptorFunc{
		"tool.before": func(_ context.Context, event string, payload json.RawMessage) (*InterceptResult, error) {
			calls.Add(1)
			if event != "tool.before" {
				t.Errorf("event = %q, want tool.before", event)
			}
			return Block("no tools today"), nil
		},
		"*": func(_ context.Context, event string, payload json.RawMessage) (*InterceptResult, error) {
			calls.Add(1)
			return Continue(), nil
		},
	}
	host, _ := startFakeHost(t, basicHandler(), Options{Interceptors: interceptors})
	host.handshake(t)

	resp := host.request(MethodExtensionIntercept, InterceptParams{
		Event: EventToolBefore, Seq: 1, Payload: json.RawMessage(`{"tool":"bash"}`), TimeoutMillis: 5000,
	})
	if resp.Err != nil {
		t.Fatalf("intercept failed: %+v", resp.Err)
	}
	var result InterceptResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("decode intercept result: %v", err)
	}
	if result.Decision != DecisionBlock || result.Reason != "no tools today" {
		t.Fatalf("result = %+v", result)
	}

	resp = host.request(MethodExtensionIntercept, InterceptParams{
		Event: EventSessionStart, Seq: 2, Payload: json.RawMessage(`{}`),
	})
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("decode wildcard result: %v", err)
	}
	if result.Decision != DecisionContinue {
		t.Fatalf("wildcard decision = %q, want continue", result.Decision)
	}
	if calls.Load() != 2 {
		t.Fatalf("interceptor calls = %d, want 2", calls.Load())
	}
}

// TestInterceptReplaceHelper checks Replace marshaling and the wire shape.
func TestInterceptReplace(t *testing.T) {
	interceptors := map[string]InterceptorFunc{
		"input.receive": func(_ context.Context, _ string, _ json.RawMessage) (*InterceptResult, error) {
			return Replace(map[string]any{"text": "rewritten", "n": 2})
		},
	}
	host, _ := startFakeHost(t, basicHandler(), Options{Interceptors: interceptors})
	host.handshake(t)
	resp := host.request(MethodExtensionIntercept, InterceptParams{
		Event: EventInputReceive, Seq: 1, Payload: json.RawMessage(`{"text":"original"}`),
	})
	if resp.Err != nil {
		t.Fatalf("intercept failed: %+v", resp.Err)
	}
	var wire struct {
		Decision    string          `json:"decision"`
		Replacement json.RawMessage `json:"replacement"`
	}
	if err := json.Unmarshal(resp.Result, &wire); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if wire.Decision != "replace" {
		t.Fatalf("decision = %q", wire.Decision)
	}
	var replacement map[string]any
	if err := json.Unmarshal(wire.Replacement, &replacement); err != nil {
		t.Fatalf("replacement is not an object: %v", err)
	}
	if replacement["text"] != "rewritten" || replacement["n"] != float64(2) {
		t.Fatalf("replacement = %v", replacement)
	}
}

// TestInterceptAllowDeny covers the permission.decision rulings.
func TestInterceptAllowDeny(t *testing.T) {
	interceptors := map[string]InterceptorFunc{
		"permission.decision": func(_ context.Context, _ string, payload json.RawMessage) (*InterceptResult, error) {
			if strings.Contains(string(payload), "dangerous") {
				return Deny("too dangerous"), nil
			}
			return Allow(), nil
		},
	}
	host, _ := startFakeHost(t, basicHandler(), Options{Interceptors: interceptors})
	host.handshake(t)
	var result InterceptResult
	resp := host.request(MethodExtensionIntercept, InterceptParams{
		Event: EventPermissionDecision, Seq: 1, Payload: json.RawMessage(`{"command":"ls"}`),
	})
	if err := json.Unmarshal(resp.Result, &result); err != nil || result.Decision != DecisionAllow {
		t.Fatalf("allow: result=%+v err=%v", result, err)
	}
	resp = host.request(MethodExtensionIntercept, InterceptParams{
		Event: EventPermissionDecision, Seq: 2, Payload: json.RawMessage(`{"command":"dangerous"}`),
	})
	if err := json.Unmarshal(resp.Result, &result); err != nil || result.Decision != DecisionDeny || result.Reason != "too dangerous" {
		t.Fatalf("deny: result=%+v err=%v", result, err)
	}
}

// TestInterceptInvalidEnvelope verifies envelope validation stays with the
// SDK: unknown events, bad seqs, and missing payload keys answer
// invalid_params without reaching the interceptor.
func TestInterceptInvalidEnvelope(t *testing.T) {
	var calls atomic.Int64
	interceptors := map[string]InterceptorFunc{
		"*": func(context.Context, string, json.RawMessage) (*InterceptResult, error) {
			calls.Add(1)
			return Continue(), nil
		},
	}
	host, _ := startFakeHost(t, basicHandler(), Options{Interceptors: interceptors})
	host.handshake(t)
	frames := []string{
		`{"event":"not.an.event","seq":1,"payload":{},"timeoutMillis":0}`,
		`{"event":"tool.before","seq":0,"payload":{},"timeoutMillis":0}`,
		`{"event":"tool.before","seq":1,"timeoutMillis":0}`,
		`{"event":"tool.before","seq":1,"payload":{},"timeoutMillis":-1,"extra":1}`,
	}
	for _, params := range frames {
		resp := host.request(MethodExtensionIntercept, json.RawMessage(params))
		if resp.Err == nil || resp.Err.Code != CodeInvalidParams {
			t.Fatalf("params %s: expected invalid_params, got %+v", params, resp.Err)
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("interceptor ran %d times on invalid envelopes", calls.Load())
	}
}

// TestInterceptHandlerPanic verifies a panicking interceptor answers the
// frozen internal error and the connection survives.
func TestInterceptHandlerPanic(t *testing.T) {
	interceptors := map[string]InterceptorFunc{
		"tool.before": func(context.Context, string, json.RawMessage) (*InterceptResult, error) {
			panic("boom")
		},
	}
	host, _ := startFakeHost(t, basicHandler(), Options{Interceptors: interceptors})
	host.handshake(t)
	resp := host.request(MethodExtensionIntercept, InterceptParams{
		Event: EventToolBefore, Seq: 1, Payload: json.RawMessage(`{}`),
	})
	if resp.Err == nil || resp.Err.Code != CodeInternal {
		t.Fatalf("expected internal error, got %+v", resp.Err)
	}
	data, _ := resp.Err.Data.(ProtocolErrorData)
	if data.Reason != ErrInternal {
		t.Fatalf("reason = %q, want internal", data.Reason)
	}
	resp = host.request(MethodExtensionIntercept, InterceptParams{
		Event: EventToolBefore, Seq: 2, Payload: json.RawMessage(`{}`),
	})
	if resp.Err == nil {
		t.Fatal("connection died after a handler panic")
	}
}

// TestInterceptDeadlineReturnsFrozenTimeout pins the equal-deadline race
// between the SDK callback budget and the host request budget. When the SDK
// timer wins, it must answer intercept_timeout rather than a generic internal
// error so the host observes one deterministic reason either way.
func TestInterceptDeadlineReturnsFrozenTimeout(t *testing.T) {
	interceptors := map[string]InterceptorFunc{
		"tool.before": func(ctx context.Context, _ string, _ json.RawMessage) (*InterceptResult, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	host, _ := startFakeHost(t, basicHandler(), Options{Interceptors: interceptors})
	host.handshake(t)
	resp := host.request(MethodExtensionIntercept, InterceptParams{
		Event: EventToolBefore, Seq: 1, Payload: json.RawMessage(`{}`), TimeoutMillis: 20,
	})
	if resp.Err == nil || resp.Err.Code != DomainErrorCode {
		t.Fatalf("expected a domain timeout error, got %+v", resp.Err)
	}
	data, _ := resp.Err.Data.(ProtocolErrorData)
	if data.Reason != ErrInterceptTimeout {
		t.Fatalf("reason = %q, want %q", data.Reason, ErrInterceptTimeout)
	}
}

// TestEventObservation checks extension/event reaches the observer and a
// panicking observer does not kill the loop.
func TestEventObservation(t *testing.T) {
	seen := make(chan string, 4)
	opts := Options{
		Observer: func(_ context.Context, event string, payload json.RawMessage) {
			seen <- event + ":" + string(payload)
		},
	}
	host, _ := startFakeHost(t, basicHandler(), opts)
	host.handshake(t)
	host.notify(MethodExtensionEvent, EventParams{Event: EventToolAfter, Payload: json.RawMessage(`{"ok":true}`)})
	select {
	case got := <-seen:
		if got != `tool.after:{"ok":true}` {
			t.Fatalf("observation = %q", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("observer not called")
	}
	// A nil observer extension must not choke on events either.
	host2, _ := startFakeHost(t, basicHandler(), Options{})
	host2.handshake(t)
	host2.notify(MethodExtensionEvent, EventParams{Event: EventToolAfter, Payload: json.RawMessage(`{}`)})
}

// TestResourcesChanged checks the resources/changed notification.
func TestResourcesChanged(t *testing.T) {
	seen := make(chan []string, 1)
	opts := Options{ResourcesChanged: func(_ context.Context, paths []string) { seen <- paths }}
	host, _ := startFakeHost(t, basicHandler(), opts)
	host.handshake(t)
	host.notify(MethodExtensionResourcesChanged, ResourcesChangedParams{Paths: []string{"skills/a", "themes/b"}})
	select {
	case paths := <-seen:
		if len(paths) != 2 || paths[0] != "skills/a" {
			t.Fatalf("paths = %v", paths)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ResourcesChanged not called")
	}
}

// TestUIActionSubmit covers the Host → Extension UI calls.
func TestUIActionSubmit(t *testing.T) {
	ui := UIHandler{
		Action: func(_ context.Context, actionID string, args map[string]string) error {
			if actionID == "fail" {
				return errTest
			}
			if actionID != "open" || args["k"] != "v" {
				return errors.New("bad action invocation")
			}
			return nil
		},
		Submit: func(_ context.Context, surfaceID string, values map[string]any) error {
			if surfaceID != "form-1" || values["name"] != "reasonix" {
				return errTest
			}
			return nil
		},
	}
	host, _ := startFakeHost(t, basicHandler(), Options{UI: ui})
	host.handshake(t)

	resp := host.request(MethodExtensionUIAction, UIActionParams{
		ActionID: "open", SessionID: "sess-1", Generation: 7, Args: map[string]string{"k": "v"},
	})
	var actionResult UIActionResult
	if err := json.Unmarshal(resp.Result, &actionResult); err != nil || !actionResult.Accepted {
		t.Fatalf("action: result=%+v err=%v respErr=%+v", actionResult, err, resp.Err)
	}
	resp = host.request(MethodExtensionUIAction, UIActionParams{ActionID: "fail", SessionID: "sess-1", Generation: 7})
	if err := json.Unmarshal(resp.Result, &actionResult); err != nil {
		t.Fatalf("action fail decode: %v", err)
	}
	if actionResult.Accepted || actionResult.Message != errTest.Error() {
		t.Fatalf("action fail result = %+v", actionResult)
	}

	resp = host.request(MethodExtensionUISubmit, UISubmitParams{
		SurfaceID: "form-1", SessionID: "sess-1", Generation: 7, Values: map[string]any{"name": "reasonix"},
	})
	var submitResult UISubmitResult
	if err := json.Unmarshal(resp.Result, &submitResult); err != nil || !submitResult.Accepted {
		t.Fatalf("submit: result=%+v err=%v", submitResult, err)
	}

	// Without UI configured the methods answer unknown_method.
	host2, _ := startFakeHost(t, basicHandler(), Options{})
	host2.handshake(t)
	resp = host2.request(MethodExtensionUIAction, UIActionParams{ActionID: "x", SessionID: "s", Generation: 1})
	if resp.Err == nil || resp.Err.Code != CodeMethodNotFound {
		t.Fatalf("expected unknown_method without UI, got %+v", resp.Err)
	}
}

// TestShutdownSequence runs the graceful stop: the fn runs with its timeout,
// {accepted:true} is answered, and Serve returns nil.
func TestShutdownSequence(t *testing.T) {
	var ran atomic.Bool
	opts := Options{Shutdown: func(ctx context.Context) {
		ran.Store(true)
		if _, hasDeadline := ctx.Deadline(); !hasDeadline {
			t.Errorf("shutdown ctx has no deadline despite timeoutMillis")
		}
	}}
	host, serveDone := startFakeHost(t, basicHandler(), opts)
	host.handshake(t)
	resp := host.request(MethodExtensionShutdown, ShutdownParams{TimeoutMillis: 5000})
	if resp.Err != nil {
		t.Fatalf("shutdown failed: %+v", resp.Err)
	}
	var result ShutdownResult
	if err := json.Unmarshal(resp.Result, &result); err != nil || !result.Accepted {
		t.Fatalf("shutdown result = %+v", result)
	}
	if !ran.Load() {
		t.Fatal("shutdown fn did not run")
	}
	err, ok := serveDone.wait(5 * time.Second)
	if !ok {
		t.Fatal("Serve did not return after shutdown")
	}
	if err != nil {
		t.Fatalf("Serve returned %v after an orderly shutdown, want nil", err)
	}
}

// TestShutdownWithoutFn still answers and exits when no Shutdown fn is set.
func TestShutdownWithoutFn(t *testing.T) {
	host, serveDone := startFakeHost(t, basicHandler(), Options{})
	host.handshake(t)
	resp := host.request(MethodExtensionShutdown, ShutdownParams{TimeoutMillis: 0})
	var result ShutdownResult
	if err := json.Unmarshal(resp.Result, &result); err != nil || !result.Accepted {
		t.Fatalf("shutdown result = %+v err=%v", result, resp.Err)
	}
	err, ok := serveDone.wait(5 * time.Second)
	if !ok {
		t.Fatal("Serve did not return")
	}
	if err != nil {
		t.Fatalf("Serve returned %v, want nil", err)
	}
}

// TestServeContextCancel verifies canceling the Serve context tears the
// transport down promptly.
func TestServeContextCancel(t *testing.T) {
	sdkStdinR, sdkStdinW := io.Pipe()
	sdkStdoutR, sdkStdoutW := io.Pipe()
	defer sdkStdoutR.Close()
	ctx, cancel := context.WithCancel(context.Background())
	serveDone := make(chan error, 1)
	go func() {
		serveDone <- Serve(ctx, basicHandler(), Options{Stdin: sdkStdinR, Stdout: sdkStdoutW})
	}()
	go io.Copy(io.Discard, sdkStdoutR)
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-serveDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Serve returned %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return after ctx cancel")
	}
	_ = sdkStdinW.Close()
}

// TestHostEOFCleanReturn covers the host closing the transport: Serve
// returns nil.
func TestHostEOFCleanReturn(t *testing.T) {
	host, serveDone := startFakeHost(t, basicHandler(), Options{})
	host.handshake(t)
	if err := host.toSDK.Close(); err != nil {
		t.Fatalf("close host pipe: %v", err)
	}
	err, ok := serveDone.wait(5 * time.Second)
	if !ok {
		t.Fatal("Serve did not return on host EOF")
	}
	if err != nil {
		t.Fatalf("Serve returned %v on host EOF, want nil", err)
	}
}
