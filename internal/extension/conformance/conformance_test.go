// Package conformance runs the Host↔SDK bidirectional conformance suite: the
// SDK's reference example (sdk/go/examples/fullsidecar) is built once per
// test run and driven against the real host sidecar client
// (internal/extension/sidecar) over its stdin/stdout, plus a raw-frame driver
// for the transport-level cases (unknown method, oversized frame, bounded
// shutdown exit status). The suite is hermetic: temp dirs, no network, no
// real providers.
package conformance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/extension/protocol"
	"reasonix/internal/extension/rpcwire"
	"reasonix/internal/extension/sidecar"
	"reasonix/internal/pluginpkg"
)

// examplePath is the built fullsidecar binary, shared by every test.
var (
	examplePath string
	exampleRoot string
)

// TestMain builds the SDK example once for the whole run. The suite skips
// cleanly when no go toolchain is available (minimal test environments); a
// present toolchain that cannot build the example is a real failure.
func TestMain(m *testing.M) {
	if _, err := exec.LookPath("go"); err != nil {
		fmt.Fprintln(os.Stderr, "conformance: go toolchain unavailable; skipping suite")
		os.Exit(0)
	}
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		fmt.Fprintln(os.Stderr, "conformance: cannot locate source root")
		os.Exit(1)
	}
	sdkDir := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "sdk", "go")
	exampleRoot = filepath.Join(sdkDir, "examples", "fullsidecar")
	dir, err := os.MkdirTemp("", "fullsidecar-conformance-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "conformance: MkdirTemp:", err)
		os.Exit(1)
	}
	defer os.RemoveAll(dir)
	binary := filepath.Join(dir, "fullsidecar")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	build := exec.Command("go", "build", "-C", sdkDir, "-o", binary, "./examples/fullsidecar")
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "conformance: build example: %v\n%s", err, out)
		os.Exit(1)
	}
	examplePath = binary
	os.Exit(m.Run())
}

// Host client fixture

const (
	testPluginID              = "full-sidecar"
	testProvider              = "plugin/full-sidecar/fake/echo"
	fixtureProviderSchemaHash = "sha256:416af537aeb7edd2ff0b96fd2ecb385bc10f900e8b320292857c5279cb5bce50"
	fixtureUIActionSchemaHash = "sha256:8532d24af25d5aaeb763c35d8c9d3283d8604a3473f8ddd06a4c910565b86aeb"
)

// startExample launches the example under the real host sidecar client with a
// manifest that declares everything the example contributes. mutate tunes the
// runtime spec (env, under-declared manifests); opts tunes ClientOptions.
func startExample(t *testing.T, mutate func(rt *pluginpkg.RuntimeSpec), opts func(*sidecar.ClientOptions)) *sidecar.Client {
	t.Helper()
	_, item := installExamplePackage(t)
	if mutate != nil {
		mutate(item.Package.Manifest.Runtime)
	}
	clientOpts := sidecar.ClientOptions{
		Package:   item.Package,
		Installed: item.Installed,
		Session:   protocol.SessionContext{SessionID: "sess-conf", WorkspaceRoot: "/ws", Generation: 1},
	}
	if opts != nil {
		opts(&clientOpts)
	}
	client, err := sidecar.StartClient(context.Background(), clientOpts)
	if err != nil {
		t.Fatalf("StartClient: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

// intercept is a small shortcut for the common blocking-intercept call.
func intercept(t *testing.T, client *sidecar.Client, event protocol.InterceptEvent, payload string) protocol.InterceptResult {
	t.Helper()
	result, err := client.Intercept(context.Background(), event, json.RawMessage(payload), 10*time.Second)
	if err != nil {
		t.Fatalf("Intercept(%s): %v", event, err)
	}
	return result
}

// decodeReplacement strict-decodes an intercept replacement into out.
func decodeReplacement(t *testing.T, result protocol.InterceptResult, out any) {
	t.Helper()
	if result.Decision != protocol.DecisionReplace {
		t.Fatalf("decision = %q (reason %q), want replace", result.Decision, result.Reason)
	}
	if len(result.Replacement) == 0 {
		t.Fatal("replace decision carries no replacement")
	}
	decoder := json.NewDecoder(bytes.NewReader(result.Replacement))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		t.Fatalf("replacement does not decode: %v", err)
	}
}

// Stub UI handler and stream router

// stubUI records host/ui/publish calls and answers host/ui/request through a
// programmable function (default: the user cancelled).
type stubUI struct {
	mu        sync.Mutex
	published []protocol.UIPublishParams
	requestFn func(p protocol.UIRequestParams) (protocol.UIRequestResult, error)
}

func (s *stubUI) Publish(_ context.Context, p protocol.UIPublishParams) (protocol.UIPublishResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.published = append(s.published, p)
	return protocol.UIPublishResult{Accepted: true}, nil
}

func (s *stubUI) Request(_ context.Context, p protocol.UIRequestParams) (protocol.UIRequestResult, error) {
	s.mu.Lock()
	fn := s.requestFn
	s.mu.Unlock()
	if fn != nil {
		return fn(p)
	}
	return protocol.UIRequestResult{Cancelled: true}, nil
}

// publishedOfKind returns the recorded publishes of one surface kind.
func (s *stubUI) publishedOfKind(kind protocol.UISurfaceKind) []protocol.UIPublishParams {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []protocol.UIPublishParams
	for _, p := range s.published {
		if p.Kind == kind {
			out = append(out, p)
		}
	}
	return out
}

func (s *stubUI) publishedCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.published)
}

