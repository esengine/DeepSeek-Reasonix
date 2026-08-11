package agent

import (
	"context"
	"encoding/json"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/tool"
)

type typedNilAgentSink struct{}

func (*typedNilAgentSink) Emit(event.Event) {}

type typedNilGate struct{}

func (*typedNilGate) Check(context.Context, string, json.RawMessage, bool) (bool, string, error) {
	return true, "", nil
}

type typedNilHooks struct{}

func (*typedNilHooks) PreToolUse(context.Context, string, json.RawMessage) (bool, string) {
	return false, ""
}
func (*typedNilHooks) PostToolUse(context.Context, string, json.RawMessage, string)               {}
func (*typedNilHooks) PostToolUseFailure(context.Context, string, json.RawMessage, string, error) {}
func (*typedNilHooks) PostLLMCall(context.Context, string, int) string                            { return "" }
func (*typedNilHooks) HasPostLLMCall() bool                                                       { return false }
func (*typedNilHooks) SubagentStop(context.Context, string)                                       {}
func (*typedNilHooks) PreCompact(context.Context, string) string                                  { return "" }

func TestNewNormalizesTypedNilInterfaces(t *testing.T) {
	var sink *typedNilAgentSink
	var gate *typedNilGate
	var hooks *typedNilHooks

	a := New(nil, tool.NewRegistry(), NewSession(""), Options{Gate: gate, Hooks: hooks}, sink)
	a.sink.Emit(event.Event{Kind: event.Text, Text: "typed nil sink should not panic"})
	if a.gate != nil {
		t.Fatal("typed nil gate should be normalized to nil")
	}
	if a.hooks != nil {
		t.Fatal("typed nil hooks should be normalized to nil")
	}
}

func TestSetGateNormalizesTypedNil(t *testing.T) {
	var gate *typedNilGate
	a := New(nil, tool.NewRegistry(), NewSession(""), Options{}, event.Discard)

	a.SetGate(gate)
	if a.gate != nil {
		t.Fatal("typed nil gate should be normalized to nil")
	}
}
