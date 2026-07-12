package control

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"reasonix/internal/agent"

	"reasonix/internal/command"
	"reasonix/internal/event"
	"reasonix/internal/hook"
	"reasonix/internal/memory"
	"reasonix/internal/provider"
	"reasonix/internal/skill"
	"reasonix/internal/tool"
)

type fakeAutoPlanClassifier struct {
	needsPlan bool
	reason    string
	err       error
	calls     int
}

func (f *fakeAutoPlanClassifier) NeedsPlan(ctx context.Context, input string, score int) (bool, string, error) {
	f.calls++
	return f.needsPlan, f.reason, f.err
}

type fakeTurnRunner struct {
	inputs               []string
	memoryCompilerInputs []string
	memoryCompilerSkips  []bool
}

func (f *fakeTurnRunner) Run(ctx context.Context, input string) error {
	f.inputs = append(f.inputs, input)
	f.memoryCompilerSkips = append(f.memoryCompilerSkips, agent.MemoryCompilerSkipFromContext(ctx))
	if source, ok := agent.MemoryCompilerSourceInputFromContext(ctx); ok {
		f.memoryCompilerInputs = append(f.memoryCompilerInputs, source)
	}
	return nil
}

type fakeLanguageRunner struct {
	fakeTurnRunner
	responseLang string
	lang         string
}

func (f *fakeLanguageRunner) SetResponseLanguage(lang string) {
	f.responseLang = lang
}

func (f *fakeLanguageRunner) SetReasoningLanguage(lang string) {
	f.lang = lang
}

func TestCustomCommandLookup(t *testing.T) {
	c := New(Options{Commands: []command.Command{{Name: "review"}, {Name: "git:commit"}}})

	if _, ok := c.CustomCommand("/review the diff"); !ok {
		t.Error("review should be found")
	}
	if _, ok := c.CustomCommand("/git:commit"); !ok {
		t.Error("git:commit should be found")
	}
	if _, ok := c.CustomCommand("/missing"); ok {
		t.Error("missing should not be found")
	}
}

func TestSkillsReflectStoreChangesAfterControllerBuild(t *testing.T) {
	project := t.TempDir()
	home := t.TempDir()
	store := skill.New(skill.Options{HomeDir: home, ProjectRoot: project, DisableBuiltins: true})
	c := New(Options{SkillStore: store, Skills: store.List()})

	if _, ok := c.RunSkill("/hot now"); ok {
		t.Fatal("skill should not exist before it is written")
	}
	writeControlSkill(t, project, ".reasonix/skills/hot/SKILL.md", "---\nname: hot\ndescription: Hot install\n---\nHot body")

	if skills := c.Skills(); len(skills) != 1 || skills[0].Name != "hot" {
		t.Fatalf("Skills() = %+v, want newly installed hot skill", skills)
	}
	sent, ok := c.RunSkill("/hot now")
	if !ok {
		t.Fatal("RunSkill should find newly installed skill")
	}
	if !strings.Contains(sent, "Hot body") || !strings.Contains(sent, "Arguments: now") {
		t.Fatalf("rendered skill = %q", sent)
	}
}

func TestSubmitSlashSubagentRunsIsolatedAndPersistsDistilledAnswer(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	sess := agent.NewSession("parent system")
	exec := agent.New(nil, tool.NewRegistry(), sess, agent.Options{}, event.Discard)
	events := make(chan event.Event, 16)
	mainRunner := &fakeTurnRunner{}
	var calls int
	var gotSkill skill.Skill
	var gotTask, gotParent, gotCallID string
	var gotPlanMode bool
	var gotHostInitiated bool
	runner := func(ctx context.Context, sk skill.Skill, task string, opts skill.SubagentRunOptions) (string, error) {
		calls++
		gotSkill = sk
		gotTask = task
		gotParent = agent.ParentSession(ctx)
		gotCallID, _, _, _ = agent.CallContext(ctx)
		gotPlanMode = agent.PlanModeFromContext(ctx)
		gotHostInitiated = opts.HostInitiated
		agent.NestedSink(ctx, event.Discard).Emit(event.Event{
			Kind: event.ToolDispatch,
			Tool: event.Tool{ID: "child-read", Name: "read_file", ReadOnly: true},
		})
		return "isolated answer", nil
	}
	c := New(Options{
		Runner:      mainRunner,
		Executor:    exec,
		Sink:        event.FuncSink(func(e event.Event) { events <- e }),
		SessionDir:  dir,
		SessionPath: path,
		Skills: []skill.Skill{{
			Name: "helper", Description: "isolated helper", Body: "secret child system prompt",
			RunAs: skill.RunSubagent, Invocation: "manual", Scope: skill.ScopeGlobal,
		}},
		SkillRunner: runner,
		ReadOnlySkillRunner: func(context.Context, skill.Skill, string, skill.SubagentRunOptions) (string, error) {
			t.Fatal("normal-mode slash invocation must not use the read-only runner")
			return "", nil
		},
		SkillProfile: func(skill.Skill) *event.Profile { return &event.Profile{Model: "test/model", Effort: "high"} },
	})
	defer c.Close()

	c.SubmitDisplay("/helper inspect auth", "/helper inspect auth")
	gotEvents := waitForTurnEvents(t, events)
	if calls != 1 || gotSkill.Name != "helper" || !strings.Contains(gotTask, "inspect auth") {
		t.Fatalf("isolated runner calls=%d skill=%q task=%q", calls, gotSkill.Name, gotTask)
	}
	if len(mainRunner.inputs) != 0 {
		t.Fatalf("main runner must not receive a runAs=subagent slash turn: %q", mainRunner.inputs)
	}
	if gotParent != agent.BranchID(path) || !strings.HasPrefix(gotCallID, "slash-skill-") || gotPlanMode || !gotHostInitiated {
		t.Fatalf("runner context parent=%q call=%q plan=%v hostInitiated=%v", gotParent, gotCallID, gotPlanMode, gotHostInitiated)
	}
	msgs := c.History()
	if len(msgs) != 3 || msgs[1].Role != provider.RoleUser || msgs[2].Role != provider.RoleAssistant {
		t.Fatalf("parent history = %+v, want system/user/assistant", msgs)
	}
	if !strings.Contains(msgs[1].Content, "inspect auth") || strings.Contains(msgs[1].Content, gotSkill.Body) {
		t.Fatalf("parent user message should contain the task but not child system prompt: %q", msgs[1].Content)
	}
	if msgs[2].Content != "isolated answer" {
		t.Fatalf("parent distilled answer = %q", msgs[2].Content)
	}
	var sawStart, sawParent, sawNested, sawText bool
	for _, e := range gotEvents {
		switch {
		case e.Kind == event.TurnStarted:
			sawStart = true
		case e.Kind == event.ToolDispatch && e.Tool.Name == "run_skill":
			sawParent = e.Tool.Profile != nil && e.Tool.Profile.Model == "test/model"
		case e.Kind == event.ToolDispatch && e.Tool.Name == "read_file":
			sawNested = e.Tool.ParentID == gotCallID && strings.HasPrefix(e.Tool.ID, gotCallID+"/")
		case e.Kind == event.Text && e.Text == "isolated answer":
			sawText = true
		}
	}
	if !sawStart || !sawParent || !sawNested || !sawText {
		t.Fatalf("slash subagent events missing: start=%v parent=%v nested=%v text=%v events=%+v", sawStart, sawParent, sawNested, sawText, gotEvents)
	}

	// The isolated turn must release the ordinary foreground admission gate so
	// the same session can keep chatting immediately afterwards.
	waitIdle(t, c)
	c.SubmitUserTurn("next turn", "next turn")
	waitForTurnEvents(t, events)
	if len(mainRunner.inputs) != 1 || !strings.Contains(mainRunner.inputs[0], "next turn") {
		t.Fatalf("normal chat did not resume after slash subagent: %q", mainRunner.inputs)
	}
}

