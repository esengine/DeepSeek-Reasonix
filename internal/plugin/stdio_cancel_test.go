package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"reasonix/internal/tool"
)

func newInMemorySDKTransport(t *testing.T, serverFactory func() *mcpsdk.Server) *sdkSessionTransport {
	t.Helper()
	lifeCtx, cancel := context.WithCancel(context.Background())
	transport := &sdkSessionTransport{
		name:            "test",
		spec:            Spec{Name: "test", Type: "http", StartupTimeout: 2 * time.Second},
		lifeCtx:         lifeCtx,
		cancel:          cancel,
		state:           SessionStateConnecting,
		reconnectDelays: []time.Duration{time.Millisecond},
	}
	transport.endpointFactory = func(ctx context.Context) (sdkEndpoint, error) {
		clientSide, serverSide := mcpsdk.NewInMemoryTransports()
		server := serverFactory()
		go func() { _ = server.Run(ctx, serverSide) }()
		return sdkEndpoint{transport: clientSide}, nil
	}
	t.Cleanup(transport.close)
	return transport
}

func TestSDKIOCallReturnsOnContextCancelAndNotifiesServer(t *testing.T) {
	started := make(chan struct{})
	cancelled := make(chan struct{})
	transport := newInMemorySDKTransport(t, func() *mcpsdk.Server {
		server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "hung", Version: "1"}, nil)
		mcpsdk.AddTool(server, &mcpsdk.Tool{Name: "wait"}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, _ map[string]any) (*mcpsdk.CallToolResult, any, error) {
			close(started)
			<-ctx.Done()
			close(cancelled)
			return nil, nil, ctx.Err()
		})
		return server
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := transport.call(ctx, "tools/call", map[string]any{"name": "wait", "arguments": map[string]any{}})
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("server did not receive tools/call")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled call error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("SDK call did not return after context cancellation")
	}
	select {
	case <-cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("server did not receive notifications/cancelled")
	}
}

func TestSDKSessionRoutesConcurrentResponsesByRequestID(t *testing.T) {
	slowStarted := make(chan struct{})
	releaseSlow := make(chan struct{})
	transport := newInMemorySDKTransport(t, func() *mcpsdk.Server {
		server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "parallel", Version: "1"}, nil)
		mcpsdk.AddTool(server, &mcpsdk.Tool{Name: "work"}, func(_ context.Context, _ *mcpsdk.CallToolRequest, input map[string]any) (*mcpsdk.CallToolResult, any, error) {
			label, _ := input["label"].(string)
			if label == "slow" {
				close(slowStarted)
				<-releaseSlow
			}
			return &mcpsdk.CallToolResult{Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: label}}}, nil, nil
		})
		return server
	})

	call := func(label string) (json.RawMessage, error) {
		return transport.call(t.Context(), "tools/call", map[string]any{"name": "work", "arguments": map[string]any{"label": label}})
	}
	slowDone := make(chan json.RawMessage, 1)
	go func() {
		result, _ := call("slow")
		slowDone <- result
	}()
	<-slowStarted
	fastDone := make(chan json.RawMessage, 1)
	go func() {
		result, _ := call("fast")
		fastDone <- result
	}()
	select {
	case result := <-fastDone:
		if !json.Valid(result) || !containsJSONText(result, "fast") {
			t.Fatalf("fast result = %s", result)
		}
	case <-time.After(time.Second):
		t.Fatal("fast request was serialized behind slow request")
	}
	close(releaseSlow)
	select {
	case result := <-slowDone:
		if !containsJSONText(result, "slow") {
			t.Fatalf("slow result = %s", result)
		}
	case <-time.After(time.Second):
		t.Fatal("slow request did not finish")
	}
}

