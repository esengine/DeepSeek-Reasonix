package control

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func TestGoalCommandAutoContinuesUntilComplete(t *testing.T) {
	prov := &scriptedTurns{turns: [][]provider.Chunk{
		textTurn("Started the goal work.\n\n[goal:continue]"),
		textTurn("Finished the goal work.\n\n[goal:complete]"),
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

	c.Submit("/goal ship the redesign")
	waitForTurnDone(t, events)

	if prov.call != 2 {
		t.Fatalf("provider calls = %d, want 2 (initial + automatic continuation)", prov.call)
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

func TestActiveGoalBlockCarriesTaskContractAndPausePolicy(t *testing.T) {
	block := activeGoalBlock("fix the parser", GoalResearchOff)
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

func TestGoalModeSkipsAutoPlanApproval(t *testing.T) {
	prov := &scriptedTurns{turns: [][]provider.Chunk{
		textTurn("Implemented the requested work.\n\n[goal:complete]"),
	}}
	ag := agent.New(prov, tool.NewRegistry(), agent.NewSession(""), agent.Options{}, event.Discard)
	approvalRequests := make(chan event.Approval, 1)
	events := make(chan event.Event, 4)
	c := New(Options{
		AutoPlan: "on",
		Runner:   ag,
		Executor: ag,
		Sink: event.FuncSink(func(e event.Event) {
			switch e.Kind {
			case event.ApprovalRequest:
				approvalRequests <- e.Approval
			case event.TurnDone:
				events <- e
			}
		}),
	})

	c.Submit("/goal 实现一个复杂功能，修改代码，补测试，并更新文档")
	waitForTurnDone(t, events)

	select {
	case approval := <-approvalRequests:
		t.Fatalf("goal mode should not request plan approval under auto-plan; got %+v", approval)
	default:
	}
	if c.PlanMode() {
		t.Fatal("goal mode should leave plan mode off")
	}
	if got := firstUserMessage(ag.Session().Messages); strings.HasPrefix(got, PlanModeMarker) {
		t.Fatalf("goal mode should not prepend plan marker, got %q", got)
	}
}

func TestPlainInputWithStrongResearchSignalAutoStartsGoal(t *testing.T) {
	prov := &scriptedTurns{turns: [][]provider.Chunk{
		textTurn("AutoResearch started and completed.\n\n[goal:complete]"),
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
	first := firstUserMessage(ag.Session().Messages)
	for _, want := range []string{
		"<active-goal>\n持续排查这个线上卡顿直到根因明确，并验证修复",
		"AutoResearch protocol",
		".reasonix/autoresearch/<task-id>/",
	} {
		if !strings.Contains(first, want) {
			t.Fatalf("auto-started goal turn missing %q:\n%s", want, first)
		}
	}
	if strings.HasPrefix(first, PlanModeMarker) {
		t.Fatalf("auto-started research goal should not enter plan mode, got %q", first)
	}
	if got := c.GoalStatus(); got != GoalStatusComplete {
		t.Fatalf("GoalStatus() = %q, want complete", got)
	}
}

func TestPlainInputAutoStartedGoalPreservesRefs(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("important referenced evidence"), 0o644); err != nil {
		t.Fatal(err)
	}
	prov := &scriptedTurns{turns: [][]provider.Chunk{
		textTurn("AutoResearch started and completed.\n\n[goal:complete]"),
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
		"<active-goal>\n持续排查直到根因明确，并验证 @notes.txt",
		"Referenced context:",
		"important referenced evidence",
		"AutoResearch protocol",
	} {
		if !strings.Contains(first, want) {
			t.Fatalf("auto-started goal with refs missing %q:\n%s", want, first)
		}
	}
}

func TestResearchGoalCreatesHostManagedAutoResearchTask(t *testing.T) {
	root := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	sessionPath := filepath.Join(root, "sessions", "s.jsonl")
	ag := agent.New(&scriptedTurns{}, tool.NewRegistry(), agent.NewSession(""), agent.Options{}, event.Discard)
	c := New(Options{WorkspaceRoot: root, SessionPath: sessionPath, Runner: ag, Executor: ag})

	c.SetGoalWithResearchMode("fix the typo and add a test", GoalResearchOn)

	data, err := os.ReadFile(goalStatePath(sessionPath))
	if err != nil {
		t.Fatalf("read goal state: %v", err)
	}
	var state goalState
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatalf("unmarshal goal state: %v", err)
	}
	if state.AutoResearchTaskID == "" {
		t.Fatalf("AutoResearchTaskID was empty in persisted goal state: %+v", state)
	}
	for _, rel := range []string{
		"state/task_spec.json",
		"state/progress.json",
		"state/findings.jsonl",
		"logs/heartbeat.jsonl",
	} {
		path := filepath.Join(root, ".reasonix", "autoresearch", state.AutoResearchTaskID, rel)
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected autoresearch file %s: %v", rel, err)
		}
	}
	var spec struct {
		SuccessCriteria []struct {
			ID       string `json:"id"`
			Required bool   `json:"required"`
		} `json:"success_criteria"`
	}
	readJSONFileForTest(t, filepath.Join(root, ".reasonix", "autoresearch", state.AutoResearchTaskID, "state", "task_spec.json"), &spec)
	if len(spec.SuccessCriteria) != 2 || spec.SuccessCriteria[0].ID != "objective_evidence" || spec.SuccessCriteria[1].ID != "verification" {
		t.Fatalf("default success criteria = %+v, want objective_evidence and verification", spec.SuccessCriteria)
	}
	for _, criterion := range spec.SuccessCriteria {
		if !criterion.Required {
			t.Fatalf("default criterion %+v was not required", criterion)
		}
	}

	composed := c.Compose("continue")
	if !strings.Contains(composed, "<autoresearch-runtime>") || !strings.Contains(composed, "task_id: "+state.AutoResearchTaskID) || !strings.Contains(composed, "objective_evidence") {
		t.Fatalf("Compose missing runtime summary for task %q:\n%s", state.AutoResearchTaskID, composed)
	}
}

func TestResearchGoalCreatedEmitsLifecycleNotice(t *testing.T) {
	root := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	events := make(chan event.Event, 4)
	c := New(Options{
		WorkspaceRoot: root,
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.Notice {
				events <- e
			}
		}),
	})

	c.SetGoalWithResearchMode("investigate lifecycle notice", GoalResearchOn)

	select {
	case e := <-events:
		if !strings.Contains(e.Text, "autoresearch task created") {
			t.Fatalf("notice = %q, want autoresearch task created", e.Text)
		}
	default:
		t.Fatal("expected autoresearch lifecycle notice")
	}
}

func TestResearchGoalRepeatedSetReusesAutoResearchTask(t *testing.T) {
	root := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	events := make(chan event.Event, 8)
	c := New(Options{
		WorkspaceRoot: root,
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.Notice {
				events <- e
			}
		}),
	})

	goal := "请持续研究当前项目的 AutoResearch 状态栏展示链路，验证任务创建、状态刷新、右侧 Context 面板展示、状态栏 chip 展示是否一致。不要只看表面现象，需要找到根因、记录 evidence，并在完成前确认所有验证步骤通过。不要修改文件"
	c.SetGoalWithResearchMode(goal, GoalResearchOn)
	_, _, _, firstTaskID := c.goals.snapshot()
	if firstTaskID == "" {
		t.Fatal("first AutoResearch task id was empty")
	}

	c.SetGoalWithResearchMode(goal, GoalResearchOn)
	c.SetGoal(goal)
	_, _, _, repeatedTaskID := c.goals.snapshot()
	if repeatedTaskID != firstTaskID {
		t.Fatalf("repeated SetGoal created a new task: got %q, want %q", repeatedTaskID, firstTaskID)
	}

	entries, err := os.ReadDir(filepath.Join(root, ".reasonix", "autoresearch"))
	if err != nil {
		t.Fatalf("read autoresearch dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("autoresearch task count = %d, want 1", len(entries))
	}

	createdNotices := 0
	for {
		select {
		case e := <-events:
			if strings.Contains(e.Text, "autoresearch task created") {
				createdNotices++
			}
		default:
			if createdNotices != 1 {
				t.Fatalf("created notices = %d, want 1", createdNotices)
			}
			return
		}
	}
}

func TestResearchGoalMissingExplicitTaskBlocksInsteadOfCreatingNewTask(t *testing.T) {
	root := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	sessionPath := filepath.Join(root, "sessions", "s.jsonl")
	var notices []string
	c := New(Options{
		WorkspaceRoot: root,
		SessionPath:   sessionPath,
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.Notice {
				notices = append(notices, e.Text)
			}
		}),
	})

	c.SetGoalWithResearchMode("resume .reasonix/autoresearch/missing-task/", GoalResearchOn)

	if got := c.GoalStatus(); got != GoalStatusBlocked {
		t.Fatalf("GoalStatus() = %q, want blocked for missing explicit AutoResearch task", got)
	}
	if got := c.goals.currentAutoResearchTaskID(); got != "" {
		t.Fatalf("current AutoResearch task id = %q, want none for missing explicit task", got)
	}
	entries, err := os.ReadDir(filepath.Join(root, ".reasonix", "autoresearch"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("ReadDir autoresearch root: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("created tasks for missing explicit resume: %+v", entries)
	}
	if !containsNotice(notices, "autoresearch resume failed") || !containsNotice(notices, "missing-task") {
		t.Fatalf("notices = %+v, want explicit resume failure", notices)
	}
}

func TestResearchGoalTurnAppendsAutoResearchHeartbeats(t *testing.T) {
	root := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	sessionPath := filepath.Join(root, "sessions", "s.jsonl")
	prov := &scriptedTurns{turns: [][]provider.Chunk{
		textTurn("Finished.\n\n[goal:complete]"),
	}}
	ag := agent.New(prov, tool.NewRegistry(), agent.NewSession(""), agent.Options{}, event.Discard)
	events := make(chan event.Event, 4)
	c := New(Options{
		WorkspaceRoot: root,
		SessionPath:   sessionPath,
		Runner:        ag,
		Executor:      ag,
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.TurnDone || e.Kind == event.Notice {
				events <- e
			}
		}),
	})

	c.Submit("/goal --research fix the typo and add a test")
	waitForTurnDone(t, events)

	data, err := os.ReadFile(goalStatePath(sessionPath))
	if err != nil {
		t.Fatalf("read goal state: %v", err)
	}
	var state goalState
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatalf("unmarshal goal state: %v", err)
	}
	heartbeats, err := c.autoResearch.Heartbeats(state.AutoResearchTaskID, 10)
	if err != nil {
		t.Fatalf("Heartbeats: %v", err)
	}
	if len(heartbeats) < 2 {
		t.Fatalf("heartbeats = %+v, want at least starting and done", heartbeats)
	}
	if heartbeats[0].Status != "starting_turn" || heartbeats[len(heartbeats)-1].Status != "turn_done" {
		t.Fatalf("heartbeats = %+v, want starting_turn then turn_done", heartbeats)
	}
}

