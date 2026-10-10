package boot

import (
	"context"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/runtime/agent"
)

// Through the real assembly: a turn that keeps a task list re-derives the
// host's tail onto every request, and that is not the conversation being
// rewritten. Reported as one, the window warns that the prefix moved on every
// turn of every ordinary task.
func TestEffectATaskListTailIsNotReportedAsARewrittenBody(t *testing.T) {
	t.Parallel()
	home := statedBootHome(t)
	dir := testenv.TempDir(t)
	rec := &browserScriptProvider{rounds: []func(string) *provider.ToolCall{
		func(string) *provider.ToolCall {
			return browserCall("todo-1", "todo_write", map[string]any{"todos": []any{
				map[string]any{"content": "look", "status": "in_progress", "step_id": "s1"},
				map[string]any{"content": "answer", "status": "pending", "step_id": "s2"},
			}})
		},
		func(string) *provider.ToolCall {
			return browserCall("done-1", "complete_step", map[string]any{"step_id": "s1", "result": "looked", "evidence": []any{map[string]any{"kind": "manual", "summary": "looked at it"}}})
		},
		func(string) *provider.ToolCall {
			return browserCall("read-1", "read_file", map[string]any{"path": "reasonix.toml"})
		},
	}}
	kind := "boot-tail-probe"
	provider.Register(kind, func(provider.Config) (provider.Provider, error) { return rec, nil })
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"
tool_approval = "yolo"

[agent]
system_prompt = "BASE"

[codegraph]
enabled = false

[[providers]]
name = "test-model"
kind = "`+kind+`"
model = "x"
`)
	approveWorkspaceAt(t, home, dir)
	var mu sync.Mutex
	var diags []*event.CacheDiagnostics
	ctrl, err := Build(context.Background(), Options{Home: home, WorkspaceRoot: dir, Sink: event.FuncSink(func(e event.Event) {
		if e.Kind == event.Usage && e.CacheDiagnostics != nil {
			mu.Lock()
			diags = append(diags, e.CacheDiagnostics)
			mu.Unlock()
		}
	})})
	if err != nil {
		t.Fatal(err)
	}
	if err := ctrl.Run(context.Background(), "do it"); err != nil {
		t.Log(err)
	}
	ctrl.Close()
	reqs := agentRequests(rec.requests())
	if len(reqs) < 3 {
		t.Fatalf("the scripted turn made %d requests", len(reqs))
	}
	tail := reqs[len(reqs)-1].Messages[len(reqs[len(reqs)-1].Messages)-1]
	if !strings.Contains(tail.Content, "[s1]") {
		t.Fatalf("the last request does not end with the task-list tail: %.120q", tail.Content)
	}
	for i := 1; i < len(reqs); i++ {
		prev, cur := agent.CaptureShape("s", nil, 0), agent.CaptureShape("s", nil, 0)
		prev.BodyChain, cur.BodyChain = agent.BodyChain(reqs[i-1].Messages), agent.BodyChain(reqs[i].Messages)
		d := agent.CompareShape(prev, cur, nil, nil)
		if d.BodyChanged || len(d.PrefixChangeReasons) > 0 {
			t.Errorf("request %d reported the body rewritten: carried=%d reasons=%v", i, d.CarriedMessages, d.PrefixChangeReasons)
		}
		if d.CarriedMessages == 0 {
			t.Errorf("request %d carried nothing the previous one sent", i)
		}
	}
	for _, d := range diags {
		if d.PrefixChanged {
			t.Errorf("a usage event reported the prefix moved: %v", d.PrefixChangeReasons)
		}
	}
}
