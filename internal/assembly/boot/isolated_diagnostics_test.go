package boot

import (
	"testing"

	"reasonix/internal/contract/event"
)

func TestIsolatedDiagnosticsPreserveHostHookFailuresWithoutModelReplay(t *testing.T) {
	var forwarded []event.Event
	sink := isolatedUsage{event.FuncSink(func(e event.Event) { forwarded = append(forwarded, e) })}
	for _, e := range []event.Event{
		{Kind: event.Notice, Code: event.NoticeCodeHookFailed},
		{Kind: event.Usage}, {Kind: event.ToolResult}, {Kind: event.Notice, Code: "unrelated"},
	} {
		sink.Emit(e)
	}
	if len(forwarded) != 2 || forwarded[0].Code != event.NoticeCodeHookFailed || forwarded[0].Source != event.UsageSourceSubagent || forwarded[1].UsageSource != event.UsageSourceSubagent {
		t.Fatalf("unexpected isolated event projection: %+v", forwarded)
	}
}