func TestSubmitInvocationDisplayExecutesStructuredEntitiesInVisualOrder(t *testing.T) {
	sess := agent.NewSession("parent system")
	exec := agent.New(nil, tool.NewRegistry(), sess, agent.Options{}, event.Discard)
	events := make(chan event.Event, 24)
	mainRunner := &fakeTurnRunner{}
	var names, tasks []string
	c := New(Options{
		Runner:   mainRunner,
		Executor: exec,
		Sink:     event.FuncSink(func(e event.Event) { events <- e }),
		Skills: []skill.Skill{
			{Name: "format", Body: "FORMAT_RULE", RunAs: skill.RunInline, Scope: skill.ScopeGlobal},
			{Name: "first", Body: "FIRST_SYSTEM", RunAs: skill.RunSubagent, Scope: skill.ScopeGlobal},
			{Name: "second", Body: "SECOND_SYSTEM", RunAs: skill.RunSubagent, Scope: skill.ScopeGlobal},
		},
		SkillRunner: func(_ context.Context, sk skill.Skill, task string, _ skill.SubagentRunOptions) (string, error) {
			names = append(names, sk.Name)
			tasks = append(tasks, task)
			return sk.Name + " answer", nil
		},
	})
	defer c.Close()

	input := "历史会话：prior\n\n当前用户问题：\ninspect auth"
	c.SubmitInvocationDisplay("inspect auth", input, []InvocationRequest{
		{Name: "second", Kind: "subagent", Offset: 20},
		{Name: "format", Kind: "skill", Offset: 0},
		{Name: "first", Kind: "subagent", Offset: 10},
	})
	waitForTurnEvents(t, events)
	waitIdle(t, c)

	if strings.Join(names, ",") != "first,second" {
		t.Fatalf("subagent execution order = %v, want visual order first,second", names)
	}
	if len(tasks) != 2 || !strings.Contains(tasks[0], "FORMAT_RULE") || !strings.Contains(tasks[0], "历史会话：prior") || tasks[0] != tasks[1] {
		t.Fatalf("structured tasks = %#v", tasks)
	}
	if len(mainRunner.inputs) != 0 {
		t.Fatalf("main runner received structured subagent turn: %q", mainRunner.inputs)
	}
	msgs := c.History()
	if len(msgs) != 4 || msgs[1].Role != provider.RoleUser || msgs[2].Content != "first answer" || msgs[3].Content != "second answer" {
		t.Fatalf("parent history = %+v", msgs)
	}
}

func TestSubmitInvocationDisplayRunsInlineSkillWithoutArguments(t *testing.T) {
	runner := &fakeTurnRunner{}
	c := New(Options{
		Runner: runner,
		Skills: []skill.Skill{{Name: "init", Body: "INITIALIZE_PROJECT", RunAs: skill.RunInline, Scope: skill.ScopeGlobal}},
	})
	c.SubmitInvocationDisplay("", "", []InvocationRequest{{Name: "init", Kind: "skill", Offset: 0}})
	waitIdle(t, c)
	if len(runner.inputs) != 1 || !strings.Contains(runner.inputs[0], "INITIALIZE_PROJECT") {
		t.Fatalf("inline-only structured input = %q", runner.inputs)
	}
}

func TestSubmitSlashSubagentUsesReadOnlyRunnerInPlanMode(t *testing.T) {
	sess := agent.NewSession("parent system")
	exec := agent.New(nil, tool.NewRegistry(), sess, agent.Options{}, event.Discard)
	events := make(chan event.Event, 12)
	var normalCalls, readOnlyCalls int
	var gotTask string
	var gotPlanMode bool
	c := New(Options{
		Executor: exec,
		Sink:     event.FuncSink(func(e event.Event) { events <- e }),
		Skills: []skill.Skill{{
			Name: "helper", Description: "isolated helper", Body: "child prompt",
			RunAs: skill.RunSubagent, Invocation: "manual", Scope: skill.ScopeGlobal,
		}},
		SkillRunner: func(context.Context, skill.Skill, string, skill.SubagentRunOptions) (string, error) {
			normalCalls++
			return "", nil
		},
		ReadOnlySkillRunner: func(ctx context.Context, _ skill.Skill, task string, _ skill.SubagentRunOptions) (string, error) {
			readOnlyCalls++
			gotTask = task
			gotPlanMode = agent.PlanModeFromContext(ctx)
			return "read-only answer", nil
		},
	})
	c.SetPlanMode(true)
	c.Submit("/helper inspect only")
	gotEvents := waitForTurnEvents(t, events)
	if normalCalls != 0 || readOnlyCalls != 1 || !gotPlanMode || !strings.Contains(gotTask, PlanModeMarker) {
		t.Fatalf("plan-mode runners normal=%d readonly=%d plan=%v task=%q", normalCalls, readOnlyCalls, gotPlanMode, gotTask)
	}
	for _, e := range gotEvents {
		if e.Kind == event.ToolDispatch && e.Tool.Name == "run_skill" && !e.Tool.ReadOnly {
			t.Fatal("plan-mode slash skill dispatch must be marked read-only")
		}
	}
}

func TestSubmitSlashSubagentRequiresTask(t *testing.T) {
	events := make(chan event.Event, 2)
	var calls int
	c := New(Options{
		Sink: event.FuncSink(func(e event.Event) { events <- e }),
		Skills: []skill.Skill{{
			Name: "helper", RunAs: skill.RunSubagent, Invocation: "manual", Scope: skill.ScopeGlobal,
		}},
		SkillRunner: func(context.Context, skill.Skill, string, skill.SubagentRunOptions) (string, error) {
			calls++
			return "", nil
		},
	})
	c.Submit("/helper")
	select {
	case e := <-events:
		if e.Kind != event.Notice || !strings.Contains(e.Text, "usage: /helper <task>") {
			t.Fatalf("event = %+v", e)
		}
	case <-time.After(time.Second):
		t.Fatal("missing usage notice")
	}
	if calls != 0 || c.Running() || len(c.History()) != 0 {
		t.Fatalf("taskless invocation calls=%d running=%v history=%v", calls, c.Running(), c.History())
	}
}

