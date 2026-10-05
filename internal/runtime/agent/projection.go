package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reasonix/internal/state/sessionstore"
	"strings"

	"reasonix/internal/contract/provider"
)

// Cache state labels for resume/preflight telemetry. They never enter the
// provider-visible prompt.
const (
	CacheStateWarm    = "warm"
	CacheStateCold    = "cold"
	CacheStateUnknown = "unknown"
)

// Compaction trigger labels.
const (
	CompactionTriggerPressure = "pressure"
	CompactionTriggerManual   = "manual"
	CompactionTriggerOverflow = "overflow"
	CompactionTriggerSnip     = "snip"
	CompactionTriggerTool     = "tool"
)

// Compaction mode labels.
const (
	CompactionModeNative     = "native"
	CompactionModeSummarized = "summarized"
	CompactionModeDegraded   = "degraded"
	CompactionModeSnip       = "snip"
)

// CompactRequest is a maintenance the user asked for. Asking is what makes it
// manual, so the automatic threshold is always waived; economics is a separate
// permission because "tidy this now" is not "spend whatever it costs".
type CompactRequest struct {
	Instructions    string
	IgnoreEconomics bool
}

// CompactVerdict answers a CompactRequest. Declining is an answer and not a
// failure: Reason names the economics that declined, so a frontend can say why
// instead of reporting a command that did not work.
type CompactVerdict struct {
	// Installed says a projection was written. It is a bool rather than a
	// CompactionOutcome because that enum's zero value is "installed", and a
	// verdict nobody filled in must not claim a fold happened.
	Installed bool
	Reason    CompactionNoopReason
}

// Compacted reports whether a projection was installed.
func (v CompactVerdict) Compacted() bool { return v.Installed }

// CompactionOutcome reports whether compactToProjection installed a projection.
type CompactionOutcome int

const (
	// CompactionInstalled means a new (or replacement) projection was saved.
	CompactionInstalled CompactionOutcome = iota
	// CompactionNoop means no fold region / economics skip / empty fold after hooks.
	CompactionNoop
)

// CompactionNoopReason identifies why an attempt folded nothing. "Already folded
// this turn" and "nothing left to fold" read alike and mean opposite things about
// what the next round can expect, so the verdict carries a code, not a sentence.
type CompactionNoopReason string

const (
	NoopNoNewClosedPrefix       CompactionNoopReason = "no_new_closed_prefix"
	NoopActiveTurnBoundary      CompactionNoopReason = "active_turn_boundary"
	NoopNoFoldableRegion        CompactionNoopReason = "no_foldable_region"
	NoopFoldBelowEconomics      CompactionNoopReason = "fold_below_economics"
	NoopInputUnchanged          CompactionNoopReason = "input_unchanged"
	NoopFoldEmptyAfterHooks     CompactionNoopReason = "fold_empty_after_hooks"
	NoopFixedPrefixAboveTrigger CompactionNoopReason = "fixed_prefix_above_trigger"
)

// CompactDeclineText says why a request was declined. The code is the identity a
// frontend matches and localizes; this is what one prints when it has no phrase
// of its own, so nobody has to invent a reason from an empty result.
func CompactDeclineText(reason CompactionNoopReason) string {
	switch reason {
	case NoopInputUnchanged:
		return "the context has not changed since the last time it was tidied"
	case NoopNoNewClosedPrefix:
		return "nothing has closed since the last checkpoint"
	case NoopFoldBelowEconomics:
		return "too little has been added to pay for another summary"
	case NoopActiveTurnBoundary:
		return "the turn in flight has to finish first"
	case NoopFoldEmptyAfterHooks:
		return "everything foldable is content a hook keeps"
	case NoopFixedPrefixAboveTrigger:
		return "the part that cannot be folded is already over the threshold"
	case NoopNoFoldableRegion:
		return "no foldable region remains"
	case "":
		return "nothing left worth folding"
	}
	return string(reason)
}

// CompactionTelemetry is the structured observability record for one
// compaction attempt. Sensitive transcript content is intentionally omitted.
type CompactionTelemetry struct {
	Trigger          string `json:"trigger"`
	CacheState       string `json:"cache_state"`
	Mode             string `json:"mode"`
	Native           bool   `json:"native"`
	SourceTokens     int    `json:"source_tokens"`
	FoldTokens       int    `json:"fold_tokens"` // summarizer input after any shortening
	Spans            int    `json:"spans"`       // summarizer calls the fold needed; 1 unless it was split
	ProjectionTokens int    `json:"projection_tokens"`
	UserTurnsKept    int    `json:"user_turns_kept"`
	UserTurnsDropped int    `json:"user_turns_dropped"` // past the retention budget, now summary-only
	// Fold coverage: the changes and failures the region produced, and how many
	// the digest did not carry.
	CoverageRequired int `json:"coverage_required,omitempty"`
	CoverageMissing  int `json:"coverage_missing,omitempty"`
	// CoverageBackstopped: the host wrote the dropped facts in itself.
	CoverageBackstopped bool `json:"coverage_backstopped,omitempty"`
	// SummaryUsage is the transaction's whole bill. The flat fields below are
	// its input/output/cache halves, kept for the existing detail line.
	SummaryUsage      sessionstore.CompactionUsage `json:"summary_usage,omitzero"`
	InputTokens       int                          `json:"input_tokens"`
	OutputTokens      int                          `json:"output_tokens"`
	CacheHitTokens    int                          `json:"cache_hit_tokens"`
	CacheMissTokens   int                          `json:"cache_miss_tokens"`
	CacheWriteTokens  int                          `json:"cache_write_tokens"`
	RequestCount      int                          `json:"request_count"`
	ProviderRequestID string                       `json:"provider_request_id,omitempty"`
	Error             string                       `json:"error,omitempty"`
}

