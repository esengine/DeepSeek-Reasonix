package control

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/diff"
	"reasonix/internal/event"
	"reasonix/internal/hook"
	"reasonix/internal/i18n"
	"reasonix/internal/jobs"
	"reasonix/internal/permission"
	"reasonix/internal/provider"
	"reasonix/internal/skill"
	"reasonix/internal/tool"
)

func subagentReadOnlyRegistry(names ...string) *tool.Registry {
	reg := tool.NewRegistry()
	for _, name := range names {
		reg.Add(fakeControlTool{name: name})
	}
	return reg
}

func TestSubmitRunsSubagentSkillViaRunner(t *testing.T) {
	exec := agent.New(nil, tool.NewRegistry(), agent.NewSession("sys"), agent.Options{}, event.Discard)
	var (
		events  []event.Event
		gotTask string
	)
	done := make(chan struct{})
	c := New(Options{
		Executor: exec,
		Registry: subagentReadOnlyRegistry("read_file"),
		Skills: []skill.Skill{{
			Name:         "scout",
			RunAs:        skill.RunSubagent,
			AllowedTools: []string{"read_file"},
		}},
		SkillRunner: func(ctx context.Context, sk skill.Skill, task string, _ skill.SubagentRunContext) (string, error) {
			if sk.Name != "scout" {
				t.Fatalf("runner skill = %q, want scout", sk.Name)
			}
			gotTask = task
			return "child answer", nil
		},
		Sink: event.FuncSink(func(e event.Event) {
			events = append(events, e)
			if e.Kind == event.TurnDone && e.Subagent != nil {
				close(done)
			}
		}),
	})

	c.Submit(`/scout "chapter one"`)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for slash subagent turn")
	}

	if gotTask != `"chapter one"` {
		t.Fatalf("runner task = %q, want raw slash args", gotTask)
	}

	var runID string
	var sawRunning, sawCompletedMessage, sawDone bool
	for _, e := range events {
		if e.Subagent == nil {
			continue
		}
		if runID == "" {
			runID = e.Subagent.ID
		}
		if e.Subagent.ID != runID {
			t.Fatalf("subagent events should share one run id, got %#v", events)
		}
		switch {
		case e.Kind == event.TurnStarted && e.Subagent.State == event.SubagentRunning:
			sawRunning = true
		case e.Kind == event.Message && e.Subagent.State == event.SubagentCompleted && e.Text == "child answer":
			sawCompletedMessage = true
		case e.Kind == event.TurnDone && e.Subagent.State == event.SubagentCompleted && e.Err == nil:
			sawDone = true
		}
	}
	if !sawRunning || !sawCompletedMessage || !sawDone {
		t.Fatalf("events = %#v, want running start, completed message, and completed turn_done", events)
	}

	hist := c.History()
	if len(hist) != 3 {
		t.Fatalf("history len = %d, want 3", len(hist))
	}
	if hist[1].Role != provider.RoleUser || hist[1].Content != `/scout "chapter one"` {
		t.Fatalf("user history = %#v, want raw slash command", hist[1])
	}
	if hist[2].Role != provider.RoleAssistant || hist[2].Content != "child answer" {
		t.Fatalf("assistant history = %#v, want child answer", hist[2])
	}
}

func TestBackgroundSubagentDoesNotDrainQueuedMemory(t *testing.T) {
	var gotTask string
	done := make(chan struct{})
	c := New(Options{
		Registry: subagentReadOnlyRegistry("read_file"),
		Skills: []skill.Skill{{
			Name:         "scout",
			RunAs:        skill.RunSubagent,
			AllowedTools: []string{"read_file"},
		}},
		SkillRunner: func(_ context.Context, _ skill.Skill, task string, _ skill.SubagentRunContext) (string, error) {
			gotTask = task
			return "child answer", nil
		},
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.TurnDone && e.Subagent != nil {
				close(done)
			}
		}),
	})
	c.QueueMemory(`Saved memory "rmb": user's balance is in RMB`)

	c.Submit("/scout inspect")
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for background subagent")
	}

	if strings.Contains(gotTask, "<memory-update>") || strings.Contains(gotTask, "user's balance is in RMB") {
		t.Fatalf("background subagent task should not drain queued memory, got %q", gotTask)
	}
	parent := c.Compose("parent turn")
	if !strings.Contains(parent, "<memory-update>") || !strings.Contains(parent, "user's balance is in RMB") {
		t.Fatalf("queued memory should remain for the parent turn, got %q", parent)
	}
	if again := c.Compose("again"); strings.Contains(again, "<memory-update>") {
		t.Fatalf("parent Compose should still drain queued memory once, got %q", again)
	}
}

