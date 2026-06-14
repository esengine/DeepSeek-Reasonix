package control

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func TestResumeRestoresRuntimeGoalState(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "resume-goal.jsonl")

	// Create a session with some content.
	s := agent.NewSession("")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "hello"})
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "hi"})
	if err := s.Save(sessionPath); err != nil {
		t.Fatalf("save session: %v", err)
	}

	// Write a runtime sidecar with an active goal.
	meta := agent.RuntimeMeta{
		SessionID: "resume-goal",
		Goal: agent.RuntimeGoalMeta{
			Text:        "ship the feature",
			Status:      GoalStatusRunning,
			Turns:       5,
			BlockCount:  1,
			BlockReason: "waiting for CI",
		},
		Run: agent.RuntimeRunMeta{
			Status: "idle",
		},
		Budget: agent.RuntimeBudgetMeta{
			MaxGoalAutoTurns: 9,
		},
	}
	if err := agent.SaveRuntimeMeta(sessionPath, meta); err != nil {
		t.Fatalf("save runtime meta: %v", err)
	}

	// Build a controller and resume.
	ag := agent.New(nil, tool.NewRegistry(), agent.NewSession(""), agent.Options{}, event.Discard)
	c := New(Options{
		Executor: ag,
		Sink:     event.Discard,
	})
	loaded, err := agent.LoadSession(sessionPath)
	if err != nil {
		t.Fatalf("load session: %v", err)
	}
	c.Resume(loaded, sessionPath)

	// Verify goal state was restored.
	if got := c.Goal(); got != "ship the feature" {
		t.Errorf("Goal() = %q, want %q", got, "ship the feature")
	}
	if got := c.GoalStatus(); got != GoalStatusRunning {
		t.Errorf("GoalStatus() = %q, want %q", got, GoalStatusRunning)
	}
	c.mu.Lock()
	turns := c.goalTurns
	blocks := c.goalBlocks
	blockReason := c.goalBlock
	turnCap := c.goalTurnCap
	c.mu.Unlock()
	if turns != 5 {
		t.Errorf("goalTurns = %d, want 5", turns)
	}
	if blocks != 1 {
		t.Errorf("goalBlocks = %d, want 1", blocks)
	}
	if blockReason != "waiting for CI" {
		t.Errorf("goalBlock = %q, want %q", blockReason, "waiting for CI")
	}
	if turnCap != 9 {
		t.Errorf("goalTurnCap = %d, want 9", turnCap)
	}
}

func TestResumeRuntimeGoalEmitsNotice(t *testing.T) {
	tests := []struct {
		name       string
		goal       string
		status     string
		wantNotice string
	}{
		{
			name:       "running",
			goal:       "ship the notification",
			status:     GoalStatusRunning,
			wantNotice: "resumed active goal: ship the notification",
		},
		{
			name:       "blocked",
			goal:       "wait for CI",
			status:     GoalStatusBlocked,
			wantNotice: "resumed blocked goal: wait for CI",
		},
		{
			name:       "complete",
			goal:       "",
			status:     GoalStatusComplete,
			wantNotice: "resumed completed goal",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			sessionPath := filepath.Join(dir, "resume-notice.jsonl")
			s := agent.NewSession("")
			s.Add(provider.Message{Role: provider.RoleUser, Content: "hello"})
			if err := s.Save(sessionPath); err != nil {
				t.Fatalf("save session: %v", err)
			}
			if err := agent.SaveRuntimeMeta(sessionPath, agent.RuntimeMeta{
				Goal: agent.RuntimeGoalMeta{Text: tt.goal, Status: tt.status},
				Run:  agent.RuntimeRunMeta{Status: "idle"},
			}); err != nil {
				t.Fatalf("SaveRuntimeMeta: %v", err)
			}

			var notices []string
			ag := agent.New(nil, tool.NewRegistry(), agent.NewSession(""), agent.Options{}, event.Discard)
			c := New(Options{
				Executor: ag,
				Sink: event.FuncSink(func(e event.Event) {
					if e.Kind == event.Notice {
						notices = append(notices, e.Text)
					}
				}),
			})
			loaded, err := agent.LoadSession(sessionPath)
			if err != nil {
				t.Fatalf("LoadSession: %v", err)
			}

			c.Resume(loaded, sessionPath)

			if len(notices) != 1 || notices[0] != tt.wantNotice {
				t.Fatalf("notices = %#v, want [%q]", notices, tt.wantNotice)
			}
		})
	}
}

func TestResumeDoesNotAutoCallModel(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "no-auto.jsonl")

	s := agent.NewSession("")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "start"})
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "ok [goal:continue]"})
	if err := s.Save(sessionPath); err != nil {
		t.Fatalf("save session: %v", err)
	}

	meta := agent.RuntimeMeta{
		Goal: agent.RuntimeGoalMeta{
			Text:   "active goal",
			Status: GoalStatusRunning,
			Turns:  1,
		},
		Run: agent.RuntimeRunMeta{Status: "idle"},
	}
	if err := agent.SaveRuntimeMeta(sessionPath, meta); err != nil {
		t.Fatalf("save runtime meta: %v", err)
	}

	// Use a provider that panics if called — resume must not call it.
	prov := &panicProvider{}
	ag := agent.New(prov, tool.NewRegistry(), agent.NewSession(""), agent.Options{}, event.Discard)
	c := New(Options{
		Runner:   ag,
		Executor: ag,
		Sink:     event.Discard,
	})
	loaded, err := agent.LoadSession(sessionPath)
	if err != nil {
		t.Fatalf("load session: %v", err)
	}

	// This should NOT panic — resume is passive.
	c.Resume(loaded, sessionPath)

	if got := c.Goal(); got != "active goal" {
		t.Errorf("Goal() = %q after resume", got)
	}
}

