package agent

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/skill"
)

type SubagentOutcomeStatus string

const (
	SubagentOutcomeCompleted SubagentOutcomeStatus = "completed"
	SubagentOutcomePartial   SubagentOutcomeStatus = "partial"
	SubagentOutcomeFailed    SubagentOutcomeStatus = "failed"
	SubagentOutcomeCancelled SubagentOutcomeStatus = "cancelled"
)

// SubagentOutcome is the bounded handoff from a child run to its parent.
type SubagentOutcome struct {
	Ref         string                `json:"ref"`
	Status      SubagentOutcomeStatus `json:"status"`
	FinalAnswer string                `json:"final_answer,omitempty"`
	ErrorCode   string                `json:"error_code,omitempty"`
	Retryable   bool                  `json:"retryable"`
}

// SubagentRunError carries a recoverable result envelope while preserving the
// original error for host status classification and logging.
type SubagentRunError struct {
	Outcome SubagentOutcome
	Cause   error
}

func (e *SubagentRunError) Error() string {
	if e == nil || e.Cause == nil {
		return "subagent run failed"
	}
	return e.Cause.Error()
}

func (e *SubagentRunError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func (e *SubagentRunError) SubagentOutput() string {
	if e == nil {
		return ""
	}
	return FormatSubagentOutcome(e.Outcome)
}

var _ skill.SubagentOutputError = (*SubagentRunError)(nil)

func NewSubagentRunError(run *SubagentRun, cause error) *SubagentRunError {
	outcome := SubagentOutcome{Status: SubagentOutcomeFailed}
	if run != nil {
		outcome.Ref = run.Ref
		outcome.FinalAnswer = latestAssistantAnswer(run.Session)
	}
	if cause == nil {
		cause = errors.New("subagent run failed")
	}
	outcome.ErrorCode, outcome.Status, outcome.Retryable = subagentErrorDisposition(cause)
	return &SubagentRunError{Outcome: outcome, Cause: cause}
}

func subagentOutcomeFromError(err error) *SubagentOutcome {
	var subErr *SubagentRunError
	if !errors.As(err, &subErr) {
		return nil
	}
	outcome := subErr.Outcome
	return &outcome
}

func (t *TaskTool) failedSubagentResult(run *SubagentRun, cause error) (string, error) {
	subErr := NewSubagentRunError(run, cause)
	var saveErr error
	if t != nil && t.transcripts != nil {
		saveErr = t.transcripts.SaveOutcome(run, subErr.Outcome)
	}
	return subErr.SubagentOutput(), errors.Join(subErr, saveErr)
}

// resolveAmbiguousSubagentFailure preserves the child result envelope for
// callers that still use the historical method name. Completion is decided by
// the child Agent's deterministic stream/tool state; no second model opinion is
// requested here.
func (t *TaskTool) resolveAmbiguousSubagentFailure(ctx context.Context, run *SubagentRun, taskText, modelRef string, sink event.Sink, cause error) (string, error) {
	subErr := NewSubagentRunError(run, cause)
	var saveErr error
	if t != nil && t.transcripts != nil {
		saveErr = t.transcripts.SaveOutcome(run, subErr.Outcome)
	}
	return subErr.SubagentOutput(), errors.Join(subErr, saveErr)
}

// ResolveAmbiguousSubagentFailure applies the same bounded child completion
// policy to boot-wired skill runners.
func (t *TaskTool) ResolveAmbiguousSubagentFailure(ctx context.Context, run *SubagentRun, taskText, modelRef string, sink event.Sink, cause error) (string, error) {
	return t.resolveAmbiguousSubagentFailure(ctx, run, taskText, modelRef, sink, cause)
}

// failBeforeSubagentRelease preserves the run envelope on setup failures that
// occur after a reference was allocated but before the normal RunProfileSpec
// cleanup/defer is installed.
func (t *TaskTool) failBeforeSubagentRelease(run *SubagentRun, cause error) (string, error) {
	result, err := t.failedSubagentResult(run, cause)
	if run != nil {
		run.Release()
	}
	return result, err
}

func subagentErrorDisposition(err error) (string, SubagentOutcomeStatus, bool) {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return "cancelled", SubagentOutcomeCancelled, false
	}
	var completion *CompletionUncertainError
	if errors.As(err, &completion) {
		return "completion_uncertain", SubagentOutcomePartial, true
	}
	var readiness *FinalReadinessError
	if errors.As(err, &readiness) {
		return "final_readiness", SubagentOutcomePartial, true
	}
	var review *ReviewUnavailableError
	if errors.As(err, &review) {
		return "review_unavailable", SubagentOutcomePartial, true
	}
	var steps *maxStepsPause
	if errors.As(err, &steps) {
		return "max_steps", SubagentOutcomePartial, true
	}
	var incomplete *IncompleteReadError
	if errors.As(err, &incomplete) {
		return "incomplete_read", SubagentOutcomePartial, true
	}
	var apiErr *provider.APIError
	if errors.As(err, &apiErr) {
		return fmt.Sprintf("provider_http_%d", apiErr.Status), SubagentOutcomeFailed, provider.RetryableStatus(apiErr.Status)
	}
	if provider.IsConnReset(err) {
		return "provider_connection", SubagentOutcomeFailed, true
	}
	return "subagent_error", SubagentOutcomeFailed, false
}

