package agent

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/extension"
	"reasonix/internal/extension/dispatch"
	"reasonix/internal/extension/protocol"
	"reasonix/internal/provider"
)

func TestCompactionPrepareReplaceGuidance(t *testing.T) {
	client := &fakeDispatchClient{interceptFn: func(ev protocol.InterceptEvent, payload json.RawMessage) (protocol.InterceptResult, error) {
		if ev == protocol.EventCompactionPrepare {
			var in dispatch.CompactionPreparePayload
			if err := json.Unmarshal(payload, &in); err != nil {
				return protocol.InterceptResult{}, err
			}
			in.Guidance = "EXTENSION GUIDANCE"
			return replaceWith(t, in), nil
		}
		return protocol.InterceptResult{Decision: protocol.DecisionContinue}, nil
	}}
	d := newExtDispatcher(client, true, nil, extension.PointCompactionPrepare)
	mp, a := newCompactionAgent(t, d)
	telemetry := ""
	a.svc.sink = event.FuncSink(func(e event.Event) {
		if e.Kind == event.Notice && e.Text == "compaction telemetry" {
			telemetry = e.Detail
		}
	})
	if err := a.CompactNow(context.Background(), ""); err != nil {
		t.Fatalf("CompactNow: %v", err)
	}
	for i, req := range mp.requests {
		if instruction := req.Messages[len(req.Messages)-1].Content; !strings.Contains(instruction, "EXTENSION GUIDANCE") {
			t.Fatalf("summarizer call %d of %d missing the replaced guidance:\n%.200q", i+1, len(mp.requests), instruction)
		}
	}
	if sc := joinContents(visibleContext(a)); !strings.Contains(sc, "SUMMARY TEXT") {
		t.Fatalf("projection missing the summary:\n%.200q", sc)
	}
	if n := client.notifyCountFor(protocol.EventCompactionPrepare); n != 1 {
		t.Fatalf("compaction.prepare events = %d, want 1", n)
	}
	if !strings.Contains(telemetry, "summary_input="+SummaryInputCachePrefix) {
		t.Fatalf("telemetry = %q, want cache-prefix summary input", telemetry)
	}
}

func TestCompactionPrepareReplaceMessages(t *testing.T) {
	client := &fakeDispatchClient{interceptFn: func(ev protocol.InterceptEvent, _ json.RawMessage) (protocol.InterceptResult, error) {
		if ev == protocol.EventCompactionPrepare {
			return replaceWith(t, dispatch.CompactionPreparePayload{
				Messages: []protocol.ProviderMessage{{Role: protocol.ProviderRoleUser, Content: "EXTENSION FOLD"}},
			}), nil
		}
		return protocol.InterceptResult{Decision: protocol.DecisionContinue}, nil
	}}
	d := newExtDispatcher(client, true, nil, extension.PointCompactionPrepare)
	mp, a := newCompactionAgent(t, d)
	telemetry := ""
	a.svc.sink = event.FuncSink(func(e event.Event) {
		if e.Kind == event.Notice && e.Text == "compaction telemetry" {
			telemetry = e.Detail
		}
	})
	if err := a.CompactNow(context.Background(), ""); err != nil {
		t.Fatalf("CompactNow: %v", err)
	}
	if got := joinContents(mp.requests[0].Messages); !strings.Contains(got, "EXTENSION FOLD") {
		t.Fatalf("summarizer messages = %.200q, want the replaced fold", got)
	}
	if !strings.Contains(telemetry, "summary_input="+SummaryInputExtensionRewritten) {
		t.Fatalf("telemetry = %q, want extension-rewritten summary input", telemetry)
	}
}

func TestCompactionPrepareGuidanceOnlyPreservesUnrepresentedMessageFields(t *testing.T) {
	client := &fakeDispatchClient{interceptFn: func(ev protocol.InterceptEvent, payload json.RawMessage) (protocol.InterceptResult, error) {
		if ev != protocol.EventCompactionPrepare {
			return protocol.InterceptResult{Decision: protocol.DecisionContinue}, nil
		}
		var in dispatch.CompactionPreparePayload
		if err := json.Unmarshal(payload, &in); err != nil {
			return protocol.InterceptResult{}, err
		}
		in.Guidance = "keep the exact provider replay state"
		return replaceWith(t, in), nil
	}}
	d := newExtDispatcher(client, true, nil, extension.PointCompactionPrepare)
	a := New(&fakeProvider{reply: "summary"}, nil, NewSession("system"), Options{Extensions: d}, event.Discard)
	fold := []provider.Message{
		{
			Role: provider.RoleAssistant, Content: "calling", ReasoningID: "reasoning-1", ReasoningStatus: "completed",
			ResponsesItems: []json.RawMessage{json.RawMessage(`{"type":"reasoning","id":"reasoning-1"}`)},
			ServerSearch:   []provider.ServerSearchCall{{ID: "search-1"}},
			ToolCalls:      []provider.ToolCall{{ID: "call-1", Name: "read", Arguments: `{}`}},
		},
		{Role: provider.RoleTool, Name: "read", ToolCallID: "call-1", Content: "bounded", RawContent: "complete tool result"},
	}
	prepared, reason, err := a.prepareVisibleCompression(context.Background(), CompactionTriggerManual, fold, "", SummaryInputCachePrefix)
	if err != nil || reason != "" {
		t.Fatalf("prepareVisibleCompression: reason=%q err=%v", reason, err)
	}
	if prepared.instructions != "keep the exact provider replay state" || prepared.inputMode != SummaryInputCachePrefix {
		t.Fatalf("prepared metadata = %+v", prepared)
	}
	want := modelInputMessages(fold)
	if !reflect.DeepEqual(prepared.fold, want) {
		t.Fatalf("guidance-only replacement changed fold:\n got=%+v\nwant=%+v", prepared.fold, want)
	}
}