func TestResumeActiveGoalInjectsBlock(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "inject.jsonl")

	s := agent.NewSession("")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "start"})
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "ok"})
	if err := s.Save(sessionPath); err != nil {
		t.Fatalf("save session: %v", err)
	}

	meta := agent.RuntimeMeta{
		Goal: agent.RuntimeGoalMeta{
			Text:   "deploy the app",
			Status: GoalStatusRunning,
			Turns:  2,
		},
		Run: agent.RuntimeRunMeta{Status: "idle"},
	}
	if err := agent.SaveRuntimeMeta(sessionPath, meta); err != nil {
		t.Fatalf("save runtime meta: %v", err)
	}

	prov := &scriptedTurns{turns: [][]provider.Chunk{
		textTurn("Working on it.\n\n[goal:complete]"),
	}}
	ag := agent.New(prov, tool.NewRegistry(), agent.NewSession(""), agent.Options{}, event.Discard)
	events := make(chan event.Event, 8)
	c := New(Options{
		Runner:   ag,
		Executor: ag,
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.TurnDone {
				events <- e
			}
		}),
	})
	loaded, err := agent.LoadSession(sessionPath)
	if err != nil {
		t.Fatalf("load session: %v", err)
	}
	c.Resume(loaded, sessionPath)

	// Now send a normal turn — it should include the <active-goal> block.
	c.Send("check status")
	waitForTurnDone(t, events)

	// The composed user message should contain the goal injection.
	msgs := ag.Session().Messages
	for _, m := range msgs {
		if m.Role == provider.RoleUser && strings.Contains(m.Content, "<active-goal>") {
			if !strings.Contains(m.Content, "deploy the app") {
				t.Errorf("active-goal block should contain goal text, got %q", m.Content)
			}
			return
		}
	}
	t.Fatal("no user message found with <active-goal> injection after resume")
}

func TestSnapshotSavesRuntimeSidecar(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "snapshot-runtime.jsonl")

	prov := &scriptedTurns{turns: [][]provider.Chunk{
		textTurn("Started.\n\n[goal:continue]"),
		textTurn("Still working.\n\n[goal:complete]"),
	}}
	ag := agent.New(prov, tool.NewRegistry(), agent.NewSession(""), agent.Options{}, event.Discard)
	events := make(chan event.Event, 8)
	c := New(Options{
		Runner:      ag,
		Executor:    ag,
		SessionPath: sessionPath,
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.TurnDone {
				events <- e
			}
		}),
	})

	c.SetGoal("build the widget")
	c.Send("go")
	waitForTurnDone(t, events)

	// The runtime sidecar should now exist.
	runtimePath := agent.RuntimeMetaPath(sessionPath)
	if _, err := os.Stat(runtimePath); err != nil {
		t.Fatalf("runtime sidecar not created: %v", err)
	}

	// Load and verify.
	m, ok, err := agent.LoadRuntimeMeta(sessionPath)
	if err != nil || !ok {
		t.Fatalf("LoadRuntimeMeta: err=%v ok=%v", err, ok)
	}
	if m.Goal.Status != GoalStatusComplete {
		t.Errorf("Goal.Status = %q, want %q", m.Goal.Status, GoalStatusComplete)
	}
	if m.Goal.Text != "" {
		t.Errorf("completed Goal.Text = %q, want empty", m.Goal.Text)
	}
}

func TestSnapshotNoGoalNoSidecar(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "no-goal.jsonl")

	prov := &scriptedTurns{turns: [][]provider.Chunk{
		textTurn("Hello!"),
	}}
	ag := agent.New(prov, tool.NewRegistry(), agent.NewSession(""), agent.Options{}, event.Discard)
	events := make(chan event.Event, 4)
	c := New(Options{
		Runner:      ag,
		Executor:    ag,
		SessionPath: sessionPath,
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.TurnDone {
				events <- e
			}
		}),
	})

	// Normal turn without goal — no runtime sidecar expected.
	c.Send("hi")
	waitForTurnDone(t, events)

	runtimePath := agent.RuntimeMetaPath(sessionPath)
	if _, err := os.Stat(runtimePath); !os.IsNotExist(err) {
		t.Errorf("runtime sidecar should not be created without active goal")
	}
}

