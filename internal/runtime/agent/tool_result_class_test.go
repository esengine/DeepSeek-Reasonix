package agent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/state/sessionstore"
)

type scriptedShell struct {
	ex  tool.ShellExecution
	err error
}

func (scriptedShell) Name() string            { return "bash" }
func (scriptedShell) Description() string     { return "" }
func (scriptedShell) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (scriptedShell) ReadOnly() bool          { return false }
func (scriptedShell) Execute(context.Context, json.RawMessage) (string, error) {
	return "", nil
}
func (s scriptedShell) ExecutionDescriptor(json.RawMessage) *tool.ShellExecution {
	return &tool.ShellExecution{}
}
func (s scriptedShell) ExecuteDetailed(context.Context, json.RawMessage) (tool.DetailedResult, error) {
	ex := s.ex
	return tool.DetailedResult{Output: "out", Execution: &ex}, s.err
}

// The host decides a call's class once: a failure carries Err on the live
// event and ToolFailure on the recorded message, and a call that only
// reports a non-completed state, such as a background start, carries neither.
func TestShellResultClassIsHostDecidedForEveryState(t *testing.T) {
	one := 1
	for _, tc := range []struct {
		name   string
		ex     tool.ShellExecution
		err    error
		failed bool
	}{
		{"background started", tool.ShellExecution{State: tool.ShellStateBackgroundStarted}, nil, false},
		{"completed", tool.ShellExecution{State: tool.ShellStateCompleted, ExitCode: new(0)}, nil, false},
		{"exit non-zero", tool.ShellExecution{State: tool.ShellStateFailed, ExitCode: &one}, errors.New("command exited"), true},
		{"timed out", tool.ShellExecution{State: tool.ShellStateTimedOut}, errors.New("command timed out"), true},
		{"cancelled", tool.ShellExecution{State: tool.ShellStateCancelled}, errors.New("context canceled"), true},
		{"not run", tool.ShellExecution{State: tool.ShellStateNotRun}, errors.New("blocked"), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reg := tool.NewRegistry()
			reg.Add(scriptedShell{ex: tc.ex, err: tc.err})
			prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
				{toolCallChunk("c1", "bash", `{"command":"echo hi"}`), {Type: provider.ChunkDone}},
				{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
			}}
			var live event.Tool
			sink := event.FuncSink(func(e event.Event) {
				if e.Kind == event.ToolResult {
					live = e.Tool
				}
			})
			sess := sessionstore.NewSession("")
			a := New(prov, reg, sess, Options{}, sink)
			_ = a.Run(context.Background(), "go")

			if (live.Err != "") != tc.failed {
				t.Errorf("live Err = %q, failed want %v", live.Err, tc.failed)
			}
			var recorded *provider.ToolFailure
			for _, m := range sess.Snapshot() {
				if m.Role == provider.RoleTool && m.ToolCallID == "c1" {
					recorded = m.ToolFailure
				}
			}
			if (recorded != nil) != tc.failed {
				t.Errorf("recorded ToolFailure = %v, failed want %v", recorded, tc.failed)
			}
			if live.Execution == nil || live.Execution.State != tc.ex.State {
				t.Errorf("live execution state = %+v, want %q", live.Execution, tc.ex.State)
			}
		})
	}
}
