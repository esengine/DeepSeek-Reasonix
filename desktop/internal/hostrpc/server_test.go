package hostrpc

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"reasonix/internal/extension/rpcwire"
)

type harness struct {
	t        *testing.T
	server   *Server
	shell    *rpcwire.Conn
	events   chan Event
	serveErr chan error
	stdinW   *io.PipeWriter
	contract Contract
	home     string
}

func newHarness(t *testing.T, target any, hooks Hooks) *harness {
	t.Helper()
	registry := mustRegistry(t, target, nil)
	contract := Build(registry, []string{"agent:event", "runtime:rebuilt"})
	home := t.TempDir()
	stdinR, stdinW := io.Pipe()
	stdoutR, stdoutW := io.Pipe()
	serviceConn := rpcwire.NewConn(stdinR, stdoutW, rpcwire.Options{StrictJSONRPC: true, Name: "desktop-host", MaxConcurrentHandlers: 512})
	shell := rpcwire.NewConn(stdoutR, stdinW, rpcwire.Options{StrictJSONRPC: true, Name: "shell", MaxQueuedNotifications: 64})
	events := make(chan Event, 64)
	shell.HandleNotify("desktop/event", func(_ context.Context, params json.RawMessage) {
		var e Event
		if err := json.Unmarshal(params, &e); err != nil {
			t.Errorf("decode event: %v", err)
			return
		}
		events <- e
	})
	server := NewServer(serviceConn, ServerConfig{
		Registry:   registry,
		Contract:   contract,
		Hooks:      hooks,
		Identity:   Identity{Version: "v1.0.0", Channel: "stable", Commit: "abc123", Home: home},
		Generation: "g-test",
	})
	serveErr := make(chan error, 1)
	ctx := t.Context()
	go func() { serveErr <- server.Serve(ctx) }()
	go func() { _ = shell.Serve(ctx) }()
	t.Cleanup(func() {
		stdinW.Close()
		stdoutW.Close()
		stdinR.Close()
		stdoutR.Close()
	})
	return &harness{t: t, server: server, shell: shell, events: events, serveErr: serveErr, stdinW: stdinW, contract: contract, home: home}
}

func (h *harness) hello() HelloParams {
	return HelloParams{
		ProtocolVersion: ProtocolVersion,
		ContractDigest:  h.contract.Digest(),
		Build:           BuildInfo{Version: "v1.0.0", Channel: "stable", Commit: "abc123"},
		Host:            HostInfo{Name: "electron", Platform: "darwin"},
		Instance:        HelloInstance{Home: h.home},
	}
}

func (h *harness) call(method string, params any, result any) error {
	h.t.Helper()
	raw, err := h.shell.Request(h.t.Context(), method, params)
	if err != nil {
		return err
	}
	if result != nil {
		if err := json.Unmarshal(raw, result); err != nil {
			h.t.Fatalf("%s: decode result %s: %v", method, raw, err)
		}
	}
	return nil
}

func (h *harness) invoke(method string, args ...any) (json.RawMessage, error) {
	h.t.Helper()
	if args == nil {
		args = []any{}
	}
	return h.shell.Request(h.t.Context(), "desktop/invoke", map[string]any{"method": method, "args": args})
}

func (h *harness) mustHello() HelloResult {
	h.t.Helper()
	var result HelloResult
	if err := h.call("desktop/hello", h.hello(), &result); err != nil {
		h.t.Fatalf("hello: %v", err)
	}
	return result
}

func assertCode(t *testing.T, err error, code int, name string) map[string]any {
	t.Helper()
	var re *rpcwire.ResponseError
	if !errors.As(err, &re) {
		t.Fatalf("error = %v, want JSON-RPC error %d", err, code)
	}
	if re.Code != code {
		t.Fatalf("code = %d (%s), want %d", re.Code, re.Message, code)
	}
	data := map[string]any{}
	if len(re.Data) > 0 {
		if err := json.Unmarshal(re.Data, &data); err != nil {
			t.Fatalf("decode error data %s: %v", re.Data, err)
		}
	}
	if name != "" && data["name"] != name {
		t.Fatalf("error data name = %v, want %s", data["name"], name)
	}
	return data
}

func TestServerRejectsEverythingBeforeHello(t *testing.T) {
	h := newHarness(t, &fixtureTarget{}, Hooks{})
	_, err := h.invoke("Platform")
	assertCode(t, err, CodeNotReady, "not_ready")
	assertCode(t, h.call("desktop/start", struct{}{}, nil), CodeNotReady, "not_ready")
	assertCode(t, h.call("desktop/shutdown", struct{}{}, nil), CodeNotReady, "not_ready")
}

