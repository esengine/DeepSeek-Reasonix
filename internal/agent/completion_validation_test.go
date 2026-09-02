package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/completioneval"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// scriptedEvaluator pops one verdict (or error) per Evaluate call and records
// the evidence it received.
type scriptedEvaluator struct {
	mu       sync.Mutex
	verdicts []completioneval.Verdict
	errs     []error
	calls    int
	evidence []completioneval.Evidence
}

func (e *scriptedEvaluator) Evaluate(_ context.Context, evidence completioneval.Evidence) (completioneval.Verdict, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	i := e.calls
	e.calls++
	e.evidence = append(e.evidence, evidence)
	if i < len(e.errs) && e.errs[i] != nil {
		return completioneval.Verdict{}, e.errs[i]
	}
	if i < len(e.verdicts) {
		return e.verdicts[i], nil
	}
	return completioneval.Verdict{Outcome: completioneval.OutcomeComplete}, nil
}

type recordingSink struct {
	mu          sync.Mutex
	events      []event.Event
	validations []event.CompletionValidationInfo
}

func (s *recordingSink) RecordCompletionValidation(info event.CompletionValidationInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.validations = append(s.validations, info)
}

func (s *recordingSink) Emit(e event.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, e)
}

func (s *recordingSink) completionValidations() []event.CompletionValidationInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]event.CompletionValidationInfo(nil), s.validations...)
}

func textTurn(text string) []provider.Chunk {
	return []provider.Chunk{{Type: provider.ChunkText, Text: text}, {Type: provider.ChunkDone}}
}

// runWithValidator is the shared harness: an agent whose candidate finals are
// judged by the scripted evaluator under the given mode.
func runWithValidator(t *testing.T, mode string, evaluator completioneval.Evaluator, turns [][]provider.Chunk, sink event.Sink) (*Agent, error) {
	t.Helper()
	prov := &scriptedProvider{name: "p", turns: turns}
	a := New(prov, tool.NewRegistry(), NewSession("sys"), Options{
		CompletionValidation: mode,
	}, sink)
	if evaluator != nil {
		a.completionEvaluator = evaluator
	}
	return a, a.Run(context.Background(), "summarize the repository layout")
}

func TestCompletionValidatorContinueThenCompleteResumesOnce(t *testing.T) {
	eval := &scriptedEvaluator{verdicts: []completioneval.Verdict{
		{Outcome: completioneval.OutcomeContinue, Reason: "only a progress note"},
		{Outcome: completioneval.OutcomeComplete, Reason: ""},
	}}
	provTurns := [][]provider.Chunk{
		textTurn("I will now start looking at the repository."),
		textTurn("The repository has three packages: agent, control, and boot."),
	}
	a, err := runWithValidator(t, CompletionValidationEnforce, eval, provTurns, event.Discard)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if eval.calls != 2 {
		t.Fatalf("evaluator calls = %d, want 2", eval.calls)
	}
	if got := lastAssistantContent(a.Session()); !strings.Contains(got, "three packages") {
		t.Fatalf("final answer = %q, want the post-resume answer", got)
	}
	if !sessionHasUserMessageContaining(a.Session(), "could not confirm this turn is complete") {
		t.Fatal("missing continuation tail message")
	}
}

func TestCompletionValidatorSecondContinuePauses(t *testing.T) {
	eval := &scriptedEvaluator{verdicts: []completioneval.Verdict{
		{Outcome: completioneval.OutcomeContinue, Reason: "still working"},
		{Outcome: completioneval.OutcomeContinue, Reason: "still working again"},
	}}
	provTurns := [][]provider.Chunk{
		textTurn("first placeholder"),
		textTurn("second placeholder"),
	}
	a, err := runWithValidator(t, CompletionValidationEnforce, eval, provTurns, event.Discard)
	var pause *CompletionUncertainError
	if err == nil || !errors.As(err, &pause) || pause.Cause != CompletionUncertainValidatorContinue {
		t.Fatalf("Run error = %v, want validator_continue pause", err)
	}
	if eval.calls != 2 {
		t.Fatalf("evaluator calls = %d, want exactly two before pausing", eval.calls)
	}
	// The candidate answer must survive in the session, not be rolled back.
	if got := lastAssistantContent(a.Session()); got != "second placeholder" {
		t.Fatalf("last assistant content = %q, want the preserved candidate", got)
	}
}

