package agent

import (
	"context"

	"reasonix/internal/contract/provider"
)

// foldSummary is what compaction reports about turning a fold into a digest.
// It is populated even when the call fails, so telemetry still records how
// large the attempt was and what its bounded retries cost.
type foldSummary struct {
	Text       string
	Mode       string
	FoldTokens int
	Spans      int
}

// summaryInputTokens sizes messages as summarizer input in the real tokens
// summaryInputBudget is expressed in; estimateTextTokens counts characters and
// would read this fold as four times its cost. The rendered transcript is what
// actually rides in the request, so its per-message framing is measured rather
// than assumed away.
func (a *contextWindow) summaryInputTokens(msgs []provider.Message) int {
	if len(msgs) == 0 {
		return 0
	}
	return a.estimatedPromptTokens([]provider.Message{{
		Role: provider.RoleUser, Content: renderTranscript(msgs),
	}})
}

// foldArchived writes the fold to its archive file. A failure to write it is
// the fold's failure: nothing else stands in for the archive.
func (a *contextWindow) foldArchived(ctx context.Context, trigger string, fold []provider.Message, instructions string, sourceTokens int) (foldSummary, CompactionTelemetry, error) {
	res, err := a.foldToArchive(fold, instructions)
	tele := compactionTelemetryFromSummary(trigger, a.cacheState(), sourceTokens, res)
	if err != nil {
		tele.Error = err.Error()
	}
	return res, tele, err
}
