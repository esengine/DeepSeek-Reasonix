package delegation

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/writeclaim"
	"reasonix/internal/state/workspacelease"
	"reasonix/internal/tools/builtin"
)

func TestOptimisticTaskRespectsSiblingWriteClaim(t *testing.T) {
	root, locks := t.TempDir(), t.TempDir()
	path := filepath.Join(root, "shared.txt")
	if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	sched := writeclaim.NewSubagentScheduler(0, 0)
	claim, err := writeclaim.NormalizeWritePaths(root, []string{path})
	if err != nil {
		t.Fatal(err)
	}
	release, err := sched.Acquire(context.Background(), writeclaim.AcquireRequest{Writer: true, WritePaths: claim, Label: "sibling writer"})
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	owner, err := workspacelease.New(root, locks, nil, workspacelease.WithoutWholeWorkspaceHold())
	if err != nil {
		t.Fatal(err)
	}
	owner.BeginRun()
	defer owner.EndRun()
	if err := owner.AcquirePaths(context.Background(), []string{path}); err != nil {
		t.Fatal(err)
	}
	reg := tool.NewRegistry()
	for _, writer := range builtin.ConfineWriters([]string{root}, builtin.SessionDataGuard{}, builtin.ManagedConfigPaths{}) {
		reg.Add(writer)
	}
	args, _ := json.Marshal(map[string]string{"path": path, "content": "optimistic child wrote"})
	p := &mockProvider{name: "audit", streams: [][]provider.Chunk{
		{{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "write", Name: "write_file", Arguments: string(args)}}, {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
	}}
	task := NewTaskToolWithOptions(TaskToolOptions{Provider: p, ParentRegistry: reg, MaxSteps: 20, SysPrompt: "sys", CheckTargetAccess: allowTestFileAccess}).
		WithTranscripts(NewSubagentStore(t.TempDir()), root, "base-model", "base-effort").
		WithScheduler(sched).WithWorkspaceLease(owner).WithOptimisticClaims(true)
	out, err := task.Execute(testTaskContext(), json.RawMessage(`{"prompt":"write shared.txt"}`))
	if err != nil {
		t.Fatalf("task: %v; %s", err, out)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "original" {
		t.Fatalf("sibling-owned file changed: content=%q; result=%s", got, out)
	}
	if len(sched.ActiveWriterClaims()) != 1 {
		t.Fatal("sibling claim was not retained")
	}

}

func TestOptimisticRegistryPreservesOpaqueTools(t *testing.T) {
	parent := tool.NewRegistry()
	for _, name := range []string{"bash", "deploy_release"} {
		parent.Add(fakeTool{name: name})
	}
	task := newTestTaskTool(t, nil, parent, "sys", "", "", nil).WithOptimisticClaims(true)
	claims, err := task.UndeclaredWriteClaim()
	if err != nil {
		t.Fatal(err)
	}
	spec := ProfileExecSpec{Grant: CapabilityGrant{WritePaths: claims}}
	reg, err := task.childRegistry(&spec, 1, writeclaim.NewWriteGrant(claims), "")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"bash", "deploy_release"} {
		if _, ok := reg.Get(name); !ok {
			t.Fatalf("optimistic registry lost %s", name)
		}
	}
}
