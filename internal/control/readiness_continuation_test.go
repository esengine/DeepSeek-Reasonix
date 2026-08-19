package control

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/i18n"
	"reasonix/internal/instruction"
	"reasonix/internal/provider"
	"reasonix/internal/sessioninbox"
	"reasonix/internal/tool"
)

func readinessContinuationController(t *testing.T, turns [][]provider.Chunk, sink event.Sink) (*Controller, *scriptedTurns) {
	return readinessContinuationControllerWithOptions(t, turns, sink, agent.Options{})
}

func readinessContinuationControllerWithOptions(t *testing.T, turns [][]provider.Chunk, sink event.Sink, opts agent.Options) (*Controller, *scriptedTurns) {
	t.Helper()
	reg := tool.NewRegistry()
	reg.Add(minimalFakeTool{name: "write_file"})
	reg.Add(minimalFakeTool{name: "read_file", readOnly: true})
	reg.Add(minimalFakeTool{name: "bash"})
	prov := &scriptedTurns{turns: turns}
	executor := agent.New(prov, reg, agent.NewSession("stable-system-prefix"), opts, event.Discard)
	c := New(Options{Runner: executor, Executor: executor, Sink: sink})
	// Readiness continuation is a delivery-floor mechanism: the standard floor
	// never pauses on a readiness gap, so these turns run under delivery.
	if err := c.SetQualityFloor(QualityFloorDelivery); err != nil {
		t.Fatalf("SetQualityFloor: %v", err)
	}
	t.Cleanup(c.Close)
	return c, prov
}

func readinessSyntheticTurns(c *Controller) int {
	if c == nil || c.executor == nil {
		return 0
	}
	count := 0
	for _, message := range c.executor.Session().Snapshot() {
		if message.Role == provider.RoleUser && IsSyntheticUserMessage(message.Content) {
			count++
		}
	}
	return count
}

func TestOrdinaryTurnAutomaticallyFinishesKnownChecks(t *testing.T) {
	notices := 0
	readinessNoticeDetail := ""
	c, prov := readinessContinuationController(t, [][]provider.Chunk{
		{toolCallChunk("write", "write_file", `{"path":"main.go","content":"package main"}`), {Type: provider.ChunkDone}},
		textTurn("implemented"),
		{toolCallChunk("verify", "bash", `{"command":"go test ./..."}`), {Type: provider.ChunkDone}},
		{toolCallChunk("review", "read_file", `{"path":"main.go"}`), {Type: provider.ChunkDone}},
		textTurn("implemented and verified"),
	}, event.FuncSink(func(e event.Event) {
		if e.Kind == event.Notice && e.Text != "" {
			notices++
			if e.Text == i18n.M.ReadinessContinuing {
				readinessNoticeDetail = e.Detail
			}
		}
	}))

	if err := newTurnOrchestrator(c).runGoalLoopWithRawDisplay(context.Background(), "update main.go", "update main.go", ""); err != nil {
		t.Fatalf("ordinary turn returned a readiness failure: %v", err)
	}
	if prov.call < 5 {
		t.Fatalf("provider calls = %d, want the host to continue through verification and review", prov.call)
	}
	if got := readinessSyntheticTurns(c); got != 1 {
		t.Fatalf("synthetic readiness turns = %d, want 1", got)
	}
	messages := c.executor.Session().Snapshot()
	if len(messages) == 0 || messages[0].Role != provider.RoleSystem || messages[0].Content != "stable-system-prefix" {
		t.Fatalf("automatic continuation changed the stable system prefix: %+v", messages)
	}
	if notices == 0 {
		t.Fatal("automatic readiness continuation did not emit a progress notice")
	}
	if readinessNoticeDetail != "" {
		t.Fatalf("automatic readiness continuation exposed notice detail %q", readinessNoticeDetail)
	}
	if c.executor.PrepareFinalReadinessRecovery() {
		t.Fatal("successful automatic continuation left a manual recovery action pending")
	}
}

