package cli

import (
	"fmt"
	"strings"

	"reasonix/internal/event"
	"reasonix/internal/i18n"
)

// maxReceiptGapLines bounds the card. A receipt long enough to scroll is a
// receipt nobody reads, and the count tail keeps the total honest.
const maxReceiptGapLines = 5

// renderReceiptCard turns the receipt into scrollback lines. The clean case
// gets one quiet line: the user just watched the tools run, so repeating the
// work back is noise. What no transcript carries is the absence, and that is
// what the card spends its lines on.
func renderReceiptCard(r *event.CompletionReceipt, width int) []string {
	if r == nil {
		return nil
	}
	gaps := receiptGapLines(r, width)
	if len(gaps) == 0 {
		if r.Verdict != "done" {
			return nil
		}
		return []string{wrapForViewport("  ✓ "+i18n.M.ReceiptVerified+receiptEvidenceTail(r), width, activeCLITheme.muted)}
	}
	lines := []string{wrapForViewport("  ⚠ "+i18n.M.ReceiptGapsHeader, width, activeCLITheme.warn)}
	shown := min(len(gaps), maxReceiptGapLines)
	for _, gap := range gaps[:shown] {
		lines = append(lines, wrapForViewport("      "+gap, width, activeCLITheme.muted))
	}
	if rest := len(gaps) - shown; rest > 0 {
		lines = append(lines, wrapForViewport("      "+fmt.Sprintf(i18n.M.ReceiptMore, rest), width, activeCLITheme.muted))
	}
	if len(r.Risks) > 0 {
		lines = append(lines, wrapForViewport("  · "+i18n.M.ReceiptRisksHeader, width, activeCLITheme.muted))
		for _, risk := range r.Risks {
			lines = append(lines, wrapForViewport("      "+risk, width, activeCLITheme.muted))
		}
	}
	return lines
}

// receiptGapLines renders each gap as "<phrase>: <detail>", falling back to the
// raw kind when a catalogue has no phrase for it — an unknown kind must still
// be shown, because silently dropping one is the failure this card exists to
// prevent.
func receiptGapLines(r *event.CompletionReceipt, width int) []string {
	out := make([]string, 0, len(r.Gaps))
	for _, gap := range r.Gaps {
		phrase := i18n.M.ReceiptGapKinds[gap.Kind]
		if phrase == "" {
			phrase = gap.Kind
		}
		if detail := receiptDetail(gap.Detail); detail != "" {
			phrase += ": " + detail
		}
		out = append(out, clipToLine(phrase, width))
	}
	return out
}

// receiptEvidenceTail names what carried the clean verdict, so "verified" is
// never an unsourced assertion.
func receiptEvidenceTail(r *event.CompletionReceipt) string {
	var parts []string
	if n := len(r.Changes); n > 0 {
		parts = append(parts, fmt.Sprintf("%d changed", n))
	}
	for _, v := range r.Verifications {
		if v.Passed && !v.Stale {
			parts = append(parts, v.Command)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return " · " + strings.Join(parts, " · ")
}

// commitReceipt appends the card to the transcript.
func (m *chatTUI) commitReceipt(r *event.CompletionReceipt) {
	for _, line := range renderReceiptCard(r, m.width) {
		m.commitLine(line)
	}
}

// receiptGapIndent is the visual indent every gap line carries; the budget
// below is what remains of the row after it.
const receiptGapIndent = 6

// clipToLine keeps one gap on one row. The phrase length varies by catalogue,
// so the budget is the row, not the detail: a per-detail cap that fits English
// overflows the moment a longer translation prefixes it.
func clipToLine(s string, width int) string {
	budget := max(width-receiptGapIndent-2, 24)
	runes := []rune(s)
	if len(runes) <= budget {
		return s
	}
	return strings.TrimSpace(string(runes[:budget])) + "…"
}

// receiptDetail drops a leading `cd <path> &&`: the run is already there, so
// the command itself is the part that identifies it.
func receiptDetail(detail string) string {
	detail = strings.TrimSpace(detail)
	if rest, ok := strings.CutPrefix(detail, "cd "); ok {
		if _, after, found := strings.Cut(rest, " && "); found {
			detail = strings.TrimSpace(after)
		}
	}
	return detail
}
