package sidecar

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/ext/extension/protocol"
	"reasonix/internal/ext/pluginpkg"
)

func TestSDKNilToolsDoNotExceedManifest(t *testing.T) {
	sdk, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "sdk", "go"))
	if err != nil {
		t.Fatal(err)
	}
	source := testenv.TempDir(t)
	main := filepath.Join(source, "main.go")
	if err := os.WriteFile(main, []byte(`package main

import (
	"context"
	"encoding/json"
	"os"

	extension "github.com/esengine/DeepSeek-Reasonix/sdk/go"
)

type handler struct{}

func (handler) Initialize(context.Context, extension.InitializeParams) (*extension.InitializeResult, error) {
	return &extension.InitializeResult{}, nil
}

func main() {
	err := extension.Serve(context.Background(), handler{}, extension.Options{
		Name: "nil-tools", Version: "0.1.0",
		Tools: map[string]extension.ToolFunc{
			"lookup": func(_ context.Context, args json.RawMessage) (string, error) { return string(args), nil },
			"disabled": nil,
		},
	})
	if err != nil { os.Exit(1) }
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(source, "nil-tools.exe")
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "build", "-o", binary, main)
	cmd.Dir = sdk
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOTOOLCHAIN=local", "GOPROXY=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build SDK tool fixture: %v\n%s", err, out)
	}
	if err := os.WriteFile(filepath.Join(source, pluginpkg.NativeManifest), []byte(`{
	"apiVersion": "reasonix.io/plugin/v2",
	"name": "nil-tools",
	"version": "0.1.0",
	"contributes": {},
	"runtime": {
		"command": "${REASONIX_PLUGIN_ROOT}/nil-tools.exe",
		"capabilities": ["tools"],
		"tools": [{"name": "lookup", "description": "Echo arguments", "inputSchema": {"type": "object"}}]
	}
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	pkg, _, err := pluginpkg.ParseDir(source)
	if err != nil {
		t.Fatal(err)
	}
	installed := pluginpkg.InstalledPlugin{Name: "nil-tools", Version: "0.1.0", Enabled: true, Root: source}
	client, err := StartClient(t.Context(), ClientOptions{Package: pkg, Installed: installed, Session: testSessionContext()})
	if err != nil {
		t.Fatalf("nil callback prevented the SDK's served tool from starting: %v", err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			t.Error(err)
		}
		waitFor(t, "SDK tool fixture process exit", 10*time.Second, client.Exited)
	}()
	if tools := client.Handshake().Tools; len(tools) != 1 || tools[0] != "lookup" || client.ServesTool("disabled") {
		t.Fatalf("served tools = %v, want only lookup", tools)
	}
	result, err := client.CallTool(t.Context(), "lookup", json.RawMessage(`{"q":"hello"}`), 5*time.Second)
	if err != nil || result.IsError || result.Content != `{"q":"hello"}` {
		t.Fatalf("served SDK tool = %+v, %v", result, err)
	}
	_, err = client.CallTool(t.Context(), "disabled", nil, 5*time.Second)
	if err == nil || protocolReason(t, err) != protocol.ErrUnknownMethod {
		t.Fatalf("nil SDK tool callback = %v, want unknown_method", err)
	}
}
