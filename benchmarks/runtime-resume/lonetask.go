package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"time"

	"reasonix/internal/agentgraph"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

// The four single-delegation arms. A lone task is not another executor: it
// reaches the same RunProfileSpec a fleet item does and asks the same scheduler
// for a slot. What these ask is whether that shared chain is wired to the
// durable authority the fan-out arms established — and where, exactly, it stops
// being wired, which is a different question from "does it have a graph".
const (
	// armTaskCompleted lets the delegation finish and the turn close. Nothing is
	// interrupted, so a loss here is not about interruption: it says the run's
	// provenance was never durable at all.
	armTaskCompleted = "task-completed"
	// armTaskRunning kills the process with the child mid-execution, the same
	// death the fan-out arms take.
	armTaskRunning = "task-running"
	// armTaskFgQueued is the live-truth arm: the ceiling is full when the
	// delegation asks, so the scheduler is holding it back. What the parent and
	// the graph say while that is true is the whole question — a picture that
	// says running while nothing has started is wrong before any restart.
	armTaskFgQueued = "task-foreground-queued"
	// armTaskBgQueued is the one the foreground arms cannot stand in for: a
	// background run hands its execution to a job and asks for a slot inside it,
	// so the parent tool call has already returned when the refusal happens.
	armTaskBgQueued = "task-background-queued"
	// armTaskBgRunning is the same handoff with the slot granted, which is where
	// ownership of the terminal moves from the caller to the job.
	armTaskBgRunning = "task-background-running"
	// The cancellation arms. They ask the one question the five before them
	// cannot: who owns the ending when work is stopped, on each side of the
	// boundary that decides whether it ever ran. A child the scheduler never
	// admitted produced nothing, so the store owes it no terminal at all.
	armTaskFgQueuedCancel  = "task-foreground-queued-cancel"
	armTaskBgQueuedCancel  = "task-background-queued-cancel"
	armTaskFgRunningCancel = "task-foreground-running-cancel"
	armTaskBgRunningCancel = "task-background-running-cancel"
)

func cancelTaskArm(name string) bool {
	switch name {
	case armTaskFgQueuedCancel, armTaskBgQueuedCancel, armTaskFgRunningCancel, armTaskBgRunningCancel:
		return true
	}
	return false
}

// beforeStartArm names the arms cancelled while the scheduler still held the
// delegation, which is the half of the boundary the store must stay out of.
func beforeStartArm(name string) bool {
	return name == armTaskFgQueuedCancel || name == armTaskBgQueuedCancel
}

func loneTaskArm(name string) bool {
	switch name {
	case armTaskCompleted, armTaskRunning, armTaskFgQueued, armTaskBgQueued, armTaskBgRunning:
		return true
	}
	return cancelTaskArm(name)
}

// queuedTaskArm names the arms that fill the ceiling before delegating, so the
// scheduler has to refuse the delegation admission.
func queuedTaskArm(name string) bool {
	return name == armTaskFgQueued || name == armTaskBgQueued || beforeStartArm(name)
}

// childEntered names the record a child leaves when it reaches the provider.
// The queued arms ask a negative — that one never did — which nothing else in
// the probe can answer.
func childEntered(sentinel string) string { return "child-entered:" + sentinel }

// childLeft names the record a child leaves when it returns. Entered without
// left is a child still inside the provider — the state a cancellation that
// never arrived leaves behind.
func childLeft(sentinel string) string { return "child-left:" + sentinel }

// childCtxDone names the record a child leaves when its own context closes,
// which is a different fact from having returned: a run whose caller stopped
// waiting returns to nobody with its context still open.
func childCtxDone(sentinel string) string { return "child-ctxdone:" + sentinel }

// probeChildEntered is where that answer rides into the observation. It is not
// a status the kernel emits: the key names the probe so no row reads it as one.
const probeChildEntered = "probe:delegated-child-entered"

func backgroundTaskArm(name string) bool {
	switch name {
	case armTaskBgQueued, armTaskBgRunning, armTaskBgQueuedCancel, armTaskBgRunningCancel,
		armLineageBackground, armLineageJobKill, armCancelBackground:
		return true
	}
	return false
}

// The sentinels one turn carries. The queued arm names the holder as well: its
// ceiling has to be occupied before the delegation asks for a slot, or the
// refusal it is built to record never happens.
const (
	taskSentinel   = "PROBE-TASK"
	taskCallID     = "probe_task"
	taskHoldsFirst = fleetHolderSentinel + " " + taskSentinel
)

