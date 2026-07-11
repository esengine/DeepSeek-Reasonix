package tool

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"reasonix/internal/diff"
)

// countingPreviewer records how many times Preview ran, so a test can prove the
// memo collapses repeat calls for the same args into one computation.
type countingPreviewer struct {
	calls *int
	err   error
}

func (countingPreviewer) Name() string                                     { return "counting" }
func (countingPreviewer) Description() string                              { return "stub" }
func (countingPreviewer) Schema() json.RawMessage                          { return json.RawMessage(`{}`) }
func (countingPreviewer) ReadOnly() bool                                   { return false }
func (countingPreviewer) Execute(context.Context, json.RawMessage) (string, error) { return "", nil }

func (c countingPreviewer) Preview(args json.RawMessage) (diff.Change, error) {
	*c.calls++
	if c.err != nil {
		return diff.Change{}, c.err
	}
	return diff.Change{Path: string(args), Kind: diff.Modify}, nil
}

func TestPreviewMemoizedReusesResultForSameArgs(t *testing.T) {
	calls := 0
	tl := countingPreviewer{calls: &calls}
	ctx := WithPreviewMemo(context.Background())
	args := json.RawMessage(`{"path":"x"}`)

	ch1, err1, ok1 := PreviewMemoized(ctx, tl, args)
	ch2, err2, ok2 := PreviewMemoized(ctx, tl, args)

	if !ok1 || !ok2 {
		t.Fatalf("ok = %v,%v, want both true (tool is a Previewer)", ok1, ok2)
	}
	if err1 != nil || err2 != nil {
		t.Fatalf("errs = %v,%v, want nil", err1, err2)
	}
	if ch1.Path != string(args) || ch2.Path != string(args) {
		t.Errorf("changes = %q,%q, want both %q", ch1.Path, ch2.Path, string(args))
	}
	if calls != 1 {
		t.Fatalf("Preview ran %d times, want 1 (memo must reuse)", calls)
	}
}

func TestPreviewMemoizedDistinctArgsEachCompute(t *testing.T) {
	calls := 0
	tl := countingPreviewer{calls: &calls}
	ctx := WithPreviewMemo(context.Background())

	PreviewMemoized(ctx, tl, json.RawMessage(`{"path":"a"}`))
	PreviewMemoized(ctx, tl, json.RawMessage(`{"path":"b"}`))

	if calls != 2 {
		t.Fatalf("Preview ran %d times for 2 distinct args, want 2", calls)
	}
}

func TestPreviewMemoizedNoMemoStillComputes(t *testing.T) {
	calls := 0
	tl := countingPreviewer{calls: &calls}
	// Plain ctx, no memo seeded: behaves like a direct Preview, no caching.
	PreviewMemoized(context.Background(), tl, json.RawMessage(`{"path":"a"}`))
	PreviewMemoized(context.Background(), tl, json.RawMessage(`{"path":"a"}`))
	if calls != 2 {
		t.Fatalf("without a memo Preview ran %d times, want 2 (no caching)", calls)
	}
}

// TestPreviewMemoizedCachesErrorResult proves an errored preview is memoized
// too — the checkpoint path relies on the error being reported (to skip the
// snapshot), and re-running Preview on the retry would waste the read.
func TestPreviewMemoizedCachesErrorResult(t *testing.T) {
	calls := 0
	sentinel := errors.New("boom")
	tl := countingPreviewer{calls: &calls, err: sentinel}
	ctx := WithPreviewMemo(context.Background())
	args := json.RawMessage(`{"path":"x"}`)

	_, err1, _ := PreviewMemoized(ctx, tl, args)
	_, err2, _ := PreviewMemoized(ctx, tl, args)
	if !errors.Is(err1, sentinel) || !errors.Is(err2, sentinel) {
		t.Fatalf("errs = %v,%v, want sentinel both", err1, err2)
	}
	if calls != 1 {
		t.Fatalf("Preview ran %d times, want 1 (error result memoized)", calls)
	}
}

// TestPreviewMemoizedNonPreviewerReportsNotOk confirms a tool that isn't a
// Previewer returns ok=false rather than panicking.
func TestPreviewMemoizedNonPreviewerReportsNotOk(t *testing.T) {
	ctx := WithPreviewMemo(context.Background())
	if _, _, ok := PreviewMemoized(ctx, plainTool{}, json.RawMessage(`{}`)); ok {
		t.Fatal("non-Previewer tool: ok = true, want false")
	}
}

// plainTool implements Tool but not Previewer.
type plainTool struct{}

func (plainTool) Name() string                                     { return "plain" }
func (plainTool) Description() string                              { return "stub" }
func (plainTool) Schema() json.RawMessage                          { return json.RawMessage(`{}`) }
func (plainTool) ReadOnly() bool                                   { return false }
func (plainTool) Execute(context.Context, json.RawMessage) (string, error) { return "", nil }