func TestCompletionValidatorFailureAndUncertainPause(t *testing.T) {
	for _, tc := range []struct {
		name  string
		eval  *scriptedEvaluator
		cause string
		class string
	}{
		{
			name:  "invalid evaluator output",
			eval:  &scriptedEvaluator{errs: []error{fmt.Errorf("%w: invalid outcome \"maybe\"", completioneval.ErrInvalidOutput)}},
			cause: CompletionUncertainValidatorFailed,
			class: "invalid_output",
		},
		{
			name:  "evaluator timeout",
			eval:  &scriptedEvaluator{errs: []error{context.DeadlineExceeded}},
			cause: CompletionUncertainValidatorFailed,
			class: "timeout",
		},
		{
			name:  "evaluator error",
			eval:  &scriptedEvaluator{errs: []error{errors.New("provider failed")}},
			cause: CompletionUncertainValidatorFailed,
			class: "error",
		},
		{
			name:  "evaluator uncertain",
			eval:  &scriptedEvaluator{verdicts: []completioneval.Verdict{{Outcome: completioneval.OutcomeUncertain, Reason: "cannot judge"}}},
			cause: CompletionUncertainValidatorUncertain,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, err := runWithValidator(t, CompletionValidationEnforce, tc.eval, [][]provider.Chunk{textTurn("placeholder answer")}, event.Discard)
			var pause *CompletionUncertainError
			if err == nil || !errors.As(err, &pause) || pause.Cause != tc.cause {
				t.Fatalf("Run error = %v, want pause cause %s", err, tc.cause)
			}
			if tc.class != "" && pause.Detail != tc.class {
				t.Fatalf("pause detail = %q, want error class %q", pause.Detail, tc.class)
			}
			if got := lastAssistantContent(a.Session()); got != "placeholder answer" {
				t.Fatalf("candidate answer lost: %q", got)
			}
		})
	}
}

func TestCompletionValidatorEnforceWithNilEvaluatorPauses(t *testing.T) {
	sink := &recordingSink{}
	a, err := runWithValidator(t, CompletionValidationEnforce, nil, [][]provider.Chunk{textTurn("apparently done")}, sink)
	var pause *CompletionUncertainError
	if !errors.As(err, &pause) || pause.Cause != CompletionUncertainValidatorFailed || pause.Detail != "unavailable" {
		t.Fatalf("Run error = %v, want fail-closed unavailable pause", err)
	}
	if got := lastAssistantContent(a.Session()); got != "apparently done" {
		t.Fatalf("candidate answer = %q, want preserved", got)
	}
	records := sink.completionValidations()
	if len(records) != 1 || records[0].Outcome != "error" || records[0].ErrorClass != "unavailable" {
		t.Fatalf("validation events = %+v, want unavailable error audit", records)
	}
}

func TestCompletionValidatorNeedsUserAndBlockedFinish(t *testing.T) {
	for _, outcome := range []completioneval.Outcome{completioneval.OutcomeNeedsUser, completioneval.OutcomeBlocked, completioneval.OutcomeComplete} {
		t.Run(string(outcome), func(t *testing.T) {
			eval := &scriptedEvaluator{verdicts: []completioneval.Verdict{{Outcome: outcome, Reason: "asks the user"}}}
			_, err := runWithValidator(t, CompletionValidationEnforce, eval, [][]provider.Chunk{textTurn("answer awaiting your choice")}, event.Discard)
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if eval.calls != 1 {
				t.Fatalf("evaluator calls = %d, want 1", eval.calls)
			}
		})
	}
}

func TestCompletionValidatorShadowRecordsButNeverChangesOutcome(t *testing.T) {
	sink := &recordingSink{}
	eval := &scriptedEvaluator{verdicts: []completioneval.Verdict{{Outcome: completioneval.OutcomeContinue, Reason: "would resume under enforce"}}}
	a, err := runWithValidator(t, CompletionValidationShadow, eval, [][]provider.Chunk{textTurn("single placeholder answer")}, sink)
	if err != nil {
		t.Fatalf("shadow Run: %v", err)
	}
	if eval.calls != 1 {
		t.Fatalf("evaluator calls = %d, want 1 (shadow still records)", eval.calls)
	}
	if got := lastAssistantContent(a.Session()); got != "single placeholder answer" {
		t.Fatalf("final answer = %q", got)
	}
	if sessionHasUserMessageContaining(a.Session(), "could not confirm this turn is complete") {
		t.Fatal("shadow mode must not append a continuation tail")
	}
	records := sink.completionValidations()
	if len(records) != 1 || records[0].Mode != CompletionValidationShadow || records[0].Outcome != "continue" || records[0].Attempt != 1 {
		t.Fatalf("validation events = %+v, want one shadow continue record", records)
	}
}

