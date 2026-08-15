package acp

import (
	"encoding/json"
	"testing"
	"time"

	"reasonix/internal/event"
)

func TestUpdateSinkWriteAccessOptionsMapScopes(t *testing.T) {
	fn := &fakeNotifier{onReq: func(_ string, params any) (json.RawMessage, error) {
		raw, _ := json.Marshal(params)
		var p PermissionRequestParams
		if err := json.Unmarshal(raw, &p); err != nil {
			t.Fatalf("permission params: %v", err)
		}
		assertACPv1PermissionOptionKinds(t, p.Options)
		if len(p.Options) != 4 ||
			p.Options[0].OptionID != "reasonix_write_once" || p.Options[0].Kind != OptAllowOnce ||
			p.Options[1].OptionID != "reasonix_write_session" || p.Options[1].Kind != OptAllowAlways ||
			p.Options[2].OptionID != "reasonix_write_project" || p.Options[2].Kind != OptAllowAlways ||
			p.Options[3].OptionID != "reasonix_write_deny" || p.Options[3].Kind != OptRejectOnce {
			t.Fatalf("write-access options = %+v", p.Options)
		}
		meta, _ := p.ToolCall.Meta["reasonix.io"].(map[string]any)
		if meta["kind"] != event.ApprovalKindWriteAccess {
			t.Fatalf("meta = %+v", meta)
		}
		res, _ := json.Marshal(PermissionRequestResult{
			Outcome: PermissionOutcome{Outcome: "selected", OptionID: "reasonix_write_project"},
		})
		return res, nil
	}}
	sink := newUpdateSink(fn, "sess-1")
	got := make(chan approveCall, 1)
	sink.bindApprove(func(id string, allow, session, persist bool) { got <- approveCall{id, allow, session, persist} })
	sink.Emit(event.Event{Kind: event.ApprovalRequest, Approval: event.Approval{
		ID: "wa-1", Tool: "bash", Subject: "install", Kind: event.ApprovalKindWriteAccess,
		WriteAccess: event.NormalizeWriteAccessApproval(&event.WriteAccessApproval{
			Directories: []string{"/tmp/out"}, DisplayDirectories: []string{"~/.local"}, Justification: "install tool",
		}),
	}})
	select {
	case c := <-got:
		if c != (approveCall{id: "wa-1", allow: true, session: true, persist: true}) {
			t.Fatalf("approve = %+v", c)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("write-access approve was never called")
	}
}

func TestUpdateSinkWriteAccessLegacyAllowOnce(t *testing.T) {
	fn := &fakeNotifier{onReq: func(_ string, _ any) (json.RawMessage, error) {
		res, _ := json.Marshal(PermissionRequestResult{Outcome: PermissionOutcome{Outcome: "selected", OptionID: string(OptAllowOnce)}})
		return res, nil
	}}
	sink := newUpdateSink(fn, "sess-1")
	got := make(chan approveCall, 1)
	sink.bindApprove(func(id string, allow, session, persist bool) { got <- approveCall{id, allow, session, persist} })
	sink.Emit(event.Event{Kind: event.ApprovalRequest, Approval: event.Approval{
		ID: "wa-2", Tool: "write_file", Kind: event.ApprovalKindWriteAccess,
		WriteAccess: &event.WriteAccessApproval{Directories: []string{"/tmp/out"}},
	}})
	select {
	case c := <-got:
		if c != (approveCall{id: "wa-2", allow: true}) {
			t.Fatalf("legacy allow_once = %+v", c)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("legacy write-access approve was never called")
	}
}
