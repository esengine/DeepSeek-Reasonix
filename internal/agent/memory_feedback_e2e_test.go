package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/agent/testutil"
	"reasonix/internal/event"
	"reasonix/internal/memorycompiler"
)

// readCompilerStrategies decodes the persisted Memory v5 strategy counters so
// the test can observe learning-side effects without exporting runtime state.
func readCompilerStrategies(t *testing.T, dir string) []memorycompiler.Strategy {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	var st struct {
		Strategies []memorycompiler.Strategy `json:"strategies"`
	}
	if err := json.Unmarshal(b, &st); err != nil {
		t.Fatalf("unmarshal state: %v", err)
	}
	return st.Strategies
}

// requireBugfixCounters asserts the persisted bugfix strategy's success/failure
// counters.
func requireBugfixCounters(t *testing.T, dir, when string, successes, failures int) {
	t.Helper()
	for _, s := range readCompilerStrategies(t, dir) {
		if s.ID != "bugfix-reproduce-first" {
			continue
		}
		if s.Successes != successes || s.Failures != failures {
			t.Fatalf("%s: counters = %d ok / %d fail, want %d ok / %d fail (%+v)",
				when, s.Successes, s.Failures, successes, failures, s)
		}
		return
	}
	t.Fatalf("%s: bugfix strategy missing from state", when)
}

func TestCorrectiveFollowUpRevisesPreviousOutcome(t *testing.T) {
	dir := t.TempDir()
	rt := memorycompiler.New(dir)
	mp := testutil.NewMock("m", testutil.Turn{Text: "done"}, testutil.Turn{Text: "sorry"})
	a := New(mp, echoRegistry(), NewSession(""), Options{MemoryCompiler: rt}, event.Discard)

	if err := a.Run(context.Background(), "fix a bug in the parser"); err != nil {
		t.Fatalf("turn 1: %v", err)
	}
	for _, s := range readCompilerStrategies(t, dir) {
		if s.ID == "bugfix-reproduce-first" && (s.Successes != 1 || s.Failures != 0) {
			t.Fatalf("turn 1 not recorded as success: %+v", s)
		}
	}

	if err := a.Run(context.Background(), "不对，还是报错"); err != nil {
		t.Fatalf("turn 2: %v", err)
	}
	for _, s := range readCompilerStrategies(t, dir) {
		if s.ID != "bugfix-reproduce-first" {
			continue
		}
		if s.Successes != 0 || s.Failures < 1 {
			t.Fatalf("corrective follow-up did not revise turn 1 outcome: %+v", s)
		}
		return
	}
	t.Fatal("bugfix strategy missing from state")
}

func TestNeutralFollowUpDoesNotReviseOutcome(t *testing.T) {
	dir := t.TempDir()
	rt := memorycompiler.New(dir)
	mp := testutil.NewMock("m", testutil.Turn{Text: "done"}, testutil.Turn{Text: "done again"})
	a := New(mp, echoRegistry(), NewSession(""), Options{MemoryCompiler: rt}, event.Discard)

	if err := a.Run(context.Background(), "fix a bug in the parser"); err != nil {
		t.Fatalf("turn 1: %v", err)
	}
	if err := a.Run(context.Background(), "fix another bug in the lexer"); err != nil {
		t.Fatalf("turn 2: %v", err)
	}
	for _, s := range readCompilerStrategies(t, dir) {
		if s.ID != "bugfix-reproduce-first" {
			continue
		}
		if s.Successes != 2 || s.Failures != 0 {
			t.Fatalf("neutral follow-up moved counters: %+v", s)
		}
		return
	}
	t.Fatal("bugfix strategy missing from state")
}

// TestStaleOutcomeRefNotRevisableAfterCompilerToggle covers the reviewer
// scenario: success turn records a ref → /memory-v5 off → intervening turn →
// on again → corrective input. The intervening turn must consume the ref even
// though the runtime was nil, so the old trace is not downgraded.
func TestStaleOutcomeRefNotRevisableAfterCompilerToggle(t *testing.T) {
	dir := t.TempDir()
	rt := memorycompiler.New(dir)
	mp := testutil.NewMock("m",
		testutil.Turn{Text: "done"},
		testutil.Turn{Text: "chat answer"},
		testutil.Turn{Text: "sorry"},
	)
	a := New(mp, echoRegistry(), NewSession(""), Options{MemoryCompiler: rt}, event.Discard)

	if err := a.Run(context.Background(), "fix a bug in the parser"); err != nil {
		t.Fatalf("turn 1: %v", err)
	}
	requireBugfixCounters(t, dir, "after turn 1", 1, 0)

	a.SetMemoryCompiler(nil) // /memory-v5 off
	if err := a.Run(context.Background(), "tell me about the weather"); err != nil {
		t.Fatalf("intervening turn: %v", err)
	}
	a.SetMemoryCompiler(memorycompiler.New(dir)) // /memory-v5 on

	if err := a.Run(context.Background(), "不对，还是报错"); err != nil {
		t.Fatalf("corrective turn: %v", err)
	}
	// The corrective input must not revise turn 1: its ref was consumed by the
	// intervening turn. (The corrective turn itself may add new samples, so
	// only assert turn 1's success survived.)
	for _, s := range readCompilerStrategies(t, dir) {
		if s.ID == "bugfix-reproduce-first" && s.Successes != 1 {
			t.Fatalf("stale ref revised turn 1 after compiler toggle: %+v", s)
		}
	}
}

// TestOutcomeRefClearedOnSessionSwitch: switching sessions breaks the
// "immediately preceding turn" relationship, so the first input of the new
// session must not revise the old session's outcome even without intervening
// turns.
func TestOutcomeRefClearedOnSessionSwitch(t *testing.T) {
	dir := t.TempDir()
	rt := memorycompiler.New(dir)
	mp := testutil.NewMock("m",
		testutil.Turn{Text: "done"},
		testutil.Turn{Text: "sorry"},
	)
	a := New(mp, echoRegistry(), NewSession(""), Options{MemoryCompiler: rt}, event.Discard)

	if err := a.Run(context.Background(), "fix a bug in the parser"); err != nil {
		t.Fatalf("turn 1: %v", err)
	}
	requireBugfixCounters(t, dir, "after turn 1", 1, 0)

	a.SetSession(NewSession("")) // resume/switch to another conversation
	if err := a.Run(context.Background(), "不对，还是报错"); err != nil {
		t.Fatalf("new-session turn: %v", err)
	}
	for _, s := range readCompilerStrategies(t, dir) {
		if s.ID == "bugfix-reproduce-first" && s.Successes != 1 {
			t.Fatalf("new session's first input revised the old session's outcome: %+v", s)
		}
	}
}
