package agent

import (
	"context"
	"encoding/json"
	"strings"
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

func TestExecuteOneRedactsToolResultBeforeHistory(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(secretOutputTool{})
	a := &Agent{tools: reg, session: NewSession("")}

	outcome := a.executeOne(context.Background(), provider.ToolCall{ID: "call_1", Name: "secret_output", Arguments: `{}`})
	if strings.Contains(outcome.output, "sk-real-secret-value-123456") {
		t.Fatalf("tool outcome leaked raw secret:\n%s", outcome.output)
	}
	if !strings.Contains(outcome.output, "DEEPSEEK_API_KEY=sk-rea") {
		t.Fatalf("tool outcome missing masked marker:\n%s", outcome.output)
	}
}
