package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"reasonix/internal/base/i18n"
	"reasonix/internal/contract/eventwire"
)

// The card says what went unproven in the reader's language, keeps a kind it
// has no phrase for, and names what carried a clean verdict.
func TestReceiptCardNamesGapsAndEvidence(t *testing.T) {
	gaps := ansi.Strip(renderReceipt(&eventwire.CompletionReceipt{Verdict: "incomplete", Gaps: []eventwire.ReceiptGap{
		{Kind: "unverified_mutation", Detail: "a.go"},
		{Kind: "some_future_kind"},
	}}, 80))
	for _, want := range []string{i18n.M.ReceiptGapsHeader, "a.go", "some_future_kind"} {
		if !strings.Contains(gaps, want) {
			t.Fatalf("gap card lacks %q:\n%s", want, gaps)
		}
	}
	if phrase := i18n.M.ReceiptGapKinds["unverified_mutation"]; phrase != "" && !strings.Contains(gaps, phrase) {
		t.Fatalf("gap card lacks the phrase %q:\n%s", phrase, gaps)
	}

	clean := ansi.Strip(renderReceipt(&eventwire.CompletionReceipt{Verdict: "done",
		Changes:       []eventwire.ReceiptChange{{Path: "a.go"}},
		Verifications: []eventwire.ReceiptVerification{{Command: "go test ./...", Passed: true}}}, 80))
	if !strings.Contains(clean, i18n.M.ReceiptVerified) || !strings.Contains(clean, "go test ./...") {
		t.Fatalf("clean card = %q", clean)
	}
}