func TestReadinessContinuationPromptStaysHiddenFromUserHistory(t *testing.T) {
	prompt := readinessContinuationPrompt(nil, []string{"verification"}, "run the missing verification")
	if !IsSyntheticUserMessage(prompt) {
		t.Fatalf("readiness continuation was treated as user-authored text: %q", prompt)
	}
	for _, forbidden := range []string{"expand scope", "repeat destructive or external actions", "exact limitation"} {
		if !strings.Contains(prompt, forbidden) {
			t.Fatalf("readiness continuation prompt = %q, want safety clause %q", prompt, forbidden)
		}
	}
}

func TestReadinessContinuationPromptIncludesOnlyReportedTodoGaps(t *testing.T) {
	todos := []evidence.TodoItem{{Content: "future task", Status: "pending"}}
	verification := readinessContinuationPrompt(todos, []string{"verification"}, "run verification")
	if strings.Contains(verification, "future task") || strings.Contains(verification, "tasks are still incomplete") {
		t.Fatalf("verification-only continuation leaked ordinary cross-turn todos: %q", verification)
	}

	todoGap := readinessContinuationPrompt(todos, []string{"todo"}, "finish the delivery plan")
	if !strings.Contains(todoGap, "future task") || !strings.Contains(todoGap, "tasks are still incomplete") {
		t.Fatalf("todo readiness gap omitted its incomplete task context: %q", todoGap)
	}
}

func TestReadinessProgressRequiresTwoDistinctEvidenceKeys(t *testing.T) {
	for _, tc := range []struct {
		name              string
		previous, current string
		want              bool
	}{
		{name: "changed", previous: "before", current: "after", want: true},
		{name: "unchanged", previous: "same", current: "same", want: false},
		{name: "missing previous", current: "after", want: false},
		{name: "missing current", previous: "before", want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := readinessMadeProgress(tc.previous, tc.current); got != tc.want {
				t.Fatalf("readinessMadeProgress(%q, %q) = %v, want %v", tc.previous, tc.current, got, tc.want)
			}
		})
	}
}

// The standard floor is the 1.23.0 balanced feel: a readiness gap is recorded
// but never pauses the turn, so the user never meets a recovery card.
func TestStandardFloorNeverPausesOnReadinessGap(t *testing.T) {
	c, prov := readinessContinuationController(t, [][]provider.Chunk{
		{toolCallChunk("write", "write_file", `{"path":"main.go","content":"package main"}`), {Type: provider.ChunkDone}},
		textTurn("implemented without checks"),
	}, event.Discard)
	if err := c.SetQualityFloor(QualityFloorStandard); err != nil {
		t.Fatalf("SetQualityFloor: %v", err)
	}

	err := newTurnOrchestrator(c).runGoalLoopWithRawDisplay(context.Background(), "update main.go", "update main.go", "")
	var readinessErr *agent.FinalReadinessError
	if errors.As(err, &readinessErr) {
		t.Fatalf("standard floor paused on a readiness gap: %v", err)
	}
	if err != nil {
		t.Fatalf("standard floor turn returned error: %v", err)
	}
	if got := readinessSyntheticTurns(c); got != 0 {
		t.Fatalf("synthetic readiness turns = %d, want 0 under the standard floor", got)
	}
	if prov.call != 2 {
		t.Fatalf("provider calls = %d, want exactly the scripted work and answer", prov.call)
	}
}

// Problem-one regression: the floor alone decides the pause. Goal mode used to
// force it through deliveryScopeActive, so a standard-floor goal session still
// met the recovery card. The goal FSM reads Agent.ReadinessResult directly, so
// suppressing the pause costs it nothing.
func TestStandardFloorSuppressesThePauseInsideGoalMode(t *testing.T) {
	c, _ := readinessContinuationController(t, [][]provider.Chunk{
		{toolCallChunk("write", "write_file", `{"path":"main.go","content":"package main"}`), {Type: provider.ChunkDone}},
		textTurn("implemented without checks"),
	}, event.Discard)
	if err := c.SetQualityFloor(QualityFloorStandard); err != nil {
		t.Fatalf("SetQualityFloor: %v", err)
	}
	c.SetGoal("ship main.go")
	t.Cleanup(c.ClearGoal)

	err := newTurnOrchestrator(c).runGoalLoopWithRawDisplay(context.Background(), "update main.go", "update main.go", "")
	var readinessErr *agent.FinalReadinessError
	if errors.As(err, &readinessErr) {
		t.Fatalf("standard floor paused inside goal mode: %v", err)
	}
	// The FSM still sees the gap through the executor, not through the error.
	if got := c.executor.ReadinessResult(); got.Ready {
		t.Fatal("executor reported ready despite the unmet readiness requirement")
	}
}