func TestSDKSessionRoutesProgressNotification(t *testing.T) {
	transport := newInMemorySDKTransport(t, func() *mcpsdk.Server {
		server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "progress", Version: "1"}, nil)
		mcpsdk.AddTool(server, &mcpsdk.Tool{Name: "index"}, func(ctx context.Context, req *mcpsdk.CallToolRequest, _ map[string]any) (*mcpsdk.CallToolResult, any, error) {
			if token := req.Params.GetProgressToken(); token != nil {
				_ = req.Session.NotifyProgress(ctx, &mcpsdk.ProgressNotificationParams{
					ProgressToken: token, Progress: 2, Total: 5, Message: "Indexing",
				})
			}
			return &mcpsdk.CallToolResult{}, nil, nil
		})
		return server
	})
	client := &Client{name: "progress", t: transport}
	progress := make(chan string, 1)
	ctx := tool.WithProgress(t.Context(), func(chunk string) { progress <- chunk })
	if _, err := client.call(ctx, "tools/call", map[string]any{"name": "index", "arguments": map[string]any{}}); err != nil {
		t.Fatalf("tools/call: %v", err)
	}
	select {
	case got := <-progress:
		if got != "Indexing (2/5)\n" {
			t.Fatalf("progress = %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("progress notification was not routed")
	}
}

func TestSDKSessionConcurrentRebuildIsSingleflight(t *testing.T) {
	var connections atomic.Int32
	transport := newInMemorySDKTransport(t, func() *mcpsdk.Server {
		connections.Add(1)
		server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "singleflight", Version: "1"}, nil)
		mcpsdk.AddTool(server, &mcpsdk.Tool{Name: "read"}, func(context.Context, *mcpsdk.CallToolRequest, map[string]any) (*mcpsdk.CallToolResult, any, error) {
			return &mcpsdk.CallToolResult{}, nil, nil
		})
		return server
	})
	first, err := transport.acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	transport.invalidate(first)

	const callers = 12
	errs := make(chan error, callers)
	for range callers {
		go func() {
			_, err := transport.acquire(t.Context())
			errs <- err
		}()
	}
	for range callers {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	if got := connections.Load(); got != 2 {
		t.Fatalf("connections = %d, want initial + one shared rebuild", got)
	}
	transport.mu.Lock()
	current := transport.current
	transport.mu.Unlock()
	if current == nil || current == first {
		t.Fatal("rebuild did not publish a new generation")
	}

	// A stale Wait callback can arrive after the replacement has already been
	// published. Re-run that exact callback path and prove its generation fence
	// cannot clear the healthy current session.
	transport.handleSessionEnd(first, mcpsdk.ErrConnectionClosed)
	transport.mu.Lock()
	stillCurrent := transport.current == current
	transport.mu.Unlock()
	if !stillCurrent {
		t.Fatal("stale generation callback cleared the replacement session")
	}
}

func TestSDKSessionDropsStaleGenerationProgress(t *testing.T) {
	transport := newInMemorySDKTransport(t, func() *mcpsdk.Server {
		return mcpsdk.NewServer(&mcpsdk.Implementation{Name: "progress-generation", Version: "1"}, nil)
	})
	first, err := transport.acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	progress := make(chan string, 2)
	stop := transport.registerProgress("same-token", func(chunk string) { progress <- chunk })
	defer stop()

	transport.dispatchSDKProgress(first.generation, &mcpsdk.ProgressNotificationParams{
		ProgressToken: "same-token", Progress: 1, Total: 2, Message: "first",
	})
	if got := <-progress; got != "first (1/2)\n" {
		t.Fatalf("first progress = %q", got)
	}

	transport.invalidate(first)
	second, err := transport.acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	transport.dispatchSDKProgress(first.generation, &mcpsdk.ProgressNotificationParams{
		ProgressToken: "same-token", Progress: 2, Total: 2, Message: "stale",
	})
	if len(progress) != 0 {
		t.Fatalf("stale generation delivered progress: %q", <-progress)
	}
	transport.dispatchSDKProgress(second.generation, &mcpsdk.ProgressNotificationParams{
		ProgressToken: "same-token", Progress: 2, Total: 2, Message: "current",
	})
	if got := <-progress; got != "current (2/2)\n" {
		t.Fatalf("current progress = %q", got)
	}
}

func containsJSONText(result json.RawMessage, want string) bool {
	var decoded struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	return json.Unmarshal(result, &decoded) == nil && len(decoded.Content) == 1 && decoded.Content[0].Text == want
}