func TestTurnDoneRuntimeStatusReturnsIdle(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "turn-done-idle.jsonl")

	prov := &scriptedTurns{turns: [][]provider.Chunk{
		textTurn("Finished.\n\n[goal:complete]"),
	}}
	ag := agent.New(prov, tool.NewRegistry(), agent.NewSession(""), agent.Options{}, event.Discard)
	events := make(chan event.Event, 4)
	c := New(Options{
		Runner:      ag,
		Executor:    ag,
		SessionPath: sessionPath,
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.TurnDone {
				events <- e
			}
		}),
	})

	c.SetGoal("finish once")
	c.Send("go")
	waitForTurnDone(t, events)

	m, ok, err := agent.LoadRuntimeMeta(sessionPath)
	if err != nil || !ok {
		t.Fatalf("LoadRuntimeMeta: err=%v ok=%v", err, ok)
	}
	if m.Run.Status == "running" {
		t.Fatalf("Run.Status persisted as running after TurnDone")
	}
	if m.Run.Status != "idle" {
		t.Fatalf("Run.Status = %q, want idle", m.Run.Status)
	}
}

func TestSnapshotPreservesSchedulerRuntime(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "preserve-scheduler.jsonl")

	sess := agent.NewSession("")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "hello"})
	ag := agent.New(nil, tool.NewRegistry(), sess, agent.Options{}, event.Discard)
	c := New(Options{Executor: ag, SessionPath: sessionPath, Sink: event.Discard})

	if err := agent.SaveRuntimeMeta(sessionPath, agent.RuntimeMeta{
		Goal: agent.RuntimeGoalMeta{Text: "old", Status: GoalStatusRunning},
		Scheduler: agent.RuntimeSchedMeta{
			Enabled:      true,
			DailyAt:      "09:00",
			Interval:     time.Hour,
			NextWakeupAt: time.Now().Add(time.Hour).UTC(),
		},
	}); err != nil {
		t.Fatalf("SaveRuntimeMeta: %v", err)
	}

	c.SetGoal("new goal")
	if err := c.Snapshot(); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	m, ok, err := agent.LoadRuntimeMeta(sessionPath)
	if err != nil || !ok {
		t.Fatalf("LoadRuntimeMeta: err=%v ok=%v", err, ok)
	}
	if !m.Scheduler.Enabled || m.Scheduler.DailyAt != "09:00" || m.Scheduler.Interval != time.Hour {
		t.Fatalf("scheduler not preserved: %+v", m.Scheduler)
	}
	if m.Goal.Text != "new goal" {
		t.Fatalf("Goal.Text = %q, want new goal", m.Goal.Text)
	}
}

func TestSnapshotPreservesFileWatchRuntime(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "preserve-watch.jsonl")

	sess := agent.NewSession("")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "hello"})
	ag := agent.New(nil, tool.NewRegistry(), sess, agent.Options{}, event.Discard)
	c := New(Options{Executor: ag, SessionPath: sessionPath, Sink: event.Discard})

	if err := agent.SaveRuntimeMeta(sessionPath, agent.RuntimeMeta{
		Goal: agent.RuntimeGoalMeta{Text: "old", Status: GoalStatusRunning},
		FileWatch: agent.RuntimeWatchMeta{
			Enabled:        true,
			Paths:          []string{"src"},
			IgnorePatterns: []string{"*.tmp"},
			Debounce:       5 * time.Second,
		},
	}); err != nil {
		t.Fatalf("SaveRuntimeMeta: %v", err)
	}

	c.SetGoal("new goal")
	if err := c.Snapshot(); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	m, ok, err := agent.LoadRuntimeMeta(sessionPath)
	if err != nil || !ok {
		t.Fatalf("LoadRuntimeMeta: err=%v ok=%v", err, ok)
	}
	if !m.FileWatch.Enabled || len(m.FileWatch.Paths) != 1 || m.FileWatch.Paths[0] != "src" {
		t.Fatalf("file watch not preserved: %+v", m.FileWatch)
	}
	if m.FileWatch.Debounce != 5*time.Second {
		t.Fatalf("Debounce = %v, want 5s", m.FileWatch.Debounce)
	}
	if m.Goal.Text != "new goal" {
		t.Fatalf("Goal.Text = %q, want new goal", m.Goal.Text)
	}
}

func TestSnapshotPreservesBudgetRuntime(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "preserve-budget.jsonl")

	sess := agent.NewSession("")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "hello"})
	ag := agent.New(nil, tool.NewRegistry(), sess, agent.Options{}, event.Discard)
	c := New(Options{Executor: ag, SessionPath: sessionPath, Sink: event.Discard})

	if err := agent.SaveRuntimeMeta(sessionPath, agent.RuntimeMeta{
		Goal: agent.RuntimeGoalMeta{Text: "old", Status: GoalStatusRunning},
		Budget: agent.RuntimeBudgetMeta{
			DailyWakeupLimit: 7,
			DailyWakeups:     2,
			WindowStartedAt:  time.Now().UTC(),
		},
	}); err != nil {
		t.Fatalf("SaveRuntimeMeta: %v", err)
	}

	c.SetGoal("new goal")
	if err := c.Snapshot(); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	m, ok, err := agent.LoadRuntimeMeta(sessionPath)
	if err != nil || !ok {
		t.Fatalf("LoadRuntimeMeta: err=%v ok=%v", err, ok)
	}
	if m.Budget.DailyWakeupLimit != 7 || m.Budget.DailyWakeups != 2 {
		t.Fatalf("budget not preserved: %+v", m.Budget)
	}
}