func TestServerHelloMismatchCodes(t *testing.T) {
	h := newHarness(t, &fixtureTarget{}, Hooks{})
	protocol := h.hello()
	protocol.ProtocolVersion = 2
	assertCode(t, h.call("desktop/hello", protocol, nil), CodeProtocolMismatch, "protocol_mismatch")

	contract := h.hello()
	contract.ContractDigest = "sha256:0000"
	data := assertCode(t, h.call("desktop/hello", contract, nil), CodeContractMismatch, "contract_mismatch")
	if data["expected"] != h.contract.Digest() {
		t.Fatalf("contract mismatch data = %v", data)
	}

	build := h.hello()
	build.Build.Version = "v2.0.0"
	assertCode(t, h.call("desktop/hello", build, nil), CodeBuildMismatch, "build_mismatch")

	instance := h.hello()
	instance.Instance.Home = t.TempDir()
	assertCode(t, h.call("desktop/hello", instance, nil), CodeInstanceMismatch, "instance_mismatch")

	_, err := h.invoke("Platform")
	assertCode(t, err, CodeNotReady, "not_ready")

	devBuild := h.hello()
	devBuild.Build.Version = "v2.0.0"
	devBuild.Instance.Dev = true
	if err := h.call("desktop/hello", devBuild, nil); err != nil {
		t.Fatalf("dev shell must skip the build check: %v", err)
	}
	assertCode(t, h.call("desktop/hello", h.hello(), nil), rpcwire.ErrInvalidRequest, "")
}

func TestServerHelloResultAndInvoke(t *testing.T) {
	hooks := Hooks{Hello: func(p HelloParams) (HelloResult, error) {
		if p.Host.Name != "electron" {
			t.Errorf("hook saw host %+v", p.Host)
		}
		return HelloResult{
			Resources: Resources{Origin: "http://127.0.0.1:1", Token: "tok"},
			Window:    &WindowGeometry{Width: 1240, Height: 720, MinWidth: 760, MinHeight: 480, ZoomFactor: 1},
		}, nil
	}}
	h := newHarness(t, &fixtureTarget{}, hooks)
	result := h.mustHello()
	if result.ProtocolVersion != 1 || result.ContractDigest != h.contract.Digest() || result.RuntimeGeneration != "g-test" {
		t.Fatalf("hello result = %+v", result)
	}
	if result.Service.Version != "v1.0.0" || result.Service.Channel != "stable" || result.Service.Commit != "abc123" || result.Service.PID <= 0 {
		t.Fatalf("service info = %+v", result.Service)
	}
	if result.Resources.Token != "tok" || result.Window == nil || result.Window.Width != 1240 {
		t.Fatalf("hook fields lost: %+v", result)
	}

	platform, err := h.invoke("Platform")
	if err != nil || string(platform) != `"test-os"` {
		t.Fatalf("Platform = %s, %v", platform, err)
	}
	void, err := h.invoke("Void")
	if err != nil || string(void) != "null" {
		t.Fatalf("Void = %s, %v", void, err)
	}
	ping, err := h.invoke("Ping", "alpha", 2)
	if err != nil || string(ping) != `{"id":"alpha","next":null}` {
		t.Fatalf("Ping = %s, %v", ping, err)
	}
	_, err = h.invoke("Fail")
	data := assertCode(t, err, CodeBusiness, "")
	if data["method"] != "Fail" || err.Error() != "boom" {
		t.Fatalf("business error = %v data %v", err, data)
	}
	_, err = h.invoke("Nope")
	data = assertCode(t, err, rpcwire.ErrMethodNotFound, "")
	if data["method"] != "Nope" {
		t.Fatalf("unknown method data = %v", data)
	}
	_, err = h.invoke("Ping", 1)
	assertCode(t, err, rpcwire.ErrInvalidParams, "")
	_, err = h.invoke("Explode")
	assertCode(t, err, rpcwire.ErrInternal, "")
}

func TestServerEventsKeepCallOrderAndSequence(t *testing.T) {
	h := newHarness(t, &fixtureTarget{}, Hooks{})
	h.mustHello()
	h.server.Emit("agent:event", map[string]any{"kind": "text"})
	h.server.Emit("runtime:rebuilt", "tab-1", 3)
	h.server.Emit("agent:ready")
	want := []struct {
		name string
		args string
	}{
		{"agent:event", `[{"kind":"text"}]`},
		{"runtime:rebuilt", `["tab-1",3]`},
		{"agent:ready", `[]`},
	}
	for i, w := range want {
		e := <-h.events
		args, _ := json.Marshal(e.Args)
		if e.Seq != int64(i+1) || e.Generation != "g-test" || e.Name != w.name || string(args) != w.args {
			t.Fatalf("event %d = %+v (args %s), want seq %d %s %s", i, e, args, i+1, w.name, w.args)
		}
	}
}

