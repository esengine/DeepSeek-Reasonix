package memorycompiler

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOutcomeForAbortedOnCancel(t *testing.T) {
	if got := outcomeFor(nil, context.Canceled); got != "aborted" {
		t.Fatalf("outcomeFor(canceled) = %q, want aborted", got)
	}
	wrapped := fmt.Errorf("run turn: %w", context.Canceled)
	if got := outcomeFor(nil, wrapped); got != "aborted" {
		t.Fatalf("outcomeFor(wrapped cancel) = %q, want aborted", got)
	}
	if got := outcomeFor(nil, fmt.Errorf("boom")); got != "failure" {
		t.Fatalf("outcomeFor(real error) = %q, want failure", got)
	}
}

func TestOutcomeForVerificationSignals(t *testing.T) {
	pass := ToolRecord{Name: "bash", Args: `{"command":"go test ./internal/foo"}`}
	fail := ToolRecord{Name: "bash", Args: `{"command":"go test ./internal/foo"}`, Error: "exit status 1"}
	plainErr := ToolRecord{Name: "bash", Args: `{"command":"git push"}`, Error: "denied"}
	plainOK := ToolRecord{Name: "read_file", Args: `{"path":"a.go"}`}

	// A passing final verification confirms success even after earlier errors.
	if got := outcomeFor([]ToolRecord{plainErr, pass}, nil); got != "success" {
		t.Fatalf("passing verification after earlier error = %q, want success", got)
	}
	// A failing verification caps the turn at partial_success even when a
	// later ordinary tool succeeded.
	if got := outcomeFor([]ToolRecord{fail, plainOK}, nil); got != "partial_success" {
		t.Fatalf("failing verification = %q, want partial_success", got)
	}
	// A passing verification followed by a later tool error falls back to the
	// ordinary last-tool heuristic.
	if got := outcomeFor([]ToolRecord{pass, plainErr}, nil); got != "partial_success" {
		t.Fatalf("verification then error = %q, want partial_success", got)
	}
	// Non-verification commands keep the old behavior.
	if got := outcomeFor([]ToolRecord{plainOK}, nil); got != "success" {
		t.Fatalf("plain success = %q, want success", got)
	}
}

func TestUpdateStrategyAbortedAndInjectedCounters(t *testing.T) {
	st := updateStrategy(nil, "general", "aborted", false)
	for _, s := range st {
		if s.Samples() != 0 {
			t.Fatalf("aborted outcome moved counters for %q: %+v", s.ID, s)
		}
	}
	st = updateStrategy(st, "general", "success", true)
	st = updateStrategy(st, "general", "failure", false)
	for _, s := range st {
		if s.ID != "general" {
			continue
		}
		if s.Successes != 1 || s.Failures != 1 || s.InjectedSuccesses != 1 || s.InjectedFailures != 0 {
			t.Fatalf("counters = %+v, want 1 success (injected), 1 failure (observed)", s)
		}
		return
	}
	t.Fatal("general strategy missing")
}

func TestAbortedTurnDoesNotMoveStrategyCounters(t *testing.T) {
	dir := t.TempDir()
	rt := New(dir)
	_, turn := rt.StartTurn(context.Background(), "fix a bug", nil)
	turn.Finish(context.Canceled)

	st := readState(t, dir)
	for _, s := range st.Strategies {
		if s.Samples() != 0 {
			t.Fatalf("aborted turn moved counters for %q: %+v", s.ID, s)
		}
	}
}

func TestIsCorrectiveFeedback(t *testing.T) {
	positives := []string{
		"不对，还是报错",
		"没修好，重新看",
		"still broken after your fix",
		"这个改动没有生效",
		"didn't fix the issue",
	}
	for _, in := range positives {
		if !IsCorrectiveFeedback(in) {
			t.Fatalf("IsCorrectiveFeedback(%q) = false, want true", in)
		}
	}
	negatives := []string{
		"",
		"这个 API 不对外开放，请实现内部版本",
		"两个面板不对称，请调整布局", // exclusion compound, not corrective
		"修复登录页的 bug",
		"thanks, looks good",
		strings.Repeat("背景说明。", 40) + "上次不对的地方已经确认了", // corrective phrase beyond the head window
	}
	for _, in := range negatives {
		if IsCorrectiveFeedback(in) {
			t.Fatalf("IsCorrectiveFeedback(%q) = true, want false", in)
		}
	}
}

