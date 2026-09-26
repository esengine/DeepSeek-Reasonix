package acp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/session/control"
)

type goalTurnSeen struct {
	goal   string
	status string
}

// TestServeGoalResumeKeepsTheRetainedObjective drives the lifecycle a client's
// Resume control needs: a cancelled Goal comes back with its objective and
// counters, and the next prompt continues it instead of replacing it.
func TestServeGoalResumeKeepsTheRetainedObjective(t *testing.T) {
	const objective = "keep this goal available for an explicit resume"
	started := make(chan struct{}, 1)
	seen := make(chan goalTurnSeen, 4)
	calls := 0
	factory := &configurableFactory{
		withCtrl: func(ctx context.Context, sink event.Sink, _ string, _ SessionParams, ctrl *control.Controller) error {
			calls++
			if calls == 1 {
				started <- struct{}{}
				<-ctx.Done()
				return ctx.Err()
			}
			seen <- goalTurnSeen{goal: ctrl.Goal(), status: ctrl.GoalStatus()}
			ctrl.ClearGoal()
			sink.Emit(event.Event{Kind: event.Text, Text: "done"})
			return nil
		},
	}
	client, stop := startServer(t, factory)
	defer stop()

	initResp := client.call(t, "initialize", InitializeParams{ProtocolVersion: 1})
	var init struct {
		AgentCapabilities struct {
			Meta map[string]json.RawMessage `json:"_meta"`
		} `json:"agentCapabilities"`
	}
	if err := json.Unmarshal(initResp.Result, &init); err != nil {
		t.Fatalf("initialize result: %v", err)
	}
	for _, method := range []string{sessionGoalResumeMethod, sessionGoalPauseMethod} {
		if _, ok := init.AgentCapabilities.Meta[method]; !ok {
			t.Fatalf("initialize does not advertise %s: %s", method, initResp.Result)
		}
	}

	newResp := client.call(t, "session/new", SessionNewParams{Cwd: testenv.TempDir(t)})
	var nr SessionNewResult
	if err := json.Unmarshal(newResp.Result, &nr); err != nil {
		t.Fatalf("session/new result: %v", err)
	}
	sid := nr.SessionID

	if resp := client.call(t, sessionGoalResumeMethod, SessionGoalParams{SessionID: sid}); resp.Error == nil || resp.Error.Code != ErrGoalNotResumable {
		t.Fatalf("resume without a goal = %+v, want code %d", resp.Error, ErrGoalNotResumable)
	}
	if resp := client.call(t, sessionGoalPauseMethod, SessionGoalParams{SessionID: sid}); resp.Error == nil || resp.Error.Code != ErrGoalNotRunning {
		t.Fatalf("pause without a goal = %+v, want code %d", resp.Error, ErrGoalNotRunning)
	}

	if resp := client.call(t, "session/set_mode", SessionSetModeParams{SessionID: sid, ModeID: sessionModeGoal}); resp.Error != nil {
		t.Fatalf("set goal mode: %+v", resp.Error)
	}
	promptCh := client.callAsync("session/prompt", SessionPromptParams{
		SessionID: sid,
		Prompt:    []ContentBlock{{Type: "text", Text: objective}},
	})
	select {
	case <-started:
	case <-time.After(rpcCallBudget(t)):
		t.Fatal("goal prompt never started")
	}
	client.notify("session/cancel", SessionCancelParams{SessionID: sid})
	if _, resp := drainPrompt(t, client, promptCh); resp.Error != nil {
		t.Fatalf("cancelled prompt: %+v", resp.Error)
	}

	before := goalStatusOf(t, client, sid)
	if before.Objective != objective || before.Status == control.GoalStatusRunning {
		t.Fatalf("goal after cancel = %+v, want the retained objective, not running", before)
	}

	resumeResp := client.call(t, sessionGoalResumeMethod, SessionGoalParams{SessionID: sid})
	if resumeResp.Error != nil {
		t.Fatalf("resume: %+v", resumeResp.Error)
	}
	var resumed SessionGoalResult
	if err := json.Unmarshal(resumeResp.Result, &resumed); err != nil {
		t.Fatalf("resume result: %v", err)
	}
	if resumed.Goal.Status != control.GoalStatusRunning || resumed.Goal.Objective != objective {
		t.Fatalf("resume result goal = %+v, want running %q", resumed.Goal, objective)
	}
	if before.Runtime != nil && (resumed.Goal.Runtime == nil || resumed.Goal.Runtime.TurnsUsed != before.Runtime.TurnsUsed) {
		t.Fatalf("resume reset the counters: before %+v after %+v", before.Runtime, resumed.Goal.Runtime)
	}
	if again := client.call(t, sessionGoalResumeMethod, SessionGoalParams{SessionID: sid}); again.Error == nil || again.Error.Code != ErrGoalNotResumable {
		t.Fatalf("resume of a running goal = %+v, want code %d", again.Error, ErrGoalNotResumable)
	}

	promptCh = client.callAsync("session/prompt", SessionPromptParams{
		SessionID: sid,
		Prompt:    []ContentBlock{{Type: "text", Text: "continue"}},
	})
	if _, resp := drainPrompt(t, client, promptCh); resp.Error != nil {
		t.Fatalf("continuation prompt: %+v", resp.Error)
	}
	got := <-seen
	if got.goal != objective || got.status != control.GoalStatusRunning {
		t.Fatalf("turn after resume saw %+v, want the retained objective running", got)
	}
}