func TestResearchGoalTurnUpdatesAutoResearchStaleProgress(t *testing.T) {
	root := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	sessionPath := filepath.Join(root, "sessions", "s.jsonl")
	prov := &scriptedTurns{turns: [][]provider.Chunk{
		textTurn("Still investigating.\n\n[goal:continue]"),
		textTurn("No new evidence without external input.\n\n[goal:blocked:needs a repro trace]"),
	}}
	ag := agent.New(prov, tool.NewRegistry(), agent.NewSession(""), agent.Options{}, event.Discard)
	events := make(chan event.Event, 8)
	c := New(Options{
		WorkspaceRoot: root,
		SessionPath:   sessionPath,
		Runner:        ag,
		Executor:      ag,
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.TurnDone || e.Kind == event.Notice {
				events <- e
			}
		}),
	})

	c.Submit("/goal --research investigate stale progress")
	waitForTurnDone(t, events)

	data, err := os.ReadFile(goalStatePath(sessionPath))
	if err != nil {
		t.Fatalf("read goal state: %v", err)
	}
	var state goalState
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatalf("unmarshal goal state: %v", err)
	}
	summary, err := c.autoResearch.Summary(state.AutoResearchTaskID)
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if summary.Iteration < 2 || summary.StaleCount != summary.Iteration || !summary.PivotRequired || summary.PivotCount != 1 {
		t.Fatalf("summary = %+v, want stale progress for every no-evidence turn and pivot required", summary)
	}
}

