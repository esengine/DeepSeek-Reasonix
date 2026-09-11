package sidecar

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/extension"
	"reasonix/internal/extension/protocol"
	"reasonix/internal/extension/rpcwire"
	"reasonix/internal/pluginpkg"
)

func TestHandshakeSuccess(t *testing.T) {
	client := startFakeClient(t, func(rt *pluginpkg.RuntimeSpec) {
		rt.Intercepts = []string{"input.receive", "tool.before"}
		rt.Env[fakeEnvInitResult] = `{"protocolVersion":"2","name":"fake-sidecar","version":"1.2.3","subscriptions":["input.receive"],"stateSchemaVersion":0}`
	}, nil)
	result := client.Handshake()
	if result.Name != "fake-sidecar" || result.Version != "1.2.3" {
		t.Fatalf("handshake identity = %q %q", result.Name, result.Version)
	}
	if len(result.Subscriptions) != 1 || result.Subscriptions[0] != "input.receive" {
		t.Fatalf("subscriptions = %v", result.Subscriptions)
	}
	if client.Crashed() {
		t.Fatal("client crashed during handshake")
	}
}

func TestInitializeParamsCarryManifestV2DependencyIdentity(t *testing.T) {
	c := &Client{
		rt: &pluginpkg.RuntimeSpec{
			Intercepts:   []string{"input.receive", "system_prompt.build"},
			Replaces:     []string{"system_prompt"},
			Capabilities: []string{"interceptors", "strategies", "providers", "ui"},
		},
		requires: []pluginpkg.CapabilityRef{{
			Namespace: "reasonix", Kind: "provider", ID: "base", VersionRange: ">=1.0.0", Optional: true,
		}},
		provides: []pluginpkg.CapabilityRef{
			{Namespace: "plugin/example", Kind: "provider", ID: "fake/echo", Version: "1.0.0", SchemaHash: "sha256:provider"},
			{Namespace: "plugin/example", Kind: "uiaction", ID: "demo", Version: "1.0.0", SchemaHash: "sha256:ui"},
		},
		session: protocol.SessionContext{SessionID: "sess", WorkspaceRoot: "/workspace", Generation: 7},
		uiHost:  protocol.UIHostDesktop,
	}

	params := c.initializeParams()
	if params.DependencySchemaVersion != protocol.DependencySchemaVersion || params.Capabilities.DependencySchemaVersion != protocol.DependencySchemaVersion {
		t.Fatalf("dependency schema versions = %d/%d, want %d", params.DependencySchemaVersion, params.Capabilities.DependencySchemaVersion, protocol.DependencySchemaVersion)
	}
	if len(params.Manifest.Requires) != 1 || params.Manifest.Requires[0].ID != "base" || params.Manifest.Requires[0].VersionRange != ">=1.0.0" || !params.Manifest.Requires[0].Optional {
		t.Fatalf("manifest requires = %+v", params.Manifest.Requires)
	}
	if len(params.Manifest.Provides) != 2 || params.Manifest.Provides[0].SchemaHash != "sha256:provider" || params.Manifest.Provides[1].SchemaHash != "sha256:ui" {
		t.Fatalf("manifest provides = %+v", params.Manifest.Provides)
	}
	if len(params.Manifest.Providers) != 1 || params.Manifest.Providers[0] != "plugin/example/fake/echo" {
		t.Fatalf("manifest providers = %v", params.Manifest.Providers)
	}
	if len(params.Manifest.UIActions) != 1 || params.Manifest.UIActions[0] != "demo" {
		t.Fatalf("manifest uiActions = %v", params.Manifest.UIActions)
	}
}