func TestBackgroundSubagentDoesNotDrainCompletedJobs(t *testing.T) {
	manager := jobs.NewManager(event.Discard)
	defer manager.Close()
	job := manager.Start("task", "scan", func(context.Context, io.Writer) (string, error) {
		return "ok", nil
	})
	manager.Wait(context.Background(), []string{job.ID}, 0)

	var gotTask string
	done := make(chan struct{})
	c := New(Options{
		Jobs:     manager,
		Registry: subagentReadOnlyRegistry("read_file"),
		Skills: []skill.Skill{{
			Name:         "scout",
			RunAs:        skill.RunSubagent,
			AllowedTools: []string{"read_file"},
		}},
		SkillRunner: func(_ context.Context, _ skill.Skill, task string, _ skill.SubagentRunContext) (string, error) {
			gotTask = task
			return "child answer", nil
		},
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.TurnDone && e.Subagent != nil {
				close(done)
			}
		}),
	})

	c.Submit("/scout inspect")
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for background subagent")
	}

	if strings.Contains(gotTask, "<background-jobs>") || strings.Contains(gotTask, job.ID) {
		t.Fatalf("background subagent task should not drain completed jobs, got %q", gotTask)
	}
	parent := c.Compose("parent turn")
	if !strings.Contains(parent, "<background-jobs>") || !strings.Contains(parent, job.ID) {
		t.Fatalf("completed job note should remain for the parent turn, got %q", parent)
	}
	if again := c.Compose("again"); strings.Contains(again, "<background-jobs>") {
		t.Fatalf("parent Compose should still drain completed jobs once, got %q", again)
	}
}

func TestSubmitSubagentSkillWithoutRunnerFailsAndDoesNotMergeHistory(t *testing.T) {
	exec := agent.New(nil, tool.NewRegistry(), agent.NewSession("sys"), agent.Options{}, event.Discard)
	done := make(chan event.Event, 1)
	var starts int
	c := New(Options{
		Executor: exec,
		Registry: subagentReadOnlyRegistry("read_file"),
		Skills: []skill.Skill{{
			Name:         "scout",
			RunAs:        skill.RunSubagent,
			AllowedTools: []string{"read_file"},
		}},
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.TurnStarted && e.Subagent != nil {
				starts++
			}
			if e.Kind == event.TurnDone && e.Subagent != nil {
				done <- e
			}
		}),
	})

	c.Submit("/scout inspect")
	var terminal event.Event
	select {
	case terminal = <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for failed subagent turn")
	}

	if starts != 1 {
		t.Fatalf("turn starts = %d, want exactly one subagent start", starts)
	}
	if terminal.Subagent == nil || terminal.Subagent.State != event.SubagentFailed {
		t.Fatalf("terminal subagent = %#v, want failed state", terminal.Subagent)
	}
	if want := "/scout is runAs=subagent but no subagent runner is configured in this session"; terminal.Subagent.Error != want {
		t.Fatalf("terminal error = %q, want %q", terminal.Subagent.Error, want)
	}
	runs := c.ListSubagents()
	if len(runs) != 1 || runs[0].State != event.SubagentFailed || runs[0].Cancelable {
		t.Fatalf("runs = %#v, want one failed retained run", runs)
	}
	detail, ok := c.SubagentDetail(runs[0].ID)
	if !ok || detail.State != event.SubagentFailed || detail.Err != terminal.Subagent.Error || detail.Answer != "" {
		t.Fatalf("detail = %#v ok=%v, want failed retained detail with no answer", detail, ok)
	}
	if hist := c.History(); len(hist) != 1 {
		t.Fatalf("failed subagent should not merge into parent transcript, history=%#v", hist)
	}
}

