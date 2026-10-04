package tui

import (
	"strings"
	"testing"

	"reasonix/internal/base/i18n"
	"reasonix/internal/contract/eventwire"
)

// inChinese draws with the Chinese catalogue for the rest of the test.
func inChinese(t *testing.T) {
	t.Helper()
	was := i18n.M
	i18n.M = i18n.Chinese
	t.Cleanup(func() { i18n.M = was })
}

// The menu says how to drive it every time it opens, in the reader's
// language, as 1.x's does — not only past eight rows, and not in English.
func TestMenuHintIsAlwaysShownInTheUILanguage(t *testing.T) {
	inChinese(t)
	m, _ := testModel(t)
	m.composer.SetValue("/tr")
	m.Update(completionMsg{line: "/tr", c: Completion{Kind: "slash", To: 3, Items: []CompletionItem{{Label: "/tree", Insert: "/tree"}}}})
	if got := strings.Join(m.menuLines(), "\n"); !strings.Contains(got, i18n.Chinese.CompHintSlash) {
		t.Fatalf("menu:\n%s", got)
	}
}

func TestTranscriptRowsTheKernelDoesNotWordAreTranslated(t *testing.T) {
	inChinese(t)
	for _, got := range []string{
		stallText(&eventwire.ProgressWatch{Cause: "tokens", TokenMultiple: 3, PromptTokens: 90000}),
		stallText(&eventwire.ProgressWatch{Cause: "perseveration"}),
		stallText(&eventwire.ProgressWatch{IdleRounds: 6}),
	} {
		if strings.Contains(got, "Whether to keep going") {
			t.Fatalf("stall notice is English: %q", got)
		}
	}
	m, _ := testModel(t)
	m.picker = &sessionPicker{query: "zzz"}
	if got := strings.Join(m.pickerPanel(), "\n"); !strings.Contains(got, i18n.Chinese.ResumePickNoMatch) || strings.Contains(got, "Type to filter") {
		t.Fatalf("picker:\n%s", got)
	}
}

// A coded host notice is worded from its typed payload in the UI language, not
// shown as the kernel's English.
func TestCodedNoticesFollowTheUILanguage(t *testing.T) {
	inChinese(t)
	budget := &Item{Code: "context_budget", Text: "Context at 83% of the compaction threshold", Detail: `{"percent":83,"remaining":135785}`}
	if got := renderNotice(budget); !strings.Contains(got, "83%") || !strings.Contains(got, "135785") || strings.Contains(got, "compaction threshold") {
		t.Fatalf("budget notice = %q", got)
	}
	steer := &Item{Code: "unapplied_steer", Text: "Guidance was not applied: sync", Detail: "sync"}
	if got := renderNotice(steer); !strings.Contains(got, "引导没有生效") || !strings.Contains(got, "sync") || strings.Contains(got, "Guidance was not applied") {
		t.Fatalf("steer notice = %q", got)
	}
	broken := &Item{Code: "context_budget", Text: "kernel english", Detail: "not json"}
	if got := renderNotice(broken); !strings.Contains(got, "kernel english") {
		t.Fatalf("an undecodable payload must fall back to the kernel's text, got %q", got)
	}
}

func TestUnappliedSteerTextIsSanitisedAndCapped(t *testing.T) {
	inChinese(t)
	hostile := "\x1b[2Jwipe\x07" + strings.Repeat("x", 2000)
	got := renderNotice(&Item{Code: "unapplied_steer", Text: "english", Detail: hostile})
	if strings.Contains(got, "\x1b[2J") || strings.Contains(got, "\x07") {
		t.Fatalf("control sequences survived: %q", got)
	}
	if strings.Count(got, "x") > 450 {
		t.Fatalf("the user text was not capped: %d runes", strings.Count(got, "x"))
	}
}

func TestFoldedCodedNoticeShowsTheLatestPayload(t *testing.T) {
	tr := fold(
		eventwire.Event{Kind: "notice", Level: "warn", Code: "context_budget", Text: "a", Detail: `{"percent":76,"remaining":50}`},
		eventwire.Event{Kind: "notice", Level: "warn", Code: "context_budget", Text: "b", Detail: `{"percent":93,"remaining":9}`},
	)
	if len(tr.Items) != 1 || tr.Items[0].Count != 2 || tr.Items[0].Detail != `{"percent":93,"remaining":9}` {
		t.Fatalf("items = %+v", tr.Items)
	}
}
