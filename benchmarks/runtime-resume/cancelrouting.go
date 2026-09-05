package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"reasonix/internal/control"
	"reasonix/internal/provider"
)

// Where a stop goes — and, first, whose stop it is. A controller owns the turns
// it admits itself and cancels those; a synchronous Run belongs to the caller
// that passed the context. Measuring the second with the first says nothing
// about either.
const (
	// armCancelTool is the control that says where a defect lives. If an
	// ordinary tool's context is cancelled and a delegation's is not, the fault
	// is in how a delegation derives its context; if neither is, it is upstream
	// of both.
	armCancelTool = "cancel-ordinary-tool"
	// armCancelBackground is the negative control, and the one that decides what
	// a fix may do: work already handed to a job has an owner of its own, and a
	// cancel that reached into it would take back the handoff.
	armCancelBackground = "cancel-background-handoff"
	// armCancelHeadlessOwner reads the other surface. A synchronous Run is never
	// admitted through the gate, so the controller holds no cancel for it and
	// the caller's own context holds the only one. Both halves are asserted: a
	// controller that started claiming this would be taking ownership from the
	// only holder that has it.
	armCancelHeadlessOwner = "cancel-headless-owner"
)

func cancelRoutingArm(name string) bool {
	switch name {
	case armCancelTool, armCancelBackground, armCancelHeadlessOwner:
		return true
	}
	return false
}

// guardedArm names the arms driven through a turn the controller admits itself,
// which is the surface a person's Stop reaches.
func guardedArm(name string) bool {
	return cancelTaskArm(name) || name == armCancelTool || name == armCancelBackground ||
		name == armSkillCancel || name == armHostCancel
}

const (
	sleepSentinel = "PROBE-SLEEP"
	sleepCallID   = "probe_sleep"
	// sleepSeconds outlasts the settling this probe waits through, so a shell
	// that ends promptly ended because something ended it.
	sleepSeconds = 30
)

// sleepCall is an ordinary foreground tool that does nothing but wait. It is
// the shell every turn can call, which is the point: it derives its context the
// way every other tool does.
func sleepCall() []provider.Chunk {
	args, _ := json.Marshal(map[string]any{
		"command":     fmt.Sprintf("sleep %d", sleepSeconds),
		"description": "waits until something stops it",
	})
	return []provider.Chunk{{
		Type:     provider.ChunkToolCall,
		ToolCall: &provider.ToolCall{ID: sleepCallID, Name: "bash", Arguments: string(args)},
	}}
}

// runCancelRoutingConstruct drives the arm's work, stops it the way that
// surface's owner stops it, and records what the stop reached. Nothing waits
// for the work to end: whether it ends at all is the measurement.
func runCancelRoutingConstruct(ctx context.Context, root armRoot, arm, bootSystem string, ctrl *control.Controller, sink *graphSink, prov *scripted, turn int) error {
	sentinel := sleepSentinel
	if arm == armCancelBackground {
		sentinel = taskSentinel
	}
	prompt := fanOutTurn(turn+1, sentinel)
	ctx, cancelCaller := context.WithCancel(ctx)
	defer cancelCaller()
	if guardedArm(arm) {
		ctrl.Send(prompt)
	} else {
		go func() { _ = ctrl.Run(ctx, prompt) }()
	}
	if err := waitForCancelSubject(sink, prov, arm); err != nil {
		return err
	}

	obs := capture("construct", arm, bootSystem, ctrl, sink, root)
	obs.Progress[probeStopReached] = stopReadings(ctrl, sink, prov, arm, cancelCaller)
	if err := writeObservation(root, obs); err != nil {
		return err
	}
	os.Exit(0)
	return nil
}

// probeStopReached carries what each stop reached into the observation. The key
// names the probe so no row reads it as a status the kernel emitted.
const probeStopReached = "probe:stop-reached"

