package agent

import (
	"context"
	"encoding/json"
	"testing"

	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

type secretOutputTool struct{}

func (secretOutputTool) Name() string            { return "secret_output" }
func (secretOutputTool) Description() string     { return "" }
func (secretOutputTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (secretOutputTool) ReadOnly() bool          { return true }
func (secretOutputTool) Execute(context.Context, json.RawMessage) (string, error) {
	return "DEEPSEEK_API_KEY=sk-real-secret-value-123456\n", nil
}

func TestExecuteOnePreservesToolResultBeforeHistory(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(secretOutputTool{})
	a := &Agent{tools: reg, session: NewSession("")}

	outcome := a.executeOne(context.Background(), provider.ToolCall{ID: "call_1", Name: "secret_output", Arguments: `{}`})
	const want = "DEEPSEEK_API_KEY=sk-real-secret-value-123456\n"
	if outcome.output != want {
		t.Fatalf("tool outcome = %q, want byte-preserving output %q", outcome.output, want)
	}
}