func TestResearchGoalCompletionIsInterceptedWhenReadinessFails(t *testing.T) {
	root := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	sessionPath := filepath.Join(root, "sessions", "s.jsonl")
	prov := &scriptedTurns{turns: [][]provider.Chunk{
		textTurn("I am done.\n\n[goal:complete]"),
		textTurn("Still need evidence.\n\n[goal:blocked:missing evidence]"),
		textTurn("Still need evidence.\n\n[goal:blocked:missing evidence]"),
		textTurn("Still need evidence.\n\n[goal:blocked:missing evidence]"),
	}}
	ag := agent.New(prov, tool.NewRegistry(), agent.NewSession(""), agent.Options{}, event.Discard)
	events := make(chan event.Event, 8)
	var notices []string
	c := New(Options{
		WorkspaceRoot: root,
		SessionPath:   sessionPath,
		Runner:        ag,
		Executor:      ag,
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.Notice {
				notices = append(notices, e.Text)
			}
			if e.Kind == event.TurnDone || e.Kind == event.Notice {
				events <- e
			}
		}),
	})

	c.SetGoalWithResearchMode("identify the root cause", GoalResearchOn)

	if err := newTurnOrchestrator(c).runGoalLoopWithRawDisplay(context.Background(), "start", "start", "start"); err != nil {
		t.Fatalf("runGoalLoopWithRawDisplay: %v", err)
	}

	if got := c.GoalStatus(); got == GoalStatusComplete {
		t.Fatalf("GoalStatus() = complete, want readiness intercept to keep goal running")
	}
	if prov.call != 4 {
		t.Fatalf("provider calls = %d, want initial + readiness intercept + blocked audit", prov.call)
	}
	if !sessionContainsUserText(ag.Session().Messages, "AutoResearch readiness check failed", "objective_evidence", "verification") {
		t.Fatalf("transcript missing readiness intercept; last user:\n%s", lastUserMessage(ag.Session().Messages))
	}
	if !containsNotice(notices, "autoresearch readiness blocked completion") {
		t.Fatalf("notices = %+v, want autoresearch readiness blocked completion", notices)
	}
}

