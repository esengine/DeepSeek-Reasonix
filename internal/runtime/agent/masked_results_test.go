package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/base/secrets/secretstest"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/state/sessionstore"
)

type envReader struct{}

func (envReader) Name() string            { return "read_env" }
func (envReader) Description() string     { return "" }
func (envReader) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (envReader) ReadOnly() bool          { return true }
func (envReader) Execute(context.Context, json.RawMessage) (string, error) {
	return "KEY=sk-env-result-0123456789\n", nil
}

// What any tool returns is masked before it is drawn or stored, so a session's
// files hold no value for another command to read.
func TestAToolsResultIsMaskedBeforeItIsKept(t *testing.T) {
	secretstest.Hold("ENV_RESULT", "sk-env-result-0123456789")
	reg := tool.NewRegistry()
	reg.Add(envReader{})
	a := New(nil, reg, sessionstore.NewSession(""), Options{Gate: &stubGate{}}, event.Discard)
	batch := a.executeBatch(context.Background(), &a.turn, []provider.ToolCall{{ID: "c1", Name: "read_env", Arguments: `{}`}})
	if strings.Contains(batch.results[0], "sk-env") || !strings.Contains(batch.results[0], "KEY=[secret:ENV_RESULT]") {
		t.Fatalf("result = %q", batch.results[0])
	}
	if strings.Contains(batch.outcomes[0].output, "sk-env") {
		t.Fatalf("outcome = %q", batch.outcomes[0].output)
	}
}

// A result is masked where it is bounded, before a large one is spilled to the
// sessions folder or handed back to a nested caller, and so is the original
// kept beside a cut.
func TestAResultIsMaskedBeforeItIsSpilledOrKept(t *testing.T) {
	secretstest.Hold("SPILL_KEY", "sk-spill-0123456789")
	a := New(nil, tool.NewRegistry(), sessionstore.NewSession(""), Options{Gate: &stubGate{}}, event.Discard)
	big := strings.Repeat("line\n", 20000) + "KEY=sk-spill-0123456789\n"
	body, _, _ := a.boundToolOutput(big, "grep", "c1", `{}`, false)
	if strings.Contains(body, "sk-spill") {
		t.Fatal("the bounded result kept the value")
	}
	var o toolOutcome
	o.truncMsg, o.bound.Kind = "cut", event.BoundTruncated
	o.keepFull(big)
	if strings.Contains(o.rawOutput, "sk-spill") || strings.Contains(o.unbounded, "sk-spill") || !strings.Contains(o.rawOutput, "[secret:SPILL_KEY]") {
		t.Fatal("the kept original holds the value")
	}
}