func TestGoalContinueUsesPersistedAutoTurnLimit(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "persisted-auto-turn-limit.jsonl")

	s := agent.NewSession("")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "continue"})
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "Still working.\n\n[goal:continue]"})
	if err := s.Save(sessionPath); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := agent.SaveRuntimeMeta(sessionPath, agent.RuntimeMeta{
		SessionID: "persisted-auto-turn-limit",
		Goal: agent.RuntimeGoalMeta{
			Text:   "finish the roadmap",
			Status: GoalStatusRunning,
			Turns:  2,
		},
		Run: agent.RuntimeRunMeta{Status: "idle"},
		Budget: agent.RuntimeBudgetMeta{
			MaxGoalAutoTurns: 4,
		},
	}); err != nil {
		t.Fatalf("SaveRuntimeMeta: %v", err)
	}

	prov := &scriptedTurns{turns: [][]provider.Chunk{
		textTurn("One more step.\n\n[goal:continue]"),
		textTurn("This should not run.\n\n[goal:continue]"),
	}}
	ag := agent.New(prov, tool.NewRegistry(), agent.NewSession(""), agent.Options{}, event.Discard)
	c := New(Options{Runner: ag, Executor: ag, SessionPath: sessionPath, Sink: event.Discard})
	loaded, err := agent.LoadSession(sessionPath)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	c.Resume(loaded, sessionPath)

	if err := c.ContinueGoal(context.Background(), "test"); err != nil {
		t.Fatalf("ContinueGoal: %v", err)
	}
	if prov.call != 1 {
		t.Fatalf("provider calls = %d, want 1", prov.call)
	}
	if got := c.GoalStatus(); got != GoalStatusBlocked {
		t.Fatalf("GoalStatus = %q, want %q", got, GoalStatusBlocked)
	}
	meta, ok, err := agent.LoadRuntimeMeta(sessionPath)
	if err != nil || !ok {
		t.Fatalf("LoadRuntimeMeta: err=%v ok=%v", err, ok)
	}
	if meta.Goal.Turns != 4 || meta.Goal.BlockReason != "goal continuation limit reached" {
		t.Fatalf("limit block not persisted: %+v", meta.Goal)
	}
	if meta.Budget.MaxGoalAutoTurns != 4 {
		t.Fatalf("MaxGoalAutoTurns = %d, want 4", meta.Budget.MaxGoalAutoTurns)
	}
}

func TestGoalContinueCanceledContextStopsGoal(t *testing.T) {
	sess := agent.NewSession("")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "continue"})
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "Still working.\n\n[goal:continue]"})
	ag := agent.New(&scriptedTurns{turns: [][]provider.Chunk{
		textTurn("This should not run.\n\n[goal:continue]"),
	}}, tool.NewRegistry(), sess, agent.Options{}, event.Discard)
	c := New(Options{Runner: ag, Executor: ag, Sink: event.Discard})
	c.SetGoal("finish the canceled work")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.ContinueGoal(ctx, "test"); err == nil {
		t.Fatal("ContinueGoal with canceled context returned nil")
	}
	if got := c.GoalStatus(); got != GoalStatusStopped {
		t.Fatalf("GoalStatus = %q, want %q", got, GoalStatusStopped)
	}
}

func TestSnapshotPreservesEventWaitRuntime(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "preserve-wait.jsonl")

	sess := agent.NewSession("")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "hello"})
	ag := agent.New(nil, tool.NewRegistry(), sess, agent.Options{}, event.Discard)
	c := New(Options{Executor: ag, SessionPath: sessionPath, Sink: event.Discard})

	if err := agent.SaveRuntimeMeta(sessionPath, agent.RuntimeMeta{
		Goal: agent.RuntimeGoalMeta{Text: "wait for CI", Status: GoalStatusRunning},
		Wait: agent.RuntimeWaitMeta{
			Kind:            "event",
			EventSource:     "github.workflow_run",
			EventStatus:     "completed",
			EventConclusion: "success",
			Subject:         "PR #42",
		},
	}); err != nil {
		t.Fatalf("SaveRuntimeMeta: %v", err)
	}

	c.SetGoal("wait for CI")
	if err := c.Snapshot(); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	m, ok, err := agent.LoadRuntimeMeta(sessionPath)
	if err != nil || !ok {
		t.Fatalf("LoadRuntimeMeta: err=%v ok=%v", err, ok)
	}
	if m.Wait.Kind != "event" || m.Wait.EventConclusion != "success" {
		t.Fatalf("event wait not preserved: %+v", m.Wait)
	}
}

func TestSnapshotPreservesTimeWaitRuntime(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "preserve-time-wait.jsonl")

	sess := agent.NewSession("")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "hello"})
	ag := agent.New(nil, tool.NewRegistry(), sess, agent.Options{}, event.Discard)
	c := New(Options{Executor: ag, SessionPath: sessionPath, Sink: event.Discard})
	until := time.Now().Add(time.Hour).UTC().Truncate(time.Second)

	if err := agent.SaveRuntimeMeta(sessionPath, agent.RuntimeMeta{
		Goal: agent.RuntimeGoalMeta{Text: "wait until later", Status: GoalStatusRunning},
		Wait: agent.RuntimeWaitMeta{
			Kind:  "time",
			Until: until,
		},
	}); err != nil {
		t.Fatalf("SaveRuntimeMeta: %v", err)
	}

	c.SetGoal("wait until later")
	if err := c.Snapshot(); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	m, ok, err := agent.LoadRuntimeMeta(sessionPath)
	if err != nil || !ok {
		t.Fatalf("LoadRuntimeMeta: err=%v ok=%v", err, ok)
	}
	if m.Wait.Kind != "time" || !m.Wait.Until.Equal(until) {
		t.Fatalf("time wait not preserved: %+v", m.Wait)
	}
}

