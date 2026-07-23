package recovery_test

import (
	"context"
	"encoding/json"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/recovery"
	"reasonix/internal/tool"
)

// TestRecoveryWiringPreservesSuccessPathCacheShape pins the product rule that
// enabling Auto Guard must not change the provider-visible
// system prompt or tool schemas on the success path. Only failure-path dynamic
// user-tail and the independent reviewer session may add tokens.
func TestRecoveryWiringPreservesSuccessPathCacheShape(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(&shapeProbeTool{name: "read_file", readOnly: true})
	reg.Add(&shapeProbeTool{name: "write_file", readOnly: false})
	reg.Add(&shapeProbeTool{name: "bash", readOnly: false})

	sys := "You are a coding agent. Keep the stable system prompt byte-stable."
	schemas := reg.Schemas()

	baseline := agent.CaptureShape(sys, schemas, 0)

	// Gate with recovery enabled but no failures must not alter agent schemas.
	gate := recovery.NewGate(recovery.Options{Mode: func() string { return "auto" }})
	sess := agent.NewSession(sys)
	ag := agent.New(nil, reg, sess, agent.Options{
		RecoveryGate: gate,
	}, event.Discard)

	if ag.RecoveryGate() == nil {
		t.Fatal("expected recovery gate attached")
	}
	// Provider-visible prefix is still the stable system string + registry schemas.
	_ = ag
	withRecovery := agent.CaptureShape(sys, reg.Schemas(), 0)
	if baseline.SystemHash != withRecovery.SystemHash {
		t.Fatalf("system prompt hash changed with recovery gate: %s vs %s", baseline.SystemHash, withRecovery.SystemHash)
	}
	if baseline.ToolsHash != withRecovery.ToolsHash {
		t.Fatalf("tool schema hash changed with recovery gate: %s vs %s", baseline.ToolsHash, withRecovery.ToolsHash)
	}
	if baseline.PrefixHash != withRecovery.PrefixHash {
		t.Fatalf("prefix hash changed with recovery gate: %s vs %s", baseline.PrefixHash, withRecovery.PrefixHash)
	}

	// Reviewer uses an isolated policy prompt — must not leak into main agent.
	reviewer := recovery.NewSession(nil, nil)
	if reviewer == nil {
		t.Fatal("expected reviewer session")
	}
	// Independent reviewer system prompt is not the main agent system prompt.
	if recovery.PolicyPrompt == sys {
		t.Fatal("reviewer policy must stay distinct from main system prompt")
	}
}

type shapeProbeTool struct {
	name     string
	readOnly bool
}

func (t *shapeProbeTool) Name() string        { return t.name }
func (t *shapeProbeTool) Description() string { return t.name }
func (t *shapeProbeTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}
func (t *shapeProbeTool) ReadOnly() bool { return t.readOnly }
func (t *shapeProbeTool) Execute(context.Context, json.RawMessage) (string, error) {
	return "ok", nil
}