func TestSubmitSlashSubagentWithoutRunnerFinishesWithError(t *testing.T) {
	events := make(chan event.Event, 2)
	c := New(Options{
		Sink: event.FuncSink(func(e event.Event) { events <- e }),
		Skills: []skill.Skill{{
			Name: "helper", RunAs: skill.RunSubagent, Invocation: "manual", Scope: skill.ScopeGlobal,
		}},
	})
	c.Submit("/helper inspect auth")
	gotEvents := waitForTurnEvents(t, events)
	waitIdle(t, c)
	if len(gotEvents) != 1 || gotEvents[0].Kind != event.TurnDone || gotEvents[0].Err == nil ||
		!strings.Contains(gotEvents[0].Err.Error(), "runner is unavailable") {
		t.Fatalf("missing terminal runner error: %+v", gotEvents)
	}
}

func TestCancelSlashSubagentStopsChildAndKeepsParentSessionUsable(t *testing.T) {
	sess := agent.NewSession("parent system")
	exec := agent.New(nil, tool.NewRegistry(), sess, agent.Options{}, event.Discard)
	events := make(chan event.Event, 12)
	started := make(chan struct{})
	c := New(Options{
		Executor: exec,
		Sink:     event.FuncSink(func(e event.Event) { events <- e }),
		Skills: []skill.Skill{{
			Name: "helper", RunAs: skill.RunSubagent, Invocation: "manual", Scope: skill.ScopeGlobal,
		}},
		SkillRunner: func(ctx context.Context, _ skill.Skill, _ string, _ skill.SubagentRunOptions) (string, error) {
			close(started)
			<-ctx.Done()
			return "", ctx.Err()
		},
	})
	c.Submit("/helper inspect auth")
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("slash subagent did not start")
	}
	c.Cancel()
	gotEvents := waitForTurnEvents(t, events)
	waitIdle(t, c)
	if c.Running() {
		t.Fatal("controller still running after cancelling slash subagent")
	}
	msgs := c.History()
	if len(msgs) != 2 || msgs[1].Role != provider.RoleUser {
		t.Fatalf("cancelled slash history = %+v, want system + preserved user task", msgs)
	}
	for _, e := range gotEvents {
		if e.Kind == event.Message || (e.Kind == event.Text && e.Text != "") {
			t.Fatalf("cancelled slash subagent emitted a final answer: %+v", e)
		}
	}
}

func waitForTurnEvents(t *testing.T, events <-chan event.Event) []event.Event {
	t.Helper()
	deadline := time.After(10 * time.Second)
	var got []event.Event
	for {
		select {
		case e := <-events:
			got = append(got, e)
			if e.Kind == event.TurnDone {
				return got
			}
		case <-deadline:
			t.Fatalf("timed out waiting for TurnDone; events=%+v", got)
		}
	}
}

func TestRunSkillUsesQualifiedPluginNameAndHiddenShortCompatibility(t *testing.T) {
	home := t.TempDir()
	pluginRoot := t.TempDir()
	writeControlSkill(t, pluginRoot, "plan/SKILL.md", "---\ndescription: Plugin plan\n---\nPlugin body")
	store := skill.New(skill.Options{
		HomeDir: home, CustomPaths: []string{pluginRoot},
		PluginPaths: map[string][]string{pluginRoot: {"superpowers"}}, DisableBuiltins: true,
	})
	c := New(Options{SkillStore: store, Skills: store.List()})

	if visible := c.SlashSkills(); len(visible) != 1 || visible[0].SlashName() != "superpowers:plan" {
		t.Fatalf("SlashSkills() = %+v", visible)
	}
	for _, input := range []string{"/superpowers:plan now", "/plan now"} {
		sent, ok := c.RunSkill(input)
		if !ok || !strings.Contains(sent, "Plugin body") || !strings.Contains(sent, "Arguments: now") {
			t.Fatalf("RunSkill(%q) = %q, %v", input, sent, ok)
		}
	}
}

