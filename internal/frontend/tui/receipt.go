package tui

import (
	"fmt"
	"strings"

	"reasonix/internal/base/i18n"
	"reasonix/internal/contract/eventwire"
	"reasonix/internal/frontend/termrender"
)

// maxReceiptGaps bounds the card: one long enough to scroll is one nobody
// reads, and the count tail keeps the total honest.
const maxReceiptGaps = 5

// renderReceipt is the end-of-turn card. A clean turn gets one quiet line
// naming what carried it; otherwise the card spends its lines on what went
// unproven, then on what the turn itself declared.
func renderReceipt(r *eventwire.CompletionReceipt, width int) string {
	if r == nil {
		return ""
	}
	row := max(width-8, 24)
	if len(r.Gaps) == 0 {
		if r.Verdict != "done" {
			return ""
		}
		return "\n" + termrender.Dim("  ✓ "+oneLine(i18n.M.ReceiptVerified+receiptEvidence(r), max(width-4, 24)))
	}
	// A plain mark: terminals draw ⚠ as a two-cell emoji over a one-cell slot.
	lines := []string{termrender.Yellow("  ! " + i18n.M.ReceiptGapsHeader)}
	for _, g := range r.Gaps[:min(len(r.Gaps), maxReceiptGaps)] {
		// An unknown kind is still shown: dropping one silently is the failure
		// this card exists to prevent.
		phrase := i18n.M.ReceiptGapKinds[g.Kind]
		if phrase == "" {
			phrase = g.Kind
		}
		if d := strings.TrimSpace(g.Detail); d != "" {
			phrase += ": " + d
		}
		lines = append(lines, termrender.Dim("      "+oneLine(phrase, row)))
	}
	if rest := len(r.Gaps) - maxReceiptGaps; rest > 0 {
		lines = append(lines, termrender.Dim("      "+fmt.Sprintf(i18n.M.ReceiptMore, rest)))
	}
	lines = appendDeclared(lines, i18n.M.ReceiptRisksHeader, r.Risks, row)
	lines = appendDeclared(lines, i18n.M.ReceiptUnverifiedHeader, r.Unverified, row)
	return "\n" + strings.Join(lines, "\n")
}

func appendDeclared(lines []string, header string, items []string, row int) []string {
	if len(items) == 0 {
		return lines
	}
	lines = append(lines, termrender.Dim("  · "+header))
	for _, it := range items {
		lines = append(lines, termrender.Dim("      "+oneLine(it, row)))
	}
	return lines
}

// receiptEvidence names what carried a clean verdict, so "verified" is never
// an unsourced assertion.
func receiptEvidence(r *eventwire.CompletionReceipt) string {
	var parts []string
	if n := len(r.Changes); n > 0 {
		parts = append(parts, fmt.Sprintf(i18n.M.ReceiptChangedFmt, n))
	}
	for _, v := range r.Verifications {
		if v.Passed && !v.Stale && !v.Inconclusive {
			parts = append(parts, v.Command)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return " · " + strings.Join(parts, " · ")
}
