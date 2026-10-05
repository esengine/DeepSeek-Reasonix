package agent

import (
	"context"
	"reflect"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/state/sessionstore"
)

func modeTestAgent() *Agent {
	sess := &sessionstore.Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "system"},
		{Role: provider.RoleUser, Content: "task"},
	}}
	return New(&fakeProvider{reply: "unused"}, tool.NewRegistry(), sess, Options{ContextWindow: 50_000, ModelRef: "m"}, event.Discard)
}

// The mode rides the request and nothing else: the messages and tools a
// request carries are the same bytes with it on as with it off.
func TestRequestModeRidesTheRequestNotThePrefix(t *testing.T) {
	a := modeTestAgent()
	off, err := a.buildSamplingRequest(context.Background(), CompactionTriggerPressure)
	if err != nil {
		t.Fatalf("buildSamplingRequest: %v", err)
	}
	a.SetRequestMode("pro")
	on, err := a.buildSamplingRequest(context.Background(), CompactionTriggerPressure)
	if err != nil {
		t.Fatalf("buildSamplingRequest: %v", err)
	}
	if off.req.Mode != "" || on.req.Mode != "pro" {
		t.Fatalf("mode off=%q on=%q, want \"\" and pro", off.req.Mode, on.req.Mode)
	}
	if !reflect.DeepEqual(off.req.Messages, on.req.Messages) || !reflect.DeepEqual(off.req.Tools, on.req.Tools) {
		t.Fatal("turning the mode on changed the request's messages or tools")
	}
}

func TestANewConversationStartsWithTheModeOff(t *testing.T) {
	a := modeTestAgent()
	a.SetRequestMode("pro")
	a.SetSession(&sessionstore.Session{})
	if got := a.RequestMode(); got != "" {
		t.Fatalf("mode after SetSession = %q, want off", got)
	}
}