func TestHandshakeProtocolVersionMismatch(t *testing.T) {
	pkg, installed := fakeSidecarPackage(t, "fakeplugin", func(rt *pluginpkg.RuntimeSpec) {
		// Peer still speaking Extension Protocol v1 major must be rejected.
		rt.Env[fakeEnvInitResult] = `{"protocolVersion":"1","name":"fake-sidecar","version":"1.0.0","stateSchemaVersion":0}`
	})
	_, err := StartClient(context.Background(), ClientOptions{Package: pkg, Installed: installed, Session: testSessionContext()})
	if err == nil {
		t.Fatal("StartClient succeeded with protocol major 1")
	}
	if reason := protocolReason(t, err); reason != protocol.ErrUnsupportedVersion {
		t.Fatalf("reason = %q, want %q", reason, protocol.ErrUnsupportedVersion)
	}
}

func TestMapRequestErrorRedactsPeerMessages(t *testing.T) {
	const secret = "sk-abcdef1234567890SECRETKEY"
	structuredData, err := json.Marshal(protocol.ProtocolErrorData{
		Reason:    protocol.ErrProviderFailed,
		Retryable: true,
	})
	if err != nil {
		t.Fatalf("marshal protocol error data: %v", err)
	}
	tests := []struct {
		name string
		data json.RawMessage
	}{
		{name: "structured protocol error", data: structuredData},
		{name: "unstructured transport error", data: json.RawMessage(`{"unexpected":true}`)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mapped := mapRequestError(&rpcwire.ResponseError{
				Code:    protocol.DomainErrorCode,
				Message: "provider rejected api_key=" + secret,
				Data:    tt.data,
			})
			if strings.Contains(mapped.Error(), secret) {
				t.Fatalf("mapped error leaked peer credential: %q", mapped)
			}
			if !strings.Contains(mapped.Error(), "****") {
				t.Fatalf("mapped error contains no redaction marker: %q", mapped)
			}
		})
	}
}