func TestControllerRecordsAutoResearchEvidence(t *testing.T) {
	root := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	c := New(Options{WorkspaceRoot: root})
	c.SetGoalWithResearchMode("verify the fix", GoalResearchOn)
	taskID := c.goals.currentAutoResearchTaskID()
	if taskID == "" {
		t.Fatal("expected autoresearch task id")
	}

	err := c.RecordAutoResearchEvidence("objective_evidence", AutoResearchEvidenceInput{
		ID:       "f-objective",
		Kind:     "file",
		Summary:  "implementation inspected",
		Source:   "file",
		Paths:    []string{"internal/control/controller.go"},
		Accepted: true,
	})
	if err != nil {
		t.Fatalf("RecordAutoResearchEvidence objective_evidence: %v", err)
	}
	err = c.RecordAutoResearchEvidence("verification", AutoResearchEvidenceInput{
		ID:       "f-verification",
		Kind:     "test",
		Summary:  "go test passed",
		Source:   "command",
		Command:  "go test ./internal/control",
		Accepted: true,
	})
	if err != nil {
		t.Fatalf("RecordAutoResearchEvidence verification: %v", err)
	}

	report, err := c.autoResearch.Readiness(taskID)
	if err != nil {
		t.Fatalf("Readiness: %v", err)
	}
	if !report.Ready {
		t.Fatalf("readiness = %+v, want ready", report)
	}
}

