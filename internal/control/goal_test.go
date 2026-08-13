package control

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/goaleval"
	"reasonix/internal/provider"
	"reasonix/internal/store"
	"reasonix/internal/tool"

	_ "reasonix/internal/tool/builtin"
)

// goalRegistry returns a registry carrying the update_goal builtin so scripted
// goal turns can report dispositions through the structured tool.
func goalRegistry() *tool.Registry {
	reg := tool.NewRegistry()
	if t, ok := tool.LookupBuiltin("update_goal"); ok {
		reg.Add(t)
	}
	return reg
}

// goalWireStatus maps FSM status values to the update_goal wire enum.
func goalWireStatus(status string) string {
	switch status {
	case GoalStatusComplete:
		return "complete"
	case GoalStatusBlocked:
		return "blocked"
	default:
		return "continue"
	}
}

// goalToolTurn models one goal turn's provider sequence: the model calls
// update_goal with the given disposition, then answers with text. Call IDs are
// unique per call so recycled scripted turns never collide in the transcript.
func goalToolTurn(status, reason, nextAction string) [][]provider.Chunk {
	args, err := json.Marshal(map[string]string{"status": goalWireStatus(status), "reason": reason, "next_action": nextAction})
	if err != nil {
		panic(err)
	}
	id := fmt.Sprintf("ug-%d", goalToolCallSeq.Add(1))
	return [][]provider.Chunk{
		{toolCallChunk(id, "update_goal", string(args)), {Type: provider.ChunkDone}},
		textTurn("worked on the goal"),
	}
}

var goalToolCallSeq atomic.Uint64

// fakeGoalEvaluator is a scripted bounded Goal evaluator for tests.
type fakeGoalEvaluator struct {
	outcome goaleval.Outcome
	reason  string
	err     error
	calls   int
}

func (f *fakeGoalEvaluator) Evaluate(_ context.Context, _ goaleval.GoalEvidence) (goaleval.Verdict, error) {
	f.calls++
	if f.err != nil {
		return goaleval.Verdict{}, f.err
	}
	return goaleval.Verdict{Outcome: f.outcome, Reason: f.reason}, nil
}

func TestGoalCommandAutoContinuesUntilComplete(t *testing.T) {
	prov := &scriptedTurns{turns: flattenTurns(
		goalToolTurn(GoalStatusRunning, "work in progress", "next step"),
		goalToolTurn(GoalStatusComplete, "", ""),
	)}
	ag := agent.New(prov, goalRegistry(), agent.NewSession(""), agent.Options{}, event.Discard)
	events := make(chan event.Event, 8)
	c := New(Options{
		Runner:   ag,
		Executor: ag,
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.TurnDone || e.Kind == event.Notice {
				events <- e
			}
		}),
	})

	c.Submit("/goal ship the redesign")
	waitForTurnDone(t, events)

	if prov.call != 4 {
		t.Fatalf("provider calls = %d, want 4 (continue report + continuation complete report)", prov.call)
	}
	if got := c.Goal(); got != "" {
		t.Fatalf("completed goal should be cleared, got %q", got)
	}
	if got := c.GoalStatus(); got != GoalStatusComplete {
		t.Fatalf("GoalStatus() = %q, want complete", got)
	}
	first := firstUserMessage(ag.Session().Messages)
	if !strings.Contains(first, "<active-goal>\nship the redesign") {
		t.Fatalf("first goal turn should include active goal block, got %q", first)
	}
	if strings.HasPrefix(first, PlanModeMarker) {
		t.Fatalf("goal mode should not enter plan mode, got %q", first)
	}
}

// flattenTurns concatenates per-goal-turn provider sequences into one flat
// scripted provider stream.
func flattenTurns(groups ...[][]provider.Chunk) [][]provider.Chunk {
	var out [][]provider.Chunk
	for _, g := range groups {
		out = append(out, g...)
	}
	return out
}

// toolCallChunk builds a provider turn carrying one tool call.
func toolCallChunk(id, name, args string) provider.Chunk {
	return provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: id, Name: name, Arguments: args}}
}

func TestActiveGoalBlockCarriesTaskContractAndPausePolicy(t *testing.T) {
	block := activeGoalBlock("fix the parser")
	for _, want := range []string{
		"Treat the user's goal as a task contract",
		"Context, Request, Output format, Constraints",
		"Pause policy",
		"irreversible or externally visible operation",
		"the requested scope has changed",
		"information only the user can provide",
		"output format and constraints are satisfied",
	} {
		if !strings.Contains(block, want) {
			t.Fatalf("active goal block missing %q:\n%s", want, block)
		}
	}
	if strings.Contains(block, "AutoResearch protocol") {
		t.Fatalf("simple goal should not include AutoResearch protocol:\n%s", block)
	}
}

