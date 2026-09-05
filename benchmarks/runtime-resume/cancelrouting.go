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

// Where a stop goes. The arms above measure who owns an ending; these ask
// something earlier: when a person cancels a turn, does the cancellation reach
// the work that turn owns? A turn that returns while its own work still runs
// has not cancelled it — it has stopped waiting, which looks the same until a
// restart reads the record.
const (
	// armCancelTool is the control that says where a defect lives. If an
	// ordinary tool's context is cancelled and a delegation's is not, the fault
	// is in how a delegation derives its context; if neither is, it is upstream
	// of both.
	armCancelTool = "cancel-ordinary-tool"
	// armCancelBackground is the negative control, and the one that decides what
	// a fix may do: work already handed to a job has an owner of its own, and a
	// cancel that reached into it would take back the handoff this step built.
	armCancelBackground = "cancel-background-handoff"
)

func cancelRoutingArm(name string) bool {
	return name == armCancelTool || name == armCancelBackground
}

const (
	sleepSentinel = "PROBE-SLEEP"
	sleepCallID   = "probe_sleep"
	// sleepSeconds outlasts any settling this probe waits through, so a shell
	// that ends promptly ended because something ended it.
	sleepSeconds = 30
)

// sleepCall is an ordinary foreground tool that does nothing but wait. It is
// the same shell every turn can call, which is the point: it derives its
// context the way every other tool does.
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

// runCancelRoutingConstruct drives the arm's work, stops the turn the way a
// person does, and records what the stop reached. Nothing waits for the work to
// end: whether it ends at all is the measurement.
func runCancelRoutingConstruct(ctx context.Context, root armRoot, arm, bootSystem string, ctrl *control.Controller, sink *graphSink, prov *scripted, turn int) error {
	sentinel := sleepSentinel
	if arm == armCancelBackground {
		sentinel = taskSentinel
	}
	go func() { _ = ctrl.Run(ctx, fanOutTurn(turn+1, sentinel)) }()
	if err := waitForCancelSubject(sink, prov, arm); err != nil {
		return err
	}

	stopped := time.Now()
	ctrl.Cancel()
	waitForTurnToEnd(ctrl)
	// Long enough that a context which was going to arrive has, and short
	// enough that the shell's own sleep cannot be what ended the wait.
	time.Sleep(3 * time.Second)

	obs := capture("construct", arm, bootSystem, ctrl, sink, root)
	obs.Progress[probeStopReached] = []string{stopReading(ctrl, prov, sink, arm, stopped)}
	if err := writeObservation(root, obs); err != nil {
		return err
	}
	os.Exit(0)
	return nil
}

// probeStopReached carries what the stop reached into the observation. The key
// names the probe so no row reads it as a status the kernel emitted.
const probeStopReached = "probe:stop-reached"

// stopReading is what the arm saw, in the terms its row is judged on: whether
// the turn ended, and whether the work it owned is still live.
func stopReading(ctrl *control.Controller, prov *scripted, sink *graphSink, arm string, stopped time.Time) string {
	live := "gone"
	if arm == armCancelTool {
		if _, ended := sink.toolResult(sleepCallID); !ended {
			live = "still running"
		}
	} else if prov.fleets.held(childEntered(childHang)) && !prov.fleets.held(childLeft(childHang)) {
		live = "still running"
	}
	return fmt.Sprintf("turn=%s work=%s after=%s",
		turnStanding(ctrl), live, time.Since(stopped).Round(time.Second))
}

func turnStanding(ctrl *control.Controller) string {
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
		if arm == armCancelTool {
			if _, seen := sink.toolDispatched(sleepCallID); seen {
				// The shell is started by the dispatch, not by the event; a
				// moment's grace keeps the stop from racing its own subject.
				time.Sleep(500 * time.Millisecond)
				return nil
			}
		} else if prov.fleets.held(childEntered(childHang)) {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return errUnexpected("work under way to stop", "nothing started")
}

// waitForTurnToEnd gives the cancelled turn its own time to unwind. What the
// arm reports is read after this: a turn still active would say the stop is
// still in flight, not that it failed to reach anything.
func waitForTurnToEnd(ctrl *control.Controller) {
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if !ctrl.Running() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// cancelRoutingRows read one reading two ways: whether the turn ended, and
// whether what it was waiting on ended with it. The tool arm and the background
// arm want opposite answers to the second, which is the whole point of running
// both — a fix that cancels everything a session knows about passes one and
// fails the other.
func cancelRoutingRows(arm string, before Observation) []row {
	reading := ""
	if seen := before.Progress[probeStopReached]; len(seen) > 0 {
		reading = seen[0]
	}
	ended := strings.Contains(reading, "turn=ended")
	live := strings.Contains(reading, "work=still running")
	rows := []row{{
		Semantic: "the turn after a stop", Authority: "control.Controller",
		Artifact: "none: in-process state", Reconstruction: "a cancelled turn ends",
		Before: "active, with work under way", After: orNone(reading), Verdict: held(ended),
	}}
	if arm == armCancelBackground {
		return append(rows, row{
			Semantic: "work already handed to a job", Authority: "jobs.Manager, the owner after the handoff",
			Artifact:       "none: in-process state",
			Reconstruction: "a stop aimed at the turn does not reach work the turn no longer owns",
			Before:         "running under its own owner", After: orNone(reading), Verdict: held(live),
		})
	}
	return append(rows, row{
		Semantic: "the tool the turn was waiting on", Authority: "the tool's own context",
		Artifact:       "none: in-process state",
		Reconstruction: "a stop reaches the work its turn is waiting on",
		Before:         "running under the turn", After: orNone(reading), Verdict: held(!live),
	})
}
