package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestSDKListsConsumeEveryPageOnOneSession(t *testing.T) {
	var connections atomic.Int32
	transport := newInMemorySDKTransport(t, func() *mcpsdk.Server {
		connections.Add(1)
		server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "paged", Version: "1"}, &mcpsdk.ServerOptions{PageSize: 1})
		for _, name := range []string{"zeta", "alpha", "middle"} {
			server.AddTool(&mcpsdk.Tool{Name: name, InputSchema: map[string]any{"type": "object"}}, nil)
			server.AddPrompt(&mcpsdk.Prompt{Name: "prompt_" + name}, nil)
			server.AddResource(&mcpsdk.Resource{URI: "test://" + name, Name: name}, nil)
		}
		return server
	})

	assertListSize := func(method, key string, want int) {
		t.Helper()
		result, err := transport.call(t.Context(), method, map[string]any{})
		if err != nil {
			t.Fatalf("%s: %v", method, err)
		}
		var payload map[string]json.RawMessage
		if err := json.Unmarshal(result, &payload); err != nil {
			t.Fatal(err)
		}
		var values []json.RawMessage
		if err := json.Unmarshal(payload[key], &values); err != nil {
			t.Fatal(err)
		}
		if len(values) != want {
			t.Fatalf("%s returned %d items, want %d", method, len(values), want)
		}
	}
	assertListSize("tools/list", "tools", 3)
	assertListSize("prompts/list", "prompts", 3)
	assertListSize("resources/list", "resources", 3)
	if got := connections.Load(); got != 1 {
		t.Fatalf("tool/prompt/resource lists opened %d sessions, want one", got)
	}
}

