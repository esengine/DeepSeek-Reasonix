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
	"reasonix/internal/memory"
	"reasonix/internal/skill"
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
	inputs []string
}

func (f *fakeTurnRunner) Run(ctx context.Context, input string) error {
	f.inputs = append(f.inputs, input)
	return nil
}

type fakeLanguageRunner struct {
	fakeTurnRunner
	lang string
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

func TestComposeReasoningLanguagePreference(t *testing.T) {
	auto := New(Options{ReasoningLanguage: "auto"})
	if got := auto.Compose("hi"); got != "hi" {
		t.Fatalf("auto reasoning language should not alter the turn, got %q", got)
	}

	zh := New(Options{ReasoningLanguage: "zh"})
	got := zh.Compose("hi")
	if !strings.HasPrefix(got, "<reasoning-language>") || !strings.Contains(got, "Simplified Chinese") || !strings.HasSuffix(got, "hi") {
		t.Fatalf("zh reasoning language should ride the user turn, got %q", got)
	}
	if stripped := StripComposePrefixes(got); stripped != "hi" {
		t.Fatalf("StripComposePrefixes = %q, want hi", stripped)
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
	if !strings.HasPrefix(got, "<reasoning-language>") || !strings.Contains(got, "Simplified Chinese") || !strings.HasSuffix(got, "hi") {
		t.Fatalf("headless Run should compose the reasoning language preference, got %q", got)
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

func TestComposeSyntheticReasoningLanguagePreference(t *testing.T) {
	c := New(Options{ReasoningLanguage: "zh"})

	got := c.ComposeSynthetic(planApprovedMessage)
	if !strings.HasPrefix(got, "<reasoning-language>") || !strings.Contains(got, "Simplified Chinese") || !strings.HasSuffix(got, planApprovedMessage) {
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
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for unknown-command notice")
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

func waitForTurnDone(t *testing.T, events <-chan event.Event) {
	t.Helper()
	deadline := time.After(2 * time.Second)
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
	if len(runner.inputs) != 1 || !strings.HasPrefix(runner.inputs[0], PlanModeMarker) {
		t.Fatalf("complex task should auto-enter plan mode, inputs=%q", runner.inputs)
	}
	if !c.PlanMode() {
		t.Fatal("controller plan mode should be on after auto-plan")
	}
	if len(notices) != 1 || !strings.Contains(notices[0], "auto plan") {
		t.Fatalf("notice = %v, want one auto-plan notice", notices)
	}
}

func TestRunTurnAutoPlanSkipsSimpleQuestion(t *testing.T) {
	runner := &fakeTurnRunner{}
	c := New(Options{AutoPlan: "on", Runner: runner})

	if err := c.runTurn(context.Background(), "解释一下这个函数做什么？"); err != nil {
		t.Fatal(err)
	}
	if len(runner.inputs) != 1 || strings.HasPrefix(runner.inputs[0], PlanModeMarker) {
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
	if len(runner.inputs) != 1 || runner.inputs[0] != input {
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
	if len(runner.inputs) != 1 || !strings.HasPrefix(runner.inputs[0], PlanModeMarker) {
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
	if len(runner.inputs) != 1 || !strings.HasPrefix(runner.inputs[0], PlanModeMarker) {
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
	if len(runner.inputs) != 1 || strings.HasPrefix(runner.inputs[0], PlanModeMarker) {
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
	if len(runner.inputs) != 1 || !strings.HasPrefix(runner.inputs[0], PlanModeMarker) {
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
	if len(runner.inputs) != 1 || !strings.HasPrefix(runner.inputs[0], PlanModeMarker) {
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
	if strings.HasPrefix(runner.inputs[0], PlanModeMarker) {
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