// FormatSubagentOutcome keeps the existing textual reference markers while
// making status and recovery explicit to the parent model and UI sinks.
func FormatSubagentOutcome(outcome SubagentOutcome) string {
	var b strings.Builder
	status := outcome.Status
	if status == "" {
		status = SubagentOutcomeFailed
	}
	if outcome.Ref != "" {
		fmt.Fprintf(&b, "Subagent reference: %s\n", outcome.Ref)
	}
	fmt.Fprintf(&b, "Subagent outcome: status=%s retryable=%t", status, outcome.Retryable)
	if outcome.ErrorCode != "" {
		fmt.Fprintf(&b, " error_code=%s", outcome.ErrorCode)
	}
	b.WriteByte('\n')
	if answer := strings.TrimSpace(outcome.FinalAnswer); answer != "" {
		b.WriteString("\n\nFinal answer:\n")
		b.WriteString(answer)
	}
	return strings.TrimSpace(b.String())
}

func ParseSubagentOutcome(text string) (SubagentOutcome, bool) {
	var outcome SubagentOutcome
	for line := range strings.SplitSeq(text, "\n") {
		line = strings.TrimSpace(line)
		if ref, ok := strings.CutPrefix(line, "Subagent reference: "); ok {
			outcome.Ref = strings.TrimSpace(ref)
			continue
		}
		if ref, ok := strings.CutPrefix(line, "Subagent reference (failed): "); ok {
			outcome.Ref = strings.TrimSpace(ref)
			continue
		}
		if status, ok := strings.CutPrefix(line, "Subagent outcome: status="); ok {
			fields := strings.Fields(status)
			if len(fields) == 0 {
				continue
			}
			outcome.Status = SubagentOutcomeStatus(fields[0])
			for _, field := range fields[1:] {
				if value, ok := strings.CutPrefix(field, "retryable="); ok {
					outcome.Retryable, _ = strconv.ParseBool(value)
				}
				if value, ok := strings.CutPrefix(field, "error_code="); ok {
					outcome.ErrorCode = value
				}
			}
		}
	}
	return outcome, validSubagentRef(outcome.Ref) && validSubagentOutcomeStatus(outcome.Status)
}

func validSubagentOutcomeStatus(status SubagentOutcomeStatus) bool {
	switch status {
	case SubagentOutcomeCompleted, SubagentOutcomePartial, SubagentOutcomeFailed, SubagentOutcomeCancelled:
		return true
	default:
		return false
	}
}

func emitSubagentLifecycle(sink event.Sink, phase, parentToolCallID, skillName, model, effort string, run *SubagentRun, outcome *SubagentOutcome) {
	if run == nil || run.Ref == "" || sink == nil {
		return
	}
	info := event.SubagentLifecycleInfo{
		Phase: phase, Ref: run.Ref, ParentToolCallID: parentToolCallID,
		Skill: skillName, Model: model, Effort: effort,
	}
	if !run.Meta.CreatedAt.IsZero() {
		info.StartUnixMs = run.Meta.CreatedAt.UnixMilli()
	}
	if outcome != nil {
		info.Status = string(outcome.Status)
		info.ErrorCode = outcome.ErrorCode
		info.Retryable = outcome.Retryable
		info.OutputBytes = len(outcome.FinalAnswer)
		info.EndUnixMs = time.Now().UnixMilli()
	} else if phase == "child_running" {
		info.Status = "running"
	} else {
		info.Status = "queued"
	}
	event.RecordSubagentLifecycle(sink, info)
}

// EmitSubagentLifecycle publishes a content-free lifecycle transition for a
// child runner implemented outside the agent package, such as boot-wired
// skills.
func EmitSubagentLifecycle(sink event.Sink, phase, parentToolCallID, skillName, model, effort string, run *SubagentRun, outcome *SubagentOutcome) {
	emitSubagentLifecycle(sink, phase, parentToolCallID, skillName, model, effort, run, outcome)
}

func terminalSubagentLifecycle(runErr error) (string, *SubagentOutcome) {
	if runErr == nil {
		return "child_completed", &SubagentOutcome{Status: SubagentOutcomeCompleted}
	}
	if outcome := subagentOutcomeFromError(runErr); outcome != nil {
		return "child_" + string(outcome.Status), outcome
	}
	subErr := NewSubagentRunError(nil, runErr)
	return "child_" + string(subErr.Outcome.Status), &subErr.Outcome
}

// TerminalSubagentLifecycle classifies a runner error for lifecycle sinks.
func TerminalSubagentLifecycle(runErr error) (string, *SubagentOutcome) {
	return terminalSubagentLifecycle(runErr)
}