func TestServeGoalPauseStopsARunningGoal(t *testing.T) {
	const objective = "ship the pause control"
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	factory := &configurableFactory{
		withCtrl: func(_ context.Context, sink event.Sink, _ string, _ SessionParams, _ *control.Controller) error {
			started <- struct{}{}
			<-release
			sink.Emit(event.Event{Kind: event.Text, Text: "working"})
			return nil
		},
	}
	client, stop := startServer(t, factory)
	defer stop()
	client.call(t, "initialize", InitializeParams{ProtocolVersion: 1})
	newResp := client.call(t, "session/new", SessionNewParams{Cwd: testenv.TempDir(t)})
	var nr SessionNewResult
	if err := json.Unmarshal(newResp.Result, &nr); err != nil {
		t.Fatalf("session/new result: %v", err)
	}
	sid := nr.SessionID
	if resp := client.call(t, "session/set_mode", SessionSetModeParams{SessionID: sid, ModeID: sessionModeGoal}); resp.Error != nil {
		t.Fatalf("set goal mode: %+v", resp.Error)
	}
	promptCh := client.callAsync("session/prompt", SessionPromptParams{
		SessionID: sid,
		Prompt:    []ContentBlock{{Type: "text", Text: objective}},
	})
	select {
	case <-started:
	case <-time.After(rpcCallBudget(t)):
		t.Fatal("goal prompt never started")
	}

	pauseResp := client.call(t, sessionGoalPauseMethod, SessionGoalParams{SessionID: sid})
	close(release)
	if pauseResp.Error != nil {
		t.Fatalf("pause: %+v", pauseResp.Error)
	}
	var paused SessionGoalResult
	if err := json.Unmarshal(pauseResp.Result, &paused); err != nil {
		t.Fatalf("pause result: %v", err)
	}
	if paused.Goal.Status == control.GoalStatusRunning || paused.Goal.Objective != objective {
		t.Fatalf("pause result goal = %+v, want a paused goal that keeps its objective", paused.Goal)
	}
	if _, resp := drainPrompt(t, client, promptCh); resp.Error != nil {
		t.Fatalf("paused prompt: %+v", resp.Error)
	}
	if got := goalStatusOf(t, client, sid); got.Status == control.GoalStatusRunning || got.Objective != objective {
		t.Fatalf("goal after the paused turn = %+v, want it still paused with its objective", got)
	}
	if resp := client.call(t, sessionGoalResumeMethod, SessionGoalParams{SessionID: sid}); resp.Error != nil {
		t.Fatalf("resume after pause: %+v", resp.Error)
	}
}

