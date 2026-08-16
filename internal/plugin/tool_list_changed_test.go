package plugin

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
	mu               sync.Mutex
	refreshActive    bool
	refreshStartedCh chan struct{}
	refreshOnce      sync.Once
	notifications    notificationRouter
	closeOnce        sync.Once
	closed           chan struct{}
}

func newNotificationToolsTransport() *notificationToolsTransport {
	return &notificationToolsTransport{closed: make(chan struct{}), refreshStartedCh: make(chan struct{})}
}

func (t *notificationToolsTransport) call(ctx context.Context, method string, _ any) (json.RawMessage, error) {
	if method != "tools/list" {
		return json.RawMessage(`{}`), nil
	}
	t.mu.Lock()
	t.refreshActive = true
	t.mu.Unlock()
	t.refreshOnce.Do(func() { close(t.refreshStartedCh) })
	<-ctx.Done()
	return nil, ctx.Err()
}

type controlledToolsTransport struct {
	mu            sync.Mutex
	notifications notificationRouter
	listCalls     int
	toolCalls     int
	emitOnList    bool
	failList      bool
	blockList     bool
	blockTool     bool
	toolName      string
	listStarted   chan int
	listRelease   chan struct{}
	toolStarted   chan struct{}
	toolRelease   chan struct{}
}

func newControlledToolsTransport() *controlledToolsTransport {
	return &controlledToolsTransport{
		toolName:    "echo",
		listStarted: make(chan int, 16), listRelease: make(chan struct{}, 16),
		toolStarted: make(chan struct{}, 1), toolRelease: make(chan struct{}, 1),
	}
}