func TestPlainInputWithStrongResearchSignalStaysNormal(t *testing.T) {
	prov := &scriptedTurns{turns: [][]provider.Chunk{
		textTurn("Here is the normal response."),
	}}
	ag := agent.New(prov, tool.NewRegistry(), agent.NewSession(""), agent.Options{}, event.Discard)
	events := make(chan event.Event, 8)
	c := New(Options{
		Runner:   ag,
		Executor: ag,
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.TurnDone || e.Kind == event.Notice {
				events <- e
			}
		}),
	})

	c.Submit("持续排查这个线上卡顿直到根因明确，并验证修复")
	waitForTurnDone(t, events)

	if prov.call != 1 {
		t.Fatalf("provider calls = %d, want 1", prov.call)
	}
	first := agent.StripTransientUserBlocks(firstUserMessage(ag.Session().Messages))
	if !strings.HasSuffix(first, "持续排查这个线上卡顿直到根因明确，并验证修复") {
		t.Fatalf("ordinary turn should preserve the original prompt suffix: %q", first)
	}
	if strings.Contains(first, "<active-goal>") || strings.Contains(first, "AutoResearch protocol") {
		t.Fatalf("ordinary prompt should not enter Goal or AutoResearch:\n%s", first)
	}
	if got := c.GoalStatus(); got != GoalStatusStopped {
		t.Fatalf("GoalStatus() = %q, want stopped", got)
	}
}

func TestPlainInputWithStrongResearchSignalPreservesRefsWithoutStartingGoal(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("important referenced evidence"), 0o644); err != nil {
		t.Fatal(err)
	}
	prov := &scriptedTurns{turns: [][]provider.Chunk{
		textTurn("Referenced normal response."),
	}}
	ag := agent.New(prov, tool.NewRegistry(), agent.NewSession(""), agent.Options{}, event.Discard)
	events := make(chan event.Event, 8)
	c := New(Options{
		WorkspaceRoot: root,
		Runner:        ag,
		Executor:      ag,
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.TurnDone || e.Kind == event.Notice {
				events <- e
			}
		}),
	})

	c.Submit("持续排查直到根因明确，并验证 @notes.txt")
	waitForTurnDone(t, events)

	first := firstUserMessage(ag.Session().Messages)
	for _, want := range []string{
		"important referenced evidence",
	} {
		if !strings.Contains(first, want) {
			t.Fatalf("ordinary turn with refs missing %q:\n%s", want, first)
		}
	}
	if strings.Contains(first, "<active-goal>") || strings.Contains(first, "AutoResearch protocol") {
		t.Fatalf("ordinary prompt with refs should not enter Goal or AutoResearch:\n%s", first)
	}
	if got := c.GoalStatus(); got != GoalStatusStopped {
		t.Fatalf("GoalStatus() = %q, want stopped", got)
	}
	if _, err := os.Stat(filepath.Join(root, ".reasonix", "autoresearch")); !os.IsNotExist(err) {
		t.Fatalf("ordinary prompt created AutoResearch state: err=%v", err)
	}
}

func TestResearchGoalUsesContinuousRuntimeWithoutArchive(t *testing.T) {
	root := t.TempDir()
	sessionPath := filepath.Join(root, "sessions", "s.jsonl")
	sess := agent.NewSession("sys")
	exec := agent.New(nil, nil, sess, agent.Options{}, event.Discard)
	c := New(Options{WorkspaceRoot: root, SessionDir: root, Executor: exec})
	c.Resume(sess, sessionPath)
	c.SetGoalWithResearchMode("fix the typo and add a test", GoalResearchOn)
	defer c.Close()
	if got := c.GoalRuntime().TurnsLimit; got != 0 {
		t.Fatalf("research Goal turns limit = %d, want unlimited", got)
	}
	// The class still drives behaviour; it just no longer mints a turn quota.
	if got := c.goals.budgetClass; got != budgetClassResearch {
		t.Fatalf("research Goal budget class = %q, want %q", got, budgetClassResearch)
	}
	if _, err := os.Stat(filepath.Join(root, ".reasonix", "autoresearch")); !os.IsNotExist(err) {
		t.Fatalf("research Goal created legacy archive: %v", err)
	}
	if composed := c.Compose("continue"); strings.Contains(composed, "AutoResearch") || strings.Contains(composed, "autoresearch") {
		t.Fatalf("Goal prompt exposes removed AutoResearch protocol:\n%s", composed)
	}
}