// TestHandshakeCapabilityViolations pins the declaration contract: anything
// the sidecar activates beyond its manifest fails the handshake with
// capability_not_declared.
func TestHandshakeCapabilityViolations(t *testing.T) {
	cases := []struct {
		name       string
		configure  func(rt *pluginpkg.RuntimeSpec)
		initResult string
	}{
		{
			name:       "subscriptions superset",
			configure:  func(rt *pluginpkg.RuntimeSpec) { rt.Intercepts = []string{"input.receive"} },
			initResult: `{"protocolVersion":"2","name":"fake","version":"1","subscriptions":["input.receive","tool.before"],"stateSchemaVersion":0}`,
		},
		{
			name:       "replaces superset",
			configure:  func(rt *pluginpkg.RuntimeSpec) { rt.Replaces = []string{"system_prompt"} },
			initResult: `{"protocolVersion":"2","name":"fake","version":"1","replaces":["system_prompt","compaction"],"stateSchemaVersion":0}`,
		},
		{
			name:       "providers without capability",
			configure:  nil,
			initResult: `{"protocolVersion":"2","name":"fake","version":"1","providers":[{"ref":"plugin/fakeplugin/openai/gpt-5"}],"stateSchemaVersion":0}`,
		},
		{
			name:       "provider ref outside plugin namespace",
			configure:  func(rt *pluginpkg.RuntimeSpec) { rt.Capabilities = []string{"providers"} },
			initResult: `{"protocolVersion":"2","name":"fake","version":"1","providers":[{"ref":"plugin/other/openai/gpt-5"}],"stateSchemaVersion":0}`,
		},
		{
			name:       "ui actions without capability",
			configure:  nil,
			initResult: `{"protocolVersion":"2","name":"fake","version":"1","uiActions":[{"actionId":"a1"}],"stateSchemaVersion":0}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pkg, installed := fakeSidecarPackage(t, "fakeplugin", func(rt *pluginpkg.RuntimeSpec) {
				if tc.configure != nil {
					tc.configure(rt)
				}
				rt.Env[fakeEnvInitResult] = tc.initResult
			})
			_, err := StartClient(context.Background(), ClientOptions{Package: pkg, Installed: installed, Session: testSessionContext()})
			if err == nil {
				t.Fatal("StartClient succeeded with an undeclared capability in use")
			}
			if reason := protocolReason(t, err); reason != protocol.ErrCapabilityNotDeclared {
				t.Fatalf("reason = %q, want %q", reason, protocol.ErrCapabilityNotDeclared)
			}
		})
	}
}

func TestHandshakeDeclaredProvidersAndUIAccepted(t *testing.T) {
	client := startFakeClient(t, func(rt *pluginpkg.RuntimeSpec) {
		rt.Capabilities = []string{"providers", "ui"}
		rt.Env[fakeEnvInitResult] = `{"protocolVersion":"2","name":"fake","version":"1",` +
			`"providers":[{"ref":"plugin/fakeplugin/openai/gpt-5"}],` +
			`"uiActions":[{"actionId":"act1","label":"Act"}],"stateSchemaVersion":0}`
	}, nil)
	result := client.Handshake()
	if len(result.Providers) != 1 || result.Providers[0].Ref != "plugin/fakeplugin/openai/gpt-5" {
		t.Fatalf("providers = %+v", result.Providers)
	}
	if len(result.UIActions) != 1 || result.UIActions[0].ActionID != "act1" {
		t.Fatalf("uiActions = %+v", result.UIActions)
	}
}

func TestHandshakeProvidesOutsideManifestRejected(t *testing.T) {
	pkg, installed := fakeSidecarPackage(t, "fakeplugin", func(rt *pluginpkg.RuntimeSpec) {
		rt.Env[fakeEnvInitResult] = `{"protocolVersion":"2","name":"fake","version":"1",` +
			`"provides":[{"namespace":"plugin/fakeplugin","kind":"provider","id":"rogue","version":"1.0.0","schemaHash":"sha256:rogue"}],` +
			`"stateSchemaVersion":0}`
	})
	pkg.Manifest.Provides = []pluginpkg.CapabilityRef{{
		Namespace: "plugin/fakeplugin", Kind: "provider", ID: "declared", Version: "1.0.0", SchemaHash: "sha256:declared",
	}}
	_, err := StartClient(context.Background(), ClientOptions{Package: pkg, Installed: installed, Session: testSessionContext()})
	if err == nil {
		t.Fatal("StartClient accepted a handshake provides entry outside the manifest ceiling")
	}
	if reason := protocolReason(t, err); reason != protocol.ErrCapabilityNotDeclared {
		t.Fatalf("reason = %q, want %q", reason, protocol.ErrCapabilityNotDeclared)
	}
}

// TestTrafficBeforeInitializedPoisons covers both E→H frame kinds arriving
// before extension/initialized: the connection is poisoned and the start
// fails with protocol_error.
func TestTrafficBeforeInitializedPoisons(t *testing.T) {
	for _, mode := range []string{"early_request", "early_notify"} {
		t.Run(mode, func(t *testing.T) {
			pkg, installed := fakeSidecarPackage(t, "fakeplugin", func(rt *pluginpkg.RuntimeSpec) {
				rt.Env[fakeEnvMode] = mode
			})
			_, err := StartClient(context.Background(), ClientOptions{Package: pkg, Installed: installed, Session: testSessionContext()})
			if err == nil {
				t.Fatal("StartClient succeeded despite pre-initialized traffic")
			}
			if reason := protocolReason(t, err); reason != protocol.ErrProtocolError {
				t.Fatalf("reason = %q, want %q", reason, protocol.ErrProtocolError)
			}
			if !strings.Contains(err.Error(), "before extension/initialized") {
				t.Fatalf("error %q does not name the gating rule", err)
			}
		})
	}
}

func TestInterceptContinueAndNotifications(t *testing.T) {
	client := startFakeClient(t, nil, nil)
	result, err := client.Intercept(context.Background(), protocol.EventInputReceive, json.RawMessage(`{"text":"hi"}`), 5*time.Second)
	if err != nil {
		t.Fatalf("Intercept: %v", err)
	}
	if result.Decision != protocol.DecisionContinue {
		t.Fatalf("decision = %q, want continue", result.Decision)
	}
	if err := client.NotifyEvent(protocol.EventSessionStart, json.RawMessage(`{"at":1}`)); err != nil {
		t.Fatalf("NotifyEvent: %v", err)
	}
	if err := client.NotifyResourcesChanged([]string{"skills/x.md"}); err != nil {
		t.Fatalf("NotifyResourcesChanged: %v", err)
	}
}

func TestInterceptTimeout(t *testing.T) {
	client := startFakeClient(t, func(rt *pluginpkg.RuntimeSpec) {
		rt.Env[fakeEnvMode] = "stall_intercept"
	}, nil)
	start := time.Now()
	_, err := client.Intercept(context.Background(), protocol.EventToolBefore, json.RawMessage(`{}`), 300*time.Millisecond)
	if err == nil {
		t.Fatal("Intercept succeeded against a stalled sidecar")
	}
	if reason := protocolReason(t, err); reason != protocol.ErrInterceptTimeout {
		t.Fatalf("reason = %q, want %q", reason, protocol.ErrInterceptTimeout)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("intercept timeout took %s, want near 300ms", elapsed)
	}
}

// TestShutdownBounded covers a sidecar that ignores extension/shutdown: the
// bounded close kills and reaps the tree within budget, and Shutdown is
// idempotent.
func TestShutdownBounded(t *testing.T) {
	client := startFakeClient(t, func(rt *pluginpkg.RuntimeSpec) {
		rt.Env[fakeEnvMode] = "ignore_shutdown"
	}, nil)
	start := time.Now()
	if err := client.Shutdown(context.Background(), 300*time.Millisecond); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	elapsed := time.Since(start)
	// 300ms request + 750ms EOF grace + kill + 5s reap must finish far below
	// this ceiling.
	if elapsed > 10*time.Second {
		t.Fatalf("bounded shutdown took %s", elapsed)
	}
	if !client.Exited() {
		t.Fatal("sidecar process still running after bounded shutdown")
	}
	// Idempotent: a second call returns immediately.
	second := time.Now()
	_ = client.Shutdown(context.Background(), 5*time.Second)
	if time.Since(second) > time.Second {
		t.Fatal("second Shutdown was not idempotent")
	}
}

// TestCrashFailsPendingAndFastAfter kills the fake sidecar mid-intercept:
// the pending call errors, the crash callback fires exactly once, and later
// calls fail fast with provider_interrupted.
func TestCrashFailsPendingAndFastAfter(t *testing.T) {
	var crashes atomic.Int32
	client := startFakeClient(t, func(rt *pluginpkg.RuntimeSpec) {
		rt.Env[fakeEnvMode] = "stall_intercept"
	}, func(opts *ClientOptions) {
		opts.OnCrash = func(error) { crashes.Add(1) }
	})

	pending := make(chan error, 1)
	go func() {
		_, err := client.Intercept(context.Background(), protocol.EventToolBefore, json.RawMessage(`{}`), 30*time.Second)
		pending <- err
	}()
	waitFor(t, "the intercept to reach the sidecar", 5*time.Second, func() bool {
		return strings.Contains(client.proc.stderr.String(), "intercept-stalled")
	})

	if err := client.proc.cmd.Process.Kill(); err != nil {
		t.Fatalf("kill fake sidecar: %v", err)
	}
	select {
	case err := <-pending:
		if err == nil {
			t.Fatal("pending Intercept succeeded after the sidecar was killed")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("pending Intercept did not error after the sidecar was killed")
	}
	waitFor(t, "crash detection", 5*time.Second, client.Crashed)
	waitFor(t, "process reaping", 5*time.Second, client.Exited)
	if got := crashes.Load(); got != 1 {
		t.Fatalf("OnCrash fired %d times, want exactly 1", got)
	}

	// Later calls fail fast with the provider_interrupted family.
	start := time.Now()
	_, err := client.Intercept(context.Background(), protocol.EventToolBefore, json.RawMessage(`{}`), 30*time.Second)
	if err == nil {
		t.Fatal("Intercept succeeded after crash")
	}
	if reason := protocolReason(t, err); reason != protocol.ErrProviderInterrupted {
		t.Fatalf("reason = %q, want %q", reason, protocol.ErrProviderInterrupted)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("post-crash Intercept was not fail-fast (%s)", elapsed)
	}
	if got := crashes.Load(); got != 1 {
		t.Fatalf("OnCrash fired %d times after fail-fast, want exactly 1", got)
	}
}

func TestTimeoutFor(t *testing.T) {
	client := startFakeClient(t, nil, nil)
	fast := []extension.InterceptorPoint{
		extension.PointInputReceive, extension.PointToolBefore,
		extension.PointToolAfter, extension.PointPermissionDecision,
	}
	for _, point := range fast {
		if got := client.TimeoutFor(point); got != fastInterceptTimeout {
			t.Fatalf("TimeoutFor(%s) = %s, want %s", point, got, fastInterceptTimeout)
		}
	}
	slow := []extension.InterceptorPoint{
		extension.PointSessionStart, extension.PointSystemPromptBuild,
		extension.PointContextPrepare, extension.PointCompactionPrepare,
	}
	for _, point := range slow {
		if got := client.TimeoutFor(point); got != slowInterceptTimeout {
			t.Fatalf("TimeoutFor(%s) = %s, want %s", point, got, slowInterceptTimeout)
		}
	}
}

func TestTimeoutForManifestOverrideClamped(t *testing.T) {
	client := startFakeClient(t, func(rt *pluginpkg.RuntimeSpec) {
		rt.TimeoutMillis = 250
	}, nil)
	if got := client.TimeoutFor(extension.PointInputReceive); got != 250*time.Millisecond {
		t.Fatalf("TimeoutFor with manifest override = %s, want 250ms", got)
	}
	clamped := startFakeClient(t, func(rt *pluginpkg.RuntimeSpec) {
		rt.TimeoutMillis = 10 * 60 * 1000 // 10 minutes
	}, nil)
	if got := clamped.TimeoutFor(extension.PointSessionStart); got != maxInterceptTimeout {
		t.Fatalf("TimeoutFor beyond the ceiling = %s, want the 60s clamp", got)
	}
}

func TestUIHandlerDefaultsToUnavailable(t *testing.T) {
	client := startFakeClient(t, nil, nil)
	_, err := client.ui.Publish(context.Background(), protocol.UIPublishParams{})
	if err == nil {
		t.Fatal("default UI handler accepted a publish")
	}
	if reason := protocolReason(t, err); reason != protocol.ErrUnknownMethod {
		t.Fatalf("reason = %q, want %q", reason, protocol.ErrUnknownMethod)
	}
}

// TestUIActionAndSubmitRoundTrip drives the host-initiated UI calls (stage 8)
// over the real wire: the fake sidecar echoes the action id and accepts the
// form submission.
func TestUIActionAndSubmitRoundTrip(t *testing.T) {
	client := startFakeClient(t, nil, nil)
	action, err := client.UIAction(context.Background(), protocol.UIActionParams{
		ActionID: "act1", SessionID: "sess-test", Generation: 1, Args: map[string]string{"k": "v"},
	})
	if err != nil {
		t.Fatalf("UIAction: %v", err)
	}
	if !action.Accepted || action.Message == "" {
		t.Fatalf("UIAction result = %+v", action)
	}
	submit, err := client.UISubmit(context.Background(), protocol.UISubmitParams{
		SurfaceID: "f1", SessionID: "sess-test", Generation: 1, Values: map[string]any{"name": "x"},
	})
	if err != nil {
		t.Fatalf("UISubmit: %v", err)
	}
	if !submit.Accepted {
		t.Fatalf("UISubmit result = %+v", submit)
	}
}

func TestStartRejectsInvalidOptions(t *testing.T) {
	pkg, installed := fakeSidecarPackage(t, "fakeplugin", nil)
	if _, err := StartClient(context.Background(), ClientOptions{
		Package:   pkg,
		Installed: installed,
		Session:   protocol.SessionContext{},
	}); err == nil {
		t.Fatal("StartClient accepted an empty session context")
	}
	pkgNoRuntime := pluginpkg.Package{Root: t.TempDir(), Manifest: pluginpkg.Manifest{Name: "x"}}
	if _, err := StartClient(context.Background(), ClientOptions{
		Package:   pkgNoRuntime,
		Installed: installed,
		Session:   testSessionContext(),
	}); err == nil {
		t.Fatal("StartClient accepted a package without a runtime")
	}
}

// TestWriteStallKillsWedgedSidecar is the deterministic regression for the
// host-availability review finding: a sidecar that stays alive but stops
// reading stdin fills the pipe, and an unbounded write would hang the host
// forever. With the write-stall bound the call fails fast, the connection
// dies, and the process tree is killed.
func TestWriteStallKillsWedgedSidecar(t *testing.T) {
	client := startFakeClient(t, func(rt *pluginpkg.RuntimeSpec) {
		rt.Env[fakeEnvMode] = "wedge_after_init"
	}, func(opts *ClientOptions) {
		opts.WriteStallBound = 100 * time.Millisecond
	})

	// Bypass Intercept's externalization (payloads over 64 KiB offload to
	// content refs) so the frame itself exceeds the OS pipe buffer.
	big := json.RawMessage(`{"pad":"` + strings.Repeat("x", 1<<20) + `"}`)
	start := time.Now()
	_, err := client.conn.Request(context.Background(), string(protocol.MethodExtensionIntercept), json.RawMessage(big))
	elapsed := time.Since(start)
	var stall *rpcwire.WriteStallError
	if !errors.As(err, &stall) {
		t.Fatalf("error = %v, want WriteStallError", err)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("stall took %s to abort, want near 100ms", elapsed)
	}

	waitFor(t, "client marked crashed", 5*time.Second, client.Crashed)
	waitFor(t, "wedged sidecar killed", 5*time.Second, client.Exited)
}

// TestWriteStallWatchdogOutlivesCallerTimeout: a per-call timeout shorter
// than the stall bound aborts only that call — the stall watchdog is an
// independent absolute bound that still fails the connection and kills the
// wedged sidecar afterwards. (Review finding: a 5s intercept ctx must not
// preempt the 10s stall watchdog.)
func TestWriteStallWatchdogOutlivesCallerTimeout(t *testing.T) {
	client := startFakeClient(t, func(rt *pluginpkg.RuntimeSpec) {
		rt.Env[fakeEnvMode] = "wedge_after_init"
	}, func(opts *ClientOptions) {
		opts.WriteStallBound = 300 * time.Millisecond
	})

	big := json.RawMessage(`{"pad":"` + strings.Repeat("x", 1<<20) + `"}`)
	// Primer: a background-context request whose frame wedges the single
	// writer for good (the fake never reads). Its write cannot be cancelled,
	// so the stall watchdog has an active write to trip on.
	go func() {
		_, _ = client.conn.Request(context.Background(), string(protocol.MethodExtensionIntercept), big)
	}()
	// The timed call cancels fast — but the primer's write outlives it, and
	// the 300ms stall watchdog still fires: the connection dies and the
	// wedged process is killed. The caller timeout did not preempt it.
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	_, err := client.conn.Request(ctx, string(protocol.MethodExtensionIntercept), big)
	elapsed := time.Since(start)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if elapsed > 3*time.Second {
		t.Fatalf("caller cancel took %s, want near 200ms", elapsed)
	}

	waitFor(t, "client marked crashed after caller cancel", 10*time.Second, client.Crashed)
	waitFor(t, "wedged sidecar killed after caller cancel", 10*time.Second, client.Exited)
}