// loneTaskCall is the delegation itself, dispatched through the tool a model
// calls. Nothing here reaches past the tool's own arguments: the probe measures
// the production path, so it must enter it the way a model does.
func loneTaskCall(arm string) []provider.Chunk {
	prompt, description := childDone+" lone task", "completes"
	if arm != armTaskCompleted {
		prompt, description = childHang+" lone task", "still executing at death"
	}
	args, _ := json.Marshal(map[string]any{
		"prompt": prompt, "description": description,
		"run_in_background": backgroundTaskArm(arm),
	})
	return []provider.Chunk{{
		Type:     provider.ChunkToolCall,
		ToolCall: &provider.ToolCall{ID: taskCallID, Name: "task", Arguments: string(args)},
	}}
}

// probeStepIDs are the host's open steps, in the order they have to be signed.
// The arm whose turn ends has to close them: a delivery is refused while the
// list is open, and a todo_write that marks an item completed on its own is
// refused too — the sign-off is the only door.
func probeStepIDs() []string { return []string{"probe_step_01", "probe_step_02", "probe_step_03"} }

// signStep signs off one step per round, which is what the host accepts: the
// sign-off promotes the next item, so two in one round sign against a list that
// no longer stands.
func signStep(id string) []provider.Chunk {
	args, _ := json.Marshal(map[string]any{
		"step_id": id,
		"result":  "the probe drove this step to its end",
		"evidence": []map[string]any{
			{"kind": "manual", "summary": "the probe established this state directly"},
		},
	})
	return []provider.Chunk{{
		Type:     provider.ChunkToolCall,
		ToolCall: &provider.ToolCall{ID: "probe_sign_" + id, Name: "complete_step", Arguments: string(args)},
	}}
}

// loneTaskSentinels is what this arm's one prompt carries.
func loneTaskSentinels(arm string) string {
	if queuedTaskArm(arm) {
		return taskHoldsFirst
	}
	return taskSentinel
}

// runLoneTaskConstruct drives one delegation and dies at the moment this arm is
// about. The completed arm is the exception that proves the rest: it lets the
// turn close, so what it loses was never about dying inside one.
func runLoneTaskConstruct(ctx context.Context, root armRoot, arm, bootSystem string, ctrl *control.Controller, sink *graphSink, prov *scripted, turn int) error {
	// One turn, whatever the arm needs in it: a session admits no second one. The
	// queued arm's ceiling is filled by a background fleet named in the same
	// prompt, whose own child says when the slot is taken.
	prompt := fanOutTurn(turn+1, loneTaskSentinels(arm))
	switch {
	case arm == armTaskCompleted:
		if err := ctrl.Run(ctx, prompt); err != nil {
			return fmt.Errorf("delegating turn: %w", err)
		}
	case guardedArm(arm):
		// A stop belongs to whoever admitted the turn, so an arm that is going
		// to be stopped is admitted the way a person's input is.
		ctrl.Send(prompt)
		if err := waitForLoneTask(sink, prov, arm); err != nil {
			return err
		}
	default:
		go func() { _ = ctrl.Run(ctx, prompt) }()
		if err := waitForLoneTask(sink, prov, arm); err != nil {
			return err
		}
	}
	stop := ""
	if cancelTaskArm(arm) {
		stop = stopDelegation(ctrl, prov, arm)
	}
	obs := capture("construct", arm, bootSystem, ctrl, sink, root)
	if stop != "" {
		obs.Progress[probeStopReached] = []string{stop}
	}
	if queuedTaskArm(arm) {
		// Whether the delegation's own child ever ran is the fact the live rows
		// are read against, and only the scripted provider knows it.
		obs.Progress[probeChildEntered] = []string{fmt.Sprint(prov.fleets.held(childEntered(childHang)))}
	}
	if err := writeObservation(root, obs); err != nil {
		return err
	}
	os.Exit(0)
	return nil
}

// stopDelegation stops the work the way a person does — the turn's own cancel
// for a foreground run, the job list and a kill for a backgrounded one — and
// waits for the orchestration to let go of it. What each layer holds then is
// this arm's whole question, so nothing is read until the record closes.
func stopDelegation(ctrl *control.Controller, prov *scripted, arm string) string {
	stopped := time.Now()
	if backgroundTaskArm(arm) {
		for _, job := range ctrl.Jobs() {
			ctrl.CancelJob(job.ID)
		}
	} else {
		ctrl.Cancel()
	}
	// Wait for the record to close, but do not demand it: whether a stop reaches
	// the work it was aimed at is what this arm measures, and an arm that failed
	// when it did not would report the finding as its own premise breaking.
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) && !settledLone(ctrl) {
		time.Sleep(50 * time.Millisecond)
	}
	live := "gone"
	if prov.fleets.held(childEntered(childHang)) && !prov.fleets.held(childLeft(childHang)) {
		live = "still running"
	}
	return fmt.Sprintf("turn=%s work=%s after=%s", turnStanding(ctrl, arm), live,
		time.Since(stopped).Round(time.Second))
}

