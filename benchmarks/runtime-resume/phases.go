package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/checkpoint"
	"reasonix/internal/control"
)

// probeTurns is enough conversation for a fold to have a region to work on:
// the system row and the first user turn are pinned, and the recent tail is
// kept verbatim, so a short session has nothing foldable between them.
const probeTurns = 8

// refoldTurns grows the transcript past the first fold by less than the fold
// keeps verbatim, so the second fold has to reach into the stored body.
const refoldTurns = 3

// runTurns drives count turns, each carrying its own marker, and returns the
// next turn number.
func runTurns(ctx context.Context, ctrl *control.Controller, from, count int) (int, error) {
	for i := range count {
		n := from + i + 1
		prompt := fmt.Sprintf("%s Probe turn %d: record the task list and keep working.", marker(n), n)
		if err := ctrl.Run(ctx, prompt); err != nil {
			return n, fmt.Errorf("turn %d: %w", n, err)
		}
	}
	return from + count, nil
}

// openDecisionAndDie leaves a question open and kills the process while the
// turn is still parked on it. It exits rather than returning: a clean shutdown
// would resolve or cancel the question, which is the one thing a process that
// dies mid-decision does not do.
func openDecisionAndDie(ctx context.Context, root armRoot, arm, bootSystem string, ctrl *control.Controller, sink *graphSink) error {
	// The gate a person answers through, wired the way chat and desktop wire
	// it. Without it the ask tool decides for itself and nothing ever blocks.
	ctrl.EnableInteractiveApproval()
	go func() { _ = ctrl.Run(ctx, marker(99)+" "+askSentinel+": ask before writing.") }()
	if err := waitForAsk(ctrl); err != nil {
		return err
	}
	obs := capture("construct", arm, bootSystem, ctrl, sink, root)
	if obs.Deferred.Executed {
		return errUnexpected("a write still held back by the open question", obs.Deferred.MarkerPath)
	}
	if err := writeObservation(root, obs); err != nil {
		return err
	}
	os.Exit(0)
	return nil
}

