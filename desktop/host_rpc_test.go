package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	goruntime "runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/desktop/internal/hostrpc"
	"reasonix/internal/config"
	"reasonix/internal/extension/rpcwire"
)

// hostRPCShell runs runHostRPC over pipes and returns the fake shell, the
// shell's end of the service stdin, and a waiter for the exit code.
func hostRPCShell(t *testing.T) (*rpcwire.Conn, *io.PipeWriter, func() (int, bool)) {
	t.Helper()
	previous := runtimeEventsEmitFallback
	stdinR, stdinW := io.Pipe()
	stdoutR, stdoutW := io.Pipe()
	exit := make(chan int, 1)
	go func() { exit <- runHostRPC(NewApp(), stdinR, stdoutW) }()
	shell := rpcwire.NewConn(stdoutR, stdinW, rpcwire.Options{StrictJSONRPC: true, Name: "shell"})
	go func() { _ = shell.Serve(t.Context()) }()
	var once sync.Once
	code, returned := 0, false
	wait := func() (int, bool) {
		once.Do(func() {
			select {
			case code = <-exit:
				returned = true
			case <-time.After(10 * time.Second):
			}
		})
		return code, returned
	}
	t.Cleanup(func() {
		stdinW.Close()
		stdoutW.Close()
		if _, ok := wait(); !ok {
			t.Error("runHostRPC did not return after the pipes closed")
		}
		runtimeEventsEmitFallback = previous
	})
	return shell, stdinW, wait
}

func hostRPCHello(t *testing.T) hostrpc.HelloParams {
	t.Helper()
	registry, err := newDesktopRegistry((*App)(nil))
	if err != nil {
		t.Fatal(err)
	}
	return hostrpc.HelloParams{
		ProtocolVersion: hostrpc.ProtocolVersion,
		ContractDigest:  hostrpc.Build(registry, hostEventNames).Digest(),
		Build:           hostrpc.BuildInfo{Version: version, Channel: channel},
		Host:            hostrpc.HostInfo{Name: "electron", Platform: goruntime.GOOS},
		Instance:        hostrpc.HelloInstance{Home: config.ReasonixHomeDir()},
	}
}

func invokeThroughShell(t *testing.T, shell *rpcwire.Conn, method string, args ...any) (json.RawMessage, error) {
	t.Helper()
	if args == nil {
		args = []any{}
	}
	return shell.Request(t.Context(), "desktop/invoke", map[string]any{"method": method, "args": args})
}

func TestHostRPCHelloThenInvoke(t *testing.T) {
	shell, _, _ := hostRPCShell(t)
	ctx := t.Context()

	_, err := invokeThroughShell(t, shell, "Platform")
	var re *rpcwire.ResponseError
	if !errors.As(err, &re) || re.Code != hostrpc.CodeNotReady {
		t.Fatalf("invoke before hello = %v, want code %d", err, hostrpc.CodeNotReady)
	}

	raw, err := shell.Request(ctx, "desktop/hello", hostRPCHello(t))
	if err != nil {
		t.Fatalf("hello: %v", err)
	}
	var hello hostrpc.HelloResult
	if err := json.Unmarshal(raw, &hello); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hello.RuntimeGeneration, "g-") || len(hello.RuntimeGeneration) != len("g-")+16 {
		t.Fatalf("runtimeGeneration = %q", hello.RuntimeGeneration)
	}
	if !strings.HasPrefix(hello.Resources.Origin, "http://127.0.0.1:") || len(hello.Resources.Token) != 64 {
		t.Fatalf("resources = %+v", hello.Resources)
	}
	if hello.Window == nil || hello.Window.MinWidth != desktopWindowMinWidth || hello.Window.Width <= 0 || hello.Window.ZoomFactor <= 0 {
		t.Fatalf("window = %+v", hello.Window)
	}
	if hello.Service.Version != version || hello.Service.Channel != channel || hello.Service.PID <= 0 {
		t.Fatalf("service = %+v", hello.Service)
	}

	platform, err := invokeThroughShell(t, shell, "Platform")
	if want, _ := json.Marshal(goruntime.GOOS); err != nil || string(platform) != string(want) {
		t.Fatalf("Platform = %s, %v", platform, err)
	}
	ver, err := invokeThroughShell(t, shell, "Version")
	if want, _ := json.Marshal(version); err != nil || string(ver) != string(want) {
		t.Fatalf("Version = %s, %v", ver, err)
	}
	_, err = invokeThroughShell(t, shell, "NoSuchMethod")
	if !errors.As(err, &re) || re.Code != rpcwire.ErrMethodNotFound {
		t.Fatalf("unknown method = %v", err)
	}

	assertResourceStatus(t, hello.Resources.Origin+"/nope", "", http.StatusUnauthorized)
	assertResourceStatus(t, hello.Resources.Origin+"/nope", hello.Resources.Token, http.StatusNotFound)
}

func TestHostRPCReturnsWhenStdinCloses(t *testing.T) {
	shell, stdinW, wait := hostRPCShell(t)
	if _, err := shell.Request(t.Context(), "desktop/hello", hostRPCHello(t)); err != nil {
		t.Fatalf("hello: %v", err)
	}
	stdinW.Close()
	code, returned := wait()
	if !returned {
		t.Fatal("runHostRPC did not return after stdin closed")
	}
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
}

func assertResourceStatus(t *testing.T, url, token string, want int) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != want {
		t.Fatalf("GET %s (token=%v) = %d, want %d", url, token != "", resp.StatusCode, want)
	}
}