// summaryContentHash fingerprints a compaction summary for projection metadata.
func summaryContentHash(summary string) string {
	if summary == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(summary))
	return hex.EncodeToString(sum[:16])
}

// projectionValid reports whether st can be reused for the current transcript
// and provider/model lineage. Fail closed: missing CoveredPrefixHash or a blank
// sidecar PromptCacheKey when the current lineage key is known forces rebuild.
func projectionValid(st sessionstore.CompactionState, msgs []provider.Message, cacheKey string, fingerprint func([]provider.Message, int) string) bool {
	if len(st.Projection.Messages) == 0 {
		return false
	}
	if !projectionLineageOK(st, cacheKey) {
		return false
	}
	return projectionContentValid(st, msgs, fingerprint)
}

// projectionLineageOK reports whether the sidecar was written for the lineage
// asking for it. A blank current key means the lineage is not known yet.
func projectionLineageOK(st sessionstore.CompactionState, cacheKey string) bool {
	if cacheKey == "" {
		return true
	}
	// Stored key must match (legacy native suffix ok).
	_, ok := lineageKeyCompatible(st.PromptCacheKey, cacheKey)
	return ok
}

// projectionContentValid reports whether st's projection body still matches the
// canonical transcript, independent of provider/model lineage. LoadProjectionSidecar
// uses it to rebind across upgrade/model/workspace key changes.
func projectionContentValid(st sessionstore.CompactionState, msgs []provider.Message, fingerprint func([]provider.Message, int) string) bool {
	if fingerprint == nil {
		fingerprint = sessionstore.CoveredPrefixHash
	}
	n := st.Projection.CoveredCount
	if len(st.Projection.Messages) == 0 || n <= 0 || n > len(msgs) {
		return false
	}
	return projectionCoversTail(st, len(msgs), fingerprint(msgs, n))
}

// projectionCoversTail is the validity judgement itself, and the covered-prefix
// hash is its whole proof: it fingerprints provider-visible canonical[:n], so a
// match says the history the digest folded is still exactly there. A transcript
// version adds nothing to that and is not durable — a reload restarts the
// counter — so it takes no part in this decision.
func projectionCoversTail(st sessionstore.CompactionState, total int, prefixHash string) bool {
	n := st.Projection.CoveredCount
	if len(st.Projection.Messages) == 0 || n <= 0 || n > total {
		return false
	}
	// Prefix hash is required; legacy sidecars without it are rebuilt.
	return st.Projection.CoveredPrefixHash != "" && prefixHash == st.Projection.CoveredPrefixHash
}

// modelVisibleFromProjection splices the projection with any messages appended
// after it was built. LocalOnly messages stay excluded via ModelMessages later.
func modelVisibleFromProjection(proj sessionstore.ContextProjection, canonical []provider.Message) []provider.Message {
	if len(proj.Messages) == 0 {
		return nil
	}
	out := append([]provider.Message(nil), proj.Messages...)
	if proj.CoveredCount >= 0 && proj.CoveredCount < len(canonical) {
		out = append(out, canonical[proj.CoveredCount:]...)
	}
	return out
}

// coalesceProjectionUserRuns keeps provider request copies compatible with
// providers that require strict user/assistant alternation. Projection
// sidecars retain logical user-turn boundaries; only the outbound copy is
// merged, leaving canonical history and range anchors untouched.
func coalesceProjectionUserRuns(msgs []provider.Message) []provider.Message {
	if len(msgs) < 2 {
		return msgs
	}
	out := make([]provider.Message, 0, len(msgs))
	for _, msg := range msgs {
		if len(out) == 0 || msg.Role != provider.RoleUser || out[len(out)-1].Role != provider.RoleUser {
			clone := msg
			clone.Images = append([]string(nil), msg.Images...)
			clone.ToolCalls = append([]provider.ToolCall(nil), msg.ToolCalls...)
			clone.ResponsesItems = append([]json.RawMessage(nil), msg.ResponsesItems...)
			out = append(out, clone)
			continue
		}

		prev := &out[len(out)-1]
		if isCompactionSummary(msg) && !isCompactionSummary(*prev) {
			prev.Content = strings.TrimRight(msg.Content, "\n") + "\n\n" + prev.Content
		} else {
			prev.Content = strings.TrimRight(prev.Content, "\n") + "\n\n" + msg.Content
		}
		// A run that swallowed a derived tail is derived from there on: its bytes
		// are this request's, not what the next one carries.
		prev.Derived = prev.Derived || msg.Derived
		prev.Images = append(prev.Images, msg.Images...)
		prev.ToolCalls = append(prev.ToolCalls, msg.ToolCalls...)
		prev.ResponsesItems = append(prev.ResponsesItems, msg.ResponsesItems...)
	}
	return out
}

// formatSummaryMessage builds the stable user-turn wrapper around a digest.
func formatSummaryMessage(summary string) provider.Message {
	return provider.Message{
		Role: provider.RoleUser,
		Content: SummaryTagOpen + "\n" +
			"Summary of earlier conversation (older messages were compacted to save context):\n" +
			summary + "\n" +
			SummaryTagClose,
	}
}