func TestLegacyGoalSidecarMigratesToContinuousRuntimeWithoutTaskID(t *testing.T) {
	root := t.TempDir()
	sessionPath := filepath.Join(root, "sessions", "s.jsonl")
	if err := os.MkdirAll(filepath.Dir(sessionPath), 0o755); err != nil {
		t.Fatal(err)
	}
	writeLegacyGoalArchive(t, root, "old-task", "archive fallback should not replace sidecar goal")
	if err := os.WriteFile(goalStatePath(sessionPath), []byte(`{"goal":"investigate runtime","status":"running","researchMode":1,"autoResearchTaskID":"old-task"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	sess := agent.NewSession("sys")
	exec := agent.New(nil, nil, sess, agent.Options{}, event.Discard)
	c := New(Options{WorkspaceRoot: root, SessionDir: root, Executor: exec})
	c.Resume(sess, sessionPath)
	defer c.Close()
	if got := c.GoalRuntime().TurnsLimit; got != 0 {
		t.Fatalf("migrated Goal turns limit = %d, want unlimited", got)
	}
	if got := c.goals.budgetClass; got != budgetClassResearch {
		t.Fatalf("migrated Goal budget class = %q, want %q", got, budgetClassResearch)
	}
	if got := c.Goal(); got != "investigate runtime" {
		t.Fatalf("migrated Goal = %q, want sidecar goal", got)
	}
	raw, err := os.ReadFile(goalStatePath(sessionPath))
	if err != nil {
		t.Fatal(err)
	}
	var state goalState
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatal(err)
	}
	if state.AutoResearchTaskID != "" {
		t.Fatalf("migrated sidecar retained old task id: %q", state.AutoResearchTaskID)
	}
}

func TestMissingExplicitLegacyTaskBlocksWithoutCreatingArchive(t *testing.T) {
	root := t.TempDir()
	c := New(Options{WorkspaceRoot: root})
	defer c.Close()
	c.SetGoalWithResearchMode("resume .reasonix/autoresearch/missing-task/", GoalResearchOn)
	if got := c.GoalStatus(); got != GoalStatusBlocked {
		t.Fatalf("GoalStatus = %q, want blocked", got)
	}
	if _, err := os.Stat(filepath.Join(root, ".reasonix", "autoresearch")); !os.IsNotExist(err) {
		t.Fatalf("missing legacy task created archive: %v", err)
	}

	c.SetGoal("resume .reasonix/autoresearch/missing-task/../../escape")
	if got := c.GoalStatus(); got != GoalStatusBlocked {
		t.Fatalf("unsafe legacy path status = %q, want blocked", got)
	}
	if got := c.Goal(); got != "resume .reasonix/autoresearch/missing-task/../../escape" {
		t.Fatalf("unsafe legacy path silently resumed a truncated task: %q", got)
	}
}

func TestAssistantEvidenceBlockIsIgnoredByUnifiedGoal(t *testing.T) {
	root := t.TempDir()
	sessionPath := filepath.Join(root, "sessions", "s.jsonl")
	turns := goalToolTurn(GoalStatusComplete, "", "")
	const evidenceBlock = `<autoresearch-evidence>{"id":"legacy-evidence","kind":"verification","summary":"must remain ordinary assistant text"}</autoresearch-evidence>`
	turns[len(turns)-1] = textTurn("worked on the goal\n" + evidenceBlock)
	prov := &scriptedTurns{turns: flattenTurns(turns)}
	ag := agent.New(prov, goalRegistry(), agent.NewSession(""), agent.Options{}, event.Discard)
	c := New(Options{WorkspaceRoot: root, SessionPath: sessionPath, Runner: ag, Executor: ag})
	defer c.Close()
	c.SetGoalWithResearchMode("verify the fix", GoalResearchOn)
	_ = newTurnOrchestrator(c).runGoalLoopWithRawDisplay(context.Background(), "start", "start", "start")
	if got := c.GoalStatus(); got != GoalStatusComplete {
		t.Fatalf("GoalStatus = %q, want complete", got)
	}
	if got := lastAssistantText(c.History()); !strings.Contains(got, evidenceBlock) {
		t.Fatalf("legacy evidence block was interpreted instead of retained as transcript text: %q", got)
	}
	if _, err := os.Stat(filepath.Join(root, ".reasonix", "autoresearch")); !os.IsNotExist(err) {
		t.Fatalf("assistant evidence created archive: %v", err)
	}
}

func TestExplicitLegacyTaskPathRestoresOriginalGoal(t *testing.T) {
	root := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	taskID := "20260630-original-goal"
	taskRoot := filepath.Join(root, ".reasonix", "autoresearch", taskID)
	if err := os.MkdirAll(filepath.Join(taskRoot, "state"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(taskRoot, "logs"), 0o755); err != nil {
		t.Fatal(err)
	}
	spec := `{"task_id":"` + taskID + `","goal":"find the original root cause","allowed_operations":{"write":true},"success_criteria":[]}`
	progress := `{"status":"running","iteration":2,"updated_at":"2026-06-30T10:00:00Z"}`
	for name, body := range map[string]string{
		"state/task_spec.json":        spec,
		"state/progress.json":         progress,
		"state/directions_tried.json": "[]\n",
		"state/findings.jsonl":        "",
		"state/iteration_log.jsonl":   "",
		"logs/heartbeat.jsonl":        "",
	} {
		if err := os.WriteFile(filepath.Join(taskRoot, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	before, err := os.ReadFile(filepath.Join(taskRoot, "state", "task_spec.json"))
	if err != nil {
		t.Fatal(err)
	}
	c := New(Options{WorkspaceRoot: root})
	defer c.Close()
	c.SetGoalWithResearchMode("resume .reasonix/autoresearch/"+taskID+"/", GoalResearchAuto)
	if got := c.Goal(); got != "find the original root cause" {
		t.Fatalf("Goal() = %q, want original archive goal", got)
	}
	if got := c.GoalRuntime().TurnsLimit; got != 0 {
		t.Fatalf("turns limit = %d, want unlimited", got)
	}
	if got := c.goals.budgetClass; got != budgetClassResearch {
		t.Fatalf("budget class = %q, want %q", got, budgetClassResearch)
	}
	if got := c.GoalStatus(); got != GoalStatusRunning {
		t.Fatalf("status = %q", got)
	}
	after, err := os.ReadFile(filepath.Join(taskRoot, "state", "task_spec.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("archive task_spec mutated during resume")
	}
}

func TestLegacySidecarEmptyGoalFilledFromArchive(t *testing.T) {
	root := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	sessionPath := filepath.Join(root, "sessions", "s.jsonl")
	if err := os.MkdirAll(filepath.Dir(sessionPath), 0o755); err != nil {
		t.Fatal(err)
	}
	taskID := "fill-from-archive"
	taskRoot := filepath.Join(root, ".reasonix", "autoresearch", taskID)
	if err := os.MkdirAll(filepath.Join(taskRoot, "state"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(taskRoot, "logs"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"state/task_spec.json":        `{"task_id":"` + taskID + `","goal":"recover me from archive","allowed_operations":{"write":true},"success_criteria":[]}`,
		"state/progress.json":         `{"status":"running","updated_at":"2026-06-30T10:00:00Z"}`,
		"state/directions_tried.json": "[]\n",
		"state/findings.jsonl":        "",
		"state/iteration_log.jsonl":   "",
		"logs/heartbeat.jsonl":        "",
	} {
		if err := os.WriteFile(filepath.Join(taskRoot, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(goalStatePath(sessionPath), []byte(`{"status":"running","researchMode":1,"autoResearchTaskID":"`+taskID+`","turnsUsed":3,"turnsLimit":40}`), 0o644); err != nil {
		t.Fatal(err)
	}
	sess := agent.NewSession("sys")
	exec := agent.New(nil, nil, sess, agent.Options{}, event.Discard)
	c := New(Options{WorkspaceRoot: root, SessionDir: root, Executor: exec})
	c.Resume(sess, sessionPath)
	defer c.Close()
	if got := c.Goal(); got != "recover me from archive" {
		t.Fatalf("Goal() = %q", got)
	}
	if got := c.GoalRuntime().TurnsUsed; got != 3 {
		t.Fatalf("turns used = %d, want preserved 3", got)
	}
	raw, err := os.ReadFile(goalStatePath(sessionPath))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "autoResearchTaskID") {
		t.Fatalf("sidecar retained task id: %s", raw)
	}
}

func TestPlainInputWithWeakResearchSignalStaysNormal(t *testing.T) {
	prov := &scriptedTurns{turns: [][]provider.Chunk{
		textTurn("Here is a normal answer."),
	}}
	ag := agent.New(prov, tool.NewRegistry(), agent.NewSession(""), agent.Options{}, event.Discard)
	events := make(chan event.Event, 4)
	c := New(Options{
		Runner:   ag,
		Executor: ag,
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.TurnDone {
				events <- e
			}
		}),
	})

	c.Submit("长期来看这个模块怎么优化？")
	waitForTurnDone(t, events)

	first := firstUserMessage(ag.Session().Messages)
	if strings.Contains(first, "<active-goal>") || strings.Contains(first, "AutoResearch protocol") {
		t.Fatalf("ordinary prompt should stay outside Goal and AutoResearch:\n%s", first)
	}
	if got := c.GoalStatus(); got != GoalStatusStopped {
		t.Fatalf("GoalStatus() = %q, want stopped", got)
	}
}

func TestCancelStopsIdleGoalWithIncompleteTodos(t *testing.T) {
	ag := agent.New(nil, nil, agent.NewSession(""), agent.Options{}, event.Discard)
	ag.SeedTodoState([]evidence.TodoItem{{Content: "finish the migration", Status: "in_progress"}})
	c := New(Options{Executor: ag, Sink: event.Discard})
	c.SetGoalWithResearchMode("finish the migration", GoalResearchOn)

	c.Cancel()

	if got := c.GoalStatus(); got != GoalStatusStopped {
		t.Fatalf("GoalStatus() = %q, want stopped", got)
	}
	if got := c.Goal(); got != "finish the migration" {
		t.Fatalf("Goal() = %q, want stopped goal text to remain for display/persistence", got)
	}
	if todos := c.Todos(); len(todos) != 1 || todos[0].Status != "in_progress" {
		t.Fatalf("Todos() after stopping idle goal = %+v, want incomplete todo retained", todos)
	}
}

func TestGoalRepeatedBlockedStopsAfterThreeTurns(t *testing.T) {
	prov := &scriptedTurns{turns: flattenTurns(
		goalToolTurn(GoalStatusBlocked, "Needs credentials.", ""),
	)}
	ag := agent.New(prov, goalRegistry(), agent.NewSession(""), agent.Options{}, event.Discard)
	events := make(chan event.Event, 8)
	c := New(Options{
		Runner:   ag,
		Executor: ag,
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.TurnDone || e.Kind == event.Notice {
				events <- e
			}
		}),
	})

	c.Submit("/goal deploy the service")
	waitForTurnDone(t, events)

	if prov.call != 2 {
		t.Fatalf("provider calls = %d, want 1 goal turn (report + final answer)", prov.call)
	}
	if got := c.GoalStatus(); got != GoalStatusBlocked {
		t.Fatalf("GoalStatus() = %q, want blocked", got)
	}
	if rt := c.GoalRuntime(); rt.StopCause != "" {
		t.Fatalf("StopCause = %q, want empty for a genuine task block", rt.StopCause)
	}
}

// TestGoalBlockedReportTransitionsImmediately pins the FSM decision: a single
// blocked report ends the goal at once — no three-turn confirmation ritual and
// no intercept.
func TestGoalBlockedReportTransitionsImmediately(t *testing.T) {
	g := &goalMachine{goal: "wait for user review", status: GoalStatusRunning}
	g.turnsLimit = unlimitedGoalTurns
	g.noProgressLimit = 0

	res := g.advance(goalAdvanceInput{
		report: &goalTurnReport{status: GoalStatusBlocked, reason: "waiting for user review"},
	})

	if res.cont {
		t.Fatal("blocked report must stop the goal loop immediately")
	}
	if res.intercept != "" {
		t.Fatalf("blocked report triggered an intercept %q", res.intercept)
	}
	if g.status != GoalStatusBlocked {
		t.Fatalf("machine status = %q, want blocked", g.status)
	}
}

func TestGoalRestartClearsBlockedAndCompletesOnRetry(t *testing.T) {
	prov := &scriptedTurns{turns: flattenTurns(
		goalToolTurn(GoalStatusBlocked, "needs credentials", ""),
		goalToolTurn(GoalStatusComplete, "", ""),
	)}
	ag := agent.New(prov, goalRegistry(), agent.NewSession(""), agent.Options{}, event.Discard)
	events := make(chan event.Event, 12)
	c := New(Options{
		Runner:   ag,
		Executor: ag,
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.TurnDone || e.Kind == event.Notice {
				events <- e
			}
		}),
	})

	c.Submit("/goal deploy the service")
	waitForTurnDone(t, events)
	if got := c.GoalStatus(); got != GoalStatusBlocked {
		t.Fatalf("first run GoalStatus() = %q, want blocked", got)
	}

	c.Submit("/goal deploy the service")
	waitForTurnDone(t, events)
	if prov.call != 4 {
		t.Fatalf("provider calls = %d, want 2 goal turns (blocked + resumed complete)", prov.call)
	}
	if got := c.GoalStatus(); got != GoalStatusComplete {
		t.Fatalf("resumed GoalStatus() = %q, want complete; a fresh run starts a clean audit", got)
	}
}

// TestIncompleteGoalTodos verifies that formatIncompleteTodos detects
// unfinished tasks and returns a formatted reminder, and returns empty
// when all todos are complete.
func TestIncompleteGoalTodos(t *testing.T) {
	prov := &scriptedTurns{turns: [][]provider.Chunk{textTurn("done")}}
	ag := agent.New(prov, tool.NewRegistry(), agent.NewSession(""), agent.Options{}, event.Discard)
	c := New(Options{Runner: ag, Executor: ag, Sink: event.Discard})
	reminder := func() string { return formatIncompleteTodos(c.goalTodos(), ag.ReadinessResult().Reason) }

	// Seed with incomplete todos.
	ag.SeedTodoState([]evidence.TodoItem{
		{Content: "Fix the parser", Status: "in_progress"},
		{Content: "Add tests", Status: "pending"},
	})
	msg := reminder()
	if msg == "" {
		t.Fatal("formatIncompleteTodos() returned empty string, expected reminder")
	}
	if !strings.Contains(msg, "Fix the parser") {
		t.Fatalf("reminder should mention 'Fix the parser', got: %q", msg)
	}
	if !strings.Contains(msg, "Add tests") {
		t.Fatalf("reminder should mention 'Add tests', got: %q", msg)
	}
	if !strings.Contains(msg, "update_goal") {
		t.Fatalf("reminder should tell the model how to finish, got: %q", msg)
	}

	// Mark all complete.
	ag.ReplaceTodoState([]evidence.TodoItem{
		{Content: "Fix the parser", Status: "completed"},
		{Content: "Add tests", Status: "completed"},
	})
	if got := reminder(); got != "" {
		t.Fatalf("formatIncompleteTodos() with all-complete = %q, want empty", got)
	}

	// Empty todo list.
	ag.ReplaceTodoState(nil)
	if got := reminder(); got != "" {
		t.Fatalf("formatIncompleteTodos() with empty list = %q, want empty", got)
	}
}

// TestGoalInterceptsCompleteWithIncompleteTodos verifies that a complete report
// with unfinished canonical todos is always rejected: the FSM continues with
// the missing requirements, and completion is accepted only once the todos are
// actually done (there is no override path anymore).
func TestGoalInterceptsCompleteWithIncompleteTodos(t *testing.T) {
	todoWrite, ok := tool.LookupBuiltin("todo_write")
	if !ok {
		t.Fatal("todo_write builtin not registered")
	}
	completeStep, ok := tool.LookupBuiltin("complete_step")
	if !ok {
		t.Fatal("complete_step builtin not registered")
	}
	reg := goalRegistry()
	reg.Add(todoWrite)
	reg.Add(completeStep)
	completeTurn := [][]provider.Chunk{
		{toolCallChunk("ug1", "update_goal", `{"status":"complete","reason":""}`), {Type: provider.ChunkDone}},
		textTurn("All done."),
	}
	fixedTurn := [][]provider.Chunk{
		{toolCallChunk("cs1", "complete_step", `{"step":"Fix the parser","result":"fixed","evidence":[{"kind":"manual","summary":"verified by inspection"}]}`), {Type: provider.ChunkDone}},
		{toolCallChunk("t1", "todo_write", `{"todos":[{"content":"Fix the parser","status":"completed"}]}`), {Type: provider.ChunkDone}},
		{toolCallChunk("ug2", "update_goal", `{"status":"complete","reason":""}`), {Type: provider.ChunkDone}},
		textTurn("All done now."),
	}
	prov := &scriptedTurns{turns: flattenTurns(completeTurn, fixedTurn)}
	ag := agent.New(prov, reg, agent.NewSession(""), agent.Options{}, event.Discard)
	// Seed incomplete todos before starting.
	ag.SeedTodoState([]evidence.TodoItem{
		{Content: "Fix the parser", Status: "in_progress"},
	})

	notices := make(chan string, 64)
	done := make(chan event.Event, 1)
	c := New(Options{
		Runner:   ag,
		Executor: ag,
		Sink: event.FuncSink(func(e event.Event) {
			switch e.Kind {
			case event.Notice:
				notices <- e.Text
			case event.TurnDone:
				done <- e
			}
		}),
	})

	c.Submit("/goal fix everything")
	<-done // wait for the entire goal loop to finish
	close(notices)

	// Collect all notices.
	var allNotices []string
	for n := range notices {
		allNotices = append(allNotices, n)
	}

	found := false
	for _, n := range allNotices {
		if strings.Contains(n, "Goal is not ready to complete yet") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected a not-ready continuation notice, got %v", allNotices)
	}
	if prov.call != 6 {
		t.Fatalf("provider calls = %d, want intercepted turn + fixed turn (2 and 4 calls)", prov.call)
	}
	if c.GoalStatus() != GoalStatusComplete {
		t.Fatalf("GoalStatus() = %q, want complete after the todos were actually finished", c.GoalStatus())
	}
}

func TestGoalAdvanceResultCannotCrossGoalLifecycle(t *testing.T) {
	newResult := func(t *testing.T, g *goalMachine) goalAdvanceResult {
		t.Helper()
		g.set("old goal", "", nil)
		res := g.advance(goalAdvanceInput{
			report: &goalTurnReport{status: GoalStatusComplete, reason: ""},
			todos: []evidence.TodoItem{{
				Content: "unfinished work from old goal",
				Status:  "in_progress",
			}},
		})
		if res.intercept == "" {
			t.Fatal("test setup: expected an incomplete-todo intercept")
		}
		return res
	}

	t.Run("current result is accepted", func(t *testing.T) {
		var g goalMachine
		res := newResult(t, &g)
		if got, ok := g.acceptContinuation(res); !ok || got != res.intercept {
			t.Fatalf("acceptContinuation() = (%q, %v), want current intercept", got, ok)
		}
	})

	t.Run("replacement goal invalidates result", func(t *testing.T) {
		var g goalMachine
		res := newResult(t, &g)
		g.set("replacement goal", "", nil)
		if got, ok := g.acceptContinuation(res); ok {
			t.Fatalf("replacement goal accepted stale intercept %q", got)
		}
	})

	t.Run("stop and resume invalidates result", func(t *testing.T) {
		var g goalMachine
		res := newResult(t, &g)
		g.stop(GoalStatusStopped, nil)
		if _, _, _, resumed := g.resume(nil); !resumed {
			t.Fatal("test setup: goal did not resume")
		}
		if got, ok := g.acceptContinuation(res); ok {
			t.Fatalf("resumed goal accepted stale intercept %q", got)
		}
	})

	t.Run("newer advance invalidates result", func(t *testing.T) {
		var g goalMachine
		res := newResult(t, &g)
		g.advance(goalAdvanceInput{report: &goalTurnReport{status: GoalStatusRunning, reason: "keep going"}})
		if got, ok := g.acceptContinuation(res); ok {
			t.Fatalf("newer FSM step accepted stale intercept %q", got)
		}
	})
}

// TestGoalCompletionRequiresAllTodosDone verifies that a goal with seeded
// incomplete canonical todos cannot complete until the model marks them done;
// the completing turn force-completes any stragglers via a synthetic todo_write
// so the frontend panel reflects the final state.
func TestGoalCompletionRequiresAllTodosDone(t *testing.T) {
	todoWrite, ok := tool.LookupBuiltin("todo_write")
	if !ok {
		t.Fatal("todo_write builtin not registered")
	}
	completeStep, ok := tool.LookupBuiltin("complete_step")
	if !ok {
		t.Fatal("complete_step builtin not registered")
	}
	reg := goalRegistry()
	reg.Add(todoWrite)
	reg.Add(completeStep)
	prov := &scriptedTurns{turns: flattenTurns(
		[][]provider.Chunk{
			{toolCallChunk("cs1", "complete_step", `{"step":"Step 1","result":"done","evidence":[{"kind":"manual","summary":"verified"}]}`), {Type: provider.ChunkDone}},
			{toolCallChunk("cs2", "complete_step", `{"step":"Step 2","result":"done","evidence":[{"kind":"manual","summary":"verified"}]}`), {Type: provider.ChunkDone}},
			{toolCallChunk("t1", "todo_write", `{"todos":[{"content":"Step 1","status":"completed"},{"content":"Step 2","status":"completed"}]}`), {Type: provider.ChunkDone}},
			{toolCallChunk("ug1", "update_goal", `{"status":"complete","reason":""}`), {Type: provider.ChunkDone}},
			textTurn("All done."),
		},
	)}
	ag := agent.New(prov, reg, agent.NewSession(""), agent.Options{}, event.Discard)
	ag.SeedTodoState([]evidence.TodoItem{
		{Content: "Step 1", Status: "in_progress"},
		{Content: "Step 2", Status: "pending"},
	})

	var tools []event.Event
	done := make(chan event.Event, 1)
	c := New(Options{
		Runner:   ag,
		Executor: ag,
		Sink: event.FuncSink(func(e event.Event) {
			switch e.Kind {
			case event.ToolDispatch, event.ToolResult:
				tools = append(tools, e)
			case event.TurnDone:
				done <- e
			}
		}),
	})

	c.Submit("/goal do everything")
	<-done // wait for the goal loop to finish

	if c.GoalStatus() != GoalStatusComplete {
		t.Fatalf("GoalStatus() = %q, want complete", c.GoalStatus())
	}

	// All todos in the executor must be completed.
	for _, td := range c.executor.CanonicalTodoState() {
		if td.Status != "completed" {
			t.Fatalf("canonical todo %q = %s, want completed", td.Content, td.Status)
		}
	}
}

// TestCompleteRemainingGoalTodosEdgeCases verifies that the helper is a no-op
// when there are no incomplete todos or no todos at all.
func TestCompleteRemainingGoalTodosEdgeCases(t *testing.T) {
	t.Run("empty todo list does nothing", func(t *testing.T) {
		ag := agent.New(nil, nil, agent.NewSession(""), agent.Options{}, event.Discard)
		c := New(Options{Executor: ag, Sink: event.Discard})
		c.completeRemainingGoalTodos()
		if len(ag.CanonicalTodoState()) != 0 {
			t.Fatal("expected no changes to empty todo list")
		}
	})

	t.Run("all completed does nothing", func(t *testing.T) {
		ag := agent.New(nil, nil, agent.NewSession(""), agent.Options{}, event.Discard)
		ag.SeedTodoState([]evidence.TodoItem{
			{Content: "A", Status: "completed"},
			{Content: "B", Status: "completed"},
		})
		var events []event.Event
		c := New(Options{
			Executor: ag,
			Sink: event.FuncSink(func(e event.Event) {
				events = append(events, e)
			}),
		})
		c.completeRemainingGoalTodos()
		if len(events) > 0 {
			t.Fatalf("expected no events when all todos already completed, got %d", len(events))
		}
	})

	t.Run("force-completes mixed todos", func(t *testing.T) {
		ag := agent.New(nil, nil, agent.NewSession(""), agent.Options{}, event.Discard)
		ag.SeedTodoState([]evidence.TodoItem{
			{Content: "A", Status: "completed"},
			{Content: "B", Status: "in_progress"},
			{Content: "C", Status: "pending"},
		})
		var captured []event.Event
		c := New(Options{
			Executor: ag,
			Sink: event.FuncSink(func(e event.Event) {
				if e.Kind == event.ToolDispatch || e.Kind == event.ToolResult {
					captured = append(captured, e)
				}
			}),
		})
		c.completeRemainingGoalTodos()
		// All must be completed.
		for _, td := range ag.CanonicalTodoState() {
			if td.Status != "completed" {
				t.Fatalf("todo %q = %s, want completed", td.Content, td.Status)
			}
		}
		// Must include a ToolDispatch+ToolResult for the synthetic todo_write.
		if len(captured) != 2 {
			t.Fatalf("expected 2 synthetic events (dispatch+result), got %d", len(captured))
		}
		if captured[0].Kind != event.ToolDispatch || captured[0].Tool.Name != "todo_write" {
			t.Fatalf("first event should be ToolDispatch for todo_write, got %+v", captured[0].Kind)
		}
		if captured[1].Kind != event.ToolResult || captured[1].Tool.Name != "todo_write" {
			t.Fatalf("second event should be ToolResult for todo_write, got %+v", captured[1].Kind)
		}
	})

	t.Run("empty-string status treated as incomplete", func(t *testing.T) {
		ag := agent.New(nil, nil, agent.NewSession(""), agent.Options{}, event.Discard)
		ag.SeedTodoState([]evidence.TodoItem{
			{Content: "A", Status: ""},
			{Content: "B", Status: "completed"},
		})
		c := New(Options{Executor: ag, Sink: event.Discard})
		c.completeRemainingGoalTodos()
		for _, td := range ag.CanonicalTodoState() {
			if td.Status != "completed" {
				t.Fatalf("empty-string todo %q should be force-completed, got %q", td.Content, td.Status)
			}
		}
	})
}

// TestRepeatedCompleteWithIncompleteTodosKeepsWorking verifies that readiness
// rejection cannot be converted into a numeric pause.
func TestRepeatedCompleteWithIncompleteTodosKeepsWorking(t *testing.T) {
	g := &goalMachine{goal: "fix everything", status: GoalStatusRunning, turnsLimit: unlimitedGoalTurns}
	todos := []evidence.TodoItem{{Content: "Fix the parser", Status: "in_progress"}}
	for i := range 101 {
		res := g.advance(goalAdvanceInput{report: &goalTurnReport{status: GoalStatusComplete}, todos: todos})
		if !res.cont || g.status != GoalStatusRunning || g.stopCause != "" {
			t.Fatalf("readiness rejection paused at turn %d: result=%+v runtime=%+v", i+1, res, g.runtimeView())
		}
	}
}

// TestSessionRotationClearsActiveGoal pins the /new & /clear goal semantics:
// a fresh session starts with no active goal (so the old goal's text stops
// injecting into its first turns), while the OLD session's persisted
// goal-state sidecar keeps the running goal so resuming it restores the goal.
func TestSessionRotationClearsActiveGoal(t *testing.T) {
	dir := t.TempDir()
	exec := agent.New(nil, nil, agent.NewSession("sys"), agent.Options{}, event.Discard)
	oldPath := filepath.Join(dir, "session.jsonl")
	c := New(Options{Executor: exec, SystemPrompt: "sys", SessionDir: dir, SessionPath: oldPath, Label: "test"})

	c.SetGoal("ship the release checklist")
	if got := c.Goal(); got != "ship the release checklist" {
		t.Fatalf("Goal() = %q after SetGoal", got)
	}
	if composed := c.Compose("hello"); !strings.Contains(composed, "<active-goal>") {
		t.Fatalf("running goal should inject into turns, composed = %q", composed)
	}

	if err := c.NewSession(); err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if got := c.Goal(); got != "" {
		t.Fatalf("Goal() after /new = %q, want empty", got)
	}
	if composed := c.Compose("hello"); strings.Contains(composed, "<active-goal>") {
		t.Fatalf("old goal leaked into the fresh session's turn: %q", composed)
	}
	// The old session keeps its running goal on disk for /resume.
	oldState, err := os.ReadFile(store.SessionGoalState(oldPath))
	if err != nil {
		t.Fatalf("read old goal state: %v", err)
	}
	if !strings.Contains(string(oldState), "ship the release checklist") || !strings.Contains(string(oldState), GoalStatusRunning) {
		t.Fatalf("old session's goal state was disturbed by /new: %s", oldState)
	}
	// The new session's sidecar records the cleared (stopped) state, so
	// profile restores read it as "no running goal".
	newState, err := os.ReadFile(store.SessionGoalState(c.SessionPath()))
	if err != nil {
		t.Fatalf("read new goal state: %v", err)
	}
	if strings.Contains(string(newState), "ship the release checklist") {
		t.Fatalf("new session's goal state carries the old goal: %s", newState)
	}

	// Same contract for /clear.
	c.SetGoal("another goal")
	if err := c.ClearSession(); err != nil {
		t.Fatalf("ClearSession: %v", err)
	}
	if got := c.Goal(); got != "" {
		t.Fatalf("Goal() after /clear = %q, want empty", got)
	}
	if composed := c.Compose("hello"); strings.Contains(composed, "<active-goal>") {
		t.Fatalf("old goal leaked into the cleared session's turn: %q", composed)
	}
}

func TestGoalSidecarRoundTripPreservesBlockedDeliveryCheckpoint(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	exec := agent.New(nil, nil, agent.NewSession("sys"), agent.Options{}, event.Discard)
	c := New(Options{Executor: exec, SessionDir: dir, SessionPath: path, Label: "test"})
	c.SetGoal("finish the delivery")
	scopeID, _, ok := c.goals.deliveryScope()
	if !ok || scopeID == "" {
		t.Fatal("Goal did not allocate a delivery scope")
	}
	cp := evidence.DeliveryCheckpoint{
		ScopeID:             scopeID,
		CriteriaEstablished: true,
		WorkObserved:        true,
		MutationObserved:    true,
		PendingMutation:     true,
	}
	statePath, data, persist := c.goals.setDeliveryCheckpoint(cp, nil)
	c.persistGoalState(statePath, data, persist)
	c.stopGoal(GoalStatusBlocked)

	freshExec := agent.New(nil, nil, agent.NewSession("sys"), agent.Options{}, event.Discard)
	fresh := New(Options{Executor: freshExec, SessionDir: dir, Label: "fresh"})
	fresh.Resume(agent.NewSession("sys"), path)
	if fresh.Goal() != "finish the delivery" || fresh.GoalStatus() != GoalStatusBlocked {
		t.Fatalf("restored Goal = (%q, %q), want blocked Goal", fresh.Goal(), fresh.GoalStatus())
	}
	if got := freshExec.DeliveryCheckpoint(); got != cp {
		t.Fatalf("restored checkpoint = %+v, want %+v", got, cp)
	}
	if !fresh.ResumeGoal() {
		t.Fatal("ResumeGoal rejected a restored blocked Goal")
	}
	id, _, ok := fresh.goals.deliveryScope()
	if !ok || id != scopeID {
		t.Fatalf("resumed scope = %q, want %q", id, scopeID)
	}
}

func TestLegacyRunningGoalSidecarAllocatesScope(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "legacy.jsonl")
	data := []byte(`{"goal":"legacy goal","status":"running"}`)
	if err := os.WriteFile(store.SessionGoalState(path), data, 0o600); err != nil {
		t.Fatal(err)
	}
	exec := agent.New(nil, nil, agent.NewSession("sys"), agent.Options{}, event.Discard)
	c := New(Options{Executor: exec, SessionDir: dir, Label: "test"})
	c.Resume(agent.NewSession("sys"), path)
	id, task, ok := c.goals.deliveryScope()
	if !ok || id == "" || task != "legacy goal" {
		t.Fatalf("legacy delivery scope = (%q, %q, %v)", id, task, ok)
	}
	if got := exec.DeliveryCheckpoint(); got.ScopeID != id {
		t.Fatalf("legacy checkpoint scope = %q, want %q", got.ScopeID, id)
	}
}