func TestSnapshotPreservesFileWaitRuntime(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "preserve-file-wait.jsonl")

	sess := agent.NewSession("")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "hello"})
	ag := agent.New(nil, tool.NewRegistry(), sess, agent.Options{}, event.Discard)
	c := New(Options{Executor: ag, SessionPath: sessionPath, Sink: event.Discard})

	if err := agent.SaveRuntimeMeta(sessionPath, agent.RuntimeMeta{
		Goal: agent.RuntimeGoalMeta{Text: "wait for output", Status: GoalStatusRunning},
		Wait: agent.RuntimeWaitMeta{
			Kind:      "file",
			FilePaths: []string{"dist/output.txt"},
			Subject:   "dist/output.txt",
		},
	}); err != nil {
		t.Fatalf("SaveRuntimeMeta: %v", err)
	}

	c.SetGoal("wait for output")
	if err := c.Snapshot(); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	m, ok, err := agent.LoadRuntimeMeta(sessionPath)
	if err != nil || !ok {
		t.Fatalf("LoadRuntimeMeta: err=%v ok=%v", err, ok)
	}
	if m.Wait.Kind != "file" || len(m.Wait.FilePaths) != 1 || m.Wait.FilePaths[0] != "dist/output.txt" {
		t.Fatalf("file wait not preserved: %+v", m.Wait)
	}
}

func TestClearGoalRemovesStaleRuntimeSidecar(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "clear-goal.jsonl")

	sess := agent.NewSession("")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "hello"})
	ag := agent.New(nil, tool.NewRegistry(), sess, agent.Options{}, event.Discard)
	c := New(Options{Executor: ag, SessionPath: sessionPath, Sink: event.Discard})

	if err := agent.SaveRuntimeMeta(sessionPath, agent.RuntimeMeta{
		Goal: agent.RuntimeGoalMeta{Text: "stale", Status: GoalStatusRunning},
		Run:  agent.RuntimeRunMeta{Status: "idle"},
	}); err != nil {
		t.Fatalf("SaveRuntimeMeta: %v", err)
	}

	c.SetGoal("stale")
	c.ClearGoal()

	if _, err := os.Stat(agent.RuntimeMetaPath(sessionPath)); !os.IsNotExist(err) {
		t.Fatalf("runtime sidecar should be removed after clearing goal, err=%v", err)
	}
}

func TestClearGoalPreservesSchedulerButClearsGoal(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "clear-goal-scheduled.jsonl")

	sess := agent.NewSession("")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "hello"})
	ag := agent.New(nil, tool.NewRegistry(), sess, agent.Options{}, event.Discard)
	c := New(Options{Executor: ag, SessionPath: sessionPath, Sink: event.Discard})

	if err := agent.SaveRuntimeMeta(sessionPath, agent.RuntimeMeta{
		Goal: agent.RuntimeGoalMeta{Text: "stale", Status: GoalStatusRunning},
		Scheduler: agent.RuntimeSchedMeta{
			Enabled: true,
			DailyAt: "09:00",
		},
	}); err != nil {
		t.Fatalf("SaveRuntimeMeta: %v", err)
	}

	c.SetGoal("stale")
	c.ClearGoal()

	m, ok, err := agent.LoadRuntimeMeta(sessionPath)
	if err != nil || !ok {
		t.Fatalf("LoadRuntimeMeta: err=%v ok=%v", err, ok)
	}
	if m.Goal.Text != "" || m.Goal.Status == GoalStatusRunning {
		t.Fatalf("goal should be cleared, got %+v", m.Goal)
	}
	if !m.Scheduler.Enabled || m.Scheduler.DailyAt != "09:00" {
		t.Fatalf("scheduler should be preserved, got %+v", m.Scheduler)
	}
}

func TestClearGoalPreservesFileWatchButClearsGoal(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "clear-goal-watch.jsonl")

	sess := agent.NewSession("")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "hello"})
	ag := agent.New(nil, tool.NewRegistry(), sess, agent.Options{}, event.Discard)
	c := New(Options{Executor: ag, SessionPath: sessionPath, Sink: event.Discard})

	if err := agent.SaveRuntimeMeta(sessionPath, agent.RuntimeMeta{
		Goal: agent.RuntimeGoalMeta{Text: "stale", Status: GoalStatusRunning},
		FileWatch: agent.RuntimeWatchMeta{
			Enabled:  true,
			Paths:    []string{"src"},
			Debounce: 2 * time.Second,
		},
	}); err != nil {
		t.Fatalf("SaveRuntimeMeta: %v", err)
	}

	c.SetGoal("stale")
	c.ClearGoal()

	m, ok, err := agent.LoadRuntimeMeta(sessionPath)
	if err != nil || !ok {
		t.Fatalf("LoadRuntimeMeta: err=%v ok=%v", err, ok)
	}
	if m.Goal.Text != "" || m.Goal.Status == GoalStatusRunning {
		t.Fatalf("goal should be cleared, got %+v", m.Goal)
	}
	if !m.FileWatch.Enabled || len(m.FileWatch.Paths) != 1 || m.FileWatch.Paths[0] != "src" {
		t.Fatalf("file watch should be preserved, got %+v", m.FileWatch)
	}
}

