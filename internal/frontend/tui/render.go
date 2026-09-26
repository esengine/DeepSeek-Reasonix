package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"reasonix/internal/base/i18n"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/eventwire"
	"reasonix/internal/contract/pricing"
	"reasonix/internal/frontend/termrender"
)

const (
	toolPreviewLines = 4
	diffPreviewLines = 24
)

// renderItem is a settled row as it goes into the scrollback. shown is how much
// of an answer's text an earlier print already carried.
func renderItem(it *Item, width, shown int, hideRail bool) string {
	switch it.Kind {
	case ItemUser:
		mark := "› "
		if it.Steer {
			mark = "↳ "
		}
		rows := strings.Split(ansi.Hardwrap(strings.TrimRight(it.Text, "\n"), max(width-5, 10), true), "\n")
		for i, r := range rows {
			rows[i] = "  " + termrender.Accent(mark+r)
			mark = "  "
		}
		return "\n" + strings.Join(rows, "\n")
	case ItemSay:
		// Thinking with nothing said after it is a step, not an answer: it gets
		// its marker and no speaker header.
		if strings.TrimSpace(it.Text) == "" {
			if it.Reasoning == "" {
				return ""
			}
			return "\n" + thought(it, width)
		}
		if shown >= len(it.Text) && shown > 0 {
			return ""
		}
		return withThought(it, shown, width, renderSayPart(it.Text[shown:], shown == 0, width, hideRail))
	case ItemTool:
		return renderTool(it, width)
	case ItemApproval:
		// An allowed call speaks for itself in the card that follows; only a
		// refusal leaves something the reader would otherwise not see.
		if it.Verdict != "deny" && it.Verdict != "revise_plan" && it.Verdict != "exit_plan" {
			return ""
		}
		return termrender.Dim(fmt.Sprintf("  ✗ declined %s %s", it.Approval.Tool, oneLine(it.Approval.Subject, width-20)))
	case ItemAsk:
		prompt := "question"
		if len(it.Ask.Questions) > 0 {
			prompt = it.Ask.Questions[0].Prompt
		}
		return termrender.Dim("  ? " + oneLine(prompt, width/2) + " → " + oneLine(it.Verdict, width/2))
	case ItemNotice:
		return renderNotice(it)
	case ItemCompaction:
		return termrender.Dim("  ⟲ " + i18n.M.CompactionTitle)
	case ItemReceipt:
		return renderReceipt(it.Receipt, width)
	case ItemUsage:
		return renderUsage(it.Usage, width)
	}
	return ""
}

// renderSayPart renders a stretch of an answer. Only the first stretch carries
// the speaker's header; the rest continue under it. A stretch is cut after a
// blank line, so each later one starts a new block and gets that line back.
func renderSayPart(text string, first bool, width int, hideRail bool) string {
	if first {
		return "\n" + termrender.AssistantBlock(text, width, hideRail)
	}
	return "\n" + indent(strings.TrimRight(termrender.RenderMarkdown(text, max(width-2, 10), hideRail), "\n"), "  ")
}

// withThought puts the thinking marker above the first stretch of an answer.
func withThought(it *Item, shown, width int, out string) string {
	if shown > 0 || it.Reasoning == "" {
		return out
	}
	return "\n" + thought(it, width) + "\n" + out
}

// thought is the thinking marker, and the thinking itself once a full-screen
// row has been opened. The marker always sits on the row's second line.
func thought(it *Item, width int) string {
	mark := "▎"
	switch it.Fold {
	case foldShut:
		mark = "▸"
	case foldOpen:
		mark = "▾"
	}
	hint := ""
	if it.Fold != foldFixed {
		hint = " (Ctrl+O)"
	}
	lines := []string{termrender.Dim("  " + mark + " " + fmt.Sprintf(i18n.M.ChatThoughtForFmt, (it.ThoughtMs+500)/1000) + hint)}
	if it.Fold == foldOpen {
		// Styled per row: the transcript is split into rows after rendering, and
		// one style spanning several would reach only the first of them.
		for l := range strings.SplitSeq(ansi.Wrap(strings.TrimSpace(it.Reasoning), max(width-6, 10), ""), "\n") {
			lines = append(lines, termrender.Dim("    "+l))
		}
	}
	return strings.Join(lines, "\n")
}

const (
	connector         = "  ⎿  "
	shellPreviewLines = 10
	shellExpandLines  = 200
)