func TestCompletionValidatorOffSendsNoRequest(t *testing.T) {
	sink := &recordingSink{}
	eval := &scriptedEvaluator{}
	_, err := runWithValidator(t, "", eval, [][]provider.Chunk{textTurn("plain answer")}, sink)
	if err != nil {
		t.Fatalf("off Run: %v", err)
	}
	if eval.calls != 0 {
		t.Fatalf("evaluator calls = %d, want 0 in off mode", eval.calls)
	}
	if len(sink.completionValidations()) != 0 {
		t.Fatal("off mode must not emit validation events")
	}
}

type stubGoalRecorder struct{}

func (stubGoalRecorder) RecordGoalReport(tool.GoalReport) (string, error) {
	return "recorded", nil
}

func TestCompletionValidatorSkipsActiveGoalTurn(t *testing.T) {
	eval := &scriptedEvaluator{}
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{textTurn("goal turn answer")}}
	a := New(prov, tool.NewRegistry(), NewSession("sys"), Options{CompletionValidation: CompletionValidationEnforce}, event.Discard)
	a.completionEvaluator = eval
	ctx := tool.WithGoalTurnRecorder(context.Background(), stubGoalRecorder{})
	if err := a.Run(ctx, "work on the goal"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if eval.calls != 0 {
		t.Fatalf("evaluator calls = %d, want 0 on an active Goal turn (Goal FSM owns it)", eval.calls)
	}
}

func TestCompletionValidatorSkipsDeterministicBoundaries(t *testing.T) {
	t.Run("max-steps pause", func(t *testing.T) {
		eval := &scriptedEvaluator{}
		reg := tool.NewRegistry()
		reg.Add(fakeTool{name: "read_file", readOnly: true})
		prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
			{toolCallChunk("r1", "read_file", `{}`), {Type: provider.ChunkDone}},
			textTurn("summarized what I have"),
		}}
		a := New(prov, reg, NewSession("sys"), Options{MaxSteps: 1, CompletionValidation: CompletionValidationEnforce}, event.Discard)
		a.completionEvaluator = eval
		err := a.Run(context.Background(), "read everything")
		var pause *maxStepsPause
		if err == nil || !errors.As(err, &pause) {
			t.Fatalf("Run error = %v, want max-steps pause", err)
		}
		if eval.calls != 0 {
			t.Fatalf("evaluator calls = %d, want 0 (host budget boundary precedes validation)", eval.calls)
		}
	})
	t.Run("empty-final retry", func(t *testing.T) {
		eval := &scriptedEvaluator{verdicts: []completioneval.Verdict{{Outcome: completioneval.OutcomeComplete}}}
		prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
			{{Type: provider.ChunkReasoning, Text: "thinking only"}, {Type: provider.ChunkDone}},
			textTurn("visible answer"),
		}}
		a := New(prov, tool.NewRegistry(), NewSession("sys"), Options{CompletionValidation: CompletionValidationEnforce}, event.Discard)
		a.completionEvaluator = eval
		if err := a.Run(context.Background(), "answer me"); err != nil {
			t.Fatalf("Run: %v", err)
		}
		if eval.calls != 1 {
			t.Fatalf("evaluator calls = %d, want 1 (only the recovered visible answer is validated)", eval.calls)
		}
	})
}

func TestCompletionValidatorEvidenceShape(t *testing.T) {
	eval := &scriptedEvaluator{verdicts: []completioneval.Verdict{{Outcome: completioneval.OutcomeComplete}}}
	_, err := runWithValidator(t, CompletionValidationEnforce, eval, [][]provider.Chunk{textTurn("the answer")}, event.Discard)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(eval.evidence) != 1 {
		t.Fatalf("evidence records = %d", len(eval.evidence))
	}
	ev := eval.evidence[0]
	if ev.CandidateAnswer != "the answer" {
		t.Fatalf("candidate = %q", ev.CandidateAnswer)
	}
	if !strings.Contains(ev.TaskText, "repository layout") {
		t.Fatalf("task text = %q", ev.TaskText)
	}
	if ev.Mode != completioneval.ModeStandard {
		t.Fatalf("mode = %q, want standard", ev.Mode)
	}
	for _, turn := range ev.RecentTurns {
		if strings.Contains(turn.Content, "the answer") && turn.Role == "assistant" && turn.Content == "the answer" {
			t.Fatalf("recent turns leaked the candidate itself: %+v", ev.RecentTurns)
		}
	}
	if !strings.Contains(ev.HostSummary, "tool receipts") || !strings.Contains(ev.HostSummary, "spend") {
		t.Fatalf("host summary = %q, want receipts and spend", ev.HostSummary)
	}
}