func TestAutoResearchEvidenceDoesNotChangeDefaultToolSurface(t *testing.T) {
	root := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	reg := tool.NewRegistry()
	New(Options{WorkspaceRoot: root, Registry: reg})
	if _, ok := reg.Get("autoresearch_record_evidence"); ok {
		t.Fatalf("autoresearch_record_evidence should not be registered in the default provider-visible tool surface; tools=%v", reg.Names())
	}
	for _, schema := range reg.Schemas() {
		if schema.Name == "autoresearch_record_evidence" {
			t.Fatalf("autoresearch_record_evidence should not appear in provider schemas: %+v", reg.Schemas())
		}
	}
}

func TestResearchGoalCompletionMarksAutoResearchTaskComplete(t *testing.T) {
	root := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	sessionPath := filepath.Join(root, "sessions", "s.jsonl")
	prov := &scriptedTurns{turns: [][]provider.Chunk{
		textTurn(`Done.

<autoresearch-evidence>
{"criterion_id":"objective_evidence","id":"f-objective","kind":"file","summary":"The implementation state was inspected directly.","source":"file","paths":["internal/control/controller.go"],"accepted":true}
</autoresearch-evidence>
<autoresearch-evidence>
{"criterion_id":"verification","id":"f-verification","kind":"test","summary":"The focused AutoResearch tests passed.","source":"command","command":"go test ./internal/control","accepted":true}
</autoresearch-evidence>

[goal:complete]`),
	}}
	ag := agent.New(prov, tool.NewRegistry(), agent.NewSession(""), agent.Options{}, event.Discard)
	var notices []string
	c := New(Options{
		WorkspaceRoot: root,
		SessionPath:   sessionPath,
		Runner:        ag,
		Executor:      ag,
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.Notice {
				notices = append(notices, e.Text)
			}
		}),
	})
	c.SetGoalWithResearchMode("verify completion lifecycle", GoalResearchOn)
	taskID := c.goals.currentAutoResearchTaskID()
	if taskID == "" {
		t.Fatal("expected autoresearch task id")
	}

	if err := newTurnOrchestrator(c).runGoalLoopWithRawDisplay(context.Background(), "start", "start", "start"); err != nil {
		t.Fatalf("runGoalLoopWithRawDisplay: %v", err)
	}

	summary, err := c.autoResearch.Summary(taskID)
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if summary.Status != "complete" {
		t.Fatalf("AutoResearch status = %q, want complete", summary.Status)
	}
	if summary.StaleCount != 0 {
		t.Fatalf("AutoResearch stale_count = %d, want 0 after accepted evidence", summary.StaleCount)
	}
	findings, err := c.autoResearch.Findings(taskID, 0)
	if err != nil {
		t.Fatalf("Findings: %v", err)
	}
	if len(findings) != 2 {
		t.Fatalf("findings = %+v, want two assistant evidence records", findings)
	}
	if !containsNotice(notices, "autoresearch task completed") {
		t.Fatalf("notices = %+v, want autoresearch task completed", notices)
	}
}

func TestResearchGoalBlockedMarksAutoResearchTaskBlocked(t *testing.T) {
	root := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	sessionPath := filepath.Join(root, "sessions", "s.jsonl")
	prov := &scriptedTurns{turns: [][]provider.Chunk{
		textTurn("Blocked.\n\n[goal:blocked:needs credentials]"),
		textTurn("Still blocked.\n\n[goal:blocked:needs credentials]"),
		textTurn("Still blocked.\n\n[goal:blocked:needs credentials]"),
	}}
	ag := agent.New(prov, tool.NewRegistry(), agent.NewSession(""), agent.Options{}, event.Discard)
	var notices []string
	c := New(Options{
		WorkspaceRoot: root,
		SessionPath:   sessionPath,
		Runner:        ag,
		Executor:      ag,
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.Notice {
				notices = append(notices, e.Text)
			}
		}),
	})
	c.SetGoalWithResearchMode("verify blocked lifecycle", GoalResearchOn)
	taskID := c.goals.currentAutoResearchTaskID()
	if taskID == "" {
		t.Fatal("expected autoresearch task id")
	}

	if err := newTurnOrchestrator(c).runGoalLoopWithRawDisplay(context.Background(), "start", "start", "start"); err != nil {
		t.Fatalf("runGoalLoopWithRawDisplay: %v", err)
	}

	summary, err := c.autoResearch.Summary(taskID)
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if summary.Status != "blocked" || !strings.Contains(summary.Blocker, "needs credentials") {
		t.Fatalf("AutoResearch summary = %+v, want blocked with reason", summary)
	}
	if !containsNotice(notices, "autoresearch task blocked") {
		t.Fatalf("notices = %+v, want autoresearch task blocked", notices)
	}
}

