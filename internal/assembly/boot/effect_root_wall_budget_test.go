package boot

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/session/control"
)

func TestEffectRootWallBudgetReachesManagedJobs(t *testing.T) {
	for _, mode := range []string{"task", "capability", "fleet"} {
		t.Run(mode, func(t *testing.T) {
			home, root := statedBootHome(t), t.TempDir()
			unblock := make(chan struct{})
			children := []*rootTokenEffectProvider{{wait: unblock}, {wait: unblock}}
			count := 1
			parent := &rootTokenEffectProvider{first: func() *provider.ToolCall {
				args := map[string]any{"prompt": "read the fixture", "model": "child-a", "profile": "explore", "tools": []string{"read_file"}, "max_steps": 8, "run_in_background": true}
				if mode == "fleet" {
					count = 2
					tasks := []map[string]any{}
					for _, model := range []string{"child-a", "child-b"} {
						tasks = append(tasks, map[string]any{"prompt": "read the fixture", "model": model, "read_only": true, "tools": []string{"read_file"}, "max_steps": 8})
					}
					return browserCall("delegate", "use_capability", map[string]any{"action": "call", "capability_id": "tool:fleet", "arguments": map[string]any{"tasks": tasks, "run_in_background": true}})
				}
				if mode == "capability" {
					return browserCall("delegate", "use_capability", map[string]any{"action": "call", "capability_id": "tool:task", "arguments": args})
				}
				return browserCall("delegate", "task", args)
			}}
			kind := uniqueKind("root-wall")
			provider.Register(kind, func(cfg provider.Config) (provider.Provider, error) {
				switch cfg.Model {
				case "child-a":
					return children[0], nil
				case "child-b":
					return children[1], nil
				default:
					return parent, nil
				}
			})
			writeFile(t, root, "fixture.txt", "fixture content")
			writeFile(t, root, "reasonix.toml", `default_model = "parent"
[codegraph]
enabled = false
[[providers]]
name = "parent"
kind = "`+kind+`"
model = "parent"
[[providers]]
name = "child-a"
kind = "`+kind+`"
model = "child-a"
[[providers]]
name = "child-b"
kind = "`+kind+`"
model = "child-b"
`)
			approveWorkspaceAt(t, home, root)
			var reads atomic.Int32
			sink := event.FuncSink(func(e event.Event) {
				if e.Kind == event.ToolResult && e.Tool.Name == "read_file" && e.Tool.Err == "" && strings.Contains(e.Tool.Output, "fixture content") {
					reads.Add(1)
				}
			})
			ctrl, err := Build(t.Context(), Options{Home: home, WorkspaceRoot: root, Sink: sink})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(ctrl.Close)
			ctrl.SetToolApprovalMode(control.ToolApprovalYolo)
			runCtx, cancel := context.WithCancel(agent.WithTaskBudget(t.Context(), agent.TaskBudget{Wall: time.Nanosecond}))
			defer cancel()
			runErr := ctrl.Run(runCtx, "delegate a fixture read")
			cancel()
			close(unblock)
			pause, ok := agent.InspectRunPause(runErr)
			if !ok || pause.Key != "time" || !pause.HostOwned {
				t.Errorf("root ended with %v (%+v), want host-owned wall landing", runErr, pause)
			}
			done := make(chan struct{})
			ctrl.JobManager().AfterUnfinished(func() { close(done) })
			select {
			case <-done:
			case <-time.After(10 * time.Second):
				t.Fatal("managed children did not finish")
			}
			for i, child := range children[:count] {
				if child.calls.Load() != 2 {
					t.Errorf("child%d requests = %d, want one read and one existing finalization round", i, child.calls.Load())
				}
			}
			if reads.Load() != int32(count) || parent.calls.Load() != 2 {
				t.Errorf("completed reads=%d parent requests=%d, want %d/2 after parent cancellation", reads.Load(), parent.calls.Load(), count)
			}
		})
	}
}
