package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/state/sessionstore"
)

// foldSummary is what compaction reports about turning a fold into a digest.
// It is populated even when the call fails, so telemetry still records how
// large the attempt was and what its bounded retries cost.
type foldSummary struct {
	Text       string
	Mode       string
	RequestID  string
	Usage      *provider.Usage
	FoldTokens int
	Spans      int
	// Coverage is what this digest kept of the facts its fold produced.
	Coverage foldCoverage
	// Input is what the summarizer was shown; nil means the whole fold.
	Input []provider.Message
	// CoverageBackstopped marks a digest the host had to complete itself.
	CoverageBackstopped bool
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

// foldOrDegrade archives a fold and, when the archive cannot be written and the
// fold is the only way out, converts that failure into a mechanical digest. The
// telemetry it returns always reports the original failure, even when the fold
// recovered from it.
func (a *contextWindow) foldOrDegrade(ctx context.Context, trigger string, mustFree bool, fold []provider.Message, instructions string, sourceTokens int) (foldSummary, CompactionTelemetry, error) {
	spend := compactionSpendFrom(ctx)
	res, err := a.foldToArchive(fold, instructions)
	tele := compactionTelemetryFromSummary(trigger, a.cacheState(), sourceTokens, res, spend.read())
	if err == nil {
		return res, tele, nil
	}
	cause := err.Error()
	if res, err = a.degradeFoldSummary(res, mustFree, fold, err); err != nil {
		tele.Error = cause
		return res, tele, err
	}
	tele = compactionTelemetryFromSummary(trigger, a.cacheState(), sourceTokens, res, spend.read())
	tele.Error = cause
	return res, tele, nil
}

const (
	// What the placeholder digest holds of the user's own words: each turn is
	// shortened to at most mechanicalUserTurnChars, and the whole list to about
	// mechanicalUserTotalChars, so a long session cannot make the stand-in as
	// large as the thing it replaces.
	mechanicalUserTurnChars  = 240
	mechanicalUserTotalChars = 16000
	mechanicalUserMinChars   = 60
)

// mechanicalFoldDigest stands in for a digest the summarizer could not produce.
// Saying the summary is missing stops the model reading the gap as "nothing was
// there". The user's words are the part nothing else holds, so they ride along,
// shortened and in order; assistant and tool content stays in the transcript.
func mechanicalFoldDigest(fold []provider.Message) string {
	return fmt.Sprintf("%d earlier message(s) were folded here to free context, but the automatic summary was unavailable. Ask the user if you need details from before this point.", len(fold)) + foldedUserWords(fold)
}

// foldedUserWords lists the user's own words from a fold, shortened and oldest
// first, as a block to append to a digest; empty when the user said nothing.
func foldedUserWords(fold []provider.Message) string {
	var b strings.Builder
	var said []string
	for _, m := range fold {
		if m.Role != provider.RoleUser || m.LocalOnly || isCompactionSummary(m) {
			continue
		}
		if text := sessionstore.UserMessageText(m); text != "" {
			said = append(said, text)
		}
	}
	if len(said) == 0 {
		return ""
	}
	each := min(mechanicalUserTurnChars, max(mechanicalUserMinChars, mechanicalUserTotalChars/len(said)))
	b.WriteString("\n\nWhat the user said in the folded part, shortened, oldest first:")
	used := 0
	for i, text := range said {
		line := shortenForDigest(text, each)
		if used+len(line) > mechanicalUserTotalChars {
			fmt.Fprintf(&b, "\n- [%d more earlier messages of the user are not listed]", len(said)-i)
			break
		}
		used += len(line)
		b.WriteString("\n- " + line)
	}
	return b.String()
}

// shortenForDigest collapses whitespace and cuts at a rune boundary.
func shortenForDigest(s string, limit int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= limit {
		return s
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}

// degradeFoldSummary turns a summarizer failure into a mechanical fold only
// where that fold is the only way out; below the ceiling the turn still goes
// out, so the error is kept and a recoverable view is not folded blind.
// Cancellation is the caller's decision, never a summarizer failure.
func (a *contextWindow) degradeFoldSummary(res foldSummary, mustFree bool, fold []provider.Message, cause error) (foldSummary, error) {
	if !mustFree || errors.Is(cause, context.Canceled) {
		return res, cause
	}
	a.svc.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn,
		Text:   "Context was compacted without a generated summary.",
		Detail: fmt.Sprintf("compaction summary unavailable (%v); folded mechanically", cause)})
	res.Text = mechanicalFoldDigest(fold)
	res.Mode = CompactionModeDegraded
	// Measured here too: without it a fold that lost everything reports the
	// same coverage as one that lost nothing.
	res.Coverage = measureFoldCoverage(fold, a.toolFactsFor, res.Text)
	return res, nil
}
