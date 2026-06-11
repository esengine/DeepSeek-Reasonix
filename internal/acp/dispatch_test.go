package acp

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"reasonix/internal/event"
)

// fakeNotifier captures Notify calls and answers Request via an injectable hook,
// standing in for *Conn in adapter unit tests.
type fakeNotifier struct {
	mu       sync.Mutex
	notifs   []capturedNotif
	onReq    func(method string, params any) (json.RawMessage, error)
	onReqCtx func(ctx context.Context, method string, params any) (json.RawMessage, error)
	reqSeen  []capturedNotif
}

type capturedNotif struct {
	method string
	params any
}

func (f *fakeNotifier) Notify(method string, params any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.notifs = append(f.notifs, capturedNotif{method, params})
	return nil
}

func (f *fakeNotifier) Request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	f.mu.Lock()
	f.reqSeen = append(f.reqSeen, capturedNotif{method, params})
	f.mu.Unlock()
	if f.onReqCtx != nil {
		return f.onReqCtx(ctx, method, params)
	}
	if f.onReq != nil {
		return f.onReq(method, params)
	}
	return nil, nil
}

// updateMap marshals the i-th captured notification's params and decodes the
// nested "update" object into a generic map for shape assertions.
func (f *fakeNotifier) updateMap(t *testing.T, i int) map[string]any {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if i >= len(f.notifs) {
		t.Fatalf("only %d notifications captured, wanted index %d", len(f.notifs), i)
	}
	n := f.notifs[i]
	if n.method != "session/update" {
		t.Fatalf("notif %d method = %q, want session/update", i, n.method)
	}
	raw, err := json.Marshal(n.params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	var decoded struct {
		SessionID string         `json:"sessionId"`
		Update    map[string]any `json:"update"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal params: %v", err)
	}
	if decoded.SessionID != "sess-1" {
		t.Errorf("notif %d sessionId = %q, want sess-1", i, decoded.SessionID)
	}
	return decoded.Update
}

func TestUpdateSinkMapsEvents(t *testing.T) {
	fn := &fakeNotifier{}
	sink := newUpdateSink(fn, "sess-1")

	sink.Emit(event.Event{Kind: event.Reasoning, Text: "thinking..."})
	sink.Emit(event.Event{Kind: event.Text, Text: "answer"})
	sink.Emit(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{
		ID: "call-1", Name: "read_file", Args: `{"path":"a.go"}`, ReadOnly: true,
	}})
	sink.Emit(event.Event{Kind: event.ToolResult, Tool: event.Tool{
		ID: "call-1", Name: "read_file", Output: "package main",
	}})
	sink.Emit(event.Event{Kind: event.ToolResult, Tool: event.Tool{
		ID: "call-2", Name: "bash", Err: "permission denied",
	}})

	if got := len(fn.notifs); got != 5 {
		t.Fatalf("emitted %d notifications, want 5", got)
	}

	// agent_thought_chunk
	u := fn.updateMap(t, 0)
	if u["sessionUpdate"] != "agent_thought_chunk" {
		t.Errorf("update 0 = %v, want agent_thought_chunk", u["sessionUpdate"])
	}
	if content, _ := u["content"].(map[string]any); content["text"] != "thinking..." {
		t.Errorf("update 0 content text = %v", content)
	}

	// agent_message_chunk
	u = fn.updateMap(t, 1)
	if u["sessionUpdate"] != "agent_message_chunk" {
		t.Errorf("update 1 = %v, want agent_message_chunk", u["sessionUpdate"])
	}

	// tool_call (pending, with kind + rawInput)
	u = fn.updateMap(t, 2)
	if u["sessionUpdate"] != "tool_call" || u["status"] != "pending" {
		t.Errorf("update 2 = %v", u)
	}
	if u["kind"] != "read" {
		t.Errorf("update 2 kind = %v, want read", u["kind"])
	}
	if u["toolCallId"] != "call-1" {
		t.Errorf("update 2 toolCallId = %v, want call-1", u["toolCallId"])
	}
	if ri, _ := u["rawInput"].(map[string]any); ri["path"] != "a.go" {
		t.Errorf("update 2 rawInput = %v", u["rawInput"])
	}

	// tool_call_update completed
	u = fn.updateMap(t, 3)
	if u["sessionUpdate"] != "tool_call_update" || u["status"] != "completed" {
		t.Errorf("update 3 = %v", u)
	}

	// tool_call_update failed surfaces the error text
	u = fn.updateMap(t, 4)
	if u["status"] != "failed" {
		t.Errorf("update 4 status = %v, want failed", u["status"])
	}
	arr, _ := u["content"].([]any)
	if len(arr) != 1 {
		t.Fatalf("update 4 content = %v", u["content"])
	}
	wrap, _ := arr[0].(map[string]any)
	inner, _ := wrap["content"].(map[string]any)
	if inner["text"] != "permission denied" {
		t.Errorf("update 4 inner text = %v, want permission denied", inner["text"])
	}
}

func TestUpdateSinkDropsAndWarns(t *testing.T) {
	fn := &fakeNotifier{}
	sink := newUpdateSink(fn, "sess-1")

	// Dropped kinds: TurnStarted, Message, Usage, Phase, and empty deltas.
	sink.Emit(event.Event{Kind: event.TurnStarted})
	sink.Emit(event.Event{Kind: event.Message, Text: "full", Reasoning: "chain"})
	sink.Emit(event.Event{Kind: event.Usage})
	sink.Emit(event.Event{Kind: event.Phase, Text: "planning"})
	sink.Emit(event.Event{Kind: event.Text, Text: ""})
	if got := len(fn.notifs); got != 0 {
		t.Fatalf("dropped kinds produced %d notifications, want 0", got)
	}

	// Warn-level notices are surfaced as a message chunk; info notices are not.
	sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: "fyi"})
	if got := len(fn.notifs); got != 0 {
		t.Fatalf("info notice produced %d notifications, want 0", got)
	}
	sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: "watch out"})
	if got := len(fn.notifs); got != 1 {
		t.Fatalf("warn notice produced %d notifications, want 1", got)
	}
	u := fn.updateMap(t, 0)
	if u["sessionUpdate"] != "agent_message_chunk" {
		t.Errorf("warn update = %v", u["sessionUpdate"])
	}
	if c, _ := u["content"].(map[string]any); !strings.Contains(c["text"].(string), "watch out") {
		t.Errorf("warn content = %v", u["content"])
	}
}

// approveCall records one approve(id, allow, session, persist) callback.
type approveCall struct {
	id      string
	allow   bool
	session bool
	persist bool
}

func TestUpdateSinkApprovalAllowAlways(t *testing.T) {
	fn := &fakeNotifier{onReq: func(method string, params any) (json.RawMessage, error) {
		if method != "session/request_permission" {
			t.Errorf("request method = %q, want session/request_permission", method)
		}
		raw, _ := json.Marshal(params)
		var p PermissionRequestParams
		if err := json.Unmarshal(raw, &p); err != nil {
			t.Fatalf("permission params: %v", err)
		}
		if p.SessionID != "sess-1" {
			t.Errorf("sessionId = %q", p.SessionID)
		}
		if p.ToolCall.Kind != "execute" {
			t.Errorf("kind = %q, want execute", p.ToolCall.Kind)
		}
		if p.ToolCall.ToolCallID != "gate-9" {
			t.Errorf("toolCallId = %q, want gate-9", p.ToolCall.ToolCallID)
		}
		res, _ := json.Marshal(PermissionRequestResult{
			Outcome: PermissionOutcome{Outcome: "selected", OptionID: string(OptAllowAlways)},
		})
		return res, nil
	}}
	sink := newUpdateSink(fn, "sess-1")
	got := make(chan approveCall, 1)
	sink.bindApprove(func(id string, allow, session, persist bool) { got <- approveCall{id, allow, session, persist} })

	sink.Emit(event.Event{Kind: event.ApprovalRequest, Approval: event.Approval{ID: "9", Tool: "bash", Subject: "rm -rf /"}})

	select {
	case c := <-got:
		if c != (approveCall{id: "9", allow: true, session: true, persist: false}) {
			t.Errorf("approve = %+v, want {9 true true}", c)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("approve was never called")
	}
}

func TestUpdateSinkApprovalBashPrefix(t *testing.T) {
	fn := &fakeNotifier{onReq: func(_ string, params any) (json.RawMessage, error) {
		raw, _ := json.Marshal(params)
		var p PermissionRequestParams
		if err := json.Unmarshal(raw, &p); err != nil {
			t.Fatalf("permission params: %v", err)
		}
		// Bash with a safe prefix now uses the same standard options as any
		// other tool (allow_always / allow_persistent); the old prefix-specific
		// OptAllowPrefix/OptPersistPrefix are gone.
		var hasSession, hasPersistent bool
		for _, opt := range p.Options {
			hasSession = hasSession || opt.OptionID == string(OptAllowAlways)
			hasPersistent = hasPersistent || opt.OptionID == string(OptAllowPersistent)
		}
		if !hasSession || !hasPersistent {
			t.Fatalf("options = %+v, want standard session and persistent choices", p.Options)
		}
		if len(p.Options) != 4 {
			t.Fatalf("options = %+v, want allow once, session, persistent, reject", p.Options)
		}
		res, _ := json.Marshal(PermissionRequestResult{
			Outcome: PermissionOutcome{Outcome: "selected", OptionID: string(OptAllowPersistent)},
		})
		return res, nil
	}}
	sink := newUpdateSink(fn, "sess-1")
	got := make(chan approveCall, 1)
	sink.bindApprove(func(id string, allow, session, persist bool) { got <- approveCall{id, allow, session, persist} })

	sink.Emit(event.Event{Kind: event.ApprovalRequest, Approval: event.Approval{ID: "10", Tool: "bash", Subject: "go test ./..."}})

	select {
	case c := <-got:
		want := approveCall{id: "10", allow: true, session: true, persist: true}
		if c != want {
			t.Errorf("approve = %+v, want %+v", c, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("approve was never called")
	}
}

func TestUpdateSinkApprovalDenied(t *testing.T) {
	// Both a "cancelled" outcome and a transport error must deny the call.
	for _, tc := range []struct {
		name string
		resp func() (json.RawMessage, error)
	}{
		{"cancelled", func() (json.RawMessage, error) {
			r, _ := json.Marshal(PermissionRequestResult{Outcome: PermissionOutcome{Outcome: "cancelled"}})
			return r, nil
		}},
		{"transport error", func() (json.RawMessage, error) {
			return nil, context.Canceled
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fn := &fakeNotifier{onReq: func(string, any) (json.RawMessage, error) { return tc.resp() }}
			sink := newUpdateSink(fn, "sess-1")
			got := make(chan approveCall, 1)
			sink.bindApprove(func(id string, allow, session, persist bool) { got <- approveCall{id, allow, session, persist} })

			sink.Emit(event.Event{Kind: event.ApprovalRequest, Approval: event.Approval{ID: "3", Tool: "edit_file"}})

			select {
			case c := <-got:
				if c.allow || c.session {
					t.Errorf("approve = %+v, want denied", c)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("approve was never called")
			}
		})
	}
}

func TestUpdateSinkApprovalUsesTurnContext(t *testing.T) {
	reqStarted := make(chan struct{})
	fn := &fakeNotifier{onReqCtx: func(ctx context.Context, _ string, _ any) (json.RawMessage, error) {
		close(reqStarted)
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	sink := newUpdateSink(fn, "sess-1")
	turnCtx, cancel := context.WithCancel(context.Background())
	sink.setTurnContext(turnCtx)
	got := make(chan approveCall, 1)
	sink.bindApprove(func(id string, allow, session, persist bool) { got <- approveCall{id, allow, session, persist} })

	sink.Emit(event.Event{Kind: event.ApprovalRequest, Approval: event.Approval{ID: "7", Tool: "bash"}})
	select {
	case <-reqStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("permission request did not start")
	}
	cancel()

	select {
	case c := <-got:
		if c.id != "7" || c.allow || c.session || c.persist {
			t.Fatalf("approve after context cancel = %+v, want denied id=7", c)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("turn context cancellation did not deny permission request")
	}
}

func TestClipKeepsValidUTF8(t *testing.T) {
	text := strings.Repeat("a", maxResultChars-1) + "界" + strings.Repeat("b", 20)
	got := clip(text)
	if !utf8.ValidString(got) {
		t.Fatalf("clip returned invalid UTF-8")
	}
	if strings.Contains(got, "\ufffd") {
		t.Fatalf("clip inserted replacement characters: %q", got[len(got)-40:])
	}
}

func TestClip(t *testing.T) {
	if got := clip("short"); got != "short" {
		t.Errorf("clip(short) = %q", got)
	}
	long := strings.Repeat("x", maxResultChars+10)
	got := clip(long)
	if !strings.HasPrefix(got, strings.Repeat("x", maxResultChars)) {
		t.Errorf("clip did not preserve the head")
	}
	if !strings.Contains(got, "10 more chars truncated") {
		t.Errorf("clip note missing: %q", got[len(got)-40:])
	}
}
