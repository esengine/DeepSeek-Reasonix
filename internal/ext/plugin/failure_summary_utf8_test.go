package plugin

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSummarizeFailureErrorCutsOnRuneBoundary(t *testing.T) {
	msg := strings.Repeat("a", 499) + strings.Repeat("é", 10)
	got := summarizeFailureError(hostDiagnosticCause(msg))
	if !utf8.ValidString(got) {
		t.Fatalf("summary is not valid UTF-8: %q", got[len(got)-8:])
	}
	if want := strings.Repeat("a", 499) + "..."; got != want {
		t.Fatalf("summary tail = %q, want %q", got[len(got)-8:], want[len(want)-8:])
	}
}

func TestSummarizeFailureErrorKeepsASCIICut(t *testing.T) {
	got := summarizeFailureError(hostDiagnosticCause(strings.Repeat("b", 600)))
	if want := strings.Repeat("b", 500) + "..."; got != want {
		t.Fatalf("len(summary) = %d, want %d", len(got), len(want))
	}
}