func TestServerRequestRoundTripsHostCalls(t *testing.T) {
	h := newHarness(t, &fixtureTarget{}, Hooks{})
	h.shell.Handle("host/window.isMaximised", func(_ context.Context, _ json.RawMessage) (any, error) {
		return map[string]bool{"value": true}, nil
	})
	h.shell.Handle("host/dialog.openDirectory", func(_ context.Context, params json.RawMessage) (any, error) {
		return nil, &rpcwire.RPCError{Code: -1, Message: "cancelled: " + string(params)}
	})
	var out struct {
		Value bool `json:"value"`
	}
	if err := h.server.Request(t.Context(), "host/window.isMaximised", struct{}{}, &out); err != nil || !out.Value {
		t.Fatalf("isMaximised = %+v, %v", out, err)
	}
	if err := h.server.Request(t.Context(), "host/window.show", map[string]string{"reason": "domReady"}, nil); err == nil {
		t.Fatal("unhandled host method must surface an error")
	}
	err := h.server.Request(t.Context(), "host/dialog.openDirectory", map[string]string{"title": "Pick"}, nil)
	var re *rpcwire.ResponseError
	if !errors.As(err, &re) || re.Code != -1 || re.Message != `cancelled: {"title":"Pick"}` {
		t.Fatalf("host error = %v", err)
	}
}

func TestServerRoutesLifecycleRequestsToHooks(t *testing.T) {
	var log []string
	hooks := Hooks{
		Start:    func(context.Context) error { log = append(log, "start"); return nil },
		DOMReady: func(context.Context) error { log = append(log, "domReady"); return nil },
		RendererAttached: func(_ context.Context, gen int) error {
			log = append(log, "renderer:"+string(rune('0'+gen)))
			return nil
		},
		BeforeClose: func(_ context.Context, reason string) bool {
			log = append(log, "beforeClose:"+reason)
			return reason == "window"
		},
		Shutdown: func(context.Context) error { log = append(log, "shutdown"); return nil },
		HostEvent: func(_ context.Context, name string, payload json.RawMessage) error {
			log = append(log, "host:"+name+":"+string(payload))
			return errors.New("unhandled host event")
		},
	}
	h := newHarness(t, &fixtureTarget{}, hooks)
	h.mustHello()
	var empty map[string]any
	for _, method := range []string{"desktop/start", "desktop/domReady"} {
		if err := h.call(method, struct{}{}, &empty); err != nil || len(empty) != 0 {
			t.Fatalf("%s = %v, %v", method, empty, err)
		}
	}
	if err := h.call("desktop/rendererAttached", map[string]int{"rendererGeneration": 7}, nil); err != nil {
		t.Fatal(err)
	}
	var close struct {
		Prevent bool `json:"prevent"`
	}
	if err := h.call("desktop/beforeClose", map[string]string{"reason": "window"}, &close); err != nil || !close.Prevent {
		t.Fatalf("beforeClose window = %+v, %v", close, err)
	}
	if err := h.call("desktop/beforeClose", map[string]string{"reason": "quit"}, &close); err != nil || close.Prevent {
		t.Fatalf("beforeClose quit = %+v, %v", close, err)
	}
	err := h.call("desktop/hostEvent", map[string]any{"name": "tray.open", "payload": []string{"x"}}, nil)
	assertCode(t, err, rpcwire.ErrInternal, "")
	if err := h.call("desktop/shutdown", struct{}{}, &empty); err != nil {
		t.Fatal(err)
	}
	if err := <-h.serveErr; err != nil {
		t.Fatalf("Serve after shutdown = %v", err)
	}
	want := "start,domReady,renderer:7,beforeClose:window,beforeClose:quit,host:tray.open:[\"x\"],shutdown"
	if got := strings.Join(log, ","); got != want {
		t.Fatalf("hook log = %s, want %s", got, want)
	}
}

func TestServerStopsWhenStdinCloses(t *testing.T) {
	h := newHarness(t, &fixtureTarget{}, Hooks{})
	h.mustHello()
	h.stdinW.Close()
	if err := <-h.serveErr; err != nil {
		t.Fatalf("Serve after EOF = %v", err)
	}
}
