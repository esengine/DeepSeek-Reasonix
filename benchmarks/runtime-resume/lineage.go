package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"reasonix/internal/control"
)

// Two trees, read at once. A stop aimed at a turn must close the turn's own
// tree and leave the job's alone; an explicit job stop does the reverse, and
// reading only one of them is how a probe calls a violated handoff correct. The
// manager's own root needs no separate question: a job still running with its
// child's context open is the root answering directly.
const (
	// armLineageForeground is the positive control: it proves the reading can
	// see an ordinary cancellation arrive.
	armLineageForeground = "lineage-foreground-cancel"
	// armLineageBackground is the one under suspicion.
	armLineageBackground = "lineage-background-cancel"
	// armLineageJobKill is the other positive control: without it, a reading
	// that could never observe a job's own cancellation would make the arm
	// above pass for the wrong reason.
	armLineageJobKill = "lineage-job-kill"
)

func lineageArm(name string) bool {
	switch name {
	case armLineageForeground, armLineageBackground, armLineageJobKill:
		return true
	}
	return false
}

// runLineageConstruct drives one delegation, stops it the way this arm's owner
// stops it, and reads both trees afterwards. It records six facts, kept apart
// on purpose: a context that closed and a call that returned are different
// events, and only their combination says whether a cancellation propagated or
// something else ended the work.
func runLineageConstruct(ctx context.Context, root armRoot, arm, bootSystem string, ctrl *control.Controller, sink *graphSink, prov *scripted, turn int) error {
	ctrl.Send(fanOutTurn(turn+1, taskSentinel))
	if err := waitFor(60*time.Second, func() bool { return prov.fleets.held(childEntered(childHang)) }); err != nil {
		return errUnexpected("a delegated child inside the provider", "none arrived")
	}
	before := lineageReading(ctrl, prov, "before")

	switch arm {
	case armLineageJobKill:
		jobs := ctrl.Jobs()
		if len(jobs) == 0 {
			return errUnexpected("a background job to stop", "none running")
		}
		for _, job := range jobs {
			ctrl.CancelJob(job.ID)
		}
	default:
		ctrl.Cancel()
	}
	// Long enough for a cancellation that was going to arrive to have arrived.
	time.Sleep(3 * time.Second)
	after := lineageReading(ctrl, prov, "after")

	obs := capture("construct", arm, bootSystem, ctrl, sink, root)
	obs.Progress[probeLineage] = []string{before, after}
	if err := writeObservation(root, obs); err != nil {
		return err
	}
	os.Exit(0)
	return nil
}

const probeLineage = "probe:lineage"

// lineageReading is the state of both trees at one instant, in the terms the
// rows judge: the turn's gate, the job's own liveness, the child's context, the
// child's call, and the durable record the orchestration keeps.
func lineageReading(ctrl *control.Controller, prov *scripted, at string) string {
	return fmt.Sprintf("%s turn=%s job=%s childCtx=%s childCall=%s record=%s",
		at, turnGateStanding(ctrl), jobStanding(ctrl),
		yesNo(prov.fleets.held(childCtxDone(childHang)), "cancelled", "open"),
		yesNo(prov.fleets.held(childLeft(childHang)), "returned", "inside"),
		loneRecordStanding(ctrl))
}

func turnGateStanding(ctrl *control.Controller) string {
	if ctrl.Running() {
		return "active"
	}
	return "ended"
}

func jobStanding(ctrl *control.Controller) string {
	if len(ctrl.Jobs()) > 0 {
		return "running"
	}
	return "gone"
}

func loneRecordStanding(ctrl *control.Controller) string {
	for _, e := range control.ExecutionHistory(ctrl.SessionPath()) {
		if e.ID == parallelLoneNodeID {
			return lifecycleOf(e)[1:]
		}
	}
	return "absent"
}

func yesNo(ok bool, yes, no string) string {
	if ok {
		return yes
	}
	return no
}

func waitFor(within time.Duration, ready func() bool) error {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if ready() {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("timed out")
}

// lineageRows read the two trees against the ownership each arm is about. The
// three sets of expectations are deliberately different: a fix that closes
// everything on a stop satisfies the foreground arm and violates both others.
func lineageRows(arm string, before Observation) []row {
	at := reading(before.Progress[probeLineage], 1)
	turnEnded := strings.Contains(at, "turn=ended")
	jobLive := strings.Contains(at, "job=running")
	childCancelled := strings.Contains(at, "childCtx=cancelled")
	recordOpen := !strings.Contains(at, "settled") && !strings.Contains(at, "record=absent")

	rows := []row{lineageRow("the turn the stop was aimed at", at, "ends", turnEnded)}
	if arm == armLineageForeground {
		return append(rows,
			lineageRow("the child's own context", at, "closes with the turn that owned it", childCancelled),
			lineageRow("the delegation record", at, "is let go of", !recordOpen))
	}
	if arm == armLineageJobKill {
		return append(rows[:0],
			lineageRow("the job the stop was aimed at", at, "ends", !jobLive),
			lineageRow("the child's own context", at, "closes with the job that owned it", childCancelled),
			lineageRow("the delegation record", at, "is let go of", !recordOpen))
	}
	return append(rows,
		lineageRow("the job the turn no longer owns", at, "keeps running", jobLive),
		lineageRow("the child's own context", at, "is not closed by another owner's stop", !childCancelled),
		lineageRow("the delegation record", at, "stays open while its child runs", recordOpen))
}

func lineageRow(semantic, got, want string, ok bool) row {
	return row{
		Semantic: semantic, Authority: "both trees, read at the same instant",
		Artifact: "none: in-process state", Reconstruction: want,
		Before: want, After: orNone(got), Verdict: held(ok),
	}
}