// stopReadings issues this surface's stop and reports what it reached. The
// headless arm issues both in order — the controller's, which owns nothing
// there, and then the caller's, which owns everything.
func stopReadings(ctrl *control.Controller, sink *graphSink, prov *scripted, arm string, cancelCaller context.CancelFunc) []string {
	// What the stop found before it was issued. Work already ended would make
	// every answer below meaningless, and this probe has made that mistake once.
	before := "live"
	if !workLive(sink, prov, arm) {
		before = "already gone"
	}
	ctrl.Cancel()
	first := "controller-cancel before=" + before + " " + reachedAfter(ctrl, sink, prov, arm)
	if arm != armCancelHeadlessOwner {
		return []string{first}
	}
	cancelCaller()
	return []string{first, "caller-cancel " + reachedAfter(ctrl, sink, prov, arm)}
}

// reachedAfter waits out the settling this probe allows and reports what is
// still live. The wait stays well below the shell's own sleep, so work that has
// ended was ended by the stop rather than by finishing.
func reachedAfter(ctrl *control.Controller, sink *graphSink, prov *scripted, arm string) string {
	stopped := time.Now()
	deadline := stopped.Add(8 * time.Second)
	for time.Now().Before(deadline) && workLive(sink, prov, arm) {
		time.Sleep(100 * time.Millisecond)
	}
	live := "gone"
	if workLive(sink, prov, arm) {
		live = "still running"
	}
	return fmt.Sprintf("turn=%s work=%s after=%s",
		turnStanding(ctrl, arm), live, time.Since(stopped).Round(time.Second))
}

func workLive(sink *graphSink, prov *scripted, arm string) bool {
	if arm == armCancelBackground {
		return prov.fleets.held(childEntered(childHang)) && !prov.fleets.held(childLeft(childHang))
	}
	_, ended := sink.toolResult(sleepCallID)
	return !ended
}

// turnStanding reports the gate only where the gate is the authority. A
// synchronous Run is never admitted through it, so reading it there would say
// "ended" about a turn it was never told about — which is how this probe first
// mistook an ownership boundary for a cancellation defect.
func turnStanding(ctrl *control.Controller, arm string) string {
	if !guardedArm(arm) {
		return "not admitted (caller-owned)"
	}
	if ctrl.Running() {
		return "still active"
	}
	return "ended"
}

// waitForCancelSubject blocks until the work this arm is about to stop is
// really under way: a stop that arrived before it would measure nothing.
func waitForCancelSubject(sink *graphSink, prov *scripted, arm string) error {
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		if arm == armCancelBackground {
			if prov.fleets.held(childEntered(childHang)) {
				return nil
			}
		} else if _, seen := sink.toolDispatched(sleepCallID); seen {
			// The shell starts with the dispatch, not with the event; a moment's
			// grace keeps the stop from racing its own subject.
			time.Sleep(500 * time.Millisecond)
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return errUnexpected("work under way to stop", "nothing started")
}

// cancelRoutingRows read the arm's own readings. The three arms want different
// answers to the same question, which is why they run together: a fix that
// cancels everything a session knows about passes one and fails two.
func cancelRoutingRows(arm string, before Observation) []row {
	readings := before.Progress[probeStopReached]
	first, second := reading(readings, 0), reading(readings, 1)
	switch arm {
	case armCancelHeadlessOwner:
		return []row{
			ownershipRow("a controller stop on a turn it never admitted", first,
				"leaves the caller's work alone", stillRunning(first)),
			ownershipRow("the caller's own stop", second,
				"ends the work it owns", !stillRunning(second)),
		}
	case armCancelBackground:
		return []row{
			ownershipRow("the turn after a stop", first, "a cancelled turn ends",
				strings.Contains(first, "turn=ended")),
			ownershipRow("work already handed to a job", first,
				"a stop aimed at the turn leaves work the turn no longer owns", stillRunning(first)),
		}
	}
	return []row{
		ownershipRow("the turn after a stop", first, "a cancelled turn ends",
			strings.Contains(first, "turn=ended")),
		ownershipRow("the tool the turn was waiting on", first,
			"a stop reaches the work its turn is waiting on", !stillRunning(first)),
	}
}

func ownershipRow(semantic, got, want string, ok bool) row {
	return row{
		Semantic: semantic, Authority: "the surface that owns this cancellation",
		Artifact: "none: in-process state", Reconstruction: want,
		Before: want, After: orNone(got), Verdict: held(ok),
	}
}

func reading(readings []string, at int) string {
	if at < len(readings) {
		return readings[at]
	}
	return ""
}

func stillRunning(reading string) bool { return strings.Contains(reading, "work=still running") }