// The delivery floor deepens verification; it does not take over persistence.
// The generic continuation stays bounded exactly as it was before the floor.
func TestDeliveryGenericReadinessContinuationStopsAfterOneTurn(t *testing.T) {
	c, prov := readinessContinuationController(t, [][]provider.Chunk{
		{toolCallChunk("write", "write_file", `{"path":"main.go","content":"package main"}`), {Type: provider.ChunkDone}},
		textTurn("implemented without checks"),
	}, event.Discard)

	err := newTurnOrchestrator(c).runGoalLoopWithRawDisplay(context.Background(), "update main.go", "update main.go", "")
	var readinessErr *agent.FinalReadinessError
	if !errors.As(err, &readinessErr) {
		t.Fatalf("generic continuation error = %v, want final readiness failure", err)
	}
	if readinessErr.Attempts != 2 {
		t.Fatalf("readiness attempts = %d, want original + one automatic turn", readinessErr.Attempts)
	}
	if got := readinessSyntheticTurns(c); got != 1 {
		t.Fatalf("synthetic readiness turns = %d, want 1", got)
	}
	if prov.call > 3 {
		t.Fatalf("provider calls = %d, want the generic gap bounded after one continuation", prov.call)
	}
}

func TestHighConfidenceReadinessGetsSecondTurnOnlyAfterProgress(t *testing.T) {
	c, _ := readinessContinuationControllerWithOptions(t, [][]provider.Chunk{
		{toolCallChunk("write", "write_file", `{"path":"main.go","content":"package main"}`), {Type: provider.ChunkDone}},
		textTurn("implemented without the required project check"),
		{toolCallChunk("review", "read_file", `{"path":"main.go"}`), {Type: provider.ChunkDone}},
		textTurn("reviewed but not verified"),
		{toolCallChunk("verify", "bash", `{"command":"go test ./..."}`), {Type: provider.ChunkDone}},
		textTurn("verified"),
	}, event.Discard, agent.Options{ProjectChecks: []instruction.VerifyCheck{{
		Command: "go test ./...", SourcePath: "AGENTS.md", Line: 3,
	}}})

	err := newTurnOrchestrator(c).runGoalLoopWithRawDisplay(context.Background(), "update main.go", "update main.go", "")
	if err != nil {
		t.Fatalf("high-confidence continuation returned error: %v", err)
	}
	if got := readinessSyntheticTurns(c); got != 2 {
		t.Fatalf("synthetic readiness turns = %d, want 2 after review progress", got)
	}
}

func TestHighConfidenceReadinessStopsAfterOneTurnWithoutProgress(t *testing.T) {
	c, _ := readinessContinuationControllerWithOptions(t, [][]provider.Chunk{
		{toolCallChunk("write", "write_file", `{"path":"main.go","content":"package main"}`), {Type: provider.ChunkDone}},
		textTurn("implemented without the required project check"),
		textTurn("still not verified"),
	}, event.Discard, agent.Options{ProjectChecks: []instruction.VerifyCheck{{
		Command: "go test ./...", SourcePath: "AGENTS.md", Line: 3,
	}}})

	err := newTurnOrchestrator(c).runGoalLoopWithRawDisplay(context.Background(), "update main.go", "update main.go", "")
	var readinessErr *agent.FinalReadinessError
	if !errors.As(err, &readinessErr) {
		t.Fatalf("high-confidence continuation error = %v, want final readiness failure", err)
	}
	if readinessErr.Attempts != 2 {
		t.Fatalf("readiness attempts = %d, want original + one no-progress turn", readinessErr.Attempts)
	}
	if readinessErr.ContinuationClass != agent.ReadinessContinuationHighConfidence {
		t.Fatalf("ContinuationClass = %q, want high confidence", readinessErr.ContinuationClass)
	}
	if got := readinessSyntheticTurns(c); got != 1 {
		t.Fatalf("synthetic readiness turns = %d, want 1 without progress", got)
	}
	if !c.executor.PrepareFinalReadinessRecovery() {
		t.Fatal("bounded continuation did not preserve the manual recovery action")
	}
}

