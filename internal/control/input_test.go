package control

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