func TestPlainInputAutoStartDoesNotMutateGoalWhenTurnRunning(t *testing.T) {
	c := New(Options{})
	c.mu.Lock()
	c.running = true
	c.mu.Unlock()

	c.Submit("持续排查这个线上卡顿直到根因明确，并验证修复")

	if got := c.Goal(); got != "" {
		t.Fatalf("rejected concurrent auto-start should not set goal, got %q", got)
	}
	if got := c.GoalStatus(); got != GoalStatusStopped {
		t.Fatalf("GoalStatus() = %q, want stopped", got)
	}
}

func TestPlainInputWithWeakResearchSignalDoesNotAutoStartGoal(t *testing.T) {
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
		t.Fatalf("weak ordinary prompt should not auto-start AutoResearch:\n%s", first)
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
	prov := &scriptedTurns{turns: [][]provider.Chunk{
		textTurn("Blocked.\n\n[goal:blocked: Needs credentials.]"),
		textTurn("Still blocked.\n\n[goal:blocked:needs-credentials]"),
		textTurn("Still blocked.\n\n[goal:blocked:NEEDS CREDENTIALS！]"),
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

	c.Submit("/goal deploy the service")
	waitForTurnDone(t, events)

	if prov.call != 3 {
		t.Fatalf("provider calls = %d, want 3 blocked attempts", prov.call)
	}
	if got := c.GoalStatus(); got != GoalStatusBlocked {
		t.Fatalf("GoalStatus() = %q, want blocked", got)
	}
}

func TestGoalRestartResetsBlockedAudit(t *testing.T) {
	prov := &scriptedTurns{turns: [][]provider.Chunk{
		textTurn("Blocked.\n\n[goal:blocked:needs credentials]"),
		textTurn("Blocked again.\n\n[goal:blocked:needs credentials]"),
		textTurn("Blocked third time.\n\n[goal:blocked:needs credentials]"),
		textTurn("Fresh blocked audit.\n\n[goal:blocked:needs credentials]"),
		textTurn("Recovered on retry.\n\n[goal:complete]"),
	}}
	ag := agent.New(prov, tool.NewRegistry(), agent.NewSession(""), agent.Options{}, event.Discard)
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
	if prov.call != 5 {
		t.Fatalf("provider calls = %d, want 5 (3 blocked + 2 resumed)", prov.call)
	}
	if got := c.GoalStatus(); got != GoalStatusComplete {
		t.Fatalf("resumed GoalStatus() = %q, want complete; blocked audit should restart", got)
	}
}

// TestIncompleteGoalTodos verifies that formatIncompleteTodos detects
// unfinished tasks and returns a formatted reminder, and returns empty
// when all todos are complete.
func TestIncompleteGoalTodos(t *testing.T) {
	prov := &scriptedTurns{turns: [][]provider.Chunk{textTurn("done")}}
	ag := agent.New(prov, tool.NewRegistry(), agent.NewSession(""), agent.Options{}, event.Discard)
	c := New(Options{Runner: ag, Executor: ag, Sink: event.Discard})
	reminder := func() string { return formatIncompleteTodos(c.goalTodos(), ag.GoalReadinessFailure()) }

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
	if !strings.Contains(msg, "todo_write") {
		t.Fatalf("reminder should suggest updating todos via todo_write, got: %q", msg)
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

// TestGoalInterceptsCompleteWithIncompleteTodos verifies that when the
// agent claims [goal:complete] but has unfinished canonical todos, the
// goal loop intercepts the first claim, then lets a second consecutive
// claim through as an override.
func TestGoalInterceptsCompleteWithIncompleteTodos(t *testing.T) {
	prov := &scriptedTurns{turns: [][]provider.Chunk{
		textTurn("All done.\n\n[goal:complete]"),
		textTurn("All done.\n\n[goal:complete]"),
	}}
	ag := agent.New(prov, tool.NewRegistry(), agent.NewSession(""), agent.Options{}, event.Discard)
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

	// Should see an intercept notice and the goal should complete
	// (second [goal:complete] overrides the intercept).
	found := false
	for _, n := range allNotices {
		if strings.Contains(n, "goal intercept") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected a 'goal intercept' notice, got %v", allNotices)
	}
	if c.GoalStatus() != GoalStatusComplete {
		t.Fatalf("GoalStatus() = %q, want complete (second [goal:complete] should override)", c.GoalStatus())
	}
}

// TestGoalOverrideCompletesRemainingTodos verifies that when the goal
// completes via the second [goal:complete] override (non-strict mode), any
// remaining incomplete canonical todos are force-completed and a synthetic
// todo_write event is emitted so the frontend panel reflects the final state.
func TestGoalOverrideCompletesRemainingTodos(t *testing.T) {
	prov := &scriptedTurns{turns: [][]provider.Chunk{
		textTurn("All done.\n\n[goal:complete]"),
		textTurn("All done.\n\n[goal:complete]"),
	}}
	ag := agent.New(prov, tool.NewRegistry(), agent.NewSession(""), agent.Options{}, event.Discard)
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

	// All todos in the executor must be force-completed.
	for _, td := range c.executor.CanonicalTodoState() {
		if td.Status != "completed" {
			t.Fatalf("canonical todo %q = %s, want completed after goal override", td.Content, td.Status)
		}
	}

	// Must have emitted a synthetic todo_write (ToolDispatch + ToolResult)
	// from completeRemainingGoalTodos.
	hasSynthetic := false
	for _, e := range tools {
		if e.Tool.ID == "goal-final" && e.Tool.Name == "todo_write" {
			hasSynthetic = true
			break
		}
	}
	if !hasSynthetic {
		t.Fatal("expected synthetic todo_write event (ID=goal-final) after goal override completion")
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

// TestStrictGoalBlocksRepeatedComplete verifies that in strict mode, every
// [goal:complete] with incomplete todos is intercepted — no override allowed.
func TestStrictGoalBlocksRepeatedComplete(t *testing.T) {
	prov := &scriptedTurns{turns: [][]provider.Chunk{
		textTurn("Done!\n\n[goal:complete]"),
	}}
	ag := agent.New(prov, tool.NewRegistry(), agent.NewSession(""), agent.Options{}, event.Discard)
	ag.SeedTodoState([]evidence.TodoItem{
		{Content: "Fix the parser", Status: "in_progress"},
	})

	c := New(Options{Runner: ag, Executor: ag, Sink: event.Discard})

	c.Submit("/goal --strict fix everything")

	// In strict mode the agent still has incomplete todos but only one
	// turn was given (the provider recycles it). The goal loop keeps
	// intercepting; when the turn-recycling hits maxGoalAutoTurns (50)
	// the goal is blocked. Verify it's not "complete".
	if c.GoalStatus() == GoalStatusComplete {
		t.Fatal("strict mode should not allow goal completion with incomplete todos")
	}
}

func readJSONFileForTest(t *testing.T, path string, out any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	if err := json.Unmarshal(data, out); err != nil {
		t.Fatalf("Unmarshal(%s): %v", path, err)
	}
}

func sessionContainsUserText(messages []provider.Message, needles ...string) bool {
	for _, msg := range messages {
		if msg.Role != provider.RoleUser {
			continue
		}
		ok := true
		for _, needle := range needles {
			if !strings.Contains(msg.Content, needle) {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

func containsNotice(notices []string, needle string) bool {
	for _, notice := range notices {
		if strings.Contains(notice, needle) {
			return true
		}
	}
	return false
}
