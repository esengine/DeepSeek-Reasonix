package delegation

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/runtime/writeclaim"
	"reasonix/internal/state/sessionstore"
	"strings"
	"sync"
	"testing"
)

type recoveryProbe struct {
	mu            sync.Mutex
	first, second int
}

func TestFleetRecoveryRejectsAnAdoptedResultThatWasContinued(t *testing.T) {
	task, ctx, _, _ := steerTask(t, SteerLimits{})
	ctx = WithExecutionSessionPath(ctx, filepath.Join(t.TempDir(), "parent.jsonl"))
	source := testSubagentSpec(t, "source")
	source.ParentSession = agent.ParentSession(ctx)
	source.WorkspaceRoot = task.workspaceRoot
	run := startedRun(t, task.transcripts, source, said(provider.RoleAssistant, "original"))
	if err := task.transcripts.SaveCompleted(run); err != nil {
		t.Fatal(err)
	}
	run.Release()
	f := NewFleetTool(task)
	r := fleetRecoveryRecord{Tasks: []fleetTaskItem{{AdoptRef: run.Ref}}, Identities: []string{bytesHash([]byte("original"))}}
	if _, err := f.recoveryTasks(ctx, r); err != nil {
		t.Fatal(err)
	}
	next, err := task.transcripts.PrepareContinue(run.Ref, source)
	if err != nil {
		t.Fatal(err)
	}
	next.Session.Add(said(provider.RoleAssistant, "updated"))
	if err := task.transcripts.SaveCompleted(next); err != nil {
		t.Fatal(err)
	}
	next.Release()
	if _, err := f.recoveryTasks(ctx, r); err == nil {
		t.Fatal("adopted result changed without explicit selection")
	}
}

func TestFleetRepeatedCallIDDoesNotOverwriteEarlierRecovery(t *testing.T) {
	root := t.TempDir()
	store := mustSubagentStore(t)
	p := &fleetBarrierProvider{}
	f := NewFleetTool(NewTaskTool(p, nil, tool.NewRegistry(), 20, 0, 0, 0, 0, "", "sys", nil, 0, "", "", nil).WithTranscripts(store, root, "base", "high").WithScheduler(writeclaim.NewSubagentScheduler(2, 2)))
	ctx := WithExecutionSessionPath(agent.WithParentSession(agent.WithCallContext(context.Background(), "repeated", event.Discard, nil, false), "parent"), filepath.Join(t.TempDir(), "parent.jsonl"))
	args := json.RawMessage(`{"tasks":[{"prompt":"one","write_paths":["a"]},{"prompt":"two","depends_on":["1"],"write_paths":["b"]}]}`)
	for range 2 {
		if _, err := f.Execute(ctx, args); err != nil {
			t.Fatal(err)
		}
	}
	out, err := f.listRecovery(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var rows []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(out), &rows); err != nil || len(rows) != 2 {
		t.Fatalf("records=%s %v", out, err)
	}
	groups := map[string]bool{}
	for _, row := range rows {
		lease, err := f.acquireRecovery(ctx, row.ID)
		if err != nil {
			t.Fatal(err)
		}
		groups[lease.record.Attempts[0]] = true
		tasks, err := f.recoveryTasks(ctx, lease.record)
		lease.close()
		if err != nil {
			t.Fatal(err)
		}
		for _, task := range tasks {
			if task.AdoptRef == "" {
				t.Fatal("earlier completion lost")
			}
		}
	}
	if len(groups) != 2 {
		t.Fatal("repeated provider id reused execution identity")
	}
}

func (*recoveryProbe) Name() string { return "recovery-probe" }
func (p *recoveryProbe) Stream(_ context.Context, r provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	second := false
	for _, m := range r.Messages {
		if strings.Contains(m.Content, "SECOND-TASK") {
			second = true
		}
	}
	if second {
		p.second++
		if p.second == 1 {
			return nil, fmt.Errorf("fixture interruption")
		}
	} else {
		p.first++
	}
	ch := make(chan provider.Chunk, 2)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "completed"}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func TestFleetRecoveryReusesCompletedNodeAcrossRuntimeReopen(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(t.TempDir(), "parent.jsonl")
	p := &recoveryProbe{}
	store := mustSubagentStore(t)
	newFleet := func() *FleetTool {
		return NewFleetTool(NewTaskTool(p, nil, tool.NewRegistry(), 20, 0, 0, 0, 0, "", "sys", nil, 0, "", "", nil).WithTranscripts(NewSubagentStore(store.dir), root, "base", "high").WithScheduler(writeclaim.NewSubagentScheduler(2, 2)))
	}
	ctx := WithExecutionSessionPath(agent.WithParentSession(agent.WithCallContext(context.Background(), "original", event.Discard, nil, false), "parent"), path)
	f := newFleet()
	if _, err := f.Execute(ctx, json.RawMessage(`{"tasks":[{"id":"one","prompt":"FIRST-TASK","write_paths":["a"]},{"id":"two","prompt":"SECOND-TASK https://saved-context.example","depends_on":["one"],"write_paths":["b"]}]}`)); err != nil {
		t.Fatal(err)
	}
	f = newFleet()
	out, err := f.Execute(ctx, json.RawMessage(`{"recovery":{"action":"list"}}`))
	if err != nil {
		t.Fatal(err)
	}
	var saved []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(out), &saved); err != nil || len(saved) != 1 {
		t.Fatalf("list=%s err=%v", out, err)
	}
	args, _ := json.Marshal(map[string]any{"recovery": map[string]string{"action": "resume", "id": saved[0].ID}})
	lease, err := f.acquireRecovery(ctx, saved[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := newFleet().Execute(ctx, args); err == nil {
		t.Fatal("duplicate recovery admitted")
	}
	lease.close()
	f.taskTool.sysPrompt = "changed persona"
	if _, err := f.Execute(ctx, args); err == nil {
		t.Fatal("changed permissions/persona admitted")
	}
	if p.first != 1 || p.second != 1 {
		t.Fatal("refused recovery executed a task")
	}
	f = newFleet()
	if out, err = f.Execute(ctx, args); err != nil || !strings.Contains(out, "Completed fleet") {
		t.Fatalf("resume=%s %v", out, err)
	}
	if p.first != 1 || p.second != 2 {
		t.Fatalf("executions first=%d second=%d", p.first, p.second)
	}
	for _, ref := range subagentRefsIn(out) {
		sess, err := sessionstore.LoadSession(store.sessionPath(ref))
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range sess.Snapshot() {
			if m.Role == provider.RoleUser && strings.Contains(m.Content, "SECOND-TASK") && (m.RawContent == "" || strings.Contains(m.RawContent, "saved-context.example")) {
				t.Fatal("saved prompt became fresh user address authority")
			}
		}
	}
	if _, err = f.Execute(ctx, args); err != nil {
		t.Fatal(err)
	}
	if p.first != 1 || p.second != 2 {
		t.Fatal("completed fleet reran")
	}
	foreign := agent.WithParentSession(ctx, "foreign")
	if _, err = f.Execute(foreign, args); err == nil {
		t.Fatal("foreign owner resumed")
	}
	if _, err = f.Execute(agent.WithSubagentDepth(ctx, 1), args); err == nil {
		t.Fatal("nested recovery admitted")
	}
	lease, err = f.acquireRecovery(ctx, saved[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	lease.record.Version = 999
	if err := lease.save(); err != nil {
		t.Fatal(err)
	}
	lease.close()
	if _, err = f.Execute(ctx, args); err == nil {
		t.Fatal("future record executed")
	}
}