// stubStreams records routed provider stream notifications.
type stubStreams struct {
	mu     sync.Mutex
	chunks []protocol.StreamChunkParams
	ends   []protocol.StreamEndParams
}

func (s *stubStreams) RouteStreamChunk(p protocol.StreamChunkParams) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.chunks = append(s.chunks, p)
}

func (s *stubStreams) RouteStreamEnd(p protocol.StreamEndParams) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ends = append(s.ends, p)
}

func (s *stubStreams) snapshot() (chunks []protocol.StreamChunkParams, ends []protocol.StreamEndParams) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]protocol.StreamChunkParams(nil), s.chunks...), append([]protocol.StreamEndParams(nil), s.ends...)
}

// waitFor polls cond until it holds or the deadline expires.
func waitFor(t *testing.T, what string, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// protocolReason extracts the frozen protocol error reason from err, whether
// it travels as a local *protocol.ProtocolError or as a wire-shaped
// *rpcwire.RPCError.
func protocolReason(t *testing.T, err error) protocol.ErrorReason {
	t.Helper()
	var protocolErr *protocol.ProtocolError
	if errors.As(err, &protocolErr) {
		return protocolErr.Reason
	}
	var rpcErr *rpcwire.RPCError
	if errors.As(err, &rpcErr) {
		var data protocol.ProtocolErrorData
		raw, _ := json.Marshal(rpcErr.Data)
		if json.Unmarshal(raw, &data) == nil && data.Reason != "" {
			return data.Reason
		}
	}
	t.Fatalf("error %v carries no protocol reason", err)
	return ""
}

// Tests: initialize handshake

// TestHandshakeAccepted proves the host accepts the example's full
// declaration: subscriptions, the system_prompt strategy slot, the namespaced
// provider, and the demo UI action.
func TestHandshakeAccepted(t *testing.T) {
	client := startExample(t, nil, nil)
	h := client.Handshake()
	if h.Name != testPluginID || h.Version != "1.0.0" {
		t.Fatalf("identity = %q/%q", h.Name, h.Version)
	}
	wantSubs := map[string]bool{"input.receive": true, "tool.before": true, "system_prompt.build": true, "session.start": true}
	if len(h.Subscriptions) != len(wantSubs) {
		t.Fatalf("subscriptions = %v", h.Subscriptions)
	}
	for _, sub := range h.Subscriptions {
		if !wantSubs[sub] {
			t.Fatalf("unexpected subscription %q in %v", sub, h.Subscriptions)
		}
	}
	if len(h.Replaces) != 1 || h.Replaces[0] != "system_prompt" {
		t.Fatalf("replaces = %v", h.Replaces)
	}
	if len(h.Providers) != 1 || h.Providers[0].Ref != testProvider {
		t.Fatalf("providers = %+v", h.Providers)
	}
	if len(h.UIActions) != 1 || h.UIActions[0].ActionID != "demo" {
		t.Fatalf("uiActions = %+v", h.UIActions)
	}
	if len(h.Provides) != 4 {
		t.Fatalf("provides = %+v", h.Provides)
	}
	wantProvides := map[string]string{
		"plugin/full-sidecar/interceptors/default":     "",
		"plugin/full-sidecar/strategies/system_prompt": "",
		"plugin/full-sidecar/provider/fake/echo":       fixtureProviderSchemaHash,
		"plugin/full-sidecar/uiaction/demo":            fixtureUIActionSchemaHash,
	}
	for _, provided := range h.Provides {
		key := provided.Namespace + "/" + provided.Kind + "/" + provided.ID
		if want, ok := wantProvides[key]; !ok || provided.SchemaHash != want {
			t.Fatalf("handshake provided capability %q has schemaHash %q", key, provided.SchemaHash)
		}
	}
}

// TestHandshakeUnderDeclaredRejected proves the manifest contract: an
// extension activating a capability its manifest did not declare is refused
// with capability_not_declared.
func TestHandshakeUnderDeclaredRejected(t *testing.T) {
	_, item := installExamplePackage(t)
	item.Package.Manifest.Runtime.Capabilities = []string{"ui"} // no "providers": the example still declares one
	_, err := sidecar.StartClient(context.Background(), sidecar.ClientOptions{
		Package:   item.Package,
		Installed: item.Installed,
		Session:   protocol.SessionContext{SessionID: "sess-conf", WorkspaceRoot: "/ws", Generation: 1},
	})
	if err == nil {
		t.Fatal("StartClient succeeded with an under-declared manifest")
	}
	if reason := protocolReason(t, err); reason != protocol.ErrCapabilityNotDeclared {
		t.Fatalf("reason = %q, want %q (err %v)", reason, protocol.ErrCapabilityNotDeclared, err)
	}
}

// Tests: intercepts and strategy

// TestInputReceiveRewrite drives the "/fs " trigger: the example replaces the
// input; ordinary input continues untouched.
func TestInputReceiveRewrite(t *testing.T) {
	client := startExample(t, nil, nil)

	result := intercept(t, client, protocol.EventInputReceive, `{"text":"/fs hello world"}`)
	var replaced struct {
		Text string `json:"text"`
	}
	decodeReplacement(t, result, &replaced)
	if replaced.Text != "hello world [rewritten by fullsidecar]" {
		t.Fatalf("rewritten text = %q", replaced.Text)
	}

	result = intercept(t, client, protocol.EventInputReceive, `{"text":"plain input"}`)
	if result.Decision != protocol.DecisionContinue {
		t.Fatalf("decision for plain input = %q, want continue", result.Decision)
	}
}

// TestToolBeforeBlockAndRewrite covers the tool interception: the denied tool
// is blocked, the rewritten tool's arguments gain the sandbox flag, and
// unrelated tools continue.
func TestToolBeforeBlockAndRewrite(t *testing.T) {
	client := startExample(t, nil, nil)

	blocked := intercept(t, client, protocol.EventToolBefore, `{"name":"dangerous_exec","arguments":"{}"}`)
	if blocked.Decision != protocol.DecisionBlock {
		t.Fatalf("decision = %q, want block", blocked.Decision)
	}
	if !strings.Contains(blocked.Reason, "dangerous_exec") {
		t.Fatalf("block reason = %q", blocked.Reason)
	}

	rewritten := intercept(t, client, protocol.EventToolBefore, `{"name":"read","arguments":"{\"path\":\"/etc/hosts\"}"}`)
	var replacement struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	}
	decodeReplacement(t, rewritten, &replacement)
	if replacement.Name != "read" {
		t.Fatalf("replacement name = %q", replacement.Name)
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(replacement.Arguments), &args); err != nil {
		t.Fatalf("rewritten arguments are not a JSON object: %v", err)
	}
	if args["sandbox"] != true || args["path"] != "/etc/hosts" {
		t.Fatalf("rewritten arguments = %v", args)
	}

	passthrough := intercept(t, client, protocol.EventToolBefore, `{"name":"write","arguments":"{}"}`)
	if passthrough.Decision != protocol.DecisionContinue {
		t.Fatalf("decision for unrelated tool = %q, want continue", passthrough.Decision)
	}
}