func TestClearGoalPreservesBudgetButClearsGoal(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "clear-goal-budget.jsonl")

	sess := agent.NewSession("")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "hello"})
	ag := agent.New(nil, tool.NewRegistry(), sess, agent.Options{}, event.Discard)
	c := New(Options{Executor: ag, SessionPath: sessionPath, Sink: event.Discard})

	if err := agent.SaveRuntimeMeta(sessionPath, agent.RuntimeMeta{
		Goal:   agent.RuntimeGoalMeta{Text: "stale", Status: GoalStatusRunning},
		Budget: agent.RuntimeBudgetMeta{DailyWakeupLimit: 3},
	}); err != nil {
		t.Fatalf("SaveRuntimeMeta: %v", err)
	}

	c.SetGoal("stale")
	c.ClearGoal()

	m, ok, err := agent.LoadRuntimeMeta(sessionPath)
	if err != nil || !ok {
		t.Fatalf("LoadRuntimeMeta: err=%v ok=%v", err, ok)
	}
	if m.Goal.Text != "" || m.Goal.Status == GoalStatusRunning {
		t.Fatalf("goal should be cleared, got %+v", m.Goal)
	}
	if m.Budget.DailyWakeupLimit != 3 {
		t.Fatalf("budget should be preserved, got %+v", m.Budget)
	}
}

func TestResumeCorruptRuntimeDoesNotBlock(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "corrupt-runtime.jsonl")

	s := agent.NewSession("")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "hello"})
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "hi"})
	if err := s.Save(sessionPath); err != nil {
		t.Fatalf("save session: %v", err)
	}

	// Write corrupt runtime sidecar.
	runtimePath := agent.RuntimeMetaPath(sessionPath)
	os.WriteFile(runtimePath, []byte("{{{not valid json"), 0o644)

	ag := agent.New(nil, tool.NewRegistry(), agent.NewSession(""), agent.Options{}, event.Discard)
	c := New(Options{
		Executor: ag,
		Sink:     event.Discard,
	})
	loaded, err := agent.LoadSession(sessionPath)
	if err != nil {
		t.Fatalf("load session: %v", err)
	}

	// Should not panic or fail — corrupt sidecar is a warning, not a blocker.
	c.Resume(loaded, sessionPath)

	// Goal state should be empty (not restored).
	if got := c.Goal(); got != "" {
		t.Errorf("Goal() = %q after corrupt runtime resume, want empty", got)
	}
}

// panicProvider panics if Stream is called — used to verify no model calls happen.
type panicProvider struct{}

func (p *panicProvider) Name() string { return "panic" }
func (p *panicProvider) Stream(_ context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	panic("model should not be called during resume")
}

// --- Milestone 2: /goal continue tests ---

func TestGoalContinueNoActiveGoal(t *testing.T) {
	ag := agent.New(nil, tool.NewRegistry(), agent.NewSession(""), agent.Options{}, event.Discard)
	var notices []string
	c := New(Options{
		Executor: ag,
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.Notice {
				notices = append(notices, e.Text)
			}
		}),
	})

	c.Submit("/goal continue")
	if len(notices) == 0 {
		t.Fatal("expected a notice for /goal continue with no active goal")
	}
	if !strings.Contains(notices[len(notices)-1], "no active goal") &&
		!strings.Contains(notices[len(notices)-1], "没有活跃目标") {
		t.Errorf("unexpected notice: %q", notices[len(notices)-1])
	}
}