func (t *controlledToolsTransport) call(ctx context.Context, method string, _ any) (json.RawMessage, error) {
	switch method {
	case "tools/list":
		t.mu.Lock()
		t.listCalls++
		call := t.listCalls
		block, fail, emit, toolName := t.blockList, t.failList, t.emitOnList, t.toolName
		t.mu.Unlock()
		t.listStarted <- call
		if block {
			select {
			case <-t.listRelease:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		if emit {
			t.notifications.dispatchNotification("notifications/tools/list_changed", nil)
		}
		if fail {
			return nil, errors.New("tools/list failed")
		}
		response, _ := json.Marshal(map[string]any{"tools": []map[string]any{{
			"name": toolName, "description": "Echo.", "inputSchema": map[string]any{"type": "object"},
		}}})
		return response, nil
	case "tools/call":
		t.mu.Lock()
		t.toolCalls++
		block := t.blockTool
		t.mu.Unlock()
		if block {
			t.toolStarted <- struct{}{}
			select {
			case <-t.toolRelease:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		return json.RawMessage(`{"content":[{"type":"text","text":"ok"}]}`), nil
	default:
		return json.RawMessage(`{}`), nil
	}
}

func (*controlledToolsTransport) notify(context.Context, string, any) error { return nil }
func (*controlledToolsTransport) close()                                    {}
func (t *controlledToolsTransport) registerNotification(method string, callback func(json.RawMessage)) func() {
	return t.notifications.registerNotification(method, callback)
}
func (t *controlledToolsTransport) emit() {
	t.notifications.dispatchNotification("notifications/tools/list_changed", nil)
}
func (t *controlledToolsTransport) counts() (list, calls int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.listCalls, t.toolCalls
}

func newControlledRefreshClient(t *testing.T, tr *controlledToolsTransport) (*Client, tool.Tool) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	client := &Client{
		name: "controlled", t: tr, spec: Spec{Name: "controlled"}, supportsToolListChanged: true,
		refresh: toolListRefreshState{ctx: ctx, cancel: cancel, wait: func(context.Context, time.Duration) error { return nil }},
	}
	tools, err := client.listTools(ctx)
	if err != nil {
		client.close()
		t.Fatalf("initial listTools: %v", err)
	}
	<-tr.listStarted // drain the synchronous initial tools/list observation
	client.watchToolListChanges()
	return client, tools[0]
}

func refreshDone(t *testing.T, client *Client) <-chan struct{} {
	t.Helper()
	client.refresh.mu.Lock()
	defer client.refresh.mu.Unlock()
	if client.refresh.cycleDone == nil {
		t.Fatal("refresh cycle did not start")
	}
	return client.refresh.cycleDone
}

func waitClosed(t *testing.T, ch <-chan struct{}, label string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", label)
	}
}

func TestToolListRefreshCoalescesNotificationBurst(t *testing.T) {
	tr := newControlledToolsTransport()
	client, _ := newControlledRefreshClient(t, tr)
	defer client.close()

	waitStarted := make(chan struct{}, 1)
	releaseWait := make(chan struct{})
	client.refresh.mu.Lock()
	client.refresh.wait = func(ctx context.Context, _ time.Duration) error {
		waitStarted <- struct{}{}
		select {
		case <-releaseWait:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	client.refresh.mu.Unlock()

	tr.emit()
	done := refreshDone(t, client)
	<-waitStarted
	for range 99 {
		tr.emit()
	}
	close(releaseWait)
	waitClosed(t, done, "coalesced refresh")
	if lists, _ := tr.counts(); lists != 2 {
		t.Fatalf("tools/list calls = %d, want initial + one coalesced refresh", lists)
	}
	if client.toolCatalogStale() {
		t.Fatal("catalog remained stale after coalesced refresh")
	}
}

func TestToolListRefreshBacksOffAndConvergesAfterRepeatedNotices(t *testing.T) {
	tr := newControlledToolsTransport()
	client, _ := newControlledRefreshClient(t, tr)
	defer client.close()
	delays := make(chan time.Duration, 4)
	releaseDelay := make(chan struct{}, 4)
	client.refresh.mu.Lock()
	client.refresh.wait = func(ctx context.Context, delay time.Duration) error {
		select {
		case delays <- delay:
		case <-ctx.Done():
			return ctx.Err()
		}
		select {
		case <-releaseDelay:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	client.refresh.mu.Unlock()
	tr.mu.Lock()
	tr.blockList = true
	tr.emitOnList = true
	tr.mu.Unlock()

	tr.emit()
	done := refreshDone(t, client)
	if delay := <-delays; delay != toolListRefreshDebounce {
		t.Fatalf("first refresh delay = %s, want %s", delay, toolListRefreshDebounce)
	}
	releaseDelay <- struct{}{}
	if call := <-tr.listStarted; call != 2 {
		t.Fatalf("first refresh call = %d, want 2", call)
	}
	tr.listRelease <- struct{}{}
	if delay := <-delays; delay != 2*toolListRefreshDebounce {
		t.Fatalf("first catch-up delay = %s, want %s", delay, 2*toolListRefreshDebounce)
	}
	releaseDelay <- struct{}{}
	if call := <-tr.listStarted; call != 3 {
		t.Fatalf("catch-up refresh call = %d, want 3", call)
	}
	tr.listRelease <- struct{}{}
	if delay := <-delays; delay != 4*toolListRefreshDebounce {
		t.Fatalf("second catch-up delay = %s, want %s", delay, 4*toolListRefreshDebounce)
	}
	tr.mu.Lock()
	tr.emitOnList = false
	tr.mu.Unlock()
	releaseDelay <- struct{}{}
	if call := <-tr.listStarted; call != 4 {
		t.Fatalf("second catch-up refresh call = %d, want 4", call)
	}
	tr.listRelease <- struct{}{}
	waitClosed(t, done, "backed-off refresh convergence")
	if lists, _ := tr.counts(); lists != 4 {
		t.Fatalf("tools/list calls = %d, want initial + three converging attempts", lists)
	}
	if client.toolCatalogStale() {
		t.Fatal("catalog remained stale after the self-notification stopped")
	}
}

func TestToolListRefreshBoundsPermanentSelfNotificationsAndRecoversOnRetry(t *testing.T) {
	tr := newControlledToolsTransport()
	client, adapter := newControlledRefreshClient(t, tr)
	defer client.close()
	delays := make(chan time.Duration, toolListRefreshMaxAttempts+2)
	releaseDelay := make(chan struct{}, toolListRefreshMaxAttempts+2)
	client.refresh.mu.Lock()
	client.refresh.wait = func(ctx context.Context, delay time.Duration) error {
		select {
		case delays <- delay:
		case <-ctx.Done():
			return ctx.Err()
		}
		select {
		case <-releaseDelay:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	client.refresh.mu.Unlock()
	tr.mu.Lock()
	tr.emitOnList = true
	tr.mu.Unlock()

	tr.emit()
	done := refreshDone(t, client)
	wantDelay := toolListRefreshDebounce
	for attempt := range toolListRefreshMaxAttempts {
		if delay := <-delays; delay != wantDelay {
			t.Fatalf("refresh attempt %d delay = %s, want %s", attempt+1, delay, wantDelay)
		}
		releaseDelay <- struct{}{}
		if call := <-tr.listStarted; call != attempt+2 {
			t.Fatalf("refresh attempt %d tools/list call = %d, want %d", attempt+1, call, attempt+2)
		}
		wantDelay = nextToolListRefreshDelay(wantDelay)
	}
	waitClosed(t, done, "bounded self-notification refresh")
	if lists, _ := tr.counts(); lists != 1+toolListRefreshMaxAttempts {
		t.Fatalf("tools/list calls = %d, want initial + %d bounded attempts", lists, toolListRefreshMaxAttempts)
	}
	if !client.toolCatalogStale() {
		t.Fatal("permanently self-notifying server incorrectly marked its catalog current")
	}
	select {
	case delay := <-delays:
		t.Fatalf("refresh cycle scheduled an unbounded extra delay %s", delay)
	default:
	}

	// A user attempt on the stale adapter fails closed and starts a fresh cycle.
	// Once the server stops self-notifying, that bounded retry converges and the
	// unchanged adapter becomes callable again.
	tr.mu.Lock()
	tr.emitOnList = false
	tr.mu.Unlock()
	if _, err := adapter.Execute(context.Background(), json.RawMessage(`{}`)); err == nil || !strings.Contains(err.Error(), "refresh is still pending or failed") {
		t.Fatalf("stale adapter error = %v, want fail-closed refresh error", err)
	}
	retryDone := refreshDone(t, client)
	if delay := <-delays; delay != toolListRefreshDebounce {
		t.Fatalf("retry refresh delay = %s, want %s", delay, toolListRefreshDebounce)
	}
	releaseDelay <- struct{}{}
	if call := <-tr.listStarted; call != 2+toolListRefreshMaxAttempts {
		t.Fatalf("retry tools/list call = %d, want %d", call, 2+toolListRefreshMaxAttempts)
	}
	waitClosed(t, retryDone, "retry convergence")
	if client.toolCatalogStale() {
		t.Fatal("catalog remained stale after the server stopped self-notifying")
	}
	if _, err := adapter.Execute(context.Background(), json.RawMessage(`{}`)); err != nil {
		t.Fatalf("adapter after bounded recovery: %v", err)
	}
	if _, calls := tr.counts(); calls != 1 {
		t.Fatalf("tools/call count = %d, want one post-recovery dispatch", calls)
	}
}

func TestToolListRefreshTimeoutUsesResolvedBudgets(t *testing.T) {
	tests := []struct {
		name string
		spec Spec
		want time.Duration
	}{
		{name: "built-in defaults", spec: Spec{}, want: defaultStartupTimeout},
		{name: "call timeout is stricter", spec: Spec{StartupTimeout: 45 * time.Second, CallTimeout: 30 * time.Second}, want: 30 * time.Second},
		{name: "startup timeout is stricter", spec: Spec{StartupTimeout: 10 * time.Second, CallTimeout: 60 * time.Second}, want: 10 * time.Second},
		{name: "global defaults", spec: Spec{DefaultStartupTimeout: 40 * time.Second, DefaultCallTimeout: 20 * time.Second}, want: 20 * time.Second},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := &Client{spec: tc.spec}
			if got := client.toolListRefreshTimeout(); got != tc.want {
				t.Fatalf("refresh timeout = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestServersDoesNotWaitForBlockedToolCall(t *testing.T) {
	tr := newControlledToolsTransport()
	client, adapter := newControlledRefreshClient(t, tr)
	defer client.close()
	tr.mu.Lock()
	tr.blockTool = true
	tr.mu.Unlock()

	callDone := make(chan error, 1)
	go func() {
		_, err := adapter.Execute(context.Background(), json.RawMessage(`{}`))
		callDone <- err
	}()
	<-tr.toolStarted

	host := &Host{clients: []*Client{client}}
	statusDone := make(chan []ServerStatus, 1)
	go func() { statusDone <- host.Servers() }()
	select {
	case statuses := <-statusDone:
		if len(statuses) != 1 || statuses[0].Tools != 1 {
			t.Fatalf("statuses = %+v, want one server with one tool", statuses)
		}
	case <-time.After(time.Second):
		tr.toolRelease <- struct{}{}
		t.Fatal("Host.Servers blocked behind tools/call")
	}
	tr.toolRelease <- struct{}{}
	if err := <-callDone; err != nil {
		t.Fatalf("blocked tool call: %v", err)
	}
}

func TestChangedCatalogPublishesAfterInFlightToolCall(t *testing.T) {
	tr := newControlledToolsTransport()
	client, oldAdapter := newControlledRefreshClient(t, tr)
	defer client.close()
	tr.mu.Lock()
	tr.blockTool = true
	tr.toolName = "echo_v2"
	tr.mu.Unlock()

	callDone := make(chan error, 1)
	go func() {
		_, err := oldAdapter.Execute(context.Background(), json.RawMessage(`{}`))
		callDone <- err
	}()
	<-tr.toolStarted
	tr.emit()
	done := refreshDone(t, client)
	<-tr.listStarted
	select {
	case <-done:
		t.Fatal("changed catalog published before the admitted tool call completed")
	default:
	}

	tr.toolRelease <- struct{}{}
	if err := <-callDone; err != nil {
		t.Fatalf("admitted tool call: %v", err)
	}
	waitClosed(t, done, "catalog publication after tool call")
	if _, err := oldAdapter.Execute(context.Background(), json.RawMessage(`{}`)); err == nil || !strings.Contains(err.Error(), "changed tool") {
		t.Fatalf("old adapter after publication = %v, want changed-tool refusal", err)
	}
	current, ok := client.cachedTools()
	if !ok || findToolByName(current, "mcp__controlled__echo_v2") == nil {
		t.Fatalf("current tools = %v, want echo_v2", toolNames(current))
	}
}

func TestServersDoesNotWaitForBlockedToolListRefresh(t *testing.T) {
	tr := newControlledToolsTransport()
	client, _ := newControlledRefreshClient(t, tr)
	defer client.close()
	tr.mu.Lock()
	tr.blockList = true
	tr.mu.Unlock()

	tr.emit()
	done := refreshDone(t, client)
	<-tr.listStarted
	host := &Host{clients: []*Client{client}}
	statusDone := make(chan []ServerStatus, 1)
	go func() { statusDone <- host.Servers() }()
	select {
	case statuses := <-statusDone:
		if len(statuses) != 1 || statuses[0].Tools != 1 {
			t.Fatalf("statuses = %+v, want previous complete snapshot", statuses)
		}
	case <-time.After(time.Second):
		tr.listRelease <- struct{}{}
		t.Fatal("Host.Servers blocked behind tools/list")
	}
	tr.listRelease <- struct{}{}
	waitClosed(t, done, "blocked refresh release")
}

func TestToolListRefreshFailureKeepsOldAdapterFailClosed(t *testing.T) {
	tr := newControlledToolsTransport()
	client, adapter := newControlledRefreshClient(t, tr)
	defer client.close()
	tr.mu.Lock()
	tr.blockList = true
	tr.failList = true
	tr.mu.Unlock()

	tr.emit()
	done := refreshDone(t, client)
	<-tr.listStarted
	tr.listRelease <- struct{}{}
	waitClosed(t, done, "failed refresh")
	if !client.toolCatalogStale() {
		t.Fatal("failed refresh incorrectly marked the old catalog current")
	}

	tr.mu.Lock()
	tr.failList = false
	tr.mu.Unlock()
	if _, err := adapter.Execute(context.Background(), json.RawMessage(`{}`)); err == nil || !strings.Contains(err.Error(), "refresh is still pending or failed") {
		t.Fatalf("stale adapter error = %v, want fail-closed refresh error", err)
	}
	if _, calls := tr.counts(); calls != 0 {
		t.Fatalf("stale adapter reached tools/call %d times", calls)
	}
	retryDone := refreshDone(t, client)
	<-tr.listStarted
	client.close()
	waitClosed(t, retryDone, "cancelled retry refresh")
}

func TestToolListRefreshNoOpPreservesAdapterGeneration(t *testing.T) {
	tr := newControlledToolsTransport()
	client, adapter := newControlledRefreshClient(t, tr)
	defer client.close()
	tr.mu.Lock()
	tr.blockList = true
	tr.mu.Unlock()
	remote := adapter.(*remoteTool)
	generation := remote.generation
	changes := make(chan struct{}, 1)
	client.setToolsChangedCallback(func([]tool.Tool) { changes <- struct{}{} })

	tr.emit()
	done := refreshDone(t, client)
	<-tr.listStarted
	tr.listRelease <- struct{}{}
	waitClosed(t, done, "no-op refresh")
	if client.toolCatalogStale() {
		t.Fatal("no-op refresh did not clear the notification revision")
	}
	if remote.generation != generation || client.catalogGeneration != generation {
		t.Fatalf("generation changed on no-op: adapter=%d catalog=%d want=%d", remote.generation, client.catalogGeneration, generation)
	}
	select {
	case <-changes:
		t.Fatal("no-op refresh published a change callback")
	default:
	}
	if _, err := adapter.Execute(context.Background(), json.RawMessage(`{}`)); err != nil {
		t.Fatalf("adapter after no-op refresh: %v", err)
	}
}

func TestClientIgnoresToolListChangedWithoutAdvertisedCapability(t *testing.T) {
	tr := newControlledToolsTransport()
	client := &Client{name: "unsupported", t: tr, spec: Spec{Name: "unsupported"}}
	client.watchToolListChanges()
	tr.notifications.mu.Lock()
	listeners := len(tr.notifications.listeners["notifications/tools/list_changed"])
	tr.notifications.mu.Unlock()
	if listeners != 0 {
		t.Fatalf("notification listeners = %d, want none without tools.listChanged", listeners)
	}
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
		name:                    "blocked",
		t:                       tr,
		spec:                    Spec{Name: "blocked"},
		supportsToolListChanged: true,
		refresh: toolListRefreshState{
			ctx:    ctx,
			cancel: cancel,
			wait:   func(context.Context, time.Duration) error { return nil },
		},
	}
	client.watchToolListChanges()
	tr.emit("notifications/tools/list_changed")
	done := refreshDone(t, client)
	waitClosed(t, tr.refreshStartedCh, "tools/list start")
	client.close()
	select {
	case <-tr.closed:
	case <-time.After(time.Second):
		t.Fatal("client close did not close transport")
	}
	waitClosed(t, done, "refresh cancellation")

	client.refresh.mu.Lock()
	defer client.refresh.mu.Unlock()
	if !client.refresh.closed {
		t.Fatal("refresh state was not closed")
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