// TestSystemPromptStrategy proves the strategy-slot replacement lands: the
// example owns system_prompt.build and wraps the base prompt.
func TestSystemPromptStrategy(t *testing.T) {
	client := startExample(t, nil, nil)
	result := intercept(t, client, protocol.EventSystemPromptBuild, `{"prompt":"BASE PROMPT","workspaceRoot":"/ws"}`)
	var replacement struct {
		Prompt        string `json:"prompt"`
		WorkspaceRoot string `json:"workspaceRoot"`
	}
	decodeReplacement(t, result, &replacement)
	if !strings.Contains(replacement.Prompt, "fullsidecar demo strategy") || !strings.Contains(replacement.Prompt, "BASE PROMPT") {
		t.Fatalf("replacement prompt = %q", replacement.Prompt)
	}
	if replacement.WorkspaceRoot != "/ws" {
		t.Fatalf("workspaceRoot = %q", replacement.WorkspaceRoot)
	}
}

// Tests: provider broker

// TestProviderCatalog fetches the extension's provider catalog through the
// host client.
func TestProviderCatalog(t *testing.T) {
	client := startExample(t, nil, nil)
	providers, err := client.ProviderCatalog(context.Background())
	if err != nil {
		t.Fatalf("ProviderCatalog: %v", err)
	}
	if len(providers) != 1 {
		t.Fatalf("catalog = %+v", providers)
	}
	desc := providers[0]
	if desc.Ref != testProvider || desc.Model != "echo" || !desc.Tools || !desc.Reasoning {
		t.Fatalf("descriptor = %+v", desc)
	}
}