func TestHighConfidenceReadinessNeverExceedsTwoTurns(t *testing.T) {
	c, _ := readinessContinuationControllerWithOptions(t, [][]provider.Chunk{
		{toolCallChunk("write", "write_file", `{"path":"main.go","content":"package main"}`), {Type: provider.ChunkDone}},
		textTurn("implemented without the required project check"),
		{toolCallChunk("review", "read_file", `{"path":"main.go"}`), {Type: provider.ChunkDone}},
		textTurn("reviewed but not verified"),
		{toolCallChunk("verify-other", "bash", `{"command":"go test ./internal/agent"}`), {Type: provider.ChunkDone}},
		textTurn("ran a different verification command"),
	}, event.Discard, agent.Options{ProjectChecks: []instruction.VerifyCheck{{
		Command: "go test ./...", SourcePath: "AGENTS.md", Line: 3,
	}}})

	err := newTurnOrchestrator(c).runGoalLoopWithRawDisplay(context.Background(), "update main.go", "update main.go", "")
	var readinessErr *agent.FinalReadinessError
	if !errors.As(err, &readinessErr) {
		t.Fatalf("high-confidence continuation error = %v, want final readiness failure", err)
	}
	if readinessErr.Attempts != 3 {
		t.Fatalf("readiness attempts = %d, want original + two automatic turns", readinessErr.Attempts)
	}
	if got := readinessSyntheticTurns(c); got != 2 {
		t.Fatalf("synthetic readiness turns = %d, want hard cap 2", got)
	}
	if !c.executor.PrepareFinalReadinessRecovery() {
		t.Fatal("hard-cap exhaustion did not preserve the manual recovery action")
	}
}