func TestSubmitSubagentDetailRetainsFullEventText(t *testing.T) {
	exec := agent.New(nil, tool.NewRegistry(), agent.NewSession("sys"), agent.Options{}, event.Discard)
	done := make(chan struct{})
	longArgs := `{"path":"` + strings.Repeat("very-long-path/", 24) + `scene.md"}`
	longText := strings.Repeat("完整内容", 120)
	c := New(Options{
		Executor: exec,
		Registry: subagentReadOnlyRegistry("read_file"),
		Skills: []skill.Skill{{
			Name:         "scout",
			RunAs:        skill.RunSubagent,
			AllowedTools: []string{"read_file"},
		}},
		SkillRunner: func(_ context.Context, _ skill.Skill, _ string, run skill.SubagentRunContext) (string, error) {
			run.Sink.Emit(event.Event{Kind: event.Reasoning, Text: longText})
			run.Sink.Emit(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{Name: "read_file", Args: longArgs}})
			return longText, nil
		},
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.TurnDone && e.Subagent != nil {
				close(done)
			}
		}),
	})

	c.Submit(`/scout check`)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for subagent")
	}

	runs := c.ListSubagents()
	if len(runs) != 1 {
		t.Fatalf("runs = %v, want one run", runs)
	}
	detail, ok := c.SubagentDetail(runs[0].ID)
	if !ok {
		t.Fatal("missing subagent detail")
	}
	var sawReasoning, sawDispatch, sawAnswer bool
	for _, ev := range detail.Events {
		switch ev.Kind {
		case event.Reasoning:
			sawReasoning = ev.Text == longText
		case event.ToolDispatch:
			sawDispatch = ev.Text == longArgs
		case event.Message:
			sawAnswer = ev.Text == longText
		}
	}
	if !sawReasoning || !sawDispatch || !sawAnswer {
		t.Fatalf("detail should retain full event text and answer, events=%#v", detail.Events)
	}
}

func TestSubagentRunsInBackgroundUsesResolvedToolReadOnly(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeControlTool{name: "read_file"})
	reg.Add(fakeControlTool{name: "bash", writer: true})
	c := New(Options{Registry: reg})

	if !c.SubagentRunsInBackground(skill.Skill{
		Name:         "scout",
		RunAs:        skill.RunSubagent,
		AllowedTools: []string{"read_file"},
	}) {
		t.Fatal("read-only subagent should run in the background")
	}
	if c.SubagentRunsInBackground(skill.Skill{
		Name:         "deploy",
		RunAs:        skill.RunSubagent,
		AllowedTools: []string{"bash"},
	}) {
		t.Fatal("writer-capable subagent should be foregrounded")
	}
	if c.SubagentRunsInBackground(skill.Skill{
		Name:         "unknown",
		RunAs:        skill.RunSubagent,
		AllowedTools: []string{"missing"},
	}) {
		t.Fatal("unknown tools should be treated as foreground writer-capable")
	}
	if New(Options{}).SubagentRunsInBackground(skill.Skill{
		Name:         "unresolved",
		RunAs:        skill.RunSubagent,
		AllowedTools: []string{"read_file"},
	}) {
		t.Fatal("subagents should be foregrounded when the tool registry is unavailable")
	}
}

func TestSubmitSubagentSkillForegroundsWriterCapableTools(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeControlTool{name: "bash", writer: true})
	exec := agent.New(nil, tool.NewRegistry(), agent.NewSession("sys"), agent.Options{}, event.Discard)
	var (
		gotRun    skill.SubagentRunContext
		gotTask   string
		parentEnd int
		childEnd  int
	)
	done := make(chan struct{})
	c := New(Options{
		Executor: exec,
		Registry: reg,
		Policy:   permission.New("ask", nil, nil, nil),
		Skills: []skill.Skill{{
			Name:         "deploy",
			RunAs:        skill.RunSubagent,
			AllowedTools: []string{"bash"},
		}},
		SkillRunner: func(_ context.Context, _ skill.Skill, task string, run skill.SubagentRunContext) (string, error) {
			gotTask = task
			gotRun = run
			return "child answer", nil
		},
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.TurnDone {
				if e.Subagent != nil {
					childEnd++
					return
				}
				parentEnd++
				close(done)
			}
		}),
	})

	c.Submit("/deploy prod")
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for foreground writer-capable subagent")
	}

	if gotTask != "prod" {
		t.Fatalf("runner task = %q, want raw slash args", gotTask)
	}
	if gotRun.Sink == nil || gotRun.Gate == nil || gotRun.Asker == nil || gotRun.PreEditHook == nil {
		t.Fatalf("foreground run context = %#v, want sink/gate/asker/pre-edit hook", gotRun)
	}
	if childEnd != 1 || parentEnd != 1 {
		t.Fatalf("turn done counts child=%d parent=%d, want both child and parent terminal events", childEnd, parentEnd)
	}
	hist := c.History()
	if len(hist) != 3 || hist[1].Content != "/deploy prod" || hist[2].Content != "child answer" {
		t.Fatalf("history = %#v, want merged slash command and child answer", hist)
	}
}