func writeControlSkill(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestComposePlanModeMarker(t *testing.T) {
	c := New(Options{}) // no executor — SetPlanMode still tracks the flag

	if got := c.Compose("hi"); got != "hi" {
		t.Errorf("plan off: Compose = %q, want verbatim", got)
	}

	c.SetPlanMode(true)
	got := c.Compose("hi")
	if !strings.HasPrefix(got, PlanModeMarker) || !strings.HasSuffix(got, "hi") {
		t.Errorf("plan on: Compose = %q, want marker-prefixed", got)
	}
}

func TestPlanModeMarkerMatchesPolicy(t *testing.T) {
	for _, want := range []string{"research", "ask", "todo_write", "read_only_task", "read_only_skill"} {
		if !strings.Contains(PlanModeMarker, want) {
			t.Fatalf("PlanModeMarker should describe %q as available:\n%s", want, PlanModeMarker)
		}
	}
	for _, forbidden := range []string{"task", "complete_step"} {
		if strings.Contains(PlanModeMarker, forbidden+" are available") || strings.Contains(PlanModeMarker, forbidden+",") {
			t.Fatalf("PlanModeMarker must not list blocked tool %q as available:\n%s", forbidden, PlanModeMarker)
		}
	}
	for _, blocked := range []string{"write files", "unsafe shell commands", "install capabilities", "mutate memory", "delegate", "mark execution steps complete"} {
		if !strings.Contains(PlanModeMarker, blocked) {
			t.Fatalf("PlanModeMarker should mention blocked capability %q:\n%s", blocked, PlanModeMarker)
		}
	}
}

func TestComposeReasoningLanguagePreference(t *testing.T) {
	auto := New(Options{ReasoningLanguage: "auto"})
	if got := auto.Compose("hi"); got != "hi" {
		t.Fatalf("auto reasoning language should not alter the turn, got %q", got)
	}

	zh := New(Options{ReasoningLanguage: "zh"})
	got := zh.Compose("hi")
	if !strings.HasPrefix(got, "<reasoning-language>") || !strings.Contains(got, "简体中文") || !strings.HasSuffix(got, "hi") {
		t.Fatalf("zh reasoning language should ride the user turn, got %q", got)
	}
	if stripped := StripComposePrefixes(got); stripped != "hi" {
		t.Fatalf("StripComposePrefixes = %q, want hi", stripped)
	}

	autoZh := New(Options{ReasoningLanguage: "auto"})
	got = autoZh.Compose("解释 AuthHandler 的 panic")
	if !strings.HasPrefix(got, "<reasoning-language>") || !strings.Contains(got, "简体中文") || !strings.HasSuffix(got, "解释 AuthHandler 的 panic") {
		t.Fatalf("auto reasoning language should infer Chinese from the user prompt, got %q", got)
	}
}

func TestRunComposesResponseLanguagePreference(t *testing.T) {
	runner := &fakeTurnRunner{}
	c := New(Options{ResponseLanguage: "en", Runner: runner})

	if err := c.Run(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
	if len(runner.inputs) != 1 {
		t.Fatalf("runner inputs = %d, want 1", len(runner.inputs))
	}
	got := runner.inputs[0]
	if !strings.HasPrefix(got, "<response-language>") || !strings.Contains(got, "use English") || !strings.HasSuffix(got, "hi") {
		t.Fatalf("headless Run should compose the response language preference, got %q", got)
	}
}

func TestRunComposesReasoningLanguagePreference(t *testing.T) {
	runner := &fakeTurnRunner{}
	c := New(Options{ReasoningLanguage: "zh", Runner: runner})

	if err := c.Run(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
	if len(runner.inputs) != 1 {
		t.Fatalf("runner inputs = %d, want 1", len(runner.inputs))
	}
	got := runner.inputs[0]
	if !strings.HasPrefix(got, "<reasoning-language>") || !strings.Contains(got, "简体中文") || !strings.HasSuffix(got, "hi") {
		t.Fatalf("headless Run should compose the reasoning language preference, got %q", got)
	}
}

func TestRunInjectsSessionStartHookContextOnce(t *testing.T) {
	runner := &fakeTurnRunner{}
	hooks := hook.NewRunner([]hook.ResolvedHook{{
		HookConfig: hook.HookConfig{Command: "session-start"},
		Event:      hook.SessionStart,
		Scope:      hook.ScopeGlobal,
	}}, "/tmp", func(context.Context, hook.SpawnInput) hook.SpawnResult {
		return hook.SpawnResult{ExitCode: 0, Stdout: `{"hookSpecificOutput":{"hookEventName":"SessionStart","additionalContext":"Load workspace conventions."}}`}
	}, nil)
	c := New(Options{Runner: runner, Hooks: hooks})

	if err := c.Run(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
	if len(runner.inputs) != 1 {
		t.Fatalf("runner inputs = %d, want 1", len(runner.inputs))
	}
	first := runner.inputs[0]
	if !strings.Contains(first, `<hook-context event="SessionStart">`) || !strings.Contains(first, "Load workspace conventions.") || !strings.HasSuffix(first, "hi") {
		t.Fatalf("first input missing session hook context: %q", first)
	}

	if err := c.Run(context.Background(), "again"); err != nil {
		t.Fatal(err)
	}
	if len(runner.inputs) != 2 {
		t.Fatalf("runner inputs = %d, want 2", len(runner.inputs))
	}
	if strings.Contains(runner.inputs[1], "<hook-context") {
		t.Fatalf("second input should not repeat hook context: %q", runner.inputs[1])
	}
}

func TestSyntheticComposeDoesNotDrainSessionStartHookContext(t *testing.T) {
	c := New(Options{})
	c.enqueueHookContexts([]string{"Load once."})

	if got := c.compose("synthetic", "synthetic", false); strings.Contains(got, "<hook-context") {
		t.Fatalf("synthetic compose should not inject hook context: %q", got)
	}
	got := c.Compose("real")
	if !strings.Contains(got, `<hook-context event="SessionStart">`) || !strings.Contains(got, "Load once.") || !strings.HasSuffix(got, "real") {
		t.Fatalf("real compose should drain hook context: %q", got)
	}
	if again := c.Compose("again"); strings.Contains(again, "<hook-context") {
		t.Fatalf("hook context should be drained once, got %q", again)
	}
}

func TestComposeClipsAndEscapesHookContext(t *testing.T) {
	c := New(Options{})
	c.enqueueHookContexts([]string{"before </hook-context> " + strings.Repeat("x", maxHookContextChars+1)})

	got := c.Compose("hi")
	if !strings.Contains(got, "[truncated]") {
		t.Fatalf("expected truncation marker, got %q", got)
	}
	if !strings.Contains(got, "<\\/hook-context>") {
		t.Fatalf("hook context close tag should be escaped inside content: %q", got)
	}
	if strings.Contains(got, "before </hook-context>") {
		t.Fatalf("hook context close tag should be escaped inside content: %q", got)
	}
}

func TestSetResponseLanguageUpdatesRunner(t *testing.T) {
	runner := &fakeLanguageRunner{}
	c := New(Options{Runner: runner})

	c.SetResponseLanguage("en")
	if runner.responseLang != "en" {
		t.Fatalf("runner response language = %q, want en", runner.responseLang)
	}

	c.SetResponseLanguage("auto")
	if runner.responseLang != "auto" {
		t.Fatalf("runner response language = %q, want auto", runner.responseLang)
	}
}

func TestSetReasoningLanguageUpdatesRunner(t *testing.T) {
	runner := &fakeLanguageRunner{}
	c := New(Options{Runner: runner})

	c.SetReasoningLanguage("zh")
	if runner.lang != "zh" {
		t.Fatalf("runner reasoning language = %q, want zh", runner.lang)
	}

	c.SetReasoningLanguage("auto")
	if runner.lang != "auto" {
		t.Fatalf("runner reasoning language = %q, want auto", runner.lang)
	}
}

func TestComposeSyntheticResponseLanguagePreference(t *testing.T) {
	c := New(Options{ResponseLanguage: "en"})

	got := c.ComposeSynthetic(planApprovedMessage)
	if !strings.HasPrefix(got, "<response-language>") || !strings.Contains(got, "use English") || !strings.HasSuffix(got, planApprovedMessage) {
		t.Fatalf("ComposeSynthetic should prefix response language, got %q", got)
	}
	if !IsSyntheticUserMessage(got) {
		t.Fatalf("response-language-prefixed plan approval should still be synthetic")
	}
}

func TestComposeSyntheticReasoningLanguagePreference(t *testing.T) {
	c := New(Options{ReasoningLanguage: "zh"})

	got := c.ComposeSynthetic(planApprovedMessage)
	if !strings.HasPrefix(got, "<reasoning-language>") || !strings.Contains(got, "简体中文") || !strings.HasSuffix(got, planApprovedMessage) {
		t.Fatalf("ComposeSynthetic should prefix reasoning language, got %q", got)
	}
	if !IsSyntheticUserMessage(got) {
		t.Fatalf("reasoning-language-prefixed plan approval should still be synthetic")
	}
}

func TestComposeIncludesActiveGoal(t *testing.T) {
	c := New(Options{})
	c.SetGoal("ship the approval redesign")

	got := c.Compose("next step?")
	if !strings.Contains(got, "<active-goal>\nship the approval redesign") {
		t.Fatalf("Compose should include active goal block, got %q", got)
	}
	if !strings.Contains(got, "[goal:complete]") || !strings.Contains(got, "[goal:blocked:<short reason>]") {
		t.Fatalf("goal block should include autonomous status markers, got %q", got)
	}
	if !strings.HasSuffix(got, "next step?") {
		t.Fatalf("user text should follow goal block: %q", got)
	}

	c.ClearGoal()
	if got := c.Compose("plain"); got != "plain" {
		t.Fatalf("cleared goal should stop injection, got %q", got)
	}
}

func TestGoalAutoResearchTriggersForLongHorizonGoals(t *testing.T) {
	root := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	c := New(Options{WorkspaceRoot: root})
	c.SetGoal("持续排查这个线上卡顿直到根因明确，并验证修复")

	got := c.Compose("next step?")
	for _, want := range []string{
		"AutoResearch protocol",
		"<autoresearch-runtime>",
		"task_id:",
		"pivot_required:",
		"stale_count >= 2",
		"durable strategy for this Goal",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("AutoResearch goal block missing %q:\n%s", want, got)
		}
	}
}

func TestGoalAutoResearchCanBeForcedOrDisabled(t *testing.T) {
	c := New(Options{})
	c.SetGoalWithResearchMode("fix the typo and add a test", GoalResearchOn)
	if got := c.Compose("start"); !strings.Contains(got, "AutoResearch protocol") {
		t.Fatalf("forced research goal should include AutoResearch protocol:\n%s", got)
	}

	c.SetGoalWithResearchMode("持续排查这个线上卡顿直到根因明确", GoalResearchOff)
	if got := c.Compose("start"); strings.Contains(got, "AutoResearch protocol") {
		t.Fatalf("simple override should suppress AutoResearch protocol:\n%s", got)
	}
}

func TestGoalCommandPreservesResearchModeFlags(t *testing.T) {
	c := New(Options{})
	if !c.applyGoalCommand("/goal --research fix the typo", "") {
		t.Fatal("goal command was not parsed")
	}
	if got := c.Compose("start"); !strings.Contains(got, "AutoResearch protocol") {
		t.Fatalf("/goal --research should force AutoResearch through command dispatch:\n%s", got)
	}

	c = New(Options{})
	if !c.applyGoalCommand("/goal --simple 持续排查这个线上卡顿直到根因明确", "") {
		t.Fatal("goal command was not parsed")
	}
	if got := c.Compose("start"); strings.Contains(got, "AutoResearch protocol") {
		t.Fatalf("/goal --simple should suppress AutoResearch through command dispatch:\n%s", got)
	}
}

func TestAutoStartResearchGoalUsesOnlyStrongSignals(t *testing.T) {
	for _, input := range []string{
		"持续排查这个线上卡顿直到根因明确，并验证修复",
		"不要原地打转，把这个方向完整做成方案并验证",
		"thoroughly implement, test, optimize, and document this feature",
		"继续 .reasonix/autoresearch/20260618-224530-cache-audit/ 这个任务",
	} {
		if !shouldAutoStartResearchGoal(input) {
			t.Fatalf("shouldAutoStartResearchGoal(%q) = false, want true", input)
		}
	}

	for _, input := range []string{
		"长期来看这个模块怎么优化？",
		"研究一下这个函数怎么用",
		"验证一下这个小修复",
		"/goal 持续排查直到根因明确",
		"!go test ./...",
	} {
		if shouldAutoStartResearchGoal(input) {
			t.Fatalf("shouldAutoStartResearchGoal(%q) = true, want false", input)
		}
	}
}

func TestParseGoalCommandResearchFlags(t *testing.T) {
	cmd, ok := ParseGoalCommand("/goal --research fix the typo")
	if !ok || cmd.Action != GoalCommandSet || cmd.Text != "fix the typo" || cmd.ResearchMode != GoalResearchOn {
		t.Fatalf("ParseGoalCommand --research = %+v ok=%v", cmd, ok)
	}

	cmd, ok = ParseGoalCommand("/goal --simple 持续排查直到根因明确")
	if !ok || cmd.Action != GoalCommandSet || cmd.Text != "持续排查直到根因明确" || cmd.ResearchMode != GoalResearchOff {
		t.Fatalf("ParseGoalCommand --simple = %+v ok=%v", cmd, ok)
	}
}

func TestGoalCommandSetsReportsAndClears(t *testing.T) {
	var notices []string
	c := New(Options{Sink: event.FuncSink(func(e event.Event) {
		if e.Kind == event.Notice {
			notices = append(notices, e.Text)
		}
	})})
	c.SetPlanMode(true)

	c.Submit("/goal finish the mode redesign")
	if got := c.Goal(); got != "finish the mode redesign" {
		t.Fatalf("Goal() = %q", got)
	}
	if c.PlanMode() {
		t.Fatal("/goal should leave plan mode")
	}
	c.Submit("/goal")
	c.Submit("/goal clear")
	if got := c.Goal(); got != "" {
		t.Fatalf("goal should be cleared, got %q", got)
	}
	joined := strings.Join(notices, "\n")
	for _, want := range []string{"goal set", "goal: finish the mode redesign", "goal cleared"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("notices missing %q: %v", want, notices)
		}
	}
}

func TestParseGoalCommandWithStrict(t *testing.T) {
	tests := []struct {
		input  string
		text   string
		strict bool
		ok     bool
	}{
		{"/goal --strict implement calculator", "implement calculator", true, true},
		{"/goal implement calculator", "implement calculator", false, true},
		{"/goal --strict", "", true, true},        // --strict shows status
		{"/goal --strict status", "", true, true}, // --strict shows status
	}
	for _, tt := range tests {
		cmd, ok := ParseGoalCommand(tt.input)
		if ok != tt.ok {
			t.Errorf("ParseGoalCommand(%q) ok = %v, want %v", tt.input, ok, tt.ok)
			continue
		}
		if !ok {
			continue
		}
		if cmd.Text != tt.text {
			t.Errorf("ParseGoalCommand(%q).Text = %q, want %q", tt.input, cmd.Text, tt.text)
		}
		if cmd.Strict != tt.strict {
			t.Errorf("ParseGoalCommand(%q).Strict = %v, want %v", tt.input, cmd.Strict, tt.strict)
		}
	}
}

func TestParseGoalCommandStrictOnlyConsumesLeadingFlags(t *testing.T) {
	structuredGoal := "implement parser\n\n  keep  spacing\nliteral --strict stays"
	cmd, ok := ParseGoalCommand("/goal --strict " + structuredGoal)
	if !ok {
		t.Fatal("ParseGoalCommand returned ok=false")
	}
	if !cmd.Strict {
		t.Fatal("leading --strict should enable strict mode")
	}
	if cmd.Text != structuredGoal {
		t.Fatalf("goal text was rewritten:\nwant %q\ngot  %q", structuredGoal, cmd.Text)
	}

	cmd, ok = ParseGoalCommand("/goal implement parser --strict literally")
	if !ok {
		t.Fatal("ParseGoalCommand with literal --strict returned ok=false")
	}
	if cmd.Strict {
		t.Fatal("non-leading --strict should remain part of the goal text")
	}
	if want := "implement parser --strict literally"; cmd.Text != want {
		t.Fatalf("goal text = %q, want %q", cmd.Text, want)
	}
}

func TestComposeDrainsQueuedMemory(t *testing.T) {
	c := New(Options{}) // no executor/memory — QueueMemory still queues a turn-tail note

	c.QueueMemory("Saved memory \"rmb\": user's balance is in RMB")
	got := c.Compose("hello")
	if !strings.Contains(got, "<memory-update>") || !strings.Contains(got, "user's balance is in RMB") {
		t.Fatalf("queued memory should ride the turn: %q", got)
	}
	if !strings.HasSuffix(got, "hello") {
		t.Fatalf("user text should follow the memory block: %q", got)
	}
	if got2 := c.Compose("again"); got2 != "again" {
		t.Fatalf("pendingMemory should drain after one turn, got %q", got2)
	}
}

func TestMemoryQuickAddNoteRequiresWhitespace(t *testing.T) {
	tests := []struct {
		in   string
		note string
		ok   bool
	}{
		{in: "# remember this", note: "remember this", ok: true},
		{in: "  #\tremember this  ", note: "remember this", ok: true},
		{in: "#7 needs work", ok: false},
		{in: "#issue needs work", ok: false},
		{in: "# Heading", note: "Heading", ok: true},
		{in: "#", ok: false},
		// Multi-line input is NOT a quick-add — it's a Markdown heading (# Context)
		// followed by structured content. Desktop users pasting COSTAR-style prompts
		// hit this when the first line starts with "# ".
		{in: "# Context\n\n- file.go\n", ok: false},
		{in: "# Heading\nmore text", ok: false},
		{in: "  # Context\n  - file.go  ", ok: false},
	}
	for _, tt := range tests {
		got, ok := MemoryQuickAddNote(tt.in)
		if ok != tt.ok || got != tt.note {
			t.Errorf("MemoryQuickAddNote(%q) = (%q,%v), want (%q,%v)", tt.in, got, ok, tt.note, tt.ok)
		}
	}
}

func TestRememberCommandNote(t *testing.T) {
	tests := []struct {
		in   string
		note string
		ok   bool
	}{
		{in: "/remember use tabs", note: "use tabs", ok: true},
		{in: " /remember\tuse tabs ", note: "use tabs", ok: true},
		{in: "/remember", ok: true},
		{in: "/remembering use tabs", ok: false},
	}
	for _, tt := range tests {
		got, ok := RememberCommandNote(tt.in)
		if ok != tt.ok || got != tt.note {
			t.Errorf("RememberCommandNote(%q) = (%q,%v), want (%q,%v)", tt.in, got, ok, tt.note, tt.ok)
		}
	}
}

func TestSubmitHashNumberStartsTurn(t *testing.T) {
	runner := &fakeTurnRunner{}
	events := make(chan event.Event, 4)
	c := New(Options{
		AutoPlan: "off",
		Runner:   runner,
		Sink: event.FuncSink(func(e event.Event) {
			events <- e
		}),
	})

	const input = "#7 needs work"
	c.Submit(input)
	waitForTurnDone(t, events)

	if len(runner.inputs) != 1 || runner.inputs[0] != input {
		t.Fatalf("#number prompt should start a model turn, inputs=%q", runner.inputs)
	}
}

func TestSubmitSlashPathDiagnosticStartsTurnWithFileContext(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX absolute file path context is covered on POSIX runners")
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "app", "src", "main", "Foo.kt")
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("fun broken() = missingSymbol\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &fakeTurnRunner{}
	events := make(chan event.Event, 4)
	c := New(Options{
		AutoPlan: "off",
		Runner:   runner,
		Sink: event.FuncSink(func(e event.Event) {
			events <- e
		}),
	})

	input := file + ":12:13: error: unresolved reference: missingSymbol"
	c.Submit(input)
	waitForTurnDone(t, events)

	if len(runner.inputs) != 1 {
		t.Fatalf("slash path diagnostic should start a model turn, inputs=%q", runner.inputs)
	}
	got := runner.inputs[0]
	if !strings.Contains(got, "Referenced context:") || !strings.Contains(got, "fun broken() = missingSymbol") {
		t.Fatalf("slash path diagnostic should attach file context, got %q", got)
	}
	if !strings.Contains(got, input) {
		t.Fatalf("slash path diagnostic should preserve original error text, got %q", got)
	}
}

func TestSubmitMissingSlashPathDiagnosticStartsTurn(t *testing.T) {
	runner := &fakeTurnRunner{}
	events := make(chan event.Event, 4)
	c := New(Options{
		AutoPlan: "off",
		Runner:   runner,
		Sink: event.FuncSink(func(e event.Event) {
			events <- e
		}),
	})

	input := "/missing/Foo.kt:12: error: file no longer exists"
	c.Submit(input)
	waitForTurnDone(t, events)

	if len(runner.inputs) != 1 || runner.inputs[0] != input {
		t.Fatalf("missing slash path diagnostic should start a raw model turn, inputs=%q", runner.inputs)
	}
}

func TestSubmitBlockCommentPrefixStartsTurn(t *testing.T) {
	runner := &fakeTurnRunner{}
	events := make(chan event.Event, 4)
	c := New(Options{
		AutoPlan: "off",
		Runner:   runner,
		Sink: event.FuncSink(func(e event.Event) {
			events <- e
		}),
	})

	input := "/**\n * 阿明\n */"
	c.Submit(input)
	waitForTurnDone(t, events)

	if len(runner.inputs) != 1 || runner.inputs[0] != input {
		t.Fatalf("block comment prefix should start a model turn, inputs=%q", runner.inputs)
	}
}

func TestSubmitUnknownSlashCommandStillReportsNotice(t *testing.T) {
	runner := &fakeTurnRunner{}
	events := make(chan event.Event, 4)
	c := New(Options{
		AutoPlan: "off",
		Runner:   runner,
		Sink: event.FuncSink(func(e event.Event) {
			events <- e
		}),
	})

	c.Submit("/definitely-not-a-command")

	if len(runner.inputs) != 0 {
		t.Fatalf("unknown slash command should not start a model turn, inputs=%q", runner.inputs)
	}
	select {
	case e := <-events:
		if e.Kind != event.Notice || !strings.Contains(e.Text, "unknown command: /definitely-not-a-command") {
			t.Fatalf("event = %+v, want unknown-command notice", e)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("timed out waiting for unknown-command notice")
	}
}

func TestSubmitUserTurnBypassesCommandDispatch(t *testing.T) {
	runner := &fakeTurnRunner{}
	events := make(chan event.Event, 4)
	c := New(Options{
		AutoPlan: "off",
		Runner:   runner,
		Sink: event.FuncSink(func(e event.Event) {
			events <- e
		}),
	})

	for _, input := range []string{"!echo should stay a prompt", "/clear"} {
		c.SubmitUserTurn(input, input)
		waitForTurnDone(t, events)
		// The next SubmitUserTurn must wait out the finishing window or it is
		// silently dropped by runGuarded — see waitIdle.
		waitIdle(t, c)
	}

	if len(runner.inputs) != 2 {
		t.Fatalf("SubmitUserTurn should start model turns, inputs=%q", runner.inputs)
	}
	if runner.inputs[0] != "!echo should stay a prompt" || runner.inputs[1] != "/clear" {
		t.Fatalf("SubmitUserTurn inputs = %q", runner.inputs)
	}
}

func TestSubmitRememberCommandQuickAddsMemory(t *testing.T) {
	dir := t.TempDir()
	runner := &fakeTurnRunner{}
	c := New(Options{
		Runner: runner,
		Memory: memory.Load(memory.Options{CWD: dir}),
	})

	c.Submit("/remember use tabs")

	if len(runner.inputs) != 0 {
		t.Fatalf("/remember should not start a model turn, inputs=%q", runner.inputs)
	}
	body, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "- use tabs") {
		t.Fatalf("memory file missing note:\n%s", body)
	}
}

// waitIdle blocks until the controller's turn-admission gate reopens.
// TurnDone is emitted INSIDE the finishing window (finishGuardedTurn sets
// running=false, finishing=true, emits, then clears finishing), and runGuarded
// silently no-ops while finishing is set — so "received TurnDone" does NOT
// mean "may submit the next turn". A submit raced into that window is
// dropped, and the next turn's TurnDone never arrives; under parallel test
// load the window is wide enough to hit (observed in CI and on a clean
// main-v2 worktree). Poll the same running||finishing gate the controller
// admission checks.
func waitIdle(t *testing.T, c *Controller) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for c.Running() {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for the controller to return to idle")
		}
		time.Sleep(time.Millisecond)
	}
}

