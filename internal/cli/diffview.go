// Renders a unified diff as line-numbered, syntax-highlighted rows on
// green/red background bars with a +/- gutter.
package cli

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/charmbracelet/x/ansi"

	"reasonix/internal/event"
	"reasonix/internal/i18n"
)

const tabWidth = 4

const (
	// diffFoldLimit is the max lines to show in a diff when folding is enabled
	// (/diff-fold toggle). 0 means show all lines.
	diffFoldLimit = 40

	bgDiffAdd = "\033[48;5;22m"
	bgDiffDel = "\033[48;5;52m"
	fgDiffAdd = "\033[1;38;5;46m"
	fgDiffDel = "\033[1;38;5;203m"
)

var (
	diffChromaStyle = styles.Get("github-dark")
	diffChromaFmt   = formatters.Get("terminal256")
	hunkRE          = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)
)

// diffStat renders a change's "+A -B" tally, green/red, omitting a zero side.
func diffStat(d event.FileDiff) string {
	parts := make([]string, 0, 2)
	if d.Added > 0 {
		parts = append(parts, green("+"+strconv.Itoa(d.Added)))
	}
	if d.Removed > 0 {
		parts = append(parts, red("-"+strconv.Itoa(d.Removed)))
	}
	return strings.Join(parts, " ")
}

func diffPath(args string) string {
	var p struct {
		Path string `json:"path"`
	}
	_ = json.Unmarshal([]byte(args), &p)
	return p.Path
}

// diffBlock renders a writer call as a header line ("✎ name path  +A -B") plus
// the highlighted, folded diff body. Returns nil when there's no textual diff.
func diffBlock(name, args string, d event.FileDiff, width, maxLines int) []string {
	if d.Diff == "" {
		return nil
	}
	path := diffPath(args)
	header := "  " + toolDot(name) + " " + toolHead(name, path, width)
	if stat := diffStat(d); stat != "" {
		header += "  " + stat
	}
	return append([]string{header}, diffBody(d, path, width, maxLines)...)
}

// diffBody renders the hunks with a line-number gutter, dropping the file and
// "@@" headers (a dim "⋮" marks each hunk jump) and folding past maxLines to a
// "+N more" footer. path selects the syntax lexer.
func diffBody(d event.FileDiff, path string, width, maxLines int) []string {
	if d.Diff == "" {
		return nil
	}
	src := strings.Split(strings.TrimRight(d.Diff, "\n"), "\n")
	// Drop the "--- a/… / +++ b/…" header pair positionally — matching the prefix
	// on every line would eat real content (a deleted SQL "-- x" renders "--- x",
	// an added "++ y" renders "+++ y").
	if len(src) >= 2 && strings.HasPrefix(src[0], "--- ") && strings.HasPrefix(src[1], "+++ ") {
		src = src[2:]
	}
	gw := gutterWidth(src)

	var rows []string
	oldNo, newNo, hunks := 0, 0, 0
	for _, ln := range src {
		if ln == "" {
			continue
		}
		switch ln[0] {
		case '@':
			if m := hunkRE.FindStringSubmatch(ln); m != nil {
				oldNo, newNo = atoi(m[1]), atoi(m[3])
			}
			if hunks > 0 {
				rows = append(rows, "  "+dim("⋮"))
			}
			hunks++
		case '+':
			rows = append(rows, diffBar('+', ln[1:], path, width, bgSGR(activeCLITheme.diffAddBG), fgSGR(activeCLITheme.success), newNo, gw))
			newNo++
		case '-':
			rows = append(rows, diffBar('-', ln[1:], path, width, bgSGR(activeCLITheme.diffDelBG), fgSGR(activeCLITheme.err), oldNo, gw))
			oldNo++
		case '\\':
			rows = append(rows, "  "+dim(clampPlain(ln, width-2)))
		default:
			code := ln
			if ln[0] == ' ' {
				code = ln[1:]
			}
			rows = append(rows, diffContext(code, path, width, newNo, gw))
			oldNo++
			newNo++
		}
	}

	if maxLines > 0 && len(rows) > maxLines {
		folded := len(rows) - (maxLines - 1)
		rows = rows[:maxLines-1]
		rows = append(rows, "  "+dim(fmt.Sprintf(i18n.M.DiffFoldedFmt, folded)))
	}
	return rows
}