func TestRecentVisibleTurnsUseUserAuthoredText(t *testing.T) {
	sess := NewSession("sys")
	sess.Messages = append(sess.Messages,
		provider.Message{Role: provider.RoleUser, Content: "<reasoning-language>English</reasoning-language>\nlegacy question"},
		provider.Message{Role: provider.RoleUser, Origin: provider.MessageOriginUser, Content: "<response-language>English</response-language>\nhost wrapped", RawContent: "exact user text"},
		provider.Message{Role: provider.RoleUser, Origin: provider.MessageOriginHost, Content: "innocent-looking host text", RawContent: "must not leak"},
		provider.Message{Role: provider.RoleUser, Content: completionContinueTailMessage()},
		provider.Message{Role: provider.RoleUser, Content: midTurnSteerMessage(completionContinueTailMessage())},
		provider.Message{Role: provider.RoleUser, Origin: provider.MessageOriginUser, Content: completionContinueTailMessage()},
		provider.Message{Role: provider.RoleUser, Origin: provider.MessageOriginUser, Content: midTurnSteerMessage("use the smaller patch"), RawContent: "use the smaller patch"},
		provider.Message{Role: provider.RoleAssistant, Content: "prior visible answer"},
		provider.Message{Role: provider.RoleUser, Content: "local sentinel", LocalOnly: true},
		provider.Message{Role: provider.RoleAssistant, Content: "candidate"},
	)
	a := New(nil, tool.NewRegistry(), sess, Options{}, event.Discard)
	got := a.recentVisibleTurns()
	want := []completioneval.ContextTurn{
		{Role: "user", Content: "exact user text"},
		{Role: "user", Content: completionContinueTailMessage()},
		{Role: "user", Content: "use the smaller patch"},
		{Role: "assistant", Content: "prior visible answer"},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("recent visible turns = %+v, want %+v", got, want)
	}
}

func TestCompletionValidatorAuditIsContentFreeAndHostOnly(t *testing.T) {
	sink := &recordingSink{}
	eval := &scriptedEvaluator{verdicts: []completioneval.Verdict{
		{Outcome: completioneval.OutcomeContinue, Reason: "secret reason text"},
		{Outcome: completioneval.OutcomeContinue, Reason: "still going"},
	}}
	_, err := runWithValidator(t, CompletionValidationEnforce, eval, [][]provider.Chunk{
		textTurn("first"),
		textTurn("second"),
	}, sink)
	var pause *CompletionUncertainError
	if err == nil || !errors.As(err, &pause) {
		t.Fatalf("Run error = %v, want pause", err)
	}
	records := sink.completionValidations()
	if len(records) != 2 {
		t.Fatalf("validation events = %d, want 2", len(records))
	}
	for _, r := range records {
		if r.Mode != CompletionValidationEnforce || r.DurationMs < 0 {
			t.Fatalf("record = %+v, want enforce mode and non-negative duration", r)
		}
	}
	if records[1].Attempt != 2 {
		t.Fatalf("second record attempt = %d, want 2", records[1].Attempt)
	}
	sink.mu.Lock()
	defer sink.mu.Unlock()
	for _, e := range sink.events {
		if strings.Contains(e.Text, "secret reason text") || strings.Contains(e.Reasoning, "secret reason text") {
			t.Fatalf("validator reason leaked through public event: %+v", e)
		}
	}
}

// The sub-agent validator session must come from the parent's factory, so
// concurrent children each hold their own session.
func TestSubagentOptionsCarryIndependentEvaluatorSession(t *testing.T) {
	var built int
	var evaluatedModel string
	var evaluatedSink event.Sink
	factory := func(modelRef string, sink event.Sink) completioneval.Evaluator {
		built++
		evaluatedModel = modelRef
		evaluatedSink = sink
		return &scriptedEvaluator{}
	}
	tt := &TaskTool{completion: taskCompletionConfig{factory: factory, mode: CompletionValidationEnforce}}
	opts := tt.subagentOptions(context.Background(), 0, nil, 0, 1, "", nil)
	opts.ModelRef = "child/worker"
	if opts.CompletionValidation != CompletionValidationEnforce {
		t.Fatalf("child validation mode = %q", opts.CompletionValidation)
	}
	if built != 0 || opts.CompletionEvaluatorFactory == nil {
		t.Fatalf("factory builds = %d, child factory set = %v", built, opts.CompletionEvaluatorFactory != nil)
	}
	childSink := &recordingSink{}
	child := New(nil, tool.NewRegistry(), NewSession("sys"), opts, childSink)
	if built != 1 {
		t.Fatalf("factory builds = %d, want one when New derives the child session", built)
	}
	if evaluatedModel != opts.ModelRef {
		t.Fatalf("evaluator model = %q, want child model %q", evaluatedModel, opts.ModelRef)
	}
	if child.completionEvaluator == nil {
		t.Fatal("child agent has no evaluator session")
	}
	if evaluatedSink != childSink {
		t.Fatal("child evaluator factory did not receive the child-owned sink")
	}
}