func TestSubmitSubagentSkillCancellationIsTerminal(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan event.Event, 1)
	c := New(Options{
		Registry: subagentReadOnlyRegistry("read_file"),
		Skills: []skill.Skill{{
			Name:         "scout",
			RunAs:        skill.RunSubagent,
			AllowedTools: []string{"read_file"},
		}},
		SkillRunner: func(ctx context.Context, _ skill.Skill, _ string, _ skill.SubagentRunContext) (string, error) {
			close(started)
			<-ctx.Done()
			<-release
			return "", ctx.Err()
		},
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.TurnDone && e.Subagent != nil {
				done <- e
			}
		}),
	})

	c.Submit("/scout inspect")
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for subagent runner")
	}
	runs := c.ListSubagents()
	if len(runs) != 1 {
		t.Fatalf("runs = %v, want one running subagent", runs)
	}
	c.CancelSubagent(runs[0].ID)
	close(release)
	select {
	case e := <-done:
		if e.Subagent.State != event.SubagentCanceled || e.Subagent.Error == "" {
			t.Fatalf("terminal event = %#v, want canceled with error", e.Subagent)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for canceled terminal event")
	}
	detail, ok := c.SubagentDetail(runs[0].ID)
	if !ok || detail.State != event.SubagentCanceled || detail.Err == "" || detail.Cancelable {
		t.Fatalf("detail = %#v ok=%v, want immutable canceled terminal detail", detail, ok)
	}
}

func TestSubagentsClearAllKeepsRunningRuns(t *testing.T) {
	c := New(Options{})
	running := c.registerSubagent("run-1", "scout", "Runner", "/scout running", "running", func() {})
	completed := c.registerSubagent("run-2", "scout", "Done", "/scout done", "done", func() {})
	c.finalizeSubagent(completed, "ok", nil)

	got := c.SubagentsText("/subagents clear all")
	if got != i18n.M.SubagentsClearedAll {
		t.Fatalf("clear notice = %q", got)
	}
	if _, ok := c.SubagentDetail(running.ID); !ok {
		t.Fatal("clear all should not remove running subagents")
	}
	if _, ok := c.SubagentDetail(completed.ID); ok {
		t.Fatal("clear all should remove terminal retained subagents")
	}
}

func TestSubagentsClearRejectsUnknownState(t *testing.T) {
	c := New(Options{})
	completed := c.registerSubagent("run-1", "scout", "Done", "/scout done", "done", func() {})
	c.finalizeSubagent(completed, "ok", nil)

	got := c.SubagentsText("/subagents clear nope")
	if got != i18n.M.SubagentsUsageClear {
		t.Fatalf("invalid clear notice = %q, want %q", got, i18n.M.SubagentsUsageClear)
	}
	if _, ok := c.SubagentDetail(completed.ID); !ok {
		t.Fatal("invalid clear input should not remove retained subagents")
	}
}

func TestSubagentsCancelReportsAmbiguousAlias(t *testing.T) {
	c := New(Options{})
	c.registerSubagent("run-1", "scout", "Same", "/scout one", "one", func() {})
	c.registerSubagent("run-2", "review", "Same", "/review two", "two", func() {})

	got := c.SubagentsText("/subagents cancel same")
	if !strings.Contains(got, "ambiguous ref") {
		t.Fatalf("cancel by ambiguous alias = %q, want ambiguity notice", got)
	}
}

func TestSubagentsListSubcommandReturnsUsage(t *testing.T) {
	c := New(Options{})

	got := c.SubagentsText("/subagents list")
	if got != i18n.M.SubagentsUsage {
		t.Fatalf("/subagents list = %q, want %q", got, i18n.M.SubagentsUsage)
	}
}

func TestForegroundSubagentPreEditHookSnapshotsCheckpoint(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeControlTool{name: "write_file", writer: true})
	exec := agent.New(nil, tool.NewRegistry(), agent.NewSession("sys"), agent.Options{}, event.Discard)
	done := make(chan struct{})
	c := New(Options{
		Executor:      exec,
		Registry:      reg,
		WorkspaceRoot: t.TempDir(),
		Policy:        permission.New("ask", nil, nil, nil),
		Skills: []skill.Skill{{
			Name:         "edit",
			RunAs:        skill.RunSubagent,
			AllowedTools: []string{"write_file"},
		}},
		SkillRunner: func(_ context.Context, _ skill.Skill, _ string, run skill.SubagentRunContext) (string, error) {
			run.PreEditHook(diff.Change{Path: "scene.md", Kind: diff.Modify, OldText: "old", NewText: "new"})
			return "done", nil
		},
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.TurnDone && e.Subagent == nil {
				close(done)
			}
		}),
	})

	c.Submit("/edit scene")
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for foreground writer turn")
	}
	if !c.checkpoints.enabled() {
		t.Fatal("foreground subagent should keep checkpoint store bound")
	}
}

