package plugin

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/tool"
)

type notificationToolsTransport struct {
	mu            sync.Mutex
	refreshActive bool
	notifications notificationRouter
	closeOnce     sync.Once
	closed        chan struct{}
}

func newNotificationToolsTransport() *notificationToolsTransport {
	return &notificationToolsTransport{closed: make(chan struct{})}
}

func (t *notificationToolsTransport) call(ctx context.Context, method string, _ any) (json.RawMessage, error) {
	if method != "tools/list" {
		return json.RawMessage(`{}`), nil
	}
	t.mu.Lock()
	t.refreshActive = true
	t.mu.Unlock()
	<-ctx.Done()
	return nil, ctx.Err()
}

func (*notificationToolsTransport) notify(context.Context, string, any) error { return nil }

func (t *notificationToolsTransport) registerNotification(method string, callback func(json.RawMessage)) func() {
	return t.notifications.registerNotification(method, callback)
}

func (t *notificationToolsTransport) emit(method string) {
	t.notifications.dispatchNotification(method, nil)
}

func (t *notificationToolsTransport) close() {
	t.closeOnce.Do(func() { close(t.closed) })
}

func (t *notificationToolsTransport) refreshStarted() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.refreshActive
}

func TestHostRefreshesToolsAfterListChangedNotification(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	startCount := filepath.Join(t.TempDir(), "starts")
	spec := Spec{
		Name:    "dynamic",
		Command: os.Args[0],
		Args:    []string{"-test.run=TestDynamicToolsHelperProcess", "--"},
		Env: map[string]string{
			"GO_WANT_DYNAMIC_TOOLS_HELPER": "1",
			"GO_WANT_HELPER_START_COUNT":   startCount,
		},
	}

	host := NewHost()
	initial, err := host.Add(ctx, spec)
	if err != nil {
		t.Fatalf("Host.Add: %v", err)
	}
	defer host.Close()
	if got := toolNames(initial); !slices.Equal(got, []string{"mcp__dynamic__load_toolset"}) {
		t.Fatalf("initial tools = %v, want load_toolset only", got)
	}

	changes := make(chan []tool.Tool, 1)
	unsubscribe := host.SubscribeToolListChanges(ctx, func(changed Spec, tools []tool.Tool) {
		if MCPRuntimeSpecMatches(changed, spec) {
			changes <- tools
		}
	})
	defer unsubscribe()
	loader := findToolByName(initial, "mcp__dynamic__load_toolset")
	if _, err := loader.Execute(ctx, json.RawMessage(`{}`)); err != nil {
		t.Fatalf("load_toolset: %v", err)
	}

	select {
	case refreshed := <-changes:
		if findToolByName(refreshed, "mcp__dynamic__list_schematic_components") == nil {
			t.Fatalf("refreshed tools = %v, want list_schematic_components", toolNames(refreshed))
		}
		if _, err := loader.Execute(ctx, json.RawMessage(`{}`)); err == nil || !strings.Contains(err.Error(), "changed tool") {
			t.Fatalf("stale pre-refresh adapter error = %v, want retryable changed-tool refusal", err)
		}
		cached, listErr := host.ToolsFor(ctx, spec.Name)
		if listErr != nil {
			t.Fatalf("ToolsFor after list_changed: %v", listErr)
		}
		if findToolByName(cached, "mcp__dynamic__list_schematic_components") == nil {
			t.Fatalf("cached tools = %v, want list_schematic_components", toolNames(cached))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("host did not publish refreshed tools after notifications/tools/list_changed")
	}
	if got := readHelperCounter(t, startCount); got != 1 {
		t.Fatalf("process starts = %d, want one persistent MCP process", got)
	}
}

func TestClientCloseCancelsBlockedToolListRefresh(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	tr := newNotificationToolsTransport()
	client := &Client{
		name: "blocked",
		t:    tr,
		spec: Spec{Name: "blocked"},
		refresh: toolListRefreshState{
			ctx:    ctx,
			cancel: cancel,
		},
	}
	client.watchToolListChanges()
	tr.emit("notifications/tools/list_changed")

	deadline := time.Now().Add(time.Second)
	for !tr.refreshStarted() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !tr.refreshStarted() {
		t.Fatal("tools/list refresh did not start")
	}
	client.close()
	select {
	case <-tr.closed:
	case <-time.After(time.Second):
		t.Fatal("client close did not close transport")
	}

	client.refresh.mu.Lock()
	defer client.refresh.mu.Unlock()
	if !client.refresh.closed || client.refresh.pending {
		t.Fatalf("refresh state after close = closed:%v pending:%v", client.refresh.closed, client.refresh.pending)
	}
}

// TestDynamicToolsHelperProcess serves a minimal stdio MCP whose tool catalog
// expands after load_toolset and advertises that change through the protocol.
func TestDynamicToolsHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_DYNAMIC_TOOLS_HELPER") != "1" {
		return
	}
	defer os.Exit(0)
	incrementHelperCounter(os.Getenv("GO_WANT_HELPER_START_COUNT"))

	loaded := false
	in := bufio.NewReader(os.Stdin)
	for {
		line, err := in.ReadBytes('\n')
		if err != nil {
			return
		}
		line = bytes.TrimSpace(line)
		var request struct {
			ID     *int            `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if len(line) == 0 || json.Unmarshal(line, &request) != nil || request.ID == nil {
			continue
		}

		var result any
		notifyChanged := false
		switch request.Method {
		case "initialize":
			result = map[string]any{
				"protocolVersion": protocolVersion,
				"serverInfo":      map[string]any{"name": "dynamic", "version": "1"},
				"capabilities":    map[string]any{"tools": map[string]any{"listChanged": true}},
			}
		case "tools/list":
			tools := []map[string]any{{
				"name": "load_toolset", "description": "Load a toolset.",
				"inputSchema": map[string]any{"type": "object"},
			}}
			if loaded {
				tools = append(tools, map[string]any{
					"name": "list_schematic_components", "description": "List schematic components.",
					"inputSchema": map[string]any{"type": "object"},
				})
			}
			result = map[string]any{"tools": tools}
		case "tools/call":
			loaded = true
			notifyChanged = true
			result = map[string]any{"content": []map[string]any{{"type": "text", "text": "loaded"}}}
		}

		response, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": *request.ID, "result": result})
		_, _ = os.Stdout.Write(append(response, '\n'))
		if notifyChanged {
			notification, _ := json.Marshal(map[string]any{
				"jsonrpc": "2.0", "method": "notifications/tools/list_changed",
			})
			_, _ = os.Stdout.Write(append(notification, '\n'))
		}
	}
}