func TestReviseOutcomeFromFeedback(t *testing.T) {
	dir := t.TempDir()
	rt := New(dir)
	_, turn := rt.StartTurn(context.Background(), "fix a bug", nil)
	turn.Finish(nil) // no tools, no error → success
	ref := turn.OutcomeRef()
	if ref.Outcome != "success" || ref.Strategy != "bugfix-reproduce-first" {
		t.Fatalf("unexpected ref: %+v", ref)
	}

	if !rt.ReviseOutcomeFromFeedback(ref, "不对，还是报错") {
		t.Fatal("revision was not applied")
	}

	st := readState(t, dir)
	for _, s := range st.Strategies {
		if s.ID != ref.Strategy {
			continue
		}
		if s.Successes != 0 || s.Failures != 1 {
			t.Fatalf("counters not flipped: %+v", s)
		}
	}
	found := false
	for _, l := range st.Learnings {
		if l.TraceID == ref.TraceID+":revision" && len(l.BadStrategies) > 0 {
			found = true
		}
	}
	if !found {
		t.Fatal("revision learning missing")
	}

	b, err := os.ReadFile(filepath.Join(dir, tracesFile))
	if err != nil {
		t.Fatalf("read traces: %v", err)
	}
	revised := false
	for _, ln := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		var tr ExecutionTrace
		if json.Unmarshal([]byte(ln), &tr) != nil || tr.ID != ref.TraceID {
			continue
		}
		revised = tr.Outcome == "failure" && strings.Contains(tr.FailureReason, "user feedback contradicted")
	}
	if !revised {
		t.Fatalf("trace outcome not rewritten:\n%s", b)
	}

	// A ref whose trace is not in this runtime's log must be a no-op.
	stale := ref
	stale.TraceID = "trace-not-there"
	if rt.ReviseOutcomeFromFeedback(stale, "还是不行") {
		t.Fatal("stale ref should not revise anything")
	}
}

func TestInjectedPersistedInTraceAndStrategy(t *testing.T) {
	dir := t.TempDir()
	rt := New(dir)
	// Seed learned state so the next turn compiles a useful contract.
	_, seed := rt.StartTurn(context.Background(), "fix a bug", nil)
	seed.RecordToolResults([]ToolRecord{
		{Name: "bash", Error: "exit status 1"},
		{Name: "bash", Error: "exit status 1"},
	})
	seed.Finish(nil)

	compiled, turn := rt.StartTurn(context.Background(), "fix the bug again", nil)
	if compiled == "" {
		t.Fatal("expected a compiled contract from learned state")
	}
	turn.Finish(nil) // injection not suppressed → Injected persists

	b, err := os.ReadFile(filepath.Join(dir, tracesFile))
	if err != nil {
		t.Fatalf("read traces: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	var last ExecutionTrace
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &last); err != nil {
		t.Fatalf("unmarshal last trace: %v", err)
	}
	if !last.Injected {
		t.Fatalf("last trace not marked injected: %+v", last)
	}

	st := readState(t, dir)
	for _, s := range st.Strategies {
		if s.ID != "bugfix-reproduce-first" {
			continue
		}
		if s.InjectedSuccesses != 1 {
			t.Fatalf("injected successes = %d, want 1 (%+v)", s.InjectedSuccesses, s)
		}
		return
	}
	t.Fatal("bugfix strategy missing")
}

func TestSuppressedInjectionNotPersistedAsInjected(t *testing.T) {
	dir := t.TempDir()
	rt := New(dir)
	_, seed := rt.StartTurn(context.Background(), "fix a bug", nil)
	seed.RecordToolResults([]ToolRecord{
		{Name: "bash", Error: "exit status 1"},
		{Name: "bash", Error: "exit status 1"},
	})
	seed.Finish(nil)

	compiled, turn := rt.StartTurn(context.Background(), "fix the bug again", nil)
	if compiled == "" {
		t.Fatal("expected a compiled contract from learned state")
	}
	turn.SuppressInjection() // observe mode
	turn.Finish(nil)

	st := readState(t, dir)
	for _, s := range st.Strategies {
		if s.InjectedSuccesses != 0 && s.ID == "bugfix-reproduce-first" {
			t.Fatalf("observe-mode turn counted as injected: %+v", s)
		}
	}
}
