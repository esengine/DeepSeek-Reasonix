package plugin

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"reasonix/internal/tool"
)

type schemaShiftTransport struct {
	mu            sync.Mutex
	listCalls     int
	schemaType    string
	listStarted   chan int
	listRelease   chan struct{}
	notifications notificationRouter
}

func (t *schemaShiftTransport) call(ctx context.Context, method string, _ any) (json.RawMessage, error) {
	if method != "tools/list" {
		return json.RawMessage(`{}`), nil
	}
	t.mu.Lock()
	t.listCalls++
	call := t.listCalls
	schemaType := t.schemaType
	t.mu.Unlock()
	select {
	case t.listStarted <- call:
	default:
	}
	select {
	case <-t.listRelease:
	default:
	}
	_ = ctx
	response, _ := json.Marshal(map[string]any{"tools": []map[string]any{{
		"name": "search", "description": "search",
		"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"q": map[string]any{"type": schemaType}}},
		"annotations": map[string]any{"readOnlyHint": true},
	}}})
	return response, nil
}

func (*schemaShiftTransport) close() {}
func (t *schemaShiftTransport) registerNotification(method string, callback func(json.RawMessage)) func() {
	return t.notifications.registerNotification(method, callback)
}

func TestListChangedInvalidatesValidatorAndGeneration(t *testing.T) {
	tr := &schemaShiftTransport{
		schemaType:  "string",
		listStarted: make(chan int, 8),
		listRelease: make(chan struct{}, 8),
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := &Client{
		name: "shift", t: tr, spec: Spec{Name: "shift"}, capabilities: clientCapabilities{toolsListChanged: true},
		refresh: toolListRefreshState{ctx: ctx, cancel: cancel, wait: func(context.Context, time.Duration) error { return nil }},
	}
	tools, err := client.listTools(ctx)
	if err != nil {
		t.Fatalf("initial list: %v", err)
	}
	adapter := tools[0]
	firstGen := adapter.(*remoteTool).generation
	got := tool.ValidateArguments(adapter, json.RawMessage(`{"q":"ok"}`))
	if got.CompileErr != nil || len(got.Violations) != 0 {
		t.Fatalf("initial validate: %+v", got)
	}
	oldFP := tool.SchemaFingerprint(adapter.Schema())

	tr.mu.Lock()
	tr.schemaType = "number"
	tr.mu.Unlock()
	releaseWait := make(chan struct{})
	client.refresh.mu.Lock()
	client.refresh.wait = func(ctx context.Context, _ time.Duration) error {
		select {
		case <-releaseWait:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	client.refresh.mu.Unlock()
	client.watchToolListChanges()
	tr.notifications.dispatchNotification("notifications/tools/list_changed", nil)
	done := refreshDone(t, client)
	close(releaseWait)
	waitClosed(t, done, "schema refresh")

	tools, ok := client.cachedTools()
	if !ok || len(tools) == 0 {
		t.Fatal("refreshed catalog missing")
	}
	adapter = tools[0]
	if adapter.(*remoteTool).generation == firstGen {
		t.Fatal("listChanged did not bump catalog generation")
	}
	newFP := tool.SchemaFingerprint(adapter.Schema())
	if newFP == oldFP {
		t.Fatal("schema fingerprint did not change")
	}
	got = tool.ValidateArguments(adapter, json.RawMessage(`{"q":"ok"}`))
	if len(got.Violations) == 0 {
		t.Fatal("new number schema accepted a string")
	}
}
