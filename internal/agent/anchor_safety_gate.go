package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"reasonix/internal/evidence"
	"reasonix/internal/provider"
)

func (a *Agent) staleAnchorEditBlock(ctx context.Context, call provider.ToolCall) (string, bool) {
	if a.task.ledger == nil || call.Name != "delete_range" {
		return "", false
	}
	rec := evidence.ReceiptFromToolCall(call.Name, json.RawMessage(call.Arguments), true, false)
	if len(rec.Paths) == 0 {
		return "", false
	}
	writeIndex, ok := a.task.ledger.LatestSuccessfulWriteIndex(rec.Paths)
	if !ok {
		return "", false
	}
	boundary := observationBoundary(ctx, a.task.ledger.ObservationBoundary())
	if a.svc.tools != nil {
		if target, _, ambiguous := a.svc.tools.ResolveCall(call.Name); target != nil && len(ambiguous) == 0 {
			audit, shadowAllowed, supported := a.recordAnchorSafetyAudit(ctx, call, target, boundary, writeIndex)
			if supported && !a.legacyAnchorSafetyGate {
				if shadowAllowed || audit.Reason == anchorReasonNativeInvalid {
					return "", false
				}
				return fmt.Sprintf(
					"blocked: [fresh read required] %q targets %s, but no eligible model-visible read covers both anchors and the intervening lines. In a separate provider round, use read_file with a window covering both anchors and the whole range, then retry; reads from the same provider batch do not count.",
					call.Name, strings.Join(rec.Paths, ", ")), true
			}
		}
	}
	if a.task.ledger.HasSuccessfulAnchorRefreshReadAfter(rec.Paths, writeIndex) {
		return "", false
	}
	return fmt.Sprintf(
		"blocked: [fresh read required] %q targets %s, which was already modified earlier this turn. Re-read the current file with read_file without offset/limit before another range deletion, or use multi_edit with exact replacements when possible. This prevents stale start/end anchors from selecting an unintended destructive span.",
		call.Name, strings.Join(rec.Paths, ", ")), true
}
