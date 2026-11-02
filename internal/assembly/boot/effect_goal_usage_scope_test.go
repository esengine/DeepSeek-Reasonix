package boot

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
)

type goalUsageScopeProvider struct {
	calls    atomic.Int32
	holdFrom atomic.Int32
	entered  chan struct{}
}

func (*goalUsageScopeProvider) Name() string { return "goal-usage-scope" }
func (p *goalUsageScopeProvider) Stream(ctx context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	n := p.calls.Add(1)
	if from := p.holdFrom.Load(); from > 0 && n >= from {
		close(p.entered)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	ch := make(chan provider.Chunk, 3)
	if n == 1 {
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: browserCall("delegate", "task", map[string]any{"prompt": "read fixture", "profile": "usage-probe", "model": "child", "tools": []string{"read_file", "read_only_task"}, "max_steps": 8, "run_in_background": true})}
	} else {
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "Background task started."}
	}
	ch <- provider.Chunk{Type: provider.ChunkUsage, Usage: &provider.Usage{PromptTokens: 1000, CompletionTokens: 100, RequestCount: 1}}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func TestEffectDelegatedUsageKeepsGoalOwner(t *testing.T) {
	for _, mode := range []string{"chat_to_goal", "replaced_goal", "nested_chat_to_goal", "same_goal", "completed_before_goal"} {
		t.Run(mode, func(t *testing.T) {
			home, root := statedBootHome(t), t.TempDir()
			writeFile(t, home, "config.toml", "[agent]\nmax_subagent_depth = 3\n")
			writeFile(t, home, "skills/usage-probe/SKILL.md", "---\nname: usage-probe\ndescription: Read a fixture\nrunAs: subagent\nread-only: true\n---\nRead fixture.txt.\n")
			unblock := make(chan struct{})
			defer func() {
				select {
				case <-unblock:
				default:
					close(unblock)
				}
			}()
			parent := &goalUsageScopeProvider{entered: make(chan struct{})}
			read := func() *provider.ToolCall {
				return browserCall("read", "read_file", map[string]any{"path": "fixture.txt"})
			}
			child := &rootTokenEffectProvider{wait: unblock, first: read}
			grandchild := &rootTokenEffectProvider{first: read}
			if mode == "nested_chat_to_goal" {
				child.first = func() *provider.ToolCall {
					return browserCall("nested", "read_only_task", map[string]any{"prompt": "read fixture", "model": "grandchild", "tools": []string{"read_file"}, "max_steps": 8})
				}
			}
			kind := uniqueKind("goal-usage-scope")
			provider.Register(kind, func(cfg provider.Config) (provider.Provider, error) {
				switch cfg.Model {
				case "child":
					return child, nil
				case "grandchild":
					return grandchild, nil
				default:
					return parent, nil
				}
			})
			writeFile(t, root, "fixture.txt", "old job usage fixture")
			writeFile(t, root, "reasonix.toml", `default_model = "parent"
[agent]
task_token_budget = 3000
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
[[providers]]
name = "grandchild"
kind = "`+kind+`"
model = "grandchild"
`)
			approveWorkspaceAt(t, home, root)
			var reads, childUsage atomic.Int32
			ctrl, err := Build(t.Context(), Options{Home: home, WorkspaceRoot: root, Sink: event.FuncSink(func(e event.Event) {
				if e.Kind == event.ToolResult && e.Tool.Name == "read_file" && e.Tool.Err == "" && strings.Contains(e.Tool.Output, "old job usage fixture") {
					reads.Add(1)
				}
				if e.Kind == event.Usage && e.UsageSource == event.UsageSourceSubagent && e.Usage != nil {
					childUsage.Add(int32(e.Usage.PromptTokens + e.Usage.CompletionTokens))
				}
			})})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(ctrl.Close)
			ctrl.EnsureSessionPath()
			ctrl.SetToolApprovalMode(control.ToolApprovalYolo)
			await := func(ch <-chan struct{}, label string) {
				t.Helper()
				select {
				case <-ch:
				case <-time.After(20 * time.Second):
					t.Fatalf("%s did not finish", label)
				}
			}
			startHeld := func() (context.CancelFunc, <-chan error) {
				ctx, cancel := context.WithCancel(t.Context())
				done := make(chan error, 1)
				go func() { done <- ctrl.RunTurn(ctx, "investigate goal fixture") }()
				await(parent.entered, "goal admission")
				return cancel, done
			}
			var cancel context.CancelFunc
			var done <-chan error
			if mode == "same_goal" || mode == "replaced_goal" {
				ctrl.SetGoal("original goal")
				parent.holdFrom.Store(2)
				cancel, done = startHeld()
				if mode == "replaced_goal" {
					cancel()
					<-done
					parent.entered = make(chan struct{})
				}
			} else {
				if err := ctrl.Run(t.Context(), "start background task"); err != nil {
					t.Fatal(err)
				}
				if parent.calls.Load() != 2 {
					t.Fatalf("initial parent calls=%d", parent.calls.Load())
				}
				parent.holdFrom.Store(3)
			}
			jobDone := make(chan struct{})
			ctrl.JobManager().AfterUnfinished(func() { close(jobDone) })
			if mode == "completed_before_goal" {
				close(unblock)
				await(jobDone, "old job")
			}
			if mode != "same_goal" {
				ctrl.SetGoal("replacement goal")
				cancel, done = startHeld()
			}
			defer func() { cancel(); <-done }()
			before := ctrl.GoalRuntime()
			if mode != "completed_before_goal" {
				close(unblock)
				await(jobDone, "old job")
			}
			after := ctrl.GoalRuntime()
			wantTokens, wantRequests := 0, 0
			if mode == "same_goal" {
				wantTokens, wantRequests = 2200, 2
			}
			if after.TokensUsed-before.TokensUsed != wantTokens || after.RequestsUsed-before.RequestsUsed != wantRequests {
				t.Fatalf("goal before=%+v after=%+v want delta %d/%d", before, after, wantTokens, wantRequests)
			}
			wantUsage := int32(2200)
			if mode == "nested_chat_to_goal" {
				wantUsage = 4400
				if grandchild.calls.Load() != 2 {
					t.Fatalf("grandchild calls=%d", grandchild.calls.Load())
				}
			}
			if reads.Load() != 1 || child.calls.Load() != 2 || childUsage.Load() != wantUsage {
				t.Fatalf("child reads/calls/usage=%d/%d/%d want 1/2/%d", reads.Load(), child.calls.Load(), childUsage.Load(), wantUsage)
			}
		})
	}
}
