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

// After a change, a command that runs code but is not a recognized check
// leaves the work unverified and counts as a possible change itself. The host
// says so at that call, once a turn, so the model does not read the new debt as
// its check having run.
func TestUncountedCheckIsExplainedOncePerTurn(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "write_file", readOnly: false, writesPaths: true})
	reg.Add(detailedShell{})
	bash := func(id, command string) []provider.Chunk {
		args, _ := json.Marshal(map[string]string{"command": command})
		return []provider.Chunk{toolCallChunk(id, "bash", string(args)), {Type: provider.ChunkDone}}
	}
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{toolCallChunk("w1", "write_file", `{"path":"f.py","content":"x=1"}`), {Type: provider.ChunkDone}},
		bash("c1", `python -c "import f; assert f.x == 1"`),
		bash("c2", `python check_f.py`),
		bash("c3", `python -m pytest -q`),
		{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, sessionstore.NewSession(""), Options{}, event.Discard)
	_ = a.Run(context.Background(), "edit")

	results := resultsByCall(prov.requests)
	const marker = "not a recognized check"
	if !strings.Contains(results["c1"], marker) || !strings.Contains(results["c1"], "recommended recognized verification commands") {
		t.Fatalf("c1 result = %q, want the uncounted-check notice with the recognized commands", results["c1"])
	}
	if strings.Contains(results["c2"], marker) {
		t.Fatalf("c2 result = %q, want the notice given once a turn", results["c2"])
	}
	if strings.Contains(results["c3"], marker) {
		t.Fatalf("c3 result = %q, want a recognized check to carry no such notice", results["c3"])
	}
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
