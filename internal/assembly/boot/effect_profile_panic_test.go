package boot

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/contract/agentgraph"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
)

type profilePanicProvider struct{ calls atomic.Int32 }

func (*profilePanicProvider) Name() string { return "profile-panic" }
func (p *profilePanicProvider) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	p.calls.Add(1)
	panic("provider callback failed")
}

func TestEffectProfilePanicGraphIsFailed(t *testing.T) {
	for _, background := range []bool{false, true} {
		for _, dynamic := range []bool{false, true} {
			t.Run(fmt.Sprintf("background=%t/capability=%t", background, dynamic), func(t *testing.T) {
				home, root := statedBootHome(t), t.TempDir()
				child := &profilePanicProvider{}
				parent := &browserScriptProvider{rounds: []func(string) *provider.ToolCall{
					func(string) *provider.ToolCall {
						args := map[string]any{"prompt": "exercise a provider failure", "model": "child", "run_in_background": background}
						if dynamic {
							return browserCall("panic-task", "use_capability", map[string]any{
								"action": "call", "capability_id": "tool:task", "arguments": args,
							})
						}
						return browserCall("panic-task", "task", args)
					},
				}}
				kind := uniqueKind("profile-panic")
				provider.Register(kind, func(cfg provider.Config) (provider.Provider, error) {
					if cfg.Model == "child" {
						return child, nil
					}
					return parent, nil
				})
				writeFile(t, root, "reasonix.toml", `default_model = "parent"
[codegraph]
enabled = false
[[providers]]
name = "parent"
kind = "`+kind+`"
model = "parent"
[[providers]]
name = "child"
kind = "`+kind+`"
model = "child"
`)
				approveWorkspaceAt(t, home, root)
				var mu sync.Mutex
				var graph agentgraph.Graph
				sink := event.FuncSink(func(e event.Event) {
					if e.Kind == event.GraphDelta && e.Graph != nil {
						mu.Lock()
						graph.Apply(*e.Graph)
						mu.Unlock()
					}
				})
				ctrl, err := Build(t.Context(), Options{Home: home, WorkspaceRoot: root, Sink: sink})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(ctrl.Close)
				ctrl.SetToolApprovalMode(control.ToolApprovalYolo)
				func() {
					defer func() { _ = recover() }()
					_ = ctrl.Run(t.Context(), "Delegate this work")
				}()
				done := make(chan struct{})
				ctrl.JobManager().AfterUnfinished(func() { close(done) })
				select {
				case <-done:
				case <-time.After(5 * time.Second):
					t.Fatal("background managed closure did not finish")
				}
				if child.calls.Load() != 1 {
					t.Fatalf("child provider calls = %d, want one", child.calls.Load())
				}
				mu.Lock()
				defer mu.Unlock()
				workers := 0
				for _, node := range graph.Nodes {
					if node.Kind == agentgraph.KindWorker {
						workers++
						if node.State != agentgraph.StateFailed {
							t.Errorf("real boot child %s ended as %s, want failed", node.ID, node.State)
						}
					}
				}
				if workers != 1 {
					t.Errorf("worker nodes = %d, want one", workers)
				}
			})
		}
	}
}
