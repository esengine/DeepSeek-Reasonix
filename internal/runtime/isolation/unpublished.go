package isolation

import (
	"context"
	"reasonix/internal/safety/evidence"
)

func onlyPublishableChanges(ctx context.Context, e *Entry) bool {
	paths := make([]string, 0, len(e.Changes))
	for _, change := range e.Changes {
		paths = append(paths, change.Path)
	}
	return e.Baseline.OnlyPathsChanged(ctx, e.Cand.WorktreeRoot, paths, 50_000)
}

func sealedContentUnchanged(ctx context.Context, e *Entry) bool {
	return e.Evidence != nil && e.Evidence.FinishedContent != nil && onlyPublishableChanges(ctx, e) &&
		sameContent(evidence.CaptureContentSnapshot(ctx, e.Cand.WorktreeRoot, 50_000), *e.Evidence.FinishedContent)
}