func TestSDKPromptAndResourceListChangesRefreshSharedSession(t *testing.T) {
	var connections atomic.Int32
	server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "surfaces", Version: "1"}, nil)
	server.AddPrompt(&mcpsdk.Prompt{Name: "prompt-one"}, nil)
	server.AddResource(&mcpsdk.Resource{URI: "test://resource-one", Name: "resource-one"}, nil)
	transport := newInMemorySDKTransport(t, func() *mcpsdk.Server {
		connections.Add(1)
		return server
	})
	refreshCtx, cancelRefresh := context.WithCancel(t.Context())
	client := &Client{
		name: "surfaces", spec: Spec{Name: "surfaces", Type: "http"}, transport: "http", t: transport,
		refresh: toolListRefreshState{ctx: refreshCtx, cancel: cancelRefresh},
	}
	if err := client.initialize(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !client.capabilities.promptsListChanged || !client.capabilities.resourcesListChanged {
		t.Fatalf("list-changed capabilities = prompts:%v resources:%v", client.capabilities.promptsListChanged, client.capabilities.resourcesListChanged)
	}
	host := NewHost()
	host.bindToolListChanges(client)
	if _, err := host.registerStartedClient(client, nil); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(host.Close)
	host.StartPhaseB(t.Context(), nil)

	waitSurfaceCounts := func(wantPrompts, wantResources int) {
		t.Helper()
		deadline := time.Now().Add(2 * time.Second)
		for {
			host.mu.RLock()
			gotPrompts, gotResources := len(host.prompts), len(host.resources)
			host.mu.RUnlock()
			if gotPrompts == wantPrompts && gotResources == wantResources {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("surface counts = %d/%d, want %d/%d", gotPrompts, gotResources, wantPrompts, wantResources)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	waitSurfaceCounts(1, 1)

	server.AddPrompt(&mcpsdk.Prompt{Name: "prompt-two"}, nil)
	server.AddResource(&mcpsdk.Resource{URI: "test://resource-two", Name: "resource-two"}, nil)
	waitSurfaceCounts(2, 2)
	if got := connections.Load(); got != 1 {
		t.Fatalf("prompt/resource refresh opened %d sessions, want one shared session", got)
	}
}

func TestSDKToolConversionPreservesProviderCatalogFingerprint(t *testing.T) {
	const wireFixture = `{"tools":[
		{"name":"zeta","description":"Z","inputSchema":{"required":["b","a"],"properties":{"b":{"type":"number"},"a":{"type":"string"}},"type":"object"},"outputSchema":{"type":"object","properties":{"ok":{"type":"boolean"}}},"annotations":{"readOnlyHint":true,"destructiveHint":false}},
		{"name":"alpha","description":"A","inputSchema":{"type":"object","properties":{}}}
	]}`
	wireClient := &Client{name: "fixture", t: &countingToolsTransport{raw: json.RawMessage(wireFixture)}}
	wireCatalog, err := wireClient.fetchToolCatalog(t.Context(), false)
	if err != nil {
		t.Fatal(err)
	}

	transport := newInMemorySDKTransport(t, func() *mcpsdk.Server {
		server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "fixture", Version: "1"}, nil)
		falseValue := false
		server.AddTool(&mcpsdk.Tool{
			Name: "zeta", Description: "Z",
			InputSchema: map[string]any{
				"required": []any{"b", "a"}, "properties": map[string]any{
					"b": map[string]any{"type": "number"}, "a": map[string]any{"type": "string"},
				}, "type": "object",
			},
			OutputSchema: map[string]any{"type": "object", "properties": map[string]any{"ok": map[string]any{"type": "boolean"}}},
			Annotations:  &mcpsdk.ToolAnnotations{ReadOnlyHint: true, DestructiveHint: &falseValue},
		}, nil)
		server.AddTool(&mcpsdk.Tool{Name: "alpha", Description: "A", InputSchema: map[string]any{"type": "object", "properties": map[string]any{}}}, nil)
		return server
	})
	sdkClient := &Client{name: "fixture", t: transport}
	sdkCatalog, err := sdkClient.fetchToolCatalog(t.Context(), false)
	if err != nil {
		t.Fatal(err)
	}

	if wireCatalog.fingerprint != sdkCatalog.fingerprint {
		t.Fatalf("catalog fingerprint changed across SDK conversion:\nwire=%x\nsdk =%x", wireCatalog.fingerprint, sdkCatalog.fingerprint)
	}
	if !reflect.DeepEqual(wireCatalog.infos, sdkCatalog.infos) {
		t.Fatalf("tool info changed:\nwire=%+v\nsdk =%+v", wireCatalog.infos, sdkCatalog.infos)
	}
	if got, want := toolCatalogBytes(sdkCatalog), toolCatalogBytes(wireCatalog); !reflect.DeepEqual(got, want) {
		t.Fatalf("provider-visible tool catalog changed:\nwire=%s\nsdk =%s", want, got)
	}
}

func toolCatalogBytes(catalog toolCatalogSnapshot) [][]byte {
	out := make([][]byte, 0, len(catalog.adapters))
	for _, adapter := range catalog.adapters {
		out = append(out, []byte(adapter.Name()+"\x00"+adapter.Description()+"\x00"+string(adapter.Schema())))
	}
	return out
}

func TestSDKSessionWaiterCancellationDoesNotCancelSharedBuild(t *testing.T) {
	lifeCtx, cancelLife := context.WithCancel(context.Background())
	transport := &sdkSessionTransport{
		name: "waiter", spec: Spec{Name: "waiter", Type: "http"}, lifeCtx: lifeCtx, cancel: cancelLife,
		state: SessionStateConnecting,
	}
	release := make(chan struct{})
	transport.endpointFactory = func(ctx context.Context) (sdkEndpoint, error) {
		<-release
		clientSide, serverSide := mcpsdk.NewInMemoryTransports()
		server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "waiter", Version: "1"}, nil)
		go func() { _ = server.Run(ctx, serverSide) }()
		return sdkEndpoint{transport: clientSide}, nil
	}
	t.Cleanup(transport.close)

	waitCtx, cancelWait := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		_, err := transport.acquire(waitCtx)
		done <- err
	}()
	cancelWait()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled waiter error = %v, want context.Canceled", err)
	}
	close(release)
	if _, err := transport.acquire(t.Context()); err != nil {
		t.Fatalf("shared build was cancelled with its first waiter: %v", err)
	}
}

func TestSDKSessionDiagnosticsRedactSessionAndConfiguredValues(t *testing.T) {
	const (
		sessionID   = "session-secret-123"
		projectPath = "/workspace/private-project"
		headerToken = "header-secret-456"
		envToken    = "environment-secret-789"
	)
	transport := &sdkSessionTransport{spec: Spec{
		WorkspaceRoot: projectPath,
		Headers:       map[string]string{"IJ_MCP_SERVER_PROJECT_PATH": projectPath, "Authorization": headerToken},
		Env:           map[string]string{"MCP_TOKEN": envToken},
	}}
	message := transport.safeErrorText(errors.New(
		"request failed (session ID: "+sessionID+"): project="+projectPath+" auth="+headerToken+" env="+envToken,
	), sessionID)
	for _, secret := range []string{sessionID, projectPath, headerToken, envToken} {
		if strings.Contains(message, secret) {
			t.Fatalf("diagnostic leaked %q: %s", secret, message)
		}
	}
}
