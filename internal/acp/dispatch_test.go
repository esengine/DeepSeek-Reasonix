package acp

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/provider"
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

func TestUpdateSinkReplayStripsSteerWrapper(t *testing.T) {
	fn := &fakeNotifier{}
	sink := newUpdateSink(fn, "sess-1")
	sink.replay([]provider.Message{{
		Role:    provider.RoleUser,
		Content: agent.MidTurnSteerPrefix + "\nuse plan B",
	}})

	u := fn.updateMap(t, 0)
	content, _ := u["content"].(map[string]any)
	if content["text"] != "use plan B" {
		t.Fatalf("replayed steer = %v, want raw user text", content["text"])
	}
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

func invalidACPv1PermissionOptionKind(options []PermissionOption) (PermissionOption, bool) {
	// ACP v1 schema only accepts these four PermissionOptionKind values. ACP hosts
	// own cross-session persistence, so Reasonix-specific persistent approvals must
	// not appear in session/request_permission options.
	valid := map[PermissionOptionKind]bool{
		OptAllowOnce:    true,
		OptAllowAlways:  true,
		OptRejectOnce:   true,
		OptRejectAlways: true,
	}
	for _, opt := range options {
		if !valid[opt.Kind] {
			return opt, true
		}
	}
	return PermissionOption{}, false
}

func assertACPv1PermissionOptionKinds(t *testing.T, options []PermissionOption) {
	t.Helper()
	if opt, ok := invalidACPv1PermissionOptionKind(options); ok {
		t.Fatalf("permission option %q uses non-ACP-v1 kind %q", opt.OptionID, opt.Kind)
	}
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
		assertACPv1PermissionOptionKinds(t, p.Options)
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

func TestUpdateSinkPermissionCarriesStructuredContext(t *testing.T) {
	fn := &fakeNotifier{onReq: func(_ string, params any) (json.RawMessage, error) {
		raw, _ := json.Marshal(params)
		var p PermissionRequestParams
		if err := json.Unmarshal(raw, &p); err != nil {
			t.Fatalf("permission params: %v", err)
		}
		if string(p.ToolCall.RawInput) != `{"path":"src/main.go","content":"next"}` {
			t.Fatalf("rawInput = %s", p.ToolCall.RawInput)
		}
		if len(p.ToolCall.Locations) != 1 || !strings.HasSuffix(filepath.ToSlash(p.ToolCall.Locations[0].Path), "/src/main.go") {
			t.Fatalf("locations = %+v", p.ToolCall.Locations)
		}
		meta, ok := p.ToolCall.Meta["reasonix.io"].(map[string]any)
		if !ok || meta["tool"] != "write_file" || meta["approvalId"] != "structured" || meta["reason"] != "write requested by the active goal" {
			t.Fatalf("metadata = %#v", p.ToolCall.Meta)
		}
		var wire map[string]any
		if err := json.Unmarshal(raw, &wire); err != nil {
			t.Fatalf("permission wire shape: %v", err)
		}
		toolCall, ok := wire["toolCall"].(map[string]any)
		if !ok {
			t.Fatalf("toolCall wire shape = %#v", wire["toolCall"])
		}
		if _, present := toolCall["reason"]; present {
			t.Fatalf("ACP v1 toolCall has non-standard root reason: %#v", toolCall)
		}
		res, _ := json.Marshal(PermissionRequestResult{Outcome: PermissionOutcome{Outcome: "selected", OptionID: string(OptRejectOnce)}})
		return res, nil
	}}
	sink := newUpdateSink(fn, "sess-structured")
	sink.bindCwd(t.TempDir())
	got := make(chan approveCall, 1)
	sink.bindApprove(func(id string, allow, session, persist bool) { got <- approveCall{id, allow, session, persist} })
	sink.Emit(event.Event{Kind: event.ApprovalRequest, Approval: event.Approval{
		ID: "structured", Tool: "write_file", Subject: "src/main.go",
		Reason:   "write requested by the active goal",
		RawInput: json.RawMessage(`{"path":"src/main.go","content":"next"}`),
	}})
	select {
	case decision := <-got:
		if decision.allow {
			t.Fatalf("rejected permission was allowed: %+v", decision)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("permission was never resolved")
	}
}

func TestUpdateSinkApprovalBashPrefix(t *testing.T) {
	fn := &fakeNotifier{onReq: func(_ string, params any) (json.RawMessage, error) {
		raw, _ := json.Marshal(params)
		var p PermissionRequestParams
		if err := json.Unmarshal(raw, &p); err != nil {
			t.Fatalf("permission params: %v", err)
		}
		// ACP permission options stay within the official spec kinds, and ACP
		// mode leaves cross-session persistence to the host.
		assertACPv1PermissionOptionKinds(t, p.Options)
		var hasOnce, hasSession, hasReject bool
		for _, opt := range p.Options {
			switch opt.OptionID {
			case string(OptAllowOnce):
				hasOnce = opt.Kind == OptAllowOnce
			case string(OptAllowAlways):
				hasSession = opt.Kind == OptAllowAlways
			case string(OptRejectOnce):
				hasReject = opt.Kind == OptRejectOnce
			default:
				t.Fatalf("unexpected ACP permission option %+v in %+v", opt, p.Options)
			}
		}
		if !hasOnce || !hasSession || !hasReject {
			t.Fatalf("options = %+v, want allow once, session, reject", p.Options)
		}
		if len(p.Options) != 3 {
			t.Fatalf("options = %+v, want allow once, session, reject", p.Options)
		}
		res, _ := json.Marshal(PermissionRequestResult{
			Outcome: PermissionOutcome{Outcome: "selected", OptionID: string(OptAllowAlways)},
		})
		return res, nil
	}}
	sink := newUpdateSink(fn, "sess-1")
	got := make(chan approveCall, 1)
	sink.bindApprove(func(id string, allow, session, persist bool) { got <- approveCall{id, allow, session, persist} })

	sink.Emit(event.Event{Kind: event.ApprovalRequest, Approval: event.Approval{ID: "10", Tool: "bash", Subject: "go test ./..."}})

	select {
	case c := <-got:
		want := approveCall{id: "10", allow: true, session: true, persist: false}
		if c != want {
			t.Errorf("approve = %+v, want %+v", c, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("approve was never called")
	}
}

func TestPermissionMetaOnlyTrustsForegroundStaticBash(t *testing.T) {
	cwd := t.TempDir()
	sink := newUpdateSink(&fakeNotifier{}, "sess-static-command")
	sink.bindCwd(cwd)

	for _, tc := range []struct {
		name     string
		rawInput string
		wantArgv []string
	}{
		{name: "static", rawInput: `{"command":"go test ./..."}`, wantArgv: []string{"go", "test", "./..."}},
		{name: "quoted static", rawInput: `{"command":"node -e 'process.exit(0)'"}`, wantArgv: []string{"node", "-e", "process.exit(0)"}},
		{name: "expansion", rawInput: `{"command":"go test $PACKAGE"}`},
		{name: "glob expansion", rawInput: `{"command":"go test ./*.go"}`},
		{name: "brace expansion", rawInput: `{"command":"printf '%s' {a,b}"}`},
		{name: "tilde expansion", rawInput: `{"command":"test -f ~/.config/reasonix.toml"}`},
		{name: "control syntax", rawInput: `{"command":"go test ./... && git status"}`},
		{name: "background", rawInput: `{"command":"go test ./...","run_in_background":true}`},
		{name: "preserved descendants", rawInput: `{"command":"go test ./...","preserve_background_processes":true}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			meta := sink.permissionMeta(event.Approval{
				ID: "command", Tool: "bash", Subject: "command", RawInput: json.RawMessage(tc.rawInput),
			})
			reasonix, ok := meta["reasonix.io"].(map[string]any)
			if !ok {
				t.Fatalf("reasonix metadata = %#v", meta)
			}
			argv, present := reasonix["argv"]
			if len(tc.wantArgv) == 0 {
				if present {
					t.Fatalf("unsafe command received trusted argv: %#v", argv)
				}
				return
			}
			got, ok := argv.([]string)
			if !ok || strings.Join(got, "\x00") != strings.Join(tc.wantArgv, "\x00") {
				t.Fatalf("argv = %#v, want %#v", argv, tc.wantArgv)
			}
			if reasonix["commandSchemaVersion"] != 1 || reasonix["cwd"] != filepath.Clean(cwd) {
				t.Fatalf("trusted command metadata = %#v", reasonix)
			}
		})
	}
}

func TestUpdateSinkSandboxEscapeApprovalOffersSessionGrant(t *testing.T) {
	fn := &fakeNotifier{onReq: func(_ string, params any) (json.RawMessage, error) {
		raw, _ := json.Marshal(params)
		var p PermissionRequestParams
		if err := json.Unmarshal(raw, &p); err != nil {
			t.Fatalf("permission params: %v", err)
		}
		assertACPv1PermissionOptionKinds(t, p.Options)
		var hasOnce, hasSession, hasReject bool
		for _, opt := range p.Options {
			switch opt.OptionID {
			case string(OptAllowOnce):
				hasOnce = opt.Kind == OptAllowOnce
			case string(OptAllowAlways):
				hasSession = opt.Kind == OptAllowAlways && opt.Name == "Use real environment for this session"
			case string(OptRejectOnce):
				hasReject = opt.Kind == OptRejectOnce
			default:
				t.Fatalf("unexpected ACP permission option %+v in %+v", opt, p.Options)
			}
		}
		if len(p.Options) != 3 || !hasOnce || !hasSession || !hasReject {
			t.Fatalf("options = %+v, want allow once, session, reject", p.Options)
		}
		res, _ := json.Marshal(PermissionRequestResult{
			Outcome: PermissionOutcome{Outcome: "selected", OptionID: string(OptAllowAlways)},
		})
		return res, nil
	}}
	sink := newUpdateSink(fn, "sess-1")
	got := make(chan approveCall, 1)
	sink.bindApprove(func(id string, allow, session, persist bool) { got <- approveCall{id, allow, session, persist} })

	sink.Emit(event.Event{Kind: event.ApprovalRequest, Approval: event.Approval{
		ID:      "11",
		Tool:    control.SandboxEscapeApprovalTool,
		Subject: "run unconfined once: go test ./...",
	}})

	select {
	case c := <-got:
		want := approveCall{id: "11", allow: true, session: true, persist: false}
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

func TestUpdateSinkAskRequestUsesPermissionChoices(t *testing.T) {
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
		if p.ToolCall.ToolCallID != "ask-ask-1-q1" {
			t.Errorf("toolCallId = %q, want ask-ask-1-q1", p.ToolCall.ToolCallID)
		}
		if p.ToolCall.Title != "Choose a target" {
			t.Errorf("title = %q", p.ToolCall.Title)
		}
		if len(p.Options) != 3 {
			t.Fatalf("options = %+v, want two answers plus cancel", p.Options)
		}
		assertACPv1PermissionOptionKinds(t, p.Options)
		if p.Options[0].Name != "Tests - Run the suite" || p.Options[0].Kind != OptAllowOnce {
			t.Fatalf("first option = %+v", p.Options[0])
		}
		res, _ := json.Marshal(PermissionRequestResult{
			Outcome: PermissionOutcome{Outcome: "selected", OptionID: "q1:2"},
		})
		return res, nil
	}}
	sink := newUpdateSink(fn, "sess-1")
	got := make(chan []event.AskAnswer, 1)
	sink.bindAnswer(func(id string, answers []event.AskAnswer) {
		if id != "ask-1" {
			t.Errorf("answer id = %q, want ask-1", id)
		}
		got <- answers
	})

	sink.Emit(event.Event{Kind: event.AskRequest, Ask: event.Ask{
		ID: "ask-1",
		Questions: []event.AskQuestion{{
			ID:     "q1",
			Header: "Topic",
			Prompt: "Choose a target",
			Options: []event.AskOption{
				{Label: "Tests", Description: "Run the suite"},
				{Label: "Docs"},
			},
		}},
	}})

	select {
	case answers := <-got:
		if len(answers) != 1 || answers[0].QuestionID != "q1" || len(answers[0].Selected) != 1 || answers[0].Selected[0] != "Docs" {
			t.Fatalf("answers = %+v, want q1 Docs", answers)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ask answer was never called")
	}
}

func TestUpdateSinkAskCancelledReturnsNoAnswers(t *testing.T) {
	fn := &fakeNotifier{onReq: func(string, any) (json.RawMessage, error) {
		res, _ := json.Marshal(PermissionRequestResult{Outcome: PermissionOutcome{Outcome: "cancelled"}})
		return res, nil
	}}
	sink := newUpdateSink(fn, "sess-1")
	got := make(chan []event.AskAnswer, 1)
	sink.bindAnswer(func(_ string, answers []event.AskAnswer) { got <- answers })

	sink.Emit(event.Event{Kind: event.AskRequest, Ask: event.Ask{
		ID: "ask-2",
		Questions: []event.AskQuestion{{
			ID:      "q1",
			Prompt:  "Continue?",
			Options: []event.AskOption{{Label: "Yes"}, {Label: "No"}},
		}},
	}})

	select {
	case answers := <-got:
		if answers != nil {
			t.Fatalf("answers = %+v, want nil on cancelled ask", answers)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ask cancellation was never returned")
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

func TestApprovalOptionsFreshDynamicToolOnlyAllowOnceOrReject(t *testing.T) {
	options := approvalOptions("extension__wipe", "extension/wipe", true)
	if len(options) != 2 || options[0].Kind != OptAllowOnce || options[1].Kind != OptRejectOnce {
		t.Fatalf("fresh dynamic-tool options = %+v, want allow-once/reject", options)
	}
	for _, option := range options {
		if option.Kind == OptAllowAlways {
			t.Fatalf("fresh dynamic-tool decision offered remembered permission: %+v", options)
		}
	}
}

func TestDynamicBashApprovalOptionsUseExactSessionLiteral(t *testing.T) {
	const command = "git status $(touch /tmp/reasonix-dynamic-approval)"
	options := approvalOptions("bash", command, false)
	if len(options) != 3 || options[1].Kind != OptAllowAlways {
		t.Fatalf("dynamic Bash options = %+v, want ordinary options with session grant", options)
	}
	want := "Bash=" + command
	if !strings.Contains(options[1].Name, want) {
		t.Fatalf("dynamic Bash session option = %q, want exact rule %q", options[1].Name, want)
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