func TestGoalContinueRunningGoal(t *testing.T) {
	prov := &scriptedTurns{turns: [][]provider.Chunk{
		textTurn("Continued work done.\n\n[goal:complete]"),
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

	// Set a goal manually (simulating a resumed state).
	c.SetGoal("deploy the service")

	c.Submit("/goal continue")
	waitForTurnDone(t, events)

	if prov.call != 1 {
		t.Fatalf("provider calls = %d, want 1", prov.call)
	}
	if got := c.GoalStatus(); got != GoalStatusComplete {
		t.Fatalf("GoalStatus() = %q, want complete", got)
	}
}

func TestGoalContinueBlockedGoalResetsBlocked(t *testing.T) {
	prov := &scriptedTurns{turns: [][]provider.Chunk{
		textTurn("Resolved the issue.\n\n[goal:complete]"),
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

	// Set a goal and manually mark it blocked.
	c.mu.Lock()
	c.goal = "deploy the service"
	c.goalStatus = GoalStatusBlocked
	c.goalBlocks = 3
	c.goalBlock = "needs credentials"
	c.mu.Unlock()

	c.Submit("/goal continue")
	waitForTurnDone(t, events)

	if prov.call != 1 {
		t.Fatalf("provider calls = %d, want 1 (blocked should reset and continue)", prov.call)
	}
	if got := c.GoalStatus(); got != GoalStatusComplete {
		t.Fatalf("GoalStatus() = %q, want complete", got)
	}
}

func TestGoalContinueCompleteGoal(t *testing.T) {
	ag := agent.New(nil, tool.NewRegistry(), agent.NewSession(""), agent.Options{}, event.Discard)
	var notices []string
	c := New(Options{
		Executor: ag,
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.Notice {
				notices = append(notices, e.Text)
			}
		}),
	})

	// Set goal as complete.
	c.mu.Lock()
	c.goal = ""
	c.goalStatus = GoalStatusComplete
	c.mu.Unlock()

	c.Submit("/goal continue")
	found := false
	for _, n := range notices {
		if strings.Contains(n, "complete") || strings.Contains(n, "完成") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'already complete' notice, got %v", notices)
	}
}

func TestGoalContinueUsesGoalContinueTurn(t *testing.T) {
	prov := &scriptedTurns{turns: [][]provider.Chunk{
		textTurn("Done.\n\n[goal:complete]"),
	}}
	ag := agent.New(prov, tool.NewRegistry(), agent.NewSession(""), agent.Options{}, event.Discard)
	events := make(chan event.Event, 8)
	c := New(Options{
		Runner:   ag,
		Executor: ag,
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.TurnDone {
				events <- e
			}
		}),
	})

	c.SetGoal("build the widget")
	// Clear the message that SetGoal would have caused if runner was wired.
	// Actually, SetGoal without runGuarded+send just sets the fields. Let's
	// directly invoke ContinueGoal.
	c.mu.Lock()
	c.goal = "build the widget"
	c.goalStatus = GoalStatusRunning
	c.goalTurns = 3
	c.mu.Unlock()

	c.Submit("/goal continue")
	waitForTurnDone(t, events)

	// The user turn sent should be goalContinueTurn, NOT "Start pursuing...".
	msgs := ag.Session().Messages
	for _, m := range msgs {
		if m.Role == provider.RoleUser && strings.Contains(m.Content, "Start pursuing") {
			t.Fatal("ContinueGoal should use goalContinueTurn, not 'Start pursuing'")
		}
	}
	// It should contain the continue instruction.
	found := false
	for _, m := range msgs {
		if m.Role == provider.RoleUser && strings.Contains(m.Content, "Continue pursuing the active goal") {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected goalContinueTurn in user messages")
	}
}

func TestGoalContinueWithContextInjectsWakeupContext(t *testing.T) {
	prov := &scriptedTurns{turns: [][]provider.Chunk{
		textTurn("Done.\n\n[goal:complete]"),
	}}
	ag := agent.New(prov, tool.NewRegistry(), agent.NewSession(""), agent.Options{}, event.Discard)
	c := New(Options{Runner: ag, Executor: ag, Sink: event.Discard})
	c.SetGoal("triage CI")
	c.mu.Lock()
	c.goal = "triage CI"
	c.goalStatus = GoalStatusRunning
	c.mu.Unlock()

	if err := c.ContinueGoalWithContext(context.Background(), "webhook:github.workflow_run", "GitHub workflow failed on main"); err != nil {
		t.Fatalf("ContinueGoalWithContext: %v", err)
	}

	var found bool
	for _, m := range ag.Session().Messages {
		if m.Role != provider.RoleUser {
			continue
		}
		if strings.Contains(m.Content, "Continue pursuing the active goal") &&
			strings.Contains(m.Content, "<wakeup-context>") &&
			strings.Contains(m.Content, "GitHub workflow failed on main") {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected continuation user message to include wakeup context")
	}
}

func TestGoalContinueRecordsStartedAndCompleteTimeline(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "goal-complete-timeline.jsonl")

	prov := &scriptedTurns{turns: [][]provider.Chunk{
		textTurn("Done.\n\n[goal:complete]"),
	}}
	ag := agent.New(prov, tool.NewRegistry(), agent.NewSession(""), agent.Options{}, event.Discard)
	c := New(Options{Runner: ag, Executor: ag, SessionPath: sessionPath, Sink: event.Discard})
	c.SetGoal("finish timeline work")

	if err := c.ContinueGoalWithContext(context.Background(), "user", ""); err != nil {
		t.Fatalf("ContinueGoalWithContext: %v", err)
	}

	events, ok, err := agent.LoadRuntimeTimeline(sessionPath, 0)
	if err != nil || !ok {
		t.Fatalf("LoadRuntimeTimeline: ok=%v err=%v", ok, err)
	}
	requireRuntimeTimelineEvent(t, events, runtimeEventGoalContinuationStarted)
	requireRuntimeTimelineEvent(t, events, runtimeEventGoalContinuationComplete)
}

func TestGoalContinueBlockedRecordsTimeline(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "goal-blocked-timeline.jsonl")

	prov := &scriptedTurns{turns: [][]provider.Chunk{
		textTurn("Blocked.\n\n[goal:blocked:needs credentials]"),
		textTurn("Still blocked.\n\n[goal:blocked:needs credentials]"),
		textTurn("Still blocked.\n\n[goal:blocked:needs credentials]"),
	}}
	ag := agent.New(prov, tool.NewRegistry(), agent.NewSession(""), agent.Options{}, event.Discard)
	c := New(Options{Runner: ag, Executor: ag, SessionPath: sessionPath, Sink: event.Discard})
	c.SetGoal("deploy the service")

	if err := c.ContinueGoalWithContext(context.Background(), "user", ""); err != nil {
		t.Fatalf("ContinueGoalWithContext: %v", err)
	}
	if got := c.GoalStatus(); got != GoalStatusBlocked {
		t.Fatalf("GoalStatus() = %q, want blocked", got)
	}

	events, ok, err := agent.LoadRuntimeTimeline(sessionPath, 0)
	if err != nil || !ok {
		t.Fatalf("LoadRuntimeTimeline: ok=%v err=%v", ok, err)
	}
	event := requireRuntimeTimelineEvent(t, events, runtimeEventGoalBlocked)
	if event.Reason != "needs credentials" {
		t.Fatalf("blocked event reason = %q, want needs credentials", event.Reason)
	}
}

