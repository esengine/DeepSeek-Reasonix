package control

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/surfacespec"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/runtime/usecap"
	"reasonix/internal/state/sessionstore"
	"testing"
)

func TestSurfaceActionQueueRetryWithdrawReplay(t *testing.T) {
	for _, proxy := range []bool{false, true} {
		for _, format := range []string{"plain", "object", "string"} {
			name := "direct/"
			if proxy {
				name = "proxy/"
			}
			t.Run(name+format, func(t *testing.T) {
				testSurfaceActionQueueRetryWithdrawReplay(t, proxy, format)
			})
		}
	}
}

func testSurfaceActionQueueRetryWithdrawReplay(t *testing.T, proxy bool, format string) {
	t.Helper()
	const spec = `{"spec_version":1,"body":[{"kind":"editable_text","key":"draft","label":"Draft","field":{"type":"string","required":true}},{"kind":"button","label":"Continue","actionId":"continue"}]}`
	registry := tool.NewRegistry()
	registry.Add(agent.NewRenderSurfaceTool())
	call := provider.ToolCall{ID: "call-1", Name: surfacespec.ToolName, Arguments: spec}
	switch format {
	case "object":
		call.Arguments = `{"arguments":` + spec + `}`
	case "string":
		quoted, err := json.Marshal(spec)
		if err != nil {
			t.Fatal(err)
		}
		call.Arguments = `{"arguments":` + string(quoted) + `}`
	}
	if proxy {
		registry.Add(usecap.NewUseCapabilityTool(context.Background(), nil, nil, registry, nil, nil, nil))
		call.Name = "use_capability"
		call.ResolvedName = surfacespec.ToolName
		call.Arguments = `{"action":"call","capability_id":"tool:render_surface","arguments":` + call.Arguments + `}`
	}
	target, _ := registry.Get(call.Name)
	effective, changed := tool.PrepareArguments(target, json.RawMessage(call.Arguments))
	if changed != (format != "plain") {
		t.Fatalf("normalization changed=%v for %s", changed, format)
	}
	if changed {
		call.ArgumentNormalization = &provider.ArgumentNormalization{
			Reason: provider.ArgumentEnvelopeUnwrapped, Original: call.Arguments, Effective: string(effective),
		}
	}
	sess := sessionstore.NewSession("system")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "task"})
	sess.Add(provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{call}})
	sess.Add(provider.Message{Role: provider.RoleTool, ToolCallID: "call-1", Content: `{"surface_id":"s","nonblocking":true}`})
	path := filepath.Join(testenv.TempDir(t), "session.jsonl")
	exec := agent.New(nil, registry, sess, agent.Options{}, event.Discard)
	c := New(Options{Registry: registry, Executor: exec, Runner: exec, SessionPath: path, Sink: event.Discard})
	defer c.Close()
	if err := c.SetInboxPaused(true); err != nil {
		t.Fatal(err)
	}
	parsed, _ := surfacespec.Parse([]byte(spec))
	if err := c.PublishNativeSurface("call-1", parsed.ID()); err != nil {
		t.Fatal(err)
	}
	state, err := c.SurfaceActionState("call-1")
	if err != nil {
		t.Fatal(err)
	}
	req := SurfaceActionRequest{Action: surfacespec.Action{SurfaceID: state.Action.SurfaceID, SessionID: state.Action.SessionID, CallID: "call-1", ActionID: "continue", RequestID: "retry-1", Values: json.RawMessage(`{"draft":"  @missing /command\n"}`)}, Delivery: "queue"}
	badValues := req
	badValues.Action.RequestID = "invalid-values"
	badValues.Action.Values = json.RawMessage(`{"draft":false}`)
	if _, err := c.SubmitSurfaceAction(badValues, nil); !errors.Is(err, surfacespec.ErrValueInvalid) {
		t.Fatalf("invalid field value: %v", err)
	}
	rec, err := c.SubmitSurfaceAction(req, nil)
	if err != nil {
		t.Fatal(err)
	}
	duplicate, err := c.SubmitSurfaceAction(req, nil)
	if err != nil || duplicate.ItemID != rec.ItemID || !duplicate.Idempotent {
		t.Fatalf("retry: %+v %v", duplicate, err)
	}
	_, env, err := c.ReadInboxItem(rec.ItemID)
	if err != nil || len(env.ReferenceErrors) != 0 || env.FrozenRefBlock != "" {
		t.Fatalf("interpreted field data: %+v %v", env, err)
	}
	if _, _, _, err := applyInboxReferences(env); err != nil {
		t.Fatal(err)
	}
	state, err = c.SurfaceActionState("call-1")
	if err != nil || state.Status != "accepted" {
		t.Fatalf("state: %+v %v", state, err)
	}
	bad := req
	bad.Action.SessionID = "other"
	if _, err = c.SubmitSurfaceAction(bad, nil); !errors.Is(err, surfacespec.ErrExpired) {
		t.Fatalf("cross-session: %v", err)
	}
	if err = c.DeleteInboxItem(rec.ItemID); err != nil {
		t.Fatal(err)
	}
	state, err = c.SurfaceActionState("call-1")
	if err != nil || state.Status != "withdrawn" {
		t.Fatalf("withdraw: %+v %v", state, err)
	}
	loaded, err := sessionstore.LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	exec.SetSession(loaded)
	found := false
	for _, message := range loaded.Snapshot() {
		for _, restored := range message.ToolCalls {
			if restored.ID == call.ID {
				found = true
				if restored.Arguments != call.Arguments || restored.ExecutionArguments() != call.ExecutionArguments() {
					t.Fatalf("history lost original/effective arguments: %+v", restored)
				}
			}
		}
	}
	if !found {
		t.Fatal("history lost the surface call")
	}
	state, err = c.SurfaceActionState("call-1")
	if err != nil || state.Status != "withdrawn" {
		t.Fatalf("replay: %+v %v", state, err)
	}
	if state.Active {
		t.Fatal("cold history regained active controls")
	}
	if err := c.PublishNativeSurface("call-1", parsed.ID()); err != nil {
		t.Fatal(err)
	}
	req.Action.RequestID = "new-after-withdraw"
	next, err := c.SubmitSurfaceAction(req, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.CancelWithInboxItems([]string{next.ItemID}, "http"); err != nil {
		t.Fatal(err)
	}
	state, err = c.SurfaceActionState("call-1")
	if err != nil || state.Status != "withdrawn" {
		t.Fatalf("cancel: %+v %v", state, err)
	}

}
