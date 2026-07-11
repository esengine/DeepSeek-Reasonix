package tool

import (
	"context"
	"encoding/json"
	"sync"

	"reasonix/internal/diff"
)

// previewMemo caches a single tool call's raw Preview result for the lifetime of
// one executeOne, so the approval-prompt preview and the checkpoint preview
// don't each recompute it (a full-file read + diff for write/edit, two stats for
// a move). It is scoped per call, never per turn: disk changes between calls in
// a batch, so a turn-wide cache could serve a stale diff. Keyed by args because
// a single call has one (tool, args) pair; a differing key simply misses and
// recomputes, which is safe.
//
// It caches the raw (diff.Change, error) that Previewer.Preview returns — not
// PreviewChange's binary-dropping (change, ok) — so a caller that must act on a
// binary or errored preview (the checkpoint snapshots binary edits too) keeps
// its own filtering. Only tools implementing Previewer are memoized.
type previewMemo struct {
	mu      sync.Mutex
	entries map[string]previewEntry
}

type previewEntry struct {
	ch  diff.Change
	err error
}

type previewMemoKey struct{}

// WithPreviewMemo returns a ctx carrying a fresh, empty preview memo. executeOne
// seeds it once per call; the gate's approval preview and the checkpoint preview
// both run on this ctx and so share the result. A ctx without a memo (any other
// approval caller — fresh/plan decisions never reach a writer's Preview) makes
// PreviewMemoized fall back to a plain, uncached preview.
func WithPreviewMemo(ctx context.Context) context.Context {
	return context.WithValue(ctx, previewMemoKey{}, &previewMemo{entries: map[string]previewEntry{}})
}

// PreviewMemoized runs t.Preview(args) with the same (diff.Change, error)
// contract as calling it directly, reusing a result already computed for the
// same args under this ctx's memo. Returns ok=false when t does not implement
// Previewer. Without a memo on ctx it computes without caching — identical
// observable behavior, just no reuse.
func PreviewMemoized(ctx context.Context, t Tool, args json.RawMessage) (diff.Change, error, bool) {
	pv, isPreviewer := t.(Previewer)
	if !isPreviewer {
		return diff.Change{}, nil, false
	}
	m, _ := ctx.Value(previewMemoKey{}).(*previewMemo)
	if m == nil {
		ch, err := pv.Preview(args)
		return ch, err, true
	}
	key := string(args)
	m.mu.Lock()
	if e, hit := m.entries[key]; hit {
		m.mu.Unlock()
		return e.ch, e.err, true
	}
	m.mu.Unlock()

	// Compute outside the lock: Preview reads files / stats, and holding the lock
	// across it would serialize unrelated tools that happen to share a ctx.
	ch, err := pv.Preview(args)

	m.mu.Lock()
	m.entries[key] = previewEntry{ch: ch, err: err}
	m.mu.Unlock()
	return ch, err, true
}
