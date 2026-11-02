package control

import (
	"strings"
	"testing"
	"unicode/utf8"

	"reasonix/internal/contract/provider"
)

func TestLatestUserTaskSummaryKeepsMultiByteTextWhole(t *testing.T) {
	// 300 CJK characters are 900 bytes but well under the 800-rune bound the
	// agent applies to its own task summary, so the text must come back whole.
	task := strings.Repeat("修复登录页面", 50)
	msgs := []provider.Message{
		{Role: provider.RoleUser, Content: task},
		{Role: provider.RoleAssistant, Content: "ok"},
	}
	got := latestUserTaskSummary(msgs)
	if !utf8.ValidString(got) {
		t.Fatalf("task summary is not valid UTF-8: %q", got[len(got)-8:])
	}
	if got != task {
		t.Fatalf("task summary = %d runes, want the whole %d-rune message", utf8.RuneCountInString(got), utf8.RuneCountInString(task))
	}
}

func TestLatestUserTaskSummaryBoundsLongTextByRunes(t *testing.T) {
	task := strings.Repeat("界", 900)
	got := latestUserTaskSummary([]provider.Message{{Role: provider.RoleUser, Content: task}})
	if want := strings.Repeat("界", 800) + "…"; got != want {
		t.Fatalf("task summary = %d runes (valid UTF-8: %v), want 800 runes plus an ellipsis",
			utf8.RuneCountInString(got), utf8.ValidString(got))
	}
}

func TestLatestUserTaskSummarySkipsBlankUserTurns(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleUser, Content: "first task"},
		{Role: provider.RoleUser, Content: "   "},
		{Role: provider.RoleAssistant, Content: "done"},
	}
	if got := latestUserTaskSummary(msgs); got != "first task" {
		t.Fatalf("task summary = %q, want %q", got, "first task")
	}
	if got := latestUserTaskSummary(nil); got != "" {
		t.Fatalf("task summary of no messages = %q, want empty", got)
	}
}