func TestSubmitSubagentSkillFailureIsTerminal(t *testing.T) {
	done := make(chan event.Event, 1)
	c := New(Options{
		Registry: subagentReadOnlyRegistry("read_file"),
		Skills: []skill.Skill{{
			Name:         "scout",
			RunAs:        skill.RunSubagent,
			AllowedTools: []string{"read_file"},
		}},
		SkillRunner: func(context.Context, skill.Skill, string, skill.SubagentRunContext) (string, error) {
			return "", errors.New("child failed")
		},
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.TurnDone && e.Subagent != nil {
				done <- e
			}
		}),
	})

	c.Submit("/scout inspect")
	select {
	case e := <-done:
		if e.Subagent.State != event.SubagentFailed || e.Subagent.Error != "child failed" {
			t.Fatalf("terminal event = %#v, want failed with error", e.Subagent)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for failed terminal event")
	}
	runs := c.ListSubagents()
	if len(runs) != 1 || runs[0].State != event.SubagentFailed || runs[0].Cancelable {
		t.Fatalf("runs = %#v, want failed non-cancelable run", runs)
	}
}

func TestSubmitSubagentHookBlockCancelsRunWithoutMergingHistory(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeControlTool{name: "bash", writer: true})
	exec := agent.New(nil, tool.NewRegistry(), agent.NewSession("sys"), agent.Options{}, event.Discard)
	var (
		childTurnDone  event.Event
		parentTurnDone event.Event
		ran            bool
	)
	done := make(chan struct{})
	hooks := hook.NewRunner(
		[]hook.ResolvedHook{{HookConfig: hook.HookConfig{Command: "gate"}, Event: hook.UserPromptSubmit}},
		t.TempDir(),
		func(context.Context, hook.SpawnInput) hook.SpawnResult {
			return hook.SpawnResult{ExitCode: 2, Stderr: "blocked by policy"}
		},
		nil,
	)
	c := New(Options{
		Executor: exec,
		Registry: reg,
		Hooks:    hooks,
		Skills: []skill.Skill{{
			Name:         "deploy",
			RunAs:        skill.RunSubagent,
			AllowedTools: []string{"bash"},
		}},
		SkillRunner: func(context.Context, skill.Skill, string, skill.SubagentRunContext) (string, error) {
			ran = true
			return "child answer", nil
		},
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind != event.TurnDone {
				return
			}
			if e.Subagent != nil {
				childTurnDone = e
				return
			}
			parentTurnDone = e
			close(done)
		}),
	})

	c.Submit("/deploy prod")
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for foreground turn to settle")
	}

	if ran {
		t.Fatal("subagent runner should not execute when PromptSubmit blocks the run")
	}
	if childTurnDone.Subagent == nil || childTurnDone.Subagent.State != event.SubagentCanceled {
		t.Fatalf("child terminal event = %#v, want canceled subagent", childTurnDone.Subagent)
	}
	if !strings.Contains(childTurnDone.Subagent.Error, "blocked by policy") {
		t.Fatalf("child terminal error = %q, want hook reason", childTurnDone.Subagent.Error)
	}
	if childTurnDone.Err == nil || !errors.Is(childTurnDone.Err, errSubagentHookBlocked) {
		t.Fatalf("child terminal err = %v, want blocked-by-hooks sentinel", childTurnDone.Err)
	}
	if parentTurnDone.Err != nil {
		t.Fatalf("parent turn should settle without surfacing a controller error, got %v", parentTurnDone.Err)
	}
	runs := c.ListSubagents()
	if len(runs) != 1 || runs[0].State != event.SubagentCanceled || runs[0].Cancelable {
		t.Fatalf("runs = %#v, want one canceled retained run", runs)
	}
	detail, ok := c.SubagentDetail(runs[0].ID)
	if !ok || detail.State != event.SubagentCanceled || !strings.Contains(detail.Err, "blocked by policy") || detail.Answer != "" {
		t.Fatalf("detail = %#v ok=%v, want canceled retained detail with no answer", detail, ok)
	}
	if hist := c.History(); len(hist) != 1 {
		t.Fatalf("blocked subagent should not merge into parent transcript, history=%#v", hist)
	}
}
