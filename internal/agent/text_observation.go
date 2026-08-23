package agent

import (
	"context"
	"encoding/json"
	"path/filepath"
	"slices"

	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

const (
	anchorReasonExactMatch     = "would_allow_exact_match"
	anchorReasonNoEligibleRead = "would_block_no_eligible_read"
	anchorReasonPartialWindow  = "would_block_partial_window"
	anchorReasonTargetChanged  = "would_block_target_changed"
	anchorReasonNativeInvalid  = "native_target_invalid"
	anchorReasonSameBatchRead  = "same_batch_read_rejected"
)

type observationBoundaryKey struct{}

func withObservationBoundary(ctx context.Context, boundary uint64) context.Context {
	return context.WithValue(ctx, observationBoundaryKey{}, boundary)
}

func observationBoundary(ctx context.Context, fallback uint64) uint64 {
	if boundary, ok := ctx.Value(observationBoundaryKey{}).(uint64); ok {
		return boundary
	}
	return fallback
}

// recordModelTextObservation records only the line hashes returned by an
// optional reader capability. It runs after host recovery guidance has been
// applied but before compatibility truncation, matching the canonical result
// promoted through RawContent on the next provider request.
func (a *Agent) recordModelTextObservation(plan *toolCallPlan, output string) {
	if a == nil || plan == nil || a.task.ledger == nil || output == "" {
		return
	}
	observer, ok := plan.runTool.(tool.ModelTextObserver)
	if !ok {
		observer, ok = plan.execTool.(tool.ModelTextObserver)
	}
	if !ok {
		return
	}
	observed, ok := observer.ObserveModelText(json.RawMessage(plan.runArgs), output)
	if !ok {
		return
	}
	a.task.ledger.RecordTextObservation(evidence.TextObservation{
		Path:       observed.Path,
		StartLine:  observed.StartLine,
		LineHashes: observed.LineHashes,
	})
}

// recordAnchorSafetyAudit computes and emits the interval-fingerprint decision.
// The newest eligible observation after the relevant write wins; observations
// created after the frozen batch boundary are tracked but cannot be used because
// the model has not seen their result yet.
func (a *Agent) recordAnchorSafetyAudit(ctx context.Context, call provider.ToolCall, target tool.Tool, boundary uint64, writeIndex int) (event.AnchorSafetyAudit, bool, bool) {
	if a == nil || a.task.ledger == nil || target == nil {
		return event.AnchorSafetyAudit{}, false, false
	}
	resolver, ok := target.(tool.AnchoredTextTarget)
	if !ok {
		return event.AnchorSafetyAudit{}, false, false
	}
	audit := event.AnchorSafetyAudit{
		Mode:          "shadow",
		TaskMode:      "interactive",
		LegacyAllowed: a.task.ledger.HasSuccessfulAnchorRefreshReadAfter(evidence.ToolCallPaths(json.RawMessage(call.Arguments)), writeIndex),
	}
	writeSequence, _ := a.task.ledger.ReceiptSequence(writeIndex)
	if a.closedLoopActive() {
		audit.TaskMode = "loop"
	}
	emit := func() (event.AnchorSafetyAudit, bool, bool) {
		event.RecordAnchorSafetyAudit(a.svc.sink, audit)
		return audit, audit.ShadowAllowed, true
	}
	resolved, err := resolver.ResolveAnchoredTextTarget(ctx, json.RawMessage(call.Arguments))
	if err != nil || resolved.Path == "" || resolved.StartLine < 1 || resolved.EndLine < resolved.StartLine || len(resolved.LineHashes) != resolved.EndLine-resolved.StartLine+1 {
		audit.Reason = anchorReasonNativeInvalid
		return emit()
	}
	audit.RangeLines = len(resolved.LineHashes)
	observations := a.task.ledger.TextObservations()
	canonicalPath := filepath.Clean(resolved.Path)
	var latest *evidence.TextObservation
	for i := range observations {
		o := observations[i]
		if filepath.Clean(o.Path) != canonicalPath {
			continue
		}
		if o.Sequence <= writeSequence {
			continue
		}
		if o.Sequence > boundary {
			audit.SameBatchReadRejected = true
			continue
		}
		if latest == nil || o.Sequence > latest.Sequence {
			copy := o
			copy.LineHashes = append([]string(nil), o.LineHashes...)
			latest = &copy
		}
	}
	if latest == nil {
		if audit.SameBatchReadRejected {
			audit.Reason = anchorReasonSameBatchRead
		} else {
			audit.Reason = anchorReasonNoEligibleRead
		}
		return emit()
	}
	audit.ObservationAge = int(boundary - latest.Sequence)
	if offset, matches := findHashSequence(latest.LineHashes, resolved.LineHashes); matches == 1 {
		_ = offset
		audit.ShadowAllowed = true
		audit.Reason = anchorReasonExactMatch
		return emit()
	}
	obsEnd := latest.StartLine + len(latest.LineHashes) - 1
	if latest.StartLine > resolved.StartLine || obsEnd < resolved.EndLine {
		audit.Reason = anchorReasonPartialWindow
	} else {
		audit.Reason = anchorReasonTargetChanged
	}
	return emit()
}

func findHashSequence(window, target []string) (offset, matches int) {
	if len(target) == 0 || len(window) < len(target) {
		return -1, 0
	}
	for i := 0; i+len(target) <= len(window); i++ {
		if slices.Equal(window[i:i+len(target)], target) {
			offset = i
			matches++
		}
	}
	return offset, matches
}