func settledLone(ctrl *control.Controller) bool {
	for _, e := range control.ExecutionHistory(ctrl.SessionPath()) {
		if e.ID == parallelLoneNodeID && !e.SettledAt.IsZero() {
			return true
		}
	}
	return false
}

// waitForLoneTask blocks until the delegation stands where this arm dies. The
// foreground arms read the live graph; the background ones cannot — a handed-off
// run publishes no node — so they read the progress the parent was told.
func waitForLoneTask(sink *graphSink, prov *scripted, arm string) error {
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		if loneTaskReady(sink, prov, arm) {
			// A refusal is a state nothing announces, so the arm holds still and
			// asks again: a child that was merely slow to start would have
			// arrived by now, and the death would be measuring a race.
			if beforeStartArm(arm) || arm == armTaskFgQueued {
				time.Sleep(2 * time.Second)
				return refusalHolding(prov, arm)
			}
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	graph, _ := sink.snapshot()
	return errUnexpected("a lone delegation "+loneTaskWanted(arm),
		nodeStates(graph)+" progress="+fmt.Sprint(sink.phaseSeries()))
}

// refusalHolding reports the arm's premise once more, after the settle: the
// delegation's child must still not have run.
func refusalHolding(prov *scripted, arm string) error {
	if prov.fleets.held(childEntered(childHang)) {
		return errUnexpected("a delegation the scheduler was still holding back", "its child ran")
	}
	return nil
}

func loneTaskReady(sink *graphSink, prov *scripted, arm string) bool {
	graph, _ := sink.snapshot()
	switch arm {
	case armTaskFgQueued, armTaskFgQueuedCancel:
		// The node is drawn — the delegation reached the graph — and its child
		// has not started, which with the ceiling full is the refusal.
		return loneNodeDrawn(graph) && !prov.fleets.held(childEntered(childHang))
	case armTaskRunning, armTaskFgRunningCancel, armTaskBgRunningCancel:
		return holdsAll(graph, []agentgraph.NodeState{agentgraph.StateRunning})
	case armTaskBgQueued, armTaskBgQueuedCancel:
		phases := sink.phaseSeries()[taskCallID]
		return len(phases) > 0 && phases[len(phases)-1] == string(subagentQueued)
	case armTaskBgRunning:
		return slices.Contains(sink.phaseSeries()[taskCallID], subagentRunning)
	}
	return false
}

// loneNodeDrawn reports whether the delegation's own node is on the graph.
func loneNodeDrawn(g agentgraph.Graph) bool {
	for _, n := range g.Nodes {
		if n.Kind == agentgraph.KindWorker && n.ID == parallelLoneNodeID {
			return true
		}
	}
	return false
}

// parallelLoneNodeID is what a lone delegation's node is called: the parent
// call, and the first slot of the fan-out numbering it shares.
const parallelLoneNodeID = taskCallID + "/sub-1"

func loneTaskWanted(arm string) string {
	switch arm {
	case armTaskRunning, armTaskFgRunningCancel, armTaskBgRunningCancel:
		return "running"
	case armTaskFgQueued, armTaskFgQueuedCancel, armTaskBgQueuedCancel:
		return "drawn while the scheduler holds it back"
	case armTaskBgQueued:
		return "queued for a slot and not started"
	}
	return "running in its job"
}

// The two phases the probe waits on. They are the parent-facing statuses a
// background delegation emits, and for a handed-off run they are the only live
// evidence there is.
const (
	subagentQueued  = "queued"
	subagentRunning = "running"
)

// progressPhase reads one subagent status event. The tool id is the parent call
// the delegation was made from, which is also what the graph node hangs under
// and what the sub-agent store records as its parent — the join this arm exists
// to check rather than assume.
func progressPhase(e event.Event) (id, phase string, ok bool) {
	if e.Kind != event.ToolProgress || e.Tool.Name != event.SubagentProgressStatusName {
		return "", "", false
	}
	return e.Tool.ID, e.Tool.Output, e.Tool.ID != ""
}