// TestProviderStream opens one stream and asserts the scripted completion
// arrives in order with contiguous seqs, a tool call, usage, and a clean end.
func TestProviderStream(t *testing.T) {
	streams := &stubStreams{}
	client := startExample(t, nil, func(o *sidecar.ClientOptions) { o.Streams = streams })

	opened, err := client.ProviderStreamOpen(context.Background(), protocol.StreamOpenParams{
		StreamID:    "s-full",
		ProviderRef: testProvider,
		Request:     protocol.ProviderRequest{Messages: []protocol.ProviderMessage{}, Tools: []protocol.ProviderToolSchema{}},
	})
	if err != nil {
		t.Fatalf("ProviderStreamOpen: %v", err)
	}
	if !opened.Accepted {
		t.Fatal("stream open was not accepted")
	}
	waitFor(t, "stream end", 10*time.Second, func() bool {
		_, ends := streams.snapshot()
		return len(ends) == 1
	})
	chunks, ends := streams.snapshot()
	if len(chunks) != 5 {
		t.Fatalf("received %d chunks, want 5: %+v", len(chunks), chunks)
	}
	for i, chunk := range chunks {
		if chunk.StreamID != "s-full" || chunk.Seq != int64(i+1) {
			t.Fatalf("chunk %d = stream %q seq %d, want s-full/%d", i, chunk.StreamID, chunk.Seq, i+1)
		}
	}
	if chunks[0].Chunk.Type != protocol.ChunkText || chunks[0].Chunk.Text != "fake-hello " {
		t.Fatalf("chunk 1 = %+v", chunks[0].Chunk)
	}
	if chunks[1].Chunk.Type != protocol.ChunkText || chunks[1].Chunk.Text != "fake-world" {
		t.Fatalf("chunk 2 = %+v", chunks[1].Chunk)
	}
	call := chunks[2].Chunk
	if call.Type != protocol.ChunkToolCall || call.ToolCall == nil || call.ToolCall.Name != "lookup" || call.ToolCall.ID != "call-1" {
		t.Fatalf("tool call chunk = %+v", call)
	}
	usage := chunks[3].Chunk
	if usage.Type != protocol.ChunkUsage || usage.Usage == nil || usage.Usage.TotalTokens != 12 || usage.Usage.PromptTokens != 5 {
		t.Fatalf("usage chunk = %+v", usage)
	}
	if chunks[4].Chunk.Type != protocol.ChunkDone {
		t.Fatalf("final chunk type = %q, want done", chunks[4].Chunk.Type)
	}
	end := ends[0]
	if end.StreamID != "s-full" || end.LastSeq != 5 || end.Error != "" || end.Interrupted {
		t.Fatalf("stream end = %+v", end)
	}
}

// TestProviderStreamCancel cancels mid-stream: the cancel is honored, the
// stream ends interrupted at the last delivered seq, and no chunk travels
// after the cancel.
func TestProviderStreamCancel(t *testing.T) {
	streams := &stubStreams{}
	client := startExample(t, func(rt *pluginpkg.RuntimeSpec) {
		rt.Env = map[string]string{"FULLSIDECAR_STREAM_INTERVAL_MS": "150"}
	}, func(o *sidecar.ClientOptions) { o.Streams = streams })

	if _, err := client.ProviderStreamOpen(context.Background(), protocol.StreamOpenParams{
		StreamID:    "s-cancel",
		ProviderRef: testProvider,
		Request:     protocol.ProviderRequest{Messages: []protocol.ProviderMessage{}, Tools: []protocol.ProviderToolSchema{}},
	}); err != nil {
		t.Fatalf("ProviderStreamOpen: %v", err)
	}
	waitFor(t, "first chunk", 10*time.Second, func() bool {
		chunks, _ := streams.snapshot()
		return len(chunks) >= 1
	})
	client.ProviderStreamCancel("s-cancel")
	waitFor(t, "stream end", 10*time.Second, func() bool {
		_, ends := streams.snapshot()
		return len(ends) == 1
	})
	chunks, ends := streams.snapshot()
	end := ends[0]
	if !end.Interrupted {
		t.Fatalf("stream end = %+v, want interrupted", end)
	}
	if end.LastSeq != 1 {
		t.Fatalf("end.lastSeq = %d, want 1", end.LastSeq)
	}
	for _, chunk := range chunks {
		if chunk.Seq > end.LastSeq {
			t.Fatalf("chunk seq %d traveled after the cancel (end %+v)", chunk.Seq, end)
		}
	}
}