// waitForAsk blocks until the host reports a question waiting on a person.
func waitForAsk(ctrl *control.Controller) error {
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		for _, d := range ctrl.Decisions() {
			if d.Kind == control.DecisionAsk {
				return nil
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	return errUnexpected("a question waiting on a person", ctrl.Decisions())
}

// rewindTurn drives the rewind a person drives, recording the projection
// before it: the tail arm takes back the turn appended after the fold, the
// covered arm lands below the fold boundary and must lose the projection
// whatever coverage says.
func rewindTurn(root armRoot, arm, bootSystem string, ctrl *control.Controller, sink *graphSink) error {
	points := ctrl.Checkpoints()
	if len(points) == 0 {
		return errUnexpected("a checkpoint to rewind to", 0)
	}
	target := points[len(points)-1].Turn
	if arm == armCoveredRewind {
		var err error
		if target, err = turnBelowFold(ctrl, points); err != nil {
			return err
		}
	}
	if err := writeObservation(root, capture(extraPhase(arm), arm, bootSystem, ctrl, sink, root)); err != nil {
		return err
	}
	if err := ctrl.Rewind(target, control.RewindConversation); err != nil {
		return fmt.Errorf("rewind: %w", err)
	}
	return nil
}

// turnBelowFold picks the checkpoint whose boundary sits just below the fold,
// read from the sidecar rather than assumed: a control aiming at a fixed turn
// stops removing folded history the moment coverage moves, and says nothing.
// A boundary at the first message is skipped — a session left holding only a
// system row is never persisted, so the restart would read the arm's work back.
func turnBelowFold(ctrl *control.Controller, points []checkpoint.Meta) (int, error) {
	covered := 0
	if st, ok, _ := agent.LoadCompactionState(ctrl.SessionPath()); ok {
		covered = st.Projection.CoveredCount
	}
	target := -1
	for _, p := range points {
		plan, err := ctrl.PrepareRewind(p.Turn, control.RewindConversation)
		if err != nil || !plan.HasBoundary {
			continue
		}
		if plan.BoundaryIndex > 1 && plan.BoundaryIndex < covered {
			target = p.Turn
		}
	}
	if target < 0 {
		return 0, errUnexpected("a checkpoint below the fold boundary", covered)
	}
	return target, nil
}

// extraPhase names the in-process observation an arm records before its
// process exits, empty when it records none.
func extraPhase(arm string) string {
	switch arm {
	case armRefoldIntoBody:
		return "prefold"
	case armTailRewind, armCoveredRewind:
		return "prerewind"
	}
	return ""
}

const probeGoal = "Prove which runtime semantics survive an OS process boundary."

// armConstruct hands the arm to the construct path it needs, reporting whether
// one took over. Each of them records its own observation and exits inside the
// turn, so a caller that sees taken has nothing left to do but report the error.
func armConstruct(ctx context.Context, root armRoot, arm, bootSystem string, ctrl *control.Controller, sink *graphSink, prov *scripted, turn int) (bool, error) {
	switch {
	case arm == armOpenDecision || successorArm(arm):
		return true, openDecisionAndDie(ctx, root, arm, bootSystem, ctrl, sink)
	case deriveArm(arm):
		return true, runDeriveConstruct(ctx, root, arm, bootSystem, ctrl, sink, prov, turn)
	case terminalArm(arm):
		return true, runTerminalConstruct(ctx, root, arm, bootSystem, ctrl, sink, turn)
	case schedulerWaitArm(arm):
		return true, runSchedulerWaitConstruct(ctx, root, arm, bootSystem, ctrl, sink, prov, turn)
	case cancelRoutingArm(arm):
		return true, runCancelRoutingConstruct(ctx, root, arm, bootSystem, ctrl, sink, prov, turn)
	case loneTaskArm(arm):
		return true, runLoneTaskConstruct(ctx, root, arm, bootSystem, ctrl, sink, prov, turn)
	case graphArm(arm) || uiArm(arm):
		return true, runFanOutConstruct(ctx, root, arm, bootSystem, ctrl, sink, turn)
	}
	return false, nil
}

// runConstruct establishes host state through the same calls a frontend makes,
// records what this process can see, and returns so the process can exit. It
// never asserts: an arm that failed to establish something is reported as not
// measured, which is the one reading a missing "before" supports.
func runConstruct(dir, arm string) error {
	root := rootFor(dir)
	if err := root.create(); err != nil {
		return err
	}
	if total, writers := schedulerLimits(arm); total > 0 {
		if err := root.writeProjectConfig(total, writers); err != nil {
			return fmt.Errorf("project config: %w", err)
		}
	}
	ctx := context.Background()
	sink := &graphSink{}
	bus, into := uiBus(arm, sink)
	ctrl, prov, err := buildRuntime(ctx, root, arm, into)
	if err != nil {
		return fmt.Errorf("build: %w", err)
	}
	defer ctrl.Close()
	// The process that dies is a window in production, and a window records what
	// it served. Without that the trajectory this session leaves behind holds
	// none of its own frames.
	recordLikeAWindow(ctrl, bus)

	bootSystem := bootSystemText(ctrl)
	ctrl.EnsureSessionPath()
	if ctrl.SessionPath() == "" {
		return errUnexpected("a session path", "")
	}
	turn, err := runTurns(ctx, ctrl, 0, probeTurns)
	if err != nil {
		return err
	}
	if err := ctrl.SetGoalDurable(probeGoal); err != nil {
		return fmt.Errorf("goal: %w", err)
	}
	if err := ctrl.Compact(ctx, "Fold the probe turn."); err != nil {
		return fmt.Errorf("compact: %w", err)
	}
	if taken, err := armConstruct(ctx, root, arm, bootSystem, ctrl, sink, prov, turn); taken {
		return err
	}
	if arm == armTodoIdentity {
		// Identity A is already written and folded away. Grow, replace the list
		// with identity B, then fold again: the second fold is what decides
		// whether the model is left holding one current identity or two.
		if turn, err = runTurns(ctx, ctrl, turn, refoldTurns); err != nil {
			return err
		}
		if err := ctrl.Run(ctx, fmt.Sprintf("%s %s: replace the task list.", marker(turn+1), retodoSentinel)); err != nil {
			return fmt.Errorf("retodo turn: %w", err)
		}
		// Grow well past the rewrite so the fold takes the new ids out of view.
		// A scenario that leaves them readable asks nothing of the note.
		if _, err = runTurns(ctx, ctrl, turn+1, probeTurns); err != nil {
			return err
		}
		if err := ctrl.Compact(ctx, "Fold again, after the identity moved."); err != nil {
			return fmt.Errorf("refold: %w", err)
		}
	} else if arm == armRefoldIntoBody {
		// Grow past the fold, record the view a second fold will operate on,
		// then fold again: its region reaches back into the stored body, which
		// has no canonical counterpart to map a new boundary onto.
		if _, err = runTurns(ctx, ctrl, turn, refoldTurns); err != nil {
			return err
		}
		if err := writeObservation(root, capture(extraPhase(arm), arm, bootSystem, ctrl, sink, root)); err != nil {
			return err
		}
		if err := ctrl.Compact(ctx, "Fold again, into the stored body."); err != nil {
			return fmt.Errorf("refold: %w", err)
		}
	} else if appendsAfterFold(arm) {
		if _, err = runTurns(ctx, ctrl, turn, 1); err != nil {
			return err
		}
	}
	if arm == armTailRewind || arm == armCoveredRewind {
		if err := rewindTurn(root, arm, bootSystem, ctrl, sink); err != nil {
			return err
		}
	}
	return writeObservation(root, capture("construct", arm, bootSystem, ctrl, sink, root))
}

// successorArm reports whether this arm starts from an interrupted barrier.
func successorArm(arm string) bool {
	_, ok := successorTurn(arm)
	return ok
}

// runSuccessor is the middle process: it inherits the interruption and does
// what a person would do next — nothing, unrelated work, or something that
// reads like an answer to the dead question. It runs a model on purpose; the
// phase that judges what the host knows still does not.
func runSuccessor(dir, arm string) error {
	root := rootFor(dir)
	before, err := readObservation(root, "construct")
	if err != nil {
		return err
	}
	ctx := context.Background()
	sink := &graphSink{}
	ctrl, _, err := buildRuntime(ctx, root, arm, sink)
	if err != nil {
		return fmt.Errorf("build: %w", err)
	}
	defer ctrl.Close()
	bootSystem := bootSystemText(ctrl)
	session, err := agent.LoadSession(before.SessionPath)
	if err != nil {
		return fmt.Errorf("load session: %w", err)
	}
	if err := ctrl.Resume(session, before.SessionPath); err != nil {
		return fmt.Errorf("resume: %w", err)
	}
	if turn, _ := successorTurn(arm); turn != "" {
		if err := ctrl.Run(ctx, turn); err != nil {
			return fmt.Errorf("successor turn: %w", err)
		}
	}
	return writeObservation(root, capture("successor", arm, bootSystem, ctrl, sink, root))
}

// runResume boots a second time against the same roots and reads host state.
// It deliberately never calls Run: the question is what the host can prove
// before a provider call, and a model that reconstructs a lost identity from
// the transcript would turn a real loss into a passing row.
func runResume(dir, arm string) error {
	root := rootFor(dir)
	before, err := readObservation(root, "construct")
	if err != nil {
		return fmt.Errorf("read construct observation: %w", err)
	}
	ctx := context.Background()
	sink := &graphSink{}
	bus, into := uiBus(arm, sink)
	ctrl, _, err := buildRuntime(ctx, root, arm, into)
	if err != nil {
		return fmt.Errorf("build: %w", err)
	}
	defer ctrl.Close()

	bootSystem := bootSystemText(ctrl)
	session, err := agent.LoadSession(before.SessionPath)
	if err != nil {
		return fmt.Errorf("load session: %w", err)
	}
	if err := ctrl.Resume(session, before.SessionPath); err != nil {
		return fmt.Errorf("resume: %w", err)
	}
	obs := capture("resume", arm, bootSystem, ctrl, sink, root)
	if uiArm(arm) {
		ui := observeUI(root, ctrl, bus, obs)
		obs.UI = &ui
	}
	return writeObservation(root, obs)
}