func TestReadinessContinuationYieldsToCancellationAndPendingUserWork(t *testing.T) {
	readinessErr := &agent.FinalReadinessError{
		Attempts: 1, Reason: "run verification", Missing: []string{"verification"},
		ContinuationClass: agent.ReadinessContinuationGeneric, ProgressKey: "initial",
	}

	t.Run("cancelled context", func(t *testing.T) {
		c, prov := readinessContinuationController(t, nil, event.Discard)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := newTurnOrchestrator(c).continueUntilReady(ctx, readinessErr)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("continuation error = %v, want context cancellation", err)
		}
		if prov.call != 0 {
			t.Fatalf("provider calls = %d, want 0 after cancellation", prov.call)
		}
	})

	t.Run("controller cancellation requested", func(t *testing.T) {
		c, prov := readinessContinuationController(t, nil, event.Discard)
		c.mu.Lock()
		c.canceling = true
		c.mu.Unlock()
		err := newTurnOrchestrator(c).continueUntilReady(context.Background(), readinessErr)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("continuation error = %v, want controller cancellation", err)
		}
		if prov.call != 0 {
			t.Fatalf("provider calls = %d, want 0 after controller cancellation", prov.call)
		}
	})

	t.Run("parked user turn", func(t *testing.T) {
		c, prov := readinessContinuationController(t, nil, event.Discard)
		c.mu.Lock()
		c.parkedTurns = append(c.parkedTurns, func(context.Context) error { return nil })
		c.mu.Unlock()
		err := newTurnOrchestrator(c).continueUntilReady(context.Background(), readinessErr)
		var got *agent.FinalReadinessError
		if !errors.As(err, &got) || got.Attempts != 1 {
			t.Fatalf("continuation error = %v, want untouched readiness failure", err)
		}
		if prov.call != 0 {
			t.Fatalf("provider calls = %d, want 0 with parked user work", prov.call)
		}
	})

	t.Run("queued inbox follow-up", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "session.jsonl")
		executor := agent.New(nil, tool.NewRegistry(), agent.NewSession(""), agent.Options{}, event.Discard)
		c := New(Options{Runner: executor, Executor: executor, SessionPath: path})
		t.Cleanup(c.Close)
		if _, err := c.EnqueueInbox(InboxRequest{Submit: "user follow-up"}); err != nil {
			t.Fatalf("EnqueueInbox: %v", err)
		}
		if err := c.SetInboxPausedPassive(true); err != nil {
			t.Fatalf("SetInboxPausedPassive: %v", err)
		}
		err := newTurnOrchestrator(c).continueUntilReady(context.Background(), readinessErr)
		var got *agent.FinalReadinessError
		if !errors.As(err, &got) || got.Attempts != 1 {
			t.Fatalf("continuation error = %v, want untouched readiness failure", err)
		}
	})

	t.Run("queued inbox follow-up from another store", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "session.jsonl")
		executor := agent.New(nil, tool.NewRegistry(), agent.NewSession(""), agent.Options{}, event.Discard)
		c := New(Options{Runner: executor, Executor: executor, SessionPath: path})
		t.Cleanup(c.Close)
		if snap := c.InboxSnapshot(); len(snap.Items) != 0 {
			t.Fatalf("initial inbox snapshot = %+v, want empty", snap)
		}

		external, err := sessioninbox.Open(path, sessioninbox.Limits{})
		if err != nil {
			t.Fatalf("open external inbox: %v", err)
		}
		defer external.Close()
		if _, err := external.Enqueue(sessioninbox.EnqueueRequest{
			Intent:   sessioninbox.IntentFollowup,
			Envelope: sessioninbox.PromptEnvelope{SubmitText: "external user follow-up"},
		}); err != nil {
			t.Fatalf("enqueue external follow-up: %v", err)
		}

		if !c.hasPendingUserWork() {
			t.Fatal("pending-user check missed a follow-up committed by another Store")
		}
	})
}

func TestReadinessContinuationZeroClassDefaultsToManualRecovery(t *testing.T) {
	c, prov := readinessContinuationController(t, nil, event.Discard)
	want := &agent.FinalReadinessError{Attempts: 1, Reason: "legacy readiness gap", Missing: []string{"verification"}}
	err := newTurnOrchestrator(c).continueUntilReady(context.Background(), want)
	var got *agent.FinalReadinessError
	if !errors.As(err, &got) || got != want {
		t.Fatalf("continuation error = %v, want the original conservative readiness error", err)
	}
	if prov.call != 0 {
		t.Fatalf("provider calls = %d, want 0 for an unclassified legacy error", prov.call)
	}
}

func TestEditedOrdinaryTurnAlsoFinishesKnownChecks(t *testing.T) {
	c, prov := readinessContinuationController(t, [][]provider.Chunk{
		{toolCallChunk("write", "write_file", `{"path":"main.go","content":"package main"}`), {Type: provider.ChunkDone}},
		textTurn("implemented"),
		{toolCallChunk("verify", "bash", `{"command":"go test ./..."}`), {Type: provider.ChunkDone}},
		{toolCallChunk("review", "read_file", `{"path":"main.go"}`), {Type: provider.ChunkDone}},
		textTurn("implemented and verified"),
	}, event.Discard)

	err := newTurnOrchestrator(c).runEditedGoalLoopWithRawDisplay(
		context.Background(), "update main.go", "update main.go", "", "old request",
	)
	if err != nil {
		t.Fatalf("edited ordinary turn returned a readiness failure: %v", err)
	}
	if prov.call < 5 {
		t.Fatalf("provider calls = %d, want edited turns to share automatic readiness continuation", prov.call)
	}
}
