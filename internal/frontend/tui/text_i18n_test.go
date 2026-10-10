package tui

import (
	"strings"
	"testing"

	"reasonix/internal/base/i18n"
	"reasonix/internal/contract/event"
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
	steer := &Item{Code: "unapplied_steer", Text: "Guidance was not applied: sync", Detail: "sync"}
	if got := renderNotice(steer); !strings.Contains(got, "引导没有生效") || !strings.Contains(got, "sync") || strings.Contains(got, "Guidance was not applied") {
		t.Fatalf("steer notice = %q", got)
	}
	unknown := &Item{Code: "no_such_code", Text: "kernel english", Detail: "not json"}
	if got := renderNotice(unknown); !strings.Contains(got, "kernel english") {
		t.Fatalf("a code with no wording must fall back to the kernel's text, got %q", got)
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
		eventwire.Event{Kind: "notice", Level: "warn", Code: "unapplied_steer", Text: "a", Detail: "first"},
		eventwire.Event{Kind: "notice", Level: "warn", Code: "unapplied_steer", Text: "b", Detail: "second"},
	)
	if len(tr.Items) != 1 || tr.Items[0].Count != 2 || tr.Items[0].Detail != "second" {
		t.Fatalf("items = %+v", tr.Items)
	}
}

func TestCompactionNoticesAndCardNameTheirReasonInTheUILanguage(t *testing.T) {
	inChinese(t)
	failed := &Item{Code: "compact_failed", Text: "compaction failed: upstream said no", Detail: "summary_failed"}
	if got := renderNotice(failed); !strings.Contains(got, "压缩失败") || !strings.Contains(got, "请求失败") || strings.Contains(got, "upstream") {
		t.Fatalf("failed notice = %q", got)
	}
	declined := &Item{Code: "compact_declined", Text: "nothing to compact — x", Detail: "input_unchanged"}
	if got := renderNotice(declined); !strings.Contains(got, "无需压缩") || !strings.Contains(got, "没有变化") {
		t.Fatalf("declined notice = %q", got)
	}
	held := &Item{Code: "compact_held", Text: "Automatic compaction is paused: kernel english", Detail: "summary_failed"}
	if got := renderNotice(held); !strings.Contains(got, "自动压缩暂缓") || !strings.Contains(got, "请求失败") || strings.Contains(got, "kernel english") {
		t.Fatalf("held notice = %q", got)
	}
	unknown := &Item{Code: "compact_failed", Text: "compaction failed: kernel english", Detail: "future_code"}
	if got := renderNotice(unknown); !strings.Contains(got, "kernel english") {
		t.Fatalf("unknown code must keep the kernel text, got %q", got)
	}
	card := &Item{Done: true, Compaction: &eventwire.Compaction{Trigger: "auto", Code: "cancelled"}}
	if got := renderCompaction(card, 80); !strings.Contains(got, "压缩被取消") {
		t.Fatalf("cancelled card = %q", got)
	}
}

// A skipped extension is worded from its typed payload: named, located, and
// with the way out, in the UI language; a payload this build cannot read keeps
// the kernel's English.
func TestSkippedExtensionNoticeFollowsTheUILanguage(t *testing.T) {
	inChinese(t)
	detail := event.ExtensionSkipped{Extension: "aipush-ask-bridge", Point: "tool.before", Reason: event.ExtensionSkipReasonNoLiveSidecar}.Encode()
	got := renderNotice(&Item{Code: event.NoticeCodeExtensionSkipped, Text: "english", Detail: detail})
	for _, want := range []string{"aipush-ask-bridge", "tool.before", "配套后台程序没有运行", "/plugins"} {
		if !strings.Contains(got, want) {
			t.Fatalf("notice %q lacks %q", got, want)
		}
	}
	if strings.Contains(got, "{") || strings.Contains(got, "english") {
		t.Fatalf("payload or fallback leaked into %q", got)
	}
	if got := renderNotice(&Item{Code: event.NoticeCodeExtensionSkipped, Text: "kernel english", Detail: "not json"}); !strings.Contains(got, "kernel english") {
		t.Fatalf("unreadable payload must keep the kernel's text, got %q", got)
	}
}

func TestRecoveredInboxNoticeFollowsTheUILanguage(t *testing.T) {
	inChinese(t)
	detail := event.InboxRecovered{Count: 2}.Encode()
	got := renderNotice(&Item{Code: event.NoticeCodeInboxRecovered, Text: "english", Detail: detail})
	if !strings.Contains(got, "已恢复 2 条未完成的指令") || !strings.Contains(got, "/queue") {
		t.Fatalf("notice = %q", got)
	}
	if strings.Contains(got, "{") || strings.Contains(got, "english") {
		t.Fatalf("payload or fallback leaked into %q", got)
	}
	if got := renderNotice(&Item{Code: event.NoticeCodeInboxRecovered, Text: "kernel english", Detail: "not json"}); !strings.Contains(got, "kernel english") {
		t.Fatalf("unreadable payload must keep the kernel's text, got %q", got)
	}
}

func TestJobNoticesFollowTheUILanguage(t *testing.T) {
	inChinese(t)
	detail := event.JobNotice{Kind: "bash", ID: "bash-126", Label: "make build"}.Encode()
	got := renderNotice(&Item{Code: event.NoticeCodeJobFinished, Text: "background bash finished: bash-126", Detail: detail})
	if !strings.Contains(got, "后台任务已结束：make build") || strings.Contains(got, "{") || strings.Contains(got, "background") {
		t.Fatalf("finished = %q", got)
	}
	bare := event.JobNotice{Kind: "bash", ID: "bash-7"}.Encode()
	if got := renderNotice(&Item{Code: event.NoticeCodeJobKilled, Text: "english", Detail: bare}); !strings.Contains(got, "后台任务已终止：bash-7") {
		t.Fatalf("killed = %q", got)
	}
	failed := event.JobNotice{Kind: "bash", ID: "bash-9", Label: "make", Error: "exit status 2"}.Encode()
	if got := renderNotice(&Item{Code: event.NoticeCodeJobFailed, Text: "english", Detail: failed}); !strings.Contains(got, "后台任务 make 失败，需要处理: exit status 2") {
		t.Fatalf("failed = %q", got)
	}
	if got := renderNotice(&Item{Code: event.NoticeCodeJobFailed, Text: "kernel english", Detail: "old diagnostic"}); !strings.Contains(got, "kernel english") {
		t.Fatalf("a failure with no payload must keep its text, got %q", got)
	}
	if got := renderNotice(&Item{Text: "background bash finished: bash-126"}); !strings.Contains(got, "background bash finished: bash-126") {
		t.Fatalf("a replayed notice with no code must keep its text, got %q", got)
	}
	if got := renderNotice(&Item{Code: event.NoticeCodeJobFinished, Text: "kernel english", Detail: "not json"}); !strings.Contains(got, "kernel english") {
		t.Fatalf("unreadable payload must keep the kernel's text, got %q", got)
	}
}