func TestGoalContinueLimitPersistsBlockedRuntimeAndTimeline(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "goal-limit.jsonl")

	prov := &panicProvider{}
	ag := agent.New(prov, tool.NewRegistry(), agent.NewSession(""), agent.Options{}, event.Discard)
	c := New(Options{Runner: ag, Executor: ag, SessionPath: sessionPath, Sink: event.Discard})
	c.mu.Lock()
	c.goal = "keep working forever"
	c.goalStatus = GoalStatusRunning
	c.goalTurns = 1
	c.goalTurnCap = 2
	c.mu.Unlock()

	if err := c.ContinueGoalWithContext(context.Background(), "cron", ""); err != nil {
		t.Fatalf("ContinueGoalWithContext: %v", err)
	}
	if got := c.GoalStatus(); got != GoalStatusBlocked {
		t.Fatalf("GoalStatus() = %q, want blocked", got)
	}

	meta, ok, err := agent.LoadRuntimeMeta(sessionPath)
	if err != nil || !ok {
		t.Fatalf("LoadRuntimeMeta: ok=%v err=%v", ok, err)
	}
	if meta.Goal.Status != GoalStatusBlocked || meta.Goal.BlockReason != goalContinuationLimitReason || meta.Goal.Turns != 2 {
		t.Fatalf("unexpected persisted goal state: %+v", meta.Goal)
	}
	if meta.Budget.MaxGoalAutoTurns != 2 {
		t.Fatalf("MaxGoalAutoTurns = %d, want 2", meta.Budget.MaxGoalAutoTurns)
	}

	events, ok, err := agent.LoadRuntimeTimeline(sessionPath, 0)
	if err != nil || !ok {
		t.Fatalf("LoadRuntimeTimeline: ok=%v err=%v", ok, err)
	}
	requireRuntimeTimelineEvent(t, events, runtimeEventGoalContinuationLimitReached)
}

func TestGoalContinueContextCancelPersistsStoppedRuntimeAndTimeline(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "goal-cancel.jsonl")

	prov := &panicProvider{}
	ag := agent.New(prov, tool.NewRegistry(), agent.NewSession(""), agent.Options{}, event.Discard)
	c := New(Options{Runner: ag, Executor: ag, SessionPath: sessionPath, Sink: event.Discard})
	c.SetGoal("stop when canceled")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := c.ContinueGoalWithContext(ctx, "user", "")
	if err == nil {
		t.Fatal("ContinueGoalWithContext on canceled context returned nil, want error")
	}
	if got := c.GoalStatus(); got != GoalStatusStopped {
		t.Fatalf("GoalStatus() = %q, want stopped", got)
	}

	meta, ok, loadErr := agent.LoadRuntimeMeta(sessionPath)
	if loadErr != nil || !ok {
		t.Fatalf("LoadRuntimeMeta: ok=%v err=%v", ok, loadErr)
	}
	if meta.Goal.Status != GoalStatusStopped || meta.Goal.Text != "stop when canceled" {
		t.Fatalf("unexpected persisted stopped goal: %+v", meta.Goal)
	}

	events, ok, loadErr := agent.LoadRuntimeTimeline(sessionPath, 0)
	if loadErr != nil || !ok {
		t.Fatalf("LoadRuntimeTimeline: ok=%v err=%v", ok, loadErr)
	}
	requireRuntimeTimelineEvent(t, events, runtimeEventGoalContinuationStarted)
	requireRuntimeTimelineEvent(t, events, runtimeEventGoalContinuationStopped)
}

func requireRuntimeTimelineEvent(t *testing.T, events []agent.RuntimeTimelineEvent, eventType string) agent.RuntimeTimelineEvent {
	t.Helper()
	for _, e := range events {
		if e.Type == eventType {
			return e
		}
	}
	t.Fatalf("runtime timeline missing %q: %+v", eventType, events)
	return agent.RuntimeTimelineEvent{}
}

func TestParseGoalCommandContinue(t *testing.T) {
	tests := []struct {
		input string
		want  GoalCommandAction
	}{
		{"/goal continue", GoalCommandContinue},
		{"/goal Continue", GoalCommandContinue},
		{"/goal CONTINUE", GoalCommandContinue},
		{"/goal resume", GoalCommandContinue},
		{"/goal Resume", GoalCommandContinue},
	}
	for _, tt := range tests {
		cmd, ok := ParseGoalCommand(tt.input)
		if !ok {
			t.Errorf("ParseGoalCommand(%q) not recognized", tt.input)
			continue
		}
		if cmd.Action != tt.want {
			t.Errorf("ParseGoalCommand(%q).Action = %d, want %d", tt.input, cmd.Action, tt.want)
		}
	}
}
