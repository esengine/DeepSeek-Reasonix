package agent

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"reasonix/internal/testenv"
	"reasonix/internal/tool"
)

func cancelTestRun(t *testing.T) (*SubagentStore, *SubagentRun) {
	t.Helper()
	store := NewSubagentStore(testenv.TempDir(t))
	run, err := store.PrepareFresh(SubagentSpec{
		Kind: "task", Name: "task", WorkspaceRoot: testenv.TempDir(t),
		ParentSession: "parent-session", SystemPrompt: "sys", Registry: tool.NewRegistry(),
	})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	t.Cleanup(run.Release)
	return store, run
}

// TestRunTerminalClassifiesWhatHappened holds the store to the distinction the
// run graph already draws. A cancelled run is not a failed one — nothing about
// the work went wrong — and a store that folds them reports work the caller
// stopped as work that broke.
func TestRunTerminalClassifiesWhatHappened(t *testing.T) {
	for name, tc := range map[string]struct {
		runErr     error
		wantStatus SubagentStatus
		wantReason string
	}{
		"succeeded":          {nil, SubagentCompleted, ""},
		"cancelled":          {context.Canceled, SubagentCancelled, TerminalCancelled},
		"deadline expired":   {context.DeadlineExceeded, SubagentCancelled, TerminalDeadline},
		"wrapped cancel":     {fmt.Errorf("child run: %w", context.Canceled), SubagentCancelled, TerminalCancelled},
		"ordinary failure":   {errors.New("the tool refused"), SubagentFailed, ""},
		"failure mentioning": {errors.New("context canceled by the remote"), SubagentFailed, ""},
	} {
		t.Run(name, func(t *testing.T) {
			store, run := cancelTestRun(t)
			task := &TaskTool{transcripts: store}
			var err error
			if tc.runErr == nil {
				err = store.SaveCompleted(run)
			} else {
				err = task.saveRunTerminal(run, tc.runErr)
			}
			if err != nil {
				t.Fatalf("save: %v", err)
			}
			meta, err := store.LoadMeta(run.Ref)
			if err != nil {
				t.Fatalf("load meta: %v", err)
			}
			if meta.Status != tc.wantStatus || meta.TerminalReason != tc.wantReason {
				t.Fatalf("status=%q reason=%q, want %q/%q",
					meta.Status, meta.TerminalReason, tc.wantStatus, tc.wantReason)
			}
		})
	}
}

// TestSaveFailedCannotSeeACancellation is the negative control, and it holds by
// shape rather than judgement: the infrastructure paths — a failed writer
// registration, a panic, a refused slot, an unsaveable transcript — call
// SaveFailed, which takes no error and so has nothing to classify. A host
// failure whose chain carries context.Canceled cannot be filed as a stop.
func TestSaveFailedCannotSeeACancellation(t *testing.T) {
	store, run := cancelTestRun(t)
	if err := store.SaveFailed(run); err != nil {
		t.Fatalf("save failed: %v", err)
	}
	meta, err := store.LoadMeta(run.Ref)
	if err != nil {
		t.Fatalf("load meta: %v", err)
	}
	if meta.Status != SubagentFailed || meta.TerminalReason != "" {
		t.Fatalf("status=%q reason=%q, want failed with no reason", meta.Status, meta.TerminalReason)
	}
}

// TestCancelledRunKeepsItsTranscript: a run cut short is as worth reading as
// one that failed, and a store that dropped the transcript would leave the
// caller with a status and nothing behind it.
func TestCancelledRunKeepsItsTranscript(t *testing.T) {
	store, run := cancelTestRun(t)
	task := &TaskTool{transcripts: store}
	if err := task.saveRunTerminal(run, context.Canceled); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := LoadSession(store.sessionPath(run.Ref)); err != nil {
		t.Fatalf("cancelled run left no readable transcript: %v", err)
	}
}