// Tests: content refs

// TestContentRefRehydration sends an intercept payload above the 64 KiB
// externalization threshold: the host moves it into a content ref, and the
// SDK pages it back transparently — the extension must see (and rewrite) the
// full payload.
func TestContentRefRehydration(t *testing.T) {
	client := startExample(t, nil, nil)
	big := strings.Repeat("x", 100<<10)
	payload, err := json.Marshal(map[string]string{"text": "/fs " + big})
	if err != nil {
		t.Fatal(err)
	}
	if len(payload) <= protocol.ExternalizeFieldBytes {
		t.Fatalf("payload is %d bytes, want above the %d threshold", len(payload), protocol.ExternalizeFieldBytes)
	}
	result, err := client.Intercept(context.Background(), protocol.EventInputReceive, payload, 15*time.Second)
	if err != nil {
		t.Fatalf("Intercept: %v", err)
	}
	var replaced struct {
		Text string `json:"text"`
	}
	decodeReplacement(t, result, &replaced)
	if replaced.Text != big+" [rewritten by fullsidecar]" {
		t.Fatalf("rehydrated text is %d bytes, want %d (full payload reassembled)", len(replaced.Text), len(big)+len(" [rewritten by fullsidecar]"))
	}
}

// Tests: UI

// TestSessionStartPublishes drives one session.start observation: the example
// publishes its status line and demo card through host/ui/publish.
func TestSessionStartPublishes(t *testing.T) {
	ui := &stubUI{}
	client := startExample(t, nil, func(o *sidecar.ClientOptions) { o.UI = ui })

	if err := client.NotifyEvent(protocol.EventSessionStart, json.RawMessage(`{"sessionPath":"/s/1","phase":"start"}`)); err != nil {
		t.Fatalf("NotifyEvent: %v", err)
	}
	waitFor(t, "status and card publish", 10*time.Second, func() bool {
		return ui.publishedCount() >= 2
	})

	statuses := ui.publishedOfKind(protocol.UISurfaceStatus)
	if len(statuses) != 1 {
		t.Fatalf("status publishes = %+v", statuses)
	}
	var status protocol.UIStatusPayload
	if err := json.Unmarshal(statuses[0].Payload, &status); err != nil {
		t.Fatalf("status payload: %v", err)
	}
	if statuses[0].SurfaceID != "fullsidecar-status" || status.Label != "fullsidecar online" {
		t.Fatalf("status surface = %q %+v", statuses[0].SurfaceID, status)
	}

	cards := ui.publishedOfKind(protocol.UISurfaceCard)
	if len(cards) != 1 {
		t.Fatalf("card publishes = %+v", cards)
	}
	var card protocol.UICardPayload
	if err := json.Unmarshal(cards[0].Payload, &card); err != nil {
		t.Fatalf("card payload: %v", err)
	}
	if cards[0].SurfaceID != "fullsidecar-card" || len(card.Actions) != 1 || card.Actions[0].ActionID != "demo" {
		t.Fatalf("card surface = %q %+v", cards[0].SurfaceID, card)
	}
}

