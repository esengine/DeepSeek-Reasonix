package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"reasonix/internal/base/i18n"
	"reasonix/internal/contract/eventwire"
)

// screenText is what the full-screen transcript holds, as plain text.
func screenText(m *model) string {
	var parts []string
	for _, b := range m.scr.blocks {
		parts = append(parts, ansi.Strip(b.render(100, false)))
	}
	return strings.Join(parts, "\n")
}

// /help answers on this screen, grouped the way 1.x groups it, and lists the
// commands the session actually answers — the TUI's own among them.
func TestHelpListsTheCommandsThisSessionAnswers(t *testing.T) {
	m, k := testModel(t)
	typeText(m, "/help")
	run(m, press(m, "enter"))
	if strings.Contains(strings.Join(k.seen(), "\n"), "POST /submit") {
		t.Fatal("/help went to the kernel")
	}
	got := screenText(m)
	for _, want := range []string{"commands", "built-in", "/compact", "custom", "/deploy", "skills",
		"subagent · review the diff", "MCP prompts", "/mcp__docs__search", "/help", "/resume",
		"type a command, or press Tab after / for completion"} {
		if !strings.Contains(got, want) {
			t.Fatalf("help is missing %q:\n%s", want, got)
		}
	}
}

// A finished fold shows its digest and how much it saved, as 1.x's card does.
func TestCompactionCardShowsTheDigestAndItsSize(t *testing.T) {
	m, _ := testModel(t)
	apply(m,
		eventwire.Event{Kind: "compaction_started", Compaction: &eventwire.Compaction{Trigger: "manual"}},
		eventwire.Event{Kind: "compaction_done", Compaction: &eventwire.Compaction{Trigger: "manual", Messages: 12,
			Summary: "## Goal\nadd div", SourceTokens: 14200, ProjectionTokens: 10600}})
	got := screenText(m)
	for _, want := range []string{"◆ " + i18n.M.CompactionTitle + " · 12 " + i18n.M.CompactionUnit + " · " + i18n.M.CompactionManual,
		"│ ## Goal", "│ add div", i18n.M.CompactionEstimatedTokens + ": ~14.2K → ~10.6K"} {
		if !strings.Contains(got, want) {
			t.Fatalf("card is missing %q:\n%s", want, got)
		}
	}
}

// /context's breakdown is shown under its summary, not dropped.
func TestContextReportShowsItsBreakdown(t *testing.T) {
	m, _ := testModel(t)
	apply(m, eventwire.Event{Kind: "notice", Code: "context_report", Text: "context 0 / 1,000,000 (0%)",
		Detail: "window            1,000,000\nthresholds        fold 800,000"})
	got := screenText(m)
	if !strings.Contains(got, "context 0 / 1,000,000") || !strings.Contains(got, "thresholds        fold 800,000") {
		t.Fatalf("report:\n%s", got)
	}
}

// A fold that wrote no digest folded nothing: no card claims it did.
func TestCompactionWithoutADigestDrawsNoCard(t *testing.T) {
	m, _ := testModel(t)
	apply(m,
		eventwire.Event{Kind: "compaction_started", Compaction: &eventwire.Compaction{Trigger: "manual"}},
		eventwire.Event{Kind: "compaction_done", Compaction: &eventwire.Compaction{Trigger: "manual"}})
	if got := screenText(m); strings.Contains(got, i18n.M.CompactionTitle) {
		t.Fatalf("an empty fold drew a card:\n%s", got)
	}
}

// A built-in named first is that built-in whatever follows it, as in 1.x:
// the line answers here and never reaches the kernel as prose.
func TestBuiltinWithTrailingArgumentsStaysLocal(t *testing.T) {
	for _, line := range []string{"/help x", "/? x", "/clear x", "/version x", "/mouse x", "/resume x", "/setup x"} {
		m, k := testModel(t)
		typeText(m, line)
		run(m, press(m, "enter"))
		if strings.Contains(strings.Join(k.seen(), "\n"), "POST /submit") {
			t.Fatalf("%q went to the kernel", line)
		}
	}
	m, _ := testModel(t)
	typeText(m, "/clear x")
	run(m, press(m, "enter"))
	if m.clearing == nil {
		t.Fatal("/clear with an argument skipped its confirmation")
	}
}