func waitForTurnDone(t *testing.T, events <-chan event.Event) {
	t.Helper()
	deadline := time.After(30 * time.Second)
	for {
		select {
		case e := <-events:
			if e.Kind == event.TurnDone {
				if e.Err != nil {
					t.Fatalf("turn finished with error: %v", e.Err)
				}
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for turn_done")
		}
	}
}

func TestRunTurnAutoPlanComplexTask(t *testing.T) {
	var notices []string
	runner := &fakeTurnRunner{}
	c := New(Options{
		AutoPlan: "on",
		Runner:   runner,
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.Notice {
				notices = append(notices, e.Text)
			}
		}),
	})

	input := "实现 GitHub issue #2395：\n- 新增配置项\n- 自动判断复杂任务\n- 补测试和文档"
	if err := c.runTurn(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	if len(runner.inputs) != 1 || !strings.HasPrefix(agent.StripTransientUserBlocks(runner.inputs[0]), PlanModeMarker) {
		t.Fatalf("complex task should auto-enter plan mode, inputs=%q", runner.inputs)
	}
	if !c.PlanMode() {
		t.Fatal("controller plan mode should be on after auto-plan")
	}
	if len(notices) != 1 || notices[0] != "Planning mode enabled for this multi-step task." {
		t.Fatalf("notice = %v, want one auto-plan notice", notices)
	}
}

