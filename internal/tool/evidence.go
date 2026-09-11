package tool

import (
	"context"
	"encoding/json"
)

// EvidenceTargetInfo is a writer's declaration of the content it is about to
// replace, resolved through the writer's own path, overlay, encoding,
// uniqueness, and interval checks. Hashes are the current content's per-line
// SHA-256 digests for Ranges, concatenated in range order; they contain no
// source text.
type EvidenceTargetInfo struct {
	Path             string
	Snapshot         string
	SourceTextDigest string
	// Absent binds create to a confirmed missing source, so a raced creation
	// cannot be overwritten using a preflight that saw no file.
	Absent bool
	// PreservesContent requires a host-captured source identity but no text
	// coverage. A byte-preserving move must continue to support binary files.
	PreservesContent bool
	// WholeFile marks a requirement that covers the file's entire current
	// content, so paged evidence may be stitched only within one snapshot.
	WholeFile bool
	Ranges    []ReadRange
	Hashes    []string
}

type expectedWriteSourceKey struct{}

// WithExpectedWriteSource carries the preflight source to the actual writer.
// The writer checks it after opening its source, closing the gate/execute gap.
func WithExpectedWriteSource(ctx context.Context, source EvidenceTargetInfo) context.Context {
	return context.WithValue(ctx, expectedWriteSourceKey{}, source)
}
func ExpectedWriteSource(ctx context.Context) (EvidenceTargetInfo, bool) {
	info, ok := ctx.Value(expectedWriteSourceKey{}).(EvidenceTargetInfo)
	return info, ok
}

// EvidenceDeclarer is an optional writer capability. The host asks the real
// writer what evidence it needs instead of trusting a model-reported write
// scope, and re-resolves the target through the same code path the write uses.
// An error means the writer's own validation should own the user-visible result.
type EvidenceDeclarer interface {
	DeclareEvidenceTarget(ctx context.Context, args json.RawMessage) (EvidenceTargetInfo, error)
}
