package agent

import (
	"context"
	"encoding/json"
	"testing"

	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

type unknownEffectTestTool struct{}

func (unknownEffectTestTool) Name() string            { return "unknown_effect" }
func (unknownEffectTestTool) Description() string     { return "fixture" }
func (unknownEffectTestTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (unknownEffectTestTool) ReadOnly() bool          { return false }
func (unknownEffectTestTool) Execute(context.Context, json.RawMessage) (string, error) {
	return "must not run", nil
}

func TestUnknownRecoveryBlocksGenericSideEffectByCanonicalCall(t *testing.T) {
	call := provider.ToolCall{ID: "call-1", Name: "unknown_effect", Arguments: `{"target":"x"}`}
	turn := &turnRuntime{unknownRecovery: map[string]provider.ToolCall{writeRecoveryKey(call): call}}
	got, handled := recoverPreviousUnknown(turn, call, unknownEffectTestTool{})
	if !handled || !got.blocked || got.executed || got.output == "" {
		t.Fatalf("unknown recovery outcome=%+v handled=%v", got, handled)
	}
	if _, ok := any(unknownEffectTestTool{}).(tool.EffectVerifier); ok {
		t.Fatal("fixture must remain an unverified side effect")
	}
}
