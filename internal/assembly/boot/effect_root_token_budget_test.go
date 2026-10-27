package boot

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/session/control"
)

type rootTokenEffectProvider struct {
	calls atomic.Int32
	first func() *provider.ToolCall
	wait  <-chan struct{}
}

func (*rootTokenEffectProvider) Name() string { return "root-token-effect" }

func (p *rootTokenEffectProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	if p.wait != nil {
		select {
		case <-p.wait:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	round := p.calls.Add(1)
	ch := make(chan provider.Chunk, 3)
	switch {
	case round == 1 && p.first != nil:
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: p.first()}
	case p.first == nil && len(req.Tools) > 0:
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: browserCall(fmt.Sprintf("read-%d", round), "read_file", map[string]any{"path": "fixture.txt"})}
	default:
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "Read finished."}
	}
	ch <- provider.Chunk{Type: provider.ChunkUsage, Usage: &provider.Usage{PromptTokens: 1000, CacheHitTokens: 900, CacheMissTokens: 100, CompletionTokens: 100, RequestCount: 1}}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func TestEffectRootTokenBudgetReachesDelegatedJobs(t *testing.T) {
	for _, mode := range []string{"foreground", "foreground_capability", "background", "background_capability", "background_fleet"} {
		t.Run(mode, func(t *testing.T) {
			home, root := statedBootHome(t), t.TempDir()
			background := mode == "background" || mode == "background_capability" || mode == "background_fleet"
			unblock := make(chan struct{})
			children := []*rootTokenEffectProvider{{}, {}}
			if background {
				for _, child := range children {
					child.wait = unblock
				}
			}
			count := 1
			parent := &rootTokenEffectProvider{first: func() *provider.ToolCall {
				args := map[string]any{"prompt": "read the fixture", "model": "child-a", "profile": "explore", "tools": []string{"read_file"}, "max_steps": 8, "run_in_background": background}
				if mode == "background_fleet" {
					count = 2
					tasks := []map[string]any{}
					for _, model := range []string{"child-a", "child-b"} {
						tasks = append(tasks, map[string]any{"prompt": "read the fixture", "model": model, "read_only": true, "tools": []string{"read_file"}, "max_steps": 8})
					}
					return browserCall("delegate", "use_capability", map[string]any{"action": "call", "capability_id": "tool:fleet", "arguments": map[string]any{"tasks": tasks, "run_in_background": true}})
				}
				if mode == "foreground_capability" || mode == "background_capability" {
					return browserCall("delegate", "use_capability", map[string]any{"action": "call", "capability_id": "tool:task", "arguments": args})
				}
				return browserCall("delegate", "task", args)
			}}
			kind := uniqueKind("root-tokens")
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
[agent]
task_token_budget = 1500
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
			runCtx, cancel := context.WithCancel(t.Context())
			defer cancel()
			runErr := ctrl.Run(runCtx, "delegate a fixture read")
			cancel()
			close(unblock)
			if !background {
				pause, ok := agent.InspectRunPause(runErr)
				if !ok || pause.Kind != "task_budget" || pause.Key != "token" || !pause.HostOwned {
					t.Errorf("root ended with %v (%+v), want token pause", runErr, pause)
				}
			} else if runErr != nil {
				t.Errorf("background submission parent: %v", runErr)
			}
			done := make(chan struct{})
			ctrl.JobManager().AfterUnfinished(func() { close(done) })
			select {
			case <-done:
			case <-time.After(10 * time.Second):
				t.Fatal("managed child did not finish")
			}
			for i, child := range children[:count] {
				if child.calls.Load() != 2 {
					t.Errorf("child%d requests = %d, want one read and one existing finalization round", i, child.calls.Load())
				}
			}
			if reads.Load() != int32(count) || parent.calls.Load() != 2 {
				t.Errorf("real read results=%d parent requests=%d, want %d/2", reads.Load(), parent.calls.Load(), count)
			}
		})
	}
}