// TestUIActionRoundTrip invokes the demo action: the example issues a
// blocking form request (answered by the stub UI handler) and publishes the
// greeting notification built from the answers.
func TestUIActionRoundTrip(t *testing.T) {
	ui := &stubUI{}
	var requested protocol.UIRequestParams
	ui.requestFn = func(p protocol.UIRequestParams) (protocol.UIRequestResult, error) {
		requested = p
		return protocol.UIRequestResult{Values: map[string]any{"name": "Ada", "loud": true}}, nil
	}
	client := startExample(t, nil, func(o *sidecar.ClientOptions) { o.UI = ui })

	result, err := client.UIAction(context.Background(), protocol.UIActionParams{
		ActionID: "demo", SessionID: "sess-conf", Generation: 1,
	})
	if err != nil {
		t.Fatalf("UIAction: %v", err)
	}
	if !result.Accepted {
		t.Fatalf("action rejected: %+v", result)
	}
	if requested.SurfaceID != "fullsidecar-demo-form" || requested.SessionID != "sess-conf" || requested.Kind != protocol.UIRequestInput {
		t.Fatalf("ui request = %+v", requested)
	}
	notifications := ui.publishedOfKind(protocol.UISurfaceNotification)
	if len(notifications) != 1 {
		t.Fatalf("notification publishes = %+v", notifications)
	}
	var notice protocol.UINotificationPayload
	if err := json.Unmarshal(notifications[0].Payload, &notice); err != nil {
		t.Fatalf("notification payload: %v", err)
	}
	if notice.Title != "HELLO, ADA!" {
		t.Fatalf("greeting = %q", notice.Title)
	}
}

// TestUISubmitRoundTrip delivers a form submission; the example acknowledges
// it with a status update.
func TestUISubmitRoundTrip(t *testing.T) {
	ui := &stubUI{}
	client := startExample(t, nil, func(o *sidecar.ClientOptions) { o.UI = ui })

	result, err := client.UISubmit(context.Background(), protocol.UISubmitParams{
		SurfaceID: "fullsidecar-demo-form", SessionID: "sess-conf", Generation: 1,
		Values: map[string]any{"name": "Ada"},
	})
	if err != nil {
		t.Fatalf("UISubmit: %v", err)
	}
	if !result.Accepted {
		t.Fatalf("submit rejected: %+v", result)
	}
	waitFor(t, "submit status publish", 10*time.Second, func() bool {
		return len(ui.publishedOfKind(protocol.UISurfaceStatus)) == 1
	})
	statuses := ui.publishedOfKind(protocol.UISurfaceStatus)
	var status protocol.UIStatusPayload
	if err := json.Unmarshal(statuses[0].Payload, &status); err != nil {
		t.Fatalf("status payload: %v", err)
	}
	if !strings.Contains(status.Label, "fullsidecar-demo-form") {
		t.Fatalf("submit status label = %q", status.Label)
	}
}

// Tests: timeout and crash

// TestInterceptTimeout stalls the example past the intercept budget; the host
// must surface the frozen intercept_timeout reason.
func TestInterceptTimeout(t *testing.T) {
	client := startExample(t, func(rt *pluginpkg.RuntimeSpec) {
		rt.Env = map[string]string{"FULLSIDECAR_STALL_ON_INPUT": "stall-me"}
	}, nil)
	started := time.Now()
	_, err := client.Intercept(context.Background(), protocol.EventInputReceive, json.RawMessage(`{"text":"stall-me"}`), 500*time.Millisecond)
	if err == nil {
		t.Fatal("Intercept succeeded against a stalling extension")
	}
	if reason := protocolReason(t, err); reason != protocol.ErrInterceptTimeout {
		t.Fatalf("reason = %q, want %q (err %v)", reason, protocol.ErrInterceptTimeout, err)
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("timeout surfaced after %s, not bounded by the 500ms budget", elapsed)
	}
}

// TestCrashMidIntercept kills the extension process while an intercept is in
// flight: the pending call errors and every later call fails fast with the
// crashed-sidecar reason.
func TestCrashMidIntercept(t *testing.T) {
	client := startExample(t, func(rt *pluginpkg.RuntimeSpec) {
		rt.Env = map[string]string{"FULLSIDECAR_CRASH_ON_INPUT": "boom"}
	}, nil)

	_, err := client.Intercept(context.Background(), protocol.EventInputReceive, json.RawMessage(`{"text":"boom"}`), 10*time.Second)
	if err == nil {
		t.Fatal("Intercept succeeded though the extension exited mid-intercept")
	}
	waitFor(t, "crash detection", 10*time.Second, client.Crashed)

	started := time.Now()
	_, err = client.Intercept(context.Background(), protocol.EventInputReceive, json.RawMessage(`{"text":"ok"}`), 10*time.Second)
	if err == nil {
		t.Fatal("Intercept on a crashed sidecar succeeded")
	}
	if reason := protocolReason(t, err); reason != protocol.ErrProviderInterrupted {
		t.Fatalf("reason = %q, want %q (err %v)", reason, protocol.ErrProviderInterrupted, err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("call on a crashed sidecar took %s, not fail-fast", elapsed)
	}
}