func TestRunTurnAutoPlanSkipsSimpleQuestion(t *testing.T) {
	runner := &fakeTurnRunner{}
	c := New(Options{AutoPlan: "on", Runner: runner})

	if err := c.runTurn(context.Background(), "解释一下这个函数做什么？"); err != nil {
		t.Fatal(err)
	}
	if len(runner.inputs) != 1 || strings.HasPrefix(agent.StripTransientUserBlocks(runner.inputs[0]), PlanModeMarker) {
		t.Fatalf("simple question should not auto-plan: inputs=%q", runner.inputs)
	}
	if c.PlanMode() {
		t.Fatal("controller plan mode should remain off")
	}
}

func TestRunTurnAutoPlanOff(t *testing.T) {
	runner := &fakeTurnRunner{}
	c := New(Options{AutoPlan: "off", Runner: runner})

	input := "实现 GitHub issue #2395：\n- 新增配置项\n- 自动判断复杂任务\n- 补测试和文档"
	if err := c.runTurn(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	if len(runner.inputs) != 1 || StripComposePrefixes(runner.inputs[0]) != input {
		t.Fatalf("auto_plan=off should compose verbatim, inputs=%q", runner.inputs)
	}
	if c.PlanMode() {
		t.Fatal("controller plan mode should remain off")
	}
}

func TestSetAutoPlanAffectsNextTurn(t *testing.T) {
	runner := &fakeTurnRunner{}
	c := New(Options{AutoPlan: "off", Runner: runner})
	c.SetAutoPlan("on")

	input := "实现 GitHub issue #2395：\n- 新增配置项\n- 自动判断复杂任务\n- 补测试和文档"
	if err := c.runTurn(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	if len(runner.inputs) != 1 || !strings.HasPrefix(agent.StripTransientUserBlocks(runner.inputs[0]), PlanModeMarker) {
		t.Fatalf("SetAutoPlan should affect next turn, inputs=%q", runner.inputs)
	}
}

func TestRunTurnAutoPlanClassifierBorderlineTrue(t *testing.T) {
	classifier := &fakeAutoPlanClassifier{needsPlan: true, reason: "borderline multi-step"}
	runner := &fakeTurnRunner{}
	c := New(Options{AutoPlan: "on", Classifier: classifier, Runner: runner})

	if err := c.runTurn(context.Background(), "实现一个小的配置入口"); err != nil {
		t.Fatal(err)
	}
	if len(runner.inputs) != 1 || !strings.HasPrefix(agent.StripTransientUserBlocks(runner.inputs[0]), PlanModeMarker) {
		t.Fatalf("classifier true should auto-plan, inputs=%q", runner.inputs)
	}
	if classifier.calls != 1 {
		t.Fatalf("classifier calls = %d, want 1", classifier.calls)
	}
}

func TestRunTurnAutoPlanClassifierBorderlineFalse(t *testing.T) {
	classifier := &fakeAutoPlanClassifier{needsPlan: false, reason: "single obvious edit"}
	runner := &fakeTurnRunner{}
	c := New(Options{AutoPlan: "on", Classifier: classifier, Runner: runner})

	if err := c.runTurn(context.Background(), "实现一个小的配置入口"); err != nil {
		t.Fatal(err)
	}
	if len(runner.inputs) != 1 || strings.HasPrefix(agent.StripTransientUserBlocks(runner.inputs[0]), PlanModeMarker) {
		t.Fatalf("classifier false should skip auto-plan, inputs=%q", runner.inputs)
	}
	if c.PlanMode() {
		t.Fatal("controller plan mode should remain off")
	}
	if classifier.calls != 1 {
		t.Fatalf("classifier calls = %d, want 1", classifier.calls)
	}
}

func TestRunTurnAutoPlanClassifierFallback(t *testing.T) {
	classifier := &fakeAutoPlanClassifier{err: errors.New("bad json")}
	runner := &fakeTurnRunner{}
	c := New(Options{AutoPlan: "on", Classifier: classifier, Runner: runner})

	if err := c.runTurn(context.Background(), "实现 README 文档更新"); err != nil {
		t.Fatal(err)
	}
	if len(runner.inputs) != 1 || !strings.HasPrefix(agent.StripTransientUserBlocks(runner.inputs[0]), PlanModeMarker) {
		t.Fatalf("score 2 should fall back to heuristic auto-plan, inputs=%q", runner.inputs)
	}
	if classifier.calls != 1 {
		t.Fatalf("classifier calls = %d, want 1", classifier.calls)
	}
}

func TestRunTurnAutoPlanTypedNilClassifierFallsBack(t *testing.T) {
	var classifier *ProviderAutoPlanClassifier
	runner := &fakeTurnRunner{}
	c := New(Options{AutoPlan: "on", Classifier: classifier, Runner: runner})

	if err := c.runTurn(context.Background(), "实现 README 文档更新"); err != nil {
		t.Fatal(err)
	}
	if len(runner.inputs) != 1 || !strings.HasPrefix(agent.StripTransientUserBlocks(runner.inputs[0]), PlanModeMarker) {
		t.Fatalf("typed nil classifier should fall back to heuristic auto-plan, inputs=%q", runner.inputs)
	}
}

func TestRunTurnAutoPlanScoresRawPromptNotResolvedRefs(t *testing.T) {
	runner := &fakeTurnRunner{}
	c := New(Options{AutoPlan: "on", Runner: runner})

	resolved := "Referenced context:\n\n" +
		strings.Repeat("实现 重构 配置 测试 文档 多个文件\n", 20) +
		"\n\n解释 @foo.go"
	if err := c.runTurnWithRaw(context.Background(), resolved, "解释 @foo.go"); err != nil {
		t.Fatal(err)
	}
	if len(runner.inputs) != 1 {
		t.Fatalf("runner inputs = %d, want 1", len(runner.inputs))
	}
	if strings.HasPrefix(agent.StripTransientUserBlocks(runner.inputs[0]), PlanModeMarker) {
		t.Fatalf("resolved context should not trigger auto-plan when raw prompt is simple: %q", runner.inputs[0])
	}
	if c.PlanMode() {
		t.Fatal("controller plan mode should remain off")
	}
}

func TestStripComposePrefixes(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "plain user message unchanged",
			input: "explain this function",
			want:  "explain this function",
		},
		{
			name:  "plan mode marker stripped",
			input: PlanModeMarker + "\n\nexplain this function",
			want:  "explain this function",
		},
		{
			name:  "legacy plan mode marker stripped",
			input: legacyPlanModeMarker + "\n\nexplain this function",
			want:  "explain this function",
		},
		{
			name:  "plan mode marker without trailing newlines",
			input: PlanModeMarker,
			want:  "",
		},
		{
			name:  "memory update block stripped",
			input: "<memory-update>\nThe following project-memory changes were just made and apply from now on:\n- Saved memory \"rmb\": user balance\n</memory-update>\n\nexplain this",
			want:  "explain this",
		},
		{
			name:  "background jobs block stripped",
			input: "<background-jobs>\n1 completed\n</background-jobs>\n\nexplain this",
			want:  "explain this",
		},
		{
			name:  "hook context block stripped",
			input: "<hook-context event=\"SessionStart\">\nLoad conventions.\n</hook-context>\n\nexplain this",
			want:  "explain this",
		},
		{
			name:  "memory and plan marker both stripped",
			input: "<memory-update>\n- note\n</memory-update>\n\n" + PlanModeMarker + "\n\nexplain this",
			want:  "explain this",
		},
		{
			name:  "empty after stripping",
			input: PlanModeMarker + "\n\n",
			want:  "",
		},
		{
			name:  "memory update only no user text",
			input: "<memory-update>\n- note\n</memory-update>\n\n",
			want:  "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StripComposePrefixes(tt.input)
			if got != tt.want {
				t.Errorf("StripComposePrefixes() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestStripReferencedContextPrefix(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "plain user message unchanged",
			input: "explain this function",
			want:  "explain this function",
		},
		{
			name:  "file reference stripped",
			input: "Referenced context:\n\n<file path=\"main.go\">\nfunc main() {}\n</file>\n\nexplain this function",
			want:  "explain this function",
		},
		{
			name:  "multiple file references stripped",
			input: "Referenced context:\n\n<file path=\"a.go\">\npackage a\n</file>\n\n<file path=\"b.go\">\npackage b\n</file>\n\ncompare these files",
			want:  "compare these files",
		},
		{
			name:  "dir reference stripped",
			input: "Referenced context:\n\n<dir path=\"src\">\nmain.go\nutil.go\n</dir>\n\nlist the files",
			want:  "list the files",
		},
		{
			name:  "resource reference stripped",
			input: "Referenced context:\n\n<resource ref=\"@server/res\">\ndata\n</resource>\n\nanalyze this",
			want:  "analyze this",
		},
		{
			name:  "image reference stripped",
			input: "Referenced context:\n\n<image path=\"screenshot.png\">\n[image attachment available at @screenshot.png]\n</image>\n\nwhat is in this image",
			want:  "what is in this image",
		},
		{
			name:  "only reference no user text",
			input: "Referenced context:\n\n<file path=\"main.go\">\nfunc main() {}\n</file>\n\n",
			want:  "",
		},
		{
			name:  "empty input",
			input: "",
			want:  "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StripReferencedContextPrefix(tt.input)
			if got != tt.want {
				t.Errorf("StripReferencedContextPrefix() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIsSyntheticUserMessage(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{
			name:  "plan approved message",
			input: planApprovedMessage,
			want:  true,
		},
		{
			name:  "plan approved message with reasoning language",
			input: reasoningLanguageBlock("zh") + "\n\n" + planApprovedMessage,
			want:  true,
		},
		{
			name:  "stream recovery interrupted tool",
			input: "The previous assistant response was interrupted while a tool call was streaming. Continue the same task now.",
			want:  true,
		},
		{
			name:  "stream recovery interrupted text",
			input: "The previous assistant response was interrupted during streaming. Continue the same task from immediately after the partial assistant message above.",
			want:  true,
		},
		{
			name:  "empty final retry",
			input: "The previous assistant response finished without any visible answer text. Continue the same task now and provide a concise visible answer.",
			want:  true,
		},
		{
			name:  "readiness retry",
			input: "Host final-answer readiness check failed. Before giving a final answer, address the missing host-observable receipts: missing evidence.",
			want:  true,
		},
		{
			name:  "executor handoff",
			input: "You are already in the executor phase. The planner's read-only limitations do not apply to you.",
			want:  true,
		},
		{
			name:  "regular user message",
			input: "explain this function",
			want:  false,
		},
		{
			name:  "plan mode marker in message",
			input: PlanModeMarker + "\n\nexplain this",
			want:  false,
		},
		{
			name:  "stream recovery interrupted before visible",
			input: "The previous assistant response was interrupted during streaming before visible answer text was completed. Continue the same task now.",
			want:  true,
		},
		{
			name:  "user quoting interrupted response not synthetic",
			input: "The previous assistant response was interrupted by my VPN, can you retry?",
			want:  false,
		},
		{
			name:  "compaction fold summary",
			input: "<compaction-summary>\nSummary of earlier conversation (older messages were compacted to save context):\nDid things with tools.\n</compaction-summary>",
			want:  true,
		},
		{
			name:  "summarize-from fold",
			input: "Summary of the later conversation (compacted from here on):\nDid more things.",
			want:  true,
		},
		{
			name:  "summarize-upto fold",
			input: "Summary of earlier conversation (compacted up to here):\nDid earlier things.",
			want:  true,
		},
		{
			name:  "user mentioning a summary is not synthetic",
			input: "Summary of what I want: fix the login bug first.",
			want:  false,
		},
		{
			name:  "mid-turn steer is not synthetic (handled separately in historyMessages)",
			input: agent.MidTurnSteerPrefix + "\nplease use smaller diffs",
			want:  false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsSyntheticUserMessage(tt.input)
			if got != tt.want {
				t.Errorf("IsSyntheticUserMessage() = %v, want %v", got, tt.want)
			}
		})
	}
}
