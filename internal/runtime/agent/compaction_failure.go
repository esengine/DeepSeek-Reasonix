package agent

import (
	"context"
	"errors"
	"fmt"
)

// Codes for a fold that was refused or failed, alongside the no-fold verdicts
// in projection.go. Only the code crosses to a frontend; the sentence stays a
// diagnostic, so a reader never has to match wording to learn which class a
// failure was.
const (
	NoopCandidateNotSmaller    CompactionNoopReason = "candidate_not_smaller"
	NoopCandidateAboveCeiling  CompactionNoopReason = "candidate_above_ceiling"
	NoopCandidateAboveTrigger  CompactionNoopReason = "candidate_above_trigger"
	NoopCandidateAbovePhysical CompactionNoopReason = "candidate_above_physical_ceiling"
	NoopSavingsBelowMinimum    CompactionNoopReason = "savings_below_minimum"
	NoopDigestLostEveryChange  CompactionNoopReason = "digest_lost_every_change"

	FailSummaryFailed        CompactionNoopReason = "summary_failed"
	FailSummaryTimeout       CompactionNoopReason = "summary_timeout"
	FailSummaryCeiling       CompactionNoopReason = "summary_ceiling"
	FailSummaryTruncated     CompactionNoopReason = "summary_truncated"
	FailSummaryNotDigest     CompactionNoopReason = "summary_not_digest"
	FailSummaryInputTooLarge CompactionNoopReason = "summary_input_too_large"
	FailContextChanged       CompactionNoopReason = "context_changed"
	FailHookRefused          CompactionNoopReason = "hook_refused"
	FailPersistFailed        CompactionNoopReason = "persist_failed"
	FailResultAboveTrigger   CompactionNoopReason = "result_above_trigger"
	FailCancelled            CompactionNoopReason = "cancelled"
	FailUnclassified         CompactionNoopReason = "unclassified"
	FailBusy                 CompactionNoopReason = "busy"
)

var (
	errSummaryInputTooLarge   = errors.New("summary input exceeds the single-request budget")
	errSummaryTimeout         = errors.New("summarizer stalled: no output within its idle bound")
	errSummaryCeiling         = errors.New("summarizer exceeded its overall time ceiling")
	errSummaryRequestFailed   = errors.New("summarizer request failed")
	errCompactionHookRefused  = errors.New("compaction extension refused the fold")
	errProjectionNotPersisted = errors.New("projection could not be persisted")
)

func compactionHookRefusal(err error) error {
	return fmt.Errorf("%w: %w", errCompactionHookRefused, err)
}

// classifySummaryError names a summarizer failure where it is known: the
// deadline is the summarizer's own only when the caller's context is still live.
func classifySummaryError(parent context.Context, err error) error {
	switch {
	case err == nil, errors.Is(err, context.Canceled), parent.Err() != nil,
		errors.Is(err, errSummaryOutputTruncated), errors.Is(err, errSummaryInputTooLarge), errors.Is(err, errSummaryNotDigest),
		errors.Is(err, errSummaryTimeout), errors.Is(err, errSummaryCeiling):
		return err
	case errors.Is(err, context.DeadlineExceeded):
		return fmt.Errorf("%w: %w", errSummaryTimeout, err)
	}
	return fmt.Errorf("%w: %w", errSummaryRequestFailed, err)
}

// compactionFailureCode classifies a fold error by identity. A rejection
// carries its own code; every other class is a sentinel its producer wrapped.
// A cancelled run is its own class; an error nobody classified is reported as
// unclassified, so a card never shows a failure with no stated reason.
func compactionFailureCode(err error) CompactionNoopReason {
	if err == nil {
		return ""
	}
	var rejected *checkpointRejection
	switch {
	case errors.As(err, &rejected):
		return rejected.code
	case errors.Is(err, errSummaryOutputTruncated):
		return FailSummaryTruncated
	case errors.Is(err, errSummaryNotDigest):
		return FailSummaryNotDigest
	case errors.Is(err, errCompressStaleContext):
		return FailContextChanged
	case errors.Is(err, errSummaryInputTooLarge):
		return FailSummaryInputTooLarge
	case errors.Is(err, errSummaryTimeout):
		return FailSummaryTimeout
	case errors.Is(err, errSummaryCeiling):
		return FailSummaryCeiling
	case errors.Is(err, errSummaryRequestFailed):
		return FailSummaryFailed
	case errors.Is(err, errCompactionHookRefused):
		return FailHookRefused
	case errors.Is(err, errProjectionNotPersisted):
		return FailPersistFailed
	case errors.Is(err, context.Canceled):
		return FailCancelled
	}
	return FailUnclassified
}

// CompactionFailureCode is the class of a fold error, for a caller that reports
// the failure to a frontend: the code is the identity, the sentence a fallback.
func CompactionFailureCode(err error) CompactionNoopReason { return compactionFailureCode(err) }