// diffBar draws one added/removed row on a full-width coloured background. The
// bg is re-applied after every chroma reset — \033[0m would otherwise end the
// bar mid-line — and padded to the bar width so it runs edge to edge.
func diffBar(sign byte, code, path string, width int, bg, signFg string, lineNo, gw int) string {
	gutter := dim(lpad(strconv.Itoa(lineNo), gw))
	barW := width - 2 - gw - 1
	if barW < 4 {
		barW = 4
	}
	code = clampPlain(code, barW-2)
	if !colorEnabled {
		return "  " + gutter + " " + string(sign) + " " + code
	}
	hl := reapplyBG(highlightCode(path, code), bg)
	pad := barW - 2 - visibleWidth(code)
	if pad < 0 {
		pad = 0
	}
	return "  " + gutter + " " + bg + signFg + string(sign) + ansiReset + bg + " " + hl + strings.Repeat(" ", pad) + ansiReset
}

// diffContext draws an unchanged line: the gutter, no background, code aligned
// under the +/- rows' code column.
func diffContext(code, path string, width, lineNo, gw int) string {
	gutter := dim(lpad(strconv.Itoa(lineNo), gw))
	return "  " + gutter + "   " + highlightClamped(code, path, width-4-gw)
}

func gutterWidth(lines []string) int {
	max := 0
	for _, ln := range lines {
		m := hunkRE.FindStringSubmatch(ln)
		if m == nil {
			continue
		}
		for _, p := range [][2]int{{1, 2}, {3, 4}} {
			end := atoi(m[p[0]])
			if m[p[1]] != "" {
				end += atoi(m[p[1]])
			} else {
				end++
			}
			if end > max {
				max = end
			}
		}
	}
	if w := len(strconv.Itoa(max)); w > 2 {
		return w
	}
	return 2
}

func lpad(s string, w int) string {
	if len(s) >= w {
		return s
	}
	return strings.Repeat(" ", w-len(s)) + s
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

func highlightClamped(code, path string, w int) string {
	c := clampPlain(code, w)
	if !colorEnabled {
		return c
	}
	return highlightCode(path, c)
}

func clampPlain(s string, w int) string {
	if w < 1 {
		w = 1
	}
	return ansi.Truncate(expandTabs(s), w, "")
}

// expandTabs replaces tabs with spaces to the next tabWidth stop. A literal tab
// has zero StringWidth but the terminal advances it to a tab stop, so leaving
// tabs in a background-bar row overflows the bar — expand them so the measured
// width matches what's drawn.
func expandTabs(s string) string {
	if !strings.ContainsRune(s, '\t') {
		return s
	}
	var b strings.Builder
	col := 0
	for _, r := range s {
		if r == '\t' {
			n := tabWidth - col%tabWidth
			for i := 0; i < n; i++ {
				b.WriteByte(' ')
			}
			col += n
			continue
		}
		b.WriteRune(r)
		col++
	}
	return b.String()
}

func reapplyBG(s, bg string) string {
	if s == "" {
		return s
	}
	return strings.ReplaceAll(s, ansiReset, ansiReset+bg)
}

// highlightCode returns code with chroma ANSI foreground colours for the lexer
// matched by path (plain fallback for unknown types). It emits no background, so
// it composes onto a diff bar; the caller re-applies the bar background.
func highlightCode(path, code string) string {
	if code == "" {
		return code
	}
	lexer := lexers.Match(path)
	if lexer == nil {
		lexer = lexers.Fallback
	}
	it, err := lexer.Tokenise(nil, code)
	if err != nil {
		return code
	}
	var b strings.Builder
	if diffChromaFmt.Format(&b, diffChromaStyle, it) != nil {
		return code
	}
	return strings.TrimRight(b.String(), "\n")
}
