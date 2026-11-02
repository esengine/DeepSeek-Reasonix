package control

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/ext/skill"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/runtime/agent/testutil"
	"reasonix/internal/state/sessionstore"
)

func TestProfileRootBudgetLifetime(t *testing.T) {
	for _, mode := range []string{"new_task", "same_scope", "new_scope", "reset", "new_session", "inherited", "context_zero"} {
		t.Run(mode, func(t *testing.T) {
			parent := agent.New(nil, tool.NewRegistry(), sessionstore.NewSession("parent"), agent.Options{TaskBudget: agent.TaskBudget{Tokens: 1500}}, event.Discard)
			var reads int
			var oldContext context.Context
			runner := func(ctx context.Context, _ skill.Skill, task string, _ skill.SubagentRunOptions) (string, error) {
				oldContext = ctx
				reg := tool.NewRegistry()
				reg.Add(fakeControlTool{name: "read_file"})
				var turns []testutil.Turn
				for i := range 3 {
					turns = append(turns, testutil.Turn{ToolCalls: []provider.ToolCall{{ID: fmt.Sprintf("read-%d", i), Name: "read_file", Arguments: `{}`}}, Usage: &provider.Usage{PromptTokens: 1000, CompletionTokens: 100, RequestCount: 1}})
				}
				turns = append(turns, testutil.Turn{Text: "done"})
				child := agent.New(testutil.NewMock("child", turns...), reg, sessionstore.NewSession("child"), agent.Options{}, event.FuncSink(func(e event.Event) {
					if e.Kind == event.ToolResult && e.Tool.Name == "read_file" && e.Tool.Err == "" {
						reads++
					}
				}))
				err := child.Run(ctx, task)
				if _, paused := agent.InspectRunPause(err); paused {
					err = nil
				}
				return "done", err
			}
			dir := t.TempDir()
			c := New(Options{Runner: parent, Executor: parent, SkillRunner: runner, SessionDir: dir, SessionPath: filepath.Join(dir, "s.jsonl"), Skills: []skill.Skill{{Name: "probe", RunAs: skill.RunSubagent}}})
			t.Cleanup(c.Close)
			ctx := t.Context()
			if mode != "new_task" && mode != "context_zero" {
				ctx = agent.WithDeliveryExecutionScope(ctx, agent.DeliveryExecutionScope{ID: "one", TaskText: "read"})
			}
			if mode == "inherited" {
				owner := agent.New(nil, tool.NewRegistry(), sessionstore.NewSession("outer"), agent.Options{TaskBudget: agent.TaskBudget{Tokens: 1500}}, event.Discard)
				ctx = owner.PrepareRootTaskBudget(ctx)
			}
			if mode == "context_zero" {
				ctx = agent.WithTaskBudget(ctx, agent.TaskBudget{})
			}
			if _, err := c.RunSubagentProfile(ctx, "probe", "read", false); err != nil {
				t.Fatal(err)
			}
			first := reads
			wantFirst, wantSecond := 2, 2
			if mode == "same_scope" || mode == "inherited" {
				wantSecond = 1
			}
			switch mode {
			case "new_scope":
				ctx = agent.WithDeliveryExecutionScope(ctx, agent.DeliveryExecutionScope{ID: "two", TaskText: "next"})
			case "reset":
				parent.ResetTaskBudget()
			case "new_session":
				parent.SetSession(sessionstore.NewSession("new"))
			case "context_zero":
				wantFirst, wantSecond = 3, 3
			}
			old := oldContext
			if _, err := c.RunSubagentProfile(ctx, "probe", "next", false); err != nil {
				t.Fatal(err)
			}
			if first != wantFirst || reads-first != wantSecond {
				t.Fatalf("reads=%d/%d want=%d/%d", first, reads-first, wantFirst, wantSecond)
			}
			if mode == "reset" {
				// A projected managed job has independent cancellation but retains its old spend.
				jobCtx := agent.InheritRootTokenBudget(t.Context(), old)
				before := reads
				if _, err := runner(jobCtx, skill.Skill{}, "old job", skill.SubagentRunOptions{}); err != nil {
					t.Fatal(err)
				}
				if reads-before != 1 {
					t.Fatalf("old job reads=%d", reads-before)
				}
			}
		})
	}
}

func TestProfileBudgetPreparationUsesTurnAdmission(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	parent := agent.New(nil, tool.NewRegistry(), sessionstore.NewSession("parent"), agent.Options{}, event.Discard)
	c := New(Options{Runner: parent, Executor: parent, Skills: []skill.Skill{{Name: "probe", RunAs: skill.RunSubagent}}, SkillRunner: func(ctx context.Context, _ skill.Skill, _ string, _ skill.SubagentRunOptions) (string, error) {
		if calls.Add(1) != 1 {
			return "unexpected concurrent run", nil
		}
		close(entered)
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-release:
			return "released", nil
		}
	}})
	t.Cleanup(c.Close)
	defer close(release)
	done := make(chan error, 1)
	go func() { _, err := c.RunSubagentProfile(t.Context(), "probe", "first", false); done <- err }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("runner did not enter")
	}
	_, second := c.RunSubagentProfile(t.Context(), "probe", "second", false)
	if !errors.Is(second, ErrTurnRunning) || calls.Load() != 1 {
		t.Errorf("second=%v calls=%d", second, calls.Load())
	}
	c.Cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("cancel=%v", err)
		}
	case <-time.After(5 * time.Second):
		t.Error("controller cancellation did not reach profile")
	}
}