func renderTool(it *Item, width int) string {
	t := it.Tool
	if t.Diff != "" {
		return "\n" + strings.Join(termrender.DiffBlock(t.Name, t.Args, event.FileDiff{Diff: t.Diff, Added: t.Added, Removed: t.Removed}, width, diffPreviewLines), "\n")
	}
	lines := []string{termrender.ToolCard(t.Name, t.Args, width)}
	avail := width - len([]rune(connector))
	switch {
	case t.Err != "":
		lines = append(lines, termrender.Dim(connector)+termrender.Red(oneLine(t.Err, avail)))
	case it.Running:
		if last := lastLine(t.Output); last != "" {
			lines = append(lines, termrender.Dim(connector+oneLine(last, avail)))
		}
	default:
		lines = append(lines, outputSummary(t.Name, t.Output, avail, it.Fold)...)
	}
	if n := len(it.Children); n > 0 {
		lines = append(lines, termrender.Dim(connector+fmt.Sprintf("%d sub-agent call(s)", n)))
	}
	return "\n" + strings.Join(lines, "\n")
}

// outputSummary leaves a marker of a finished call: a shell command's first
// lines, since what it printed is what the user ran it for, and a line count
// for any other tool, whose output the model already has.
func outputSummary(name, out string, width int, f outputFold) []string {
	out = strings.TrimRight(out, "\n")
	if out == "" {
		return nil
	}
	src := strings.Split(out, "\n")
	if !termrender.IsShellTool(name) {
		return []string{termrender.Dim(connector + fmt.Sprintf("%d lines", len(src)))}
	}
	limit := shellPreviewLines
	if f == foldOpen {
		limit = shellExpandLines
	}
	shown := src[:min(len(src), limit)]
	lines := make([]string, 0, len(shown)+1)
	for i, l := range shown {
		gutter := strings.Repeat(" ", len([]rune(connector)))
		if i == 0 {
			gutter = connector
		}
		lines = append(lines, termrender.Dim(gutter+oneLine(l, width)))
	}
	hint := ""
	if f != foldFixed {
		hint = " (Ctrl+B)"
	}
	if extra := len(src) - len(shown); extra > 0 {
		lines = append(lines, termrender.Dim(strings.Repeat(" ", len([]rune(connector)))+fmt.Sprintf("… %d more lines", extra)+hint))
	}
	return lines
}

// renderUsage is what one model request cost, under a quiet rule: history,
// so it stays in the scrollback in a quieter voice than the footer.
func renderUsage(u *eventwire.Usage, width int) string {
	total := shortTokens(u.TotalTokens) + " tok"
	if u.Estimated {
		total = "≈" + total
	}
	groups := []string{total}
	if u.PromptTokens > 0 {
		fresh := u.CacheMissTokens
		if fresh == 0 {
			fresh = max(u.PromptTokens-u.CacheHitTokens, 0)
		}
		groups = append(groups, "in "+shortTokens(u.PromptTokens), "cached "+shortTokens(u.CacheHitTokens), "new "+shortTokens(fresh))
	}
	groups = append(groups, "out "+shortTokens(u.CompletionTokens))
	if u.ReasoningTokens > 0 {
		groups = append(groups, "reasoning "+shortTokens(u.ReasoningTokens))
	}
	if u.Cost > 0 {
		code := u.CurrencyCode
		if code == "" {
			code = u.Currency
		}
		groups = append(groups, fmt.Sprintf("≈%s%.4f", pricing.CurrencySymbol(code), u.Cost))
	}
	if u.Estimated {
		groups = append(groups, "estimated")
	}
	for i, g := range groups {
		groups[i] = footerValue(g)
	}
	rule := footerIndent + termrender.ThemeFg(termrender.ActiveTheme().Border, strings.Repeat("─", max(width-1-len(footerIndent), 1)))
	return "\n" + rule + "\n" + footerIndent + footerLabel(i18n.M.ChatTurnReceiptLabel) + "  " + strings.Join(groups, footerLabel(" · "))
}

func renderNotice(it *Item) string {
	mark := termrender.Dim("  · ")
	switch it.Level {
	case "error":
		mark = termrender.Red("  ✗ ")
	case "warn", "warning":
		mark = termrender.Yellow("  ! ")
	}
	text := it.Text
	if it.Count > 1 {
		text += termrender.Dim(fmt.Sprintf(" (×%d)", it.Count))
	}
	return mark + text
}

// oneLine fits s on one row of width cells: a wide rune takes two, so the cut
// is measured on screen rather than in runes.
func oneLine(s string, width int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if width > 1 && termrender.VisibleWidth(s) > width {
		return ansi.Truncate(s, width, "…")
	}
	return s
}

func lastLine(s string) string {
	s = strings.TrimRight(s, "\n")
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		return s[i+1:]
	}
	return s
}

func indent(block, prefix string) string {
	lines := strings.Split(block, "\n")
	for i, l := range lines {
		if l != "" {
			lines[i] = prefix + l
		}
	}
	return strings.Join(lines, "\n")
}