func goalStatusOf(t *testing.T, client *rpcClient, sid string) ReasonixStatusGoal {
	t.Helper()
	resp := client.call(t, sessionStatusMethod, SessionStatusParams{SessionID: sid})
	if resp.Error != nil {
		t.Fatalf("status: %+v", resp.Error)
	}
	var st ReasonixSessionStatus
	if err := json.Unmarshal(resp.Result, &st); err != nil {
		t.Fatalf("status result: %v", err)
	}
	return st.Goal
}

// A terminal turn error leaves the controller's Goal running while status
// labels it failed: a plain prompt continues it, resume has nothing to resume,
// and pause replaces the label with the paused state.
func TestServeGoalFailedLabelKeepsTheGoalRunning(t *testing.T) {
	const objective = "survive a failed turn"
	seen := make(chan goalTurnSeen, 4)
	factory := &configurableFactory{
		withCtrl: func(_ context.Context, _ event.Sink, _ string, _ SessionParams, ctrl *control.Controller) error {
			seen <- goalTurnSeen{goal: ctrl.Goal(), status: ctrl.GoalStatus()}
			return errors.New("provider exploded")
		},
	}
	client, stop := startServer(t, factory)
	defer stop()
	client.call(t, "initialize", InitializeParams{ProtocolVersion: 1})
	newResp := client.call(t, "session/new", SessionNewParams{Cwd: testenv.TempDir(t)})
	var nr SessionNewResult
	if err := json.Unmarshal(newResp.Result, &nr); err != nil {
		t.Fatalf("session/new result: %v", err)
	}
	sid := nr.SessionID
	if resp := client.call(t, "session/set_mode", SessionSetModeParams{SessionID: sid, ModeID: sessionModeGoal}); resp.Error != nil {
		t.Fatalf("set goal mode: %+v", resp.Error)
	}
	prompt := func(text string) {
		t.Helper()
		ch := client.callAsync("session/prompt", SessionPromptParams{SessionID: sid, Prompt: []ContentBlock{{Type: "text", Text: text}}})
		if _, resp := drainPrompt(t, client, ch); resp.Error != nil {
			t.Fatalf("prompt %q: %+v", text, resp.Error)
		}
		<-seen
	}
	prompt(objective)
	if got := goalStatusOf(t, client, sid); got.Status != "failed" || got.Objective != objective {
		t.Fatalf("goal after a failed turn = %+v, want failed with its objective", got)
	}
	if resp := client.call(t, sessionGoalResumeMethod, SessionGoalParams{SessionID: sid}); resp.Error == nil || resp.Error.Code != ErrGoalNotResumable {
		t.Fatalf("resume of a failed-but-running goal = %+v, want code %d", resp.Error, ErrGoalNotResumable)
	}

	ch := client.callAsync("session/prompt", SessionPromptParams{SessionID: sid, Prompt: []ContentBlock{{Type: "text", Text: "continue"}}})
	if _, resp := drainPrompt(t, client, ch); resp.Error != nil {
		t.Fatalf("continuation prompt: %+v", resp.Error)
	}
	if got := <-seen; got.goal != objective || got.status != control.GoalStatusRunning {
		t.Fatalf("plain prompt after failure saw %+v, want the same goal running", got)
	}

	pauseResp := client.call(t, sessionGoalPauseMethod, SessionGoalParams{SessionID: sid})
	if pauseResp.Error != nil {
		t.Fatalf("pause: %+v", pauseResp.Error)
	}
	var paused SessionGoalResult
	if err := json.Unmarshal(pauseResp.Result, &paused); err != nil {
		t.Fatalf("pause result: %v", err)
	}
	if paused.Goal.Status == "failed" || paused.Goal.Status == control.GoalStatusRunning || paused.Goal.Objective != objective {
		t.Fatalf("pause result goal = %+v, want the paused state, not the failed label", paused.Goal)
	}
	if got := goalStatusOf(t, client, sid); got.Status != paused.Goal.Status {
		t.Fatalf("status after pause = %+v, want %q", got, paused.Goal.Status)
	}
}
