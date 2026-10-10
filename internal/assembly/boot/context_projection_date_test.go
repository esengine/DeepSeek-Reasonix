package boot

import (
	"strings"
	"testing"
	"time"

	"reasonix/internal/contract/provider"
)

func userMessageWith(t *testing.T, req provider.Request, text string) string {
	t.Helper()
	for _, m := range req.Messages {
		if m.Role == provider.RoleUser && strings.Contains(m.Content, text) {
			return m.Content
		}
	}
	t.Fatalf("no user message carries %q", text)
	return ""
}

// The calendar date is host state the model cannot read for itself. It rides
// the turn, not the prefix; it is delivered once per day, so repeat turns on one
// day are byte-identical; and the first turn after midnight carries the new one.
func TestProjectionDateRidesTheTurnAndChangesOnlyWithTheDay(t *testing.T) {
	zone := time.FixedZone("UTC+8", 8*3600)
	now := time.Date(2026, 10, 10, 9, 0, 0, 0, zone)
	h := newProjectionHarnessAt(t, "ctxproj-date", "", "", func() time.Time { return now })

	first := h.turn("turn-alpha")
	if got := projectionOf(t, first, "turn-alpha"); !strings.Contains(got, "<current-date>\nToday's date is 2026-10-10 (Saturday); the local time zone is UTC+08:00.\n</current-date>") {
		t.Fatalf("the date did not reach the request's user turn:\n%s", got)
	}
	if strings.Contains(systemOf(first), "2026-10-10") {
		t.Fatal("cache-boundary: the date reached the system prefix")
	}

	now = time.Date(2026, 10, 10, 23, 59, 0, 0, zone)
	second := h.turn("turn-beta")
	if strings.Contains(projectionOf(t, second, "turn-beta"), "<current-date>") {
		t.Fatalf("a second turn on the same day carried the date again:\n%s", projectionOf(t, second, "turn-beta"))
	}
	if a, b := userMessageWith(t, first, "turn-alpha"), userMessageWith(t, second, "turn-alpha"); a != b {
		t.Fatalf("the history the second turn replays differs from what the first sent:\nfirst diff site: %q", firstDivergence(a, b))
	}
	if a, b := systemOf(first), systemOf(second); a != b {
		t.Fatalf("cache-boundary: the prefix moved between turns:\nfirst diff site: %q", firstDivergence(a, b))
	}

	now = time.Date(2026, 10, 11, 0, 1, 0, 0, zone)
	third := h.turn("turn-gamma")
	if got := projectionOf(t, third, "turn-gamma"); !strings.Contains(got, "Today's date is 2026-10-11 (Sunday)") {
		t.Fatalf("the first turn after midnight did not carry the new date:\n%s", got)
	}
	if a, b := systemOf(first), systemOf(third); a != b {
		t.Fatalf("cache-boundary: crossing midnight moved the prefix:\nfirst diff site: %q", firstDivergence(a, b))
	}
}
