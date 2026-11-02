package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/state/sessionstore"
)

// detailedShell stands in for the bash tool's structured execution, which is
// what the host classifies a command's verification from.
type detailedShell struct{}

func (detailedShell) Name() string            { return "bash" }
func (detailedShell) Description() string     { return "" }
func (detailedShell) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (detailedShell) ReadOnly() bool          { return false }
func (detailedShell) Execute(context.Context, json.RawMessage) (string, error) {
	return "ok", nil
}
func (detailedShell) ExecutionDescriptor(json.RawMessage) *tool.ShellExecution {
	return &tool.ShellExecution{}
}
func (detailedShell) ExecuteDetailed(context.Context, json.RawMessage) (tool.DetailedResult, error) {
	zero := 0
	return tool.DetailedResult{Output: "ok", Execution: &tool.ShellExecution{ExitCode: &zero}}, nil
}

func resultsByCall(reqs []provider.Request) map[string]string {
	out := map[string]string{}
	for _, req := range reqs {
		for _, m := range req.Messages {
			if m.Role == provider.RoleTool {
				out[m.ToolCallID] = m.Content
			}
		}
	}
	return out
}

func TestReadOnlyCommandCarriesNoUncountedCheckNotice(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "write_file", readOnly: false, writesPaths: true})
	reg.Add(detailedShell{})
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{toolCallChunk("w1", "write_file", `{"path":"f.py","content":"x=1"}`), {Type: provider.ChunkDone}},
		{toolCallChunk("r1", "bash", `{"command":"cat f.py"}`), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, sessionstore.NewSession(""), Options{}, event.Discard)
	_ = a.Run(context.Background(), "edit")
	if got := resultsByCall(prov.requests)["r1"]; strings.Contains(got, "not a recognized check") {
		t.Fatalf("a read-only inspection was told it is not a check: %q", got)
	}
}
