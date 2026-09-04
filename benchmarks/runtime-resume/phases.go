package main

import (
	"context"
	"fmt"

	"reasonix/internal/agent"
)

// probeTurns is enough conversation for a fold to have a region to work on:
// the system row and the first user turn are pinned, and the recent tail is
// kept verbatim, so a short session has nothing foldable between them.
const probeTurns = 8

const probeGoal = "Prove which runtime semantics survive an OS process boundary."

// runConstruct establishes host state through the same calls a frontend makes,
// records what this process can see, and returns so the process can exit. It
// never asserts: an arm that failed to establish something is reported as not
// measured, which is the one reading a missing "before" supports.
func runConstruct(dir, arm string) error {
	root := rootFor(dir)
	if err := root.create(); err != nil {
		return err
	}
	ctx := context.Background()
	sink := &graphSink{}
	ctrl, err := buildRuntime(ctx, root, sink)
	if err != nil {
		return fmt.Errorf("build: %w", err)
	}
	defer ctrl.Close()

	bootSystem := bootSystemText(ctrl)
	ctrl.EnsureSessionPath()
	if ctrl.SessionPath() == "" {
		return errUnexpected("a session path", "")
	}
	for i := range probeTurns {
		if err := ctrl.Run(ctx, fmt.Sprintf("Probe turn %d: record the task list and keep working.", i+1)); err != nil {
			return fmt.Errorf("turn %d: %w", i+1, err)
		}
	}
	if err := ctrl.SetGoalDurable(probeGoal); err != nil {
		return fmt.Errorf("goal: %w", err)
	}
	if err := ctrl.Compact(ctx, "Fold the probe turn."); err != nil {
		return fmt.Errorf("compact: %w", err)
	}
	if appendsAfterFold(arm) {
		if err := ctrl.Run(ctx, "Probe turn after the fold."); err != nil {
			return fmt.Errorf("turn after fold: %w", err)
		}
	}
	return writeObservation(root, capture("construct", arm, bootSystem, ctrl, sink))
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
	ctrl, err := buildRuntime(ctx, root, sink)
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
	return writeObservation(root, capture("resume", arm, bootSystem, ctrl, sink))
}
