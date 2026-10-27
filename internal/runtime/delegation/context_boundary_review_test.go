package delegation

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent"
)

func TestNestedTaskWithoutSelectionDoesNotInheritParentResults(t *testing.T) {
	prov := &mockProvider{name: "sub", chunks: []provider.Chunk{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}}}
	reg := tool.NewRegistry()
	reg.Add(fakeReadFileTool{})
	task := newTestTaskTool(t, prov, reg, "sys", "", "", nil).WithTranscripts(mustSubagentStore(t), t.TempDir(), "m", "")
	ctx := agent.WithSubagentDepth(withTaskContext(context.Background(), ContextRequest{Upstream: []UpstreamResult{{ID: "private", Ref: "sa_parent", Answer: "UNSELECTED-PARENT-CONTEXT"}}}), 1)
	if _, err := task.Execute(ctx, json.RawMessage(`{"prompt":"independent task"}`)); err != nil {
		t.Fatal(err)
	}
	for _, message := range prov.requests[0].Messages {
		if strings.Contains(message.Content, "UNSELECTED-PARENT-CONTEXT") {
			t.Fatal("unselected parent context leaked into the nested request")
		}
	}
}

func TestNestedForwardUsesSuppliedSnapshotNotUpdatedSource(t *testing.T) {
	task, ctx, _, _ := steerTask(t, SteerLimits{})
	source := testSubagentSpec(t, "source")
	source.ParentSession, source.WorkspaceRoot = agent.ParentSession(ctx), task.workspaceRoot
	run := startedRun(t, task.transcripts, source, said(provider.RoleAssistant, "ORIGINAL-SNAPSHOT"))
	if err := task.transcripts.SaveCompleted(run); err != nil {
		t.Fatal(err)
	}
	run.Release()
	initial := ProfileExecSpec{Context: ContextRequest{ResultRefs: []string{run.Ref}}}
	if err := task.resolveTaskContext(ctx, &initial); err != nil {
		t.Fatal(err)
	}
	continued, err := task.transcripts.PrepareContinue(run.Ref, source)
	if err != nil {
		t.Fatal(err)
	}
	continued.Session.Add(said(provider.RoleAssistant, "NEW-UNSUPPLIED-CONTENT"))
	if err := task.transcripts.SaveCompleted(continued); err != nil {
		t.Fatal(err)
	}
	continued.Release()
	ctx = agent.WithSubagentDepth(withTaskContext(ctx, initial.Context), 1)
	spec := ProfileExecSpec{Context: ContextRequest{ResultRefs: []string{run.Ref}}}
	if err := task.resolveTaskContext(ctx, &spec); err != nil {
		t.Fatal(err)
	}
	if len(spec.Context.Upstream) != 1 || spec.Context.Upstream[0].Answer != "ORIGINAL-SNAPSHOT" {
		t.Fatalf("forwarded %+v", spec.Context.Upstream)
	}
}

func TestRunningTaskIDDoesNotDropSelectedContext(t *testing.T) {
	task, ctx, run, st := steerTask(t, SteerLimits{})
	if _, err := taskCall(t, task, ctx, map[string]any{"task_id": run.Ref, "prompt": "inspect", "context_summary": "SELECTED-SUMMARY"}); err != nil {
		t.Fatal(err)
	}
	if len(st.host) != 1 || !strings.Contains(st.host[0], "SELECTED-SUMMARY") {
		t.Fatalf("context silently lost: %q", st.host)
	}
}

func TestRunningTaskIDCannotForwardAnUnselectedResult(t *testing.T) {
	task, ctx, run, st := steerTask(t, SteerLimits{})
	ctx = agent.WithSubagentDepth(ctx, 1)
	if _, err := taskCall(t, task, ctx, map[string]any{"task_id": run.Ref, "prompt": "inspect", "context_refs": []string{"sa_unselected"}}); err == nil {
		t.Fatal("nested guidance accepted an unsupplied reference")
	}
	if st.calls != 0 {
		t.Fatal("invalid context sent partial guidance")
	}
}

func TestOpeningContextDoesNotBecomeRawUserInputWithoutAParentTurn(t *testing.T) {
	for _, readOnly := range []bool{false, true} {
		t.Run(fmt.Sprint(readOnly), func(t *testing.T) {
			prov := &mockProvider{name: "sub", streams: [][]provider.Chunk{
				{{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "probe", Name: "read_file", Arguments: `{"path":"source"}`}}, {Type: provider.ChunkDone}},
				{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
			}}
			reg := tool.NewRegistry()
			probe := &contextAddressProbe{}
			reg.Add(probe)
			task := newTestTaskTool(t, prov, reg, "sys", "", "", nil).WithTranscripts(mustSubagentStore(t), t.TempDir(), "m", "")
			spec, err := task.buildTaskSpec(context.Background(), "inspect source", "", "", nil, nil, 0, "", "", "", "", false, readOnly)
			if err != nil {
				t.Fatal(err)
			}
			spec.Context.Summary = "https://untrusted.example/context.mp4"
			if _, err := task.RunProfileSpec(context.Background(), spec); err != nil {
				t.Fatal(err)
			}
			if !probe.called {
				t.Fatal("address authority probe never ran")
			}
			if len(probe.addresses) != 0 {
				t.Fatalf("selected data became user address authority: %v", probe.addresses)
			}
		})
	}
}

type contextAddressProbe struct {
	fakeReadFileTool
	called    bool
	addresses []string
}

func (p *contextAddressProbe) Execute(ctx context.Context, _ json.RawMessage) (string, error) {
	p.called = true
	p.addresses = tool.UserAddressesFrom(ctx)
	return "checked", nil
}
