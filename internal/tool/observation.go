package tool

import (
	"context"
	"encoding/json"
)

// ModelTextObservation describes the contiguous, line-numbered window that a
// reader returned to the model. Line numbers are 1-based and LineHashes are
// SHA-256 digests of the decoded line text, without read_file's line prefix.
// The observation is host-only; it must never be added to a provider request.
type ModelTextObservation struct {
	Path       string
	StartLine  int
	LineHashes []string
}

// ModelTextObserver is an optional reader capability. The agent passes the
// final unbounded tool result before compatibility truncation, so a reader can
// report exactly the text window that will be available through RawContent on
// the next provider request without retaining source text.
type ModelTextObserver interface {
	ObserveModelText(args json.RawMessage, output string) (ModelTextObservation, bool)
}

// AnchoredTextTargetInfo is the current, canonical target resolved by an
// anchor-based writer. The closed interval always includes both anchor lines,
// even when the writer's public operation is exclusive. LineHashes contain no
// source text and are used only for host-side shadow comparison.
type AnchoredTextTargetInfo struct {
	Path       string
	Inclusive  bool
	StartLine  int
	EndLine    int
	LineHashes []string
}

// AnchoredTextTarget is an optional writer capability used by the host's
// shadow safety audit. Resolving a target must reuse the writer's real path,
// overlay, encoding, uniqueness, and interval checks. An error means the
// writer's native validation should own the user-visible result.
type AnchoredTextTarget interface {
	ResolveAnchoredTextTarget(context.Context, json.RawMessage) (AnchoredTextTargetInfo, error)
}
