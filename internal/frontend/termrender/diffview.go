// Renders a unified diff as line-numbered, syntax-highlighted rows on
// Green/red background bars with a +/- gutter.
package termrender

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"

	"reasonix/internal/base/i18n"
	"reasonix/internal/contract/event"
)

const tabWidth = 4

const (
	bgDiffAdd = "\033[48;5;22m"
	bgDiffDel = "\033[48;5;52m"
	fgDiffAdd = "\033[1;38;5;46m"
	fgDiffDel = "\033[1;38;5;203m"
)

var (
	diffChromaFmt = formatters.Get("terminal256")
	hunkRE        = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)
)

// Resolve on each render so runtime theme switches and theme-sweep preview
// frames cannot retain syntax colours from the previous light/dark mode.
func activeDiffChromaStyle() *chroma.Style {
	mode := chroma.Dark
	if activeTheme.Name == "light" {
		mode = chroma.Light
	}
	return styles.GetForMode("github-dark", mode)
}

// DiffStat renders a change's "+A -B" tally, green/red, omitting a zero side.
func DiffStat(d event.FileDiff) string {
	parts := make([]string, 0, 2)
	if d.Added > 0 {
		parts = append(parts, Green("+"+strconv.Itoa(d.Added)))
	}
	if d.Removed > 0 {
		parts = append(parts, Red("-"+strconv.Itoa(d.Removed)))
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

// DiffBlock renders a writer call as a header line ("✎ name path  +A -B") plus
// the highlighted, folded diff body. Returns nil when there's no textual diff.
// A configured [cli].diff_formatter (e.g. delta) takes the whole diff on stdin
// and its stdout is re-emitted under the header with non-SGR escapes stripped,
// mirroring the fenced-diff path — the built-in body (and its fold) is the fallback.
func DiffBlock(name, args string, d event.FileDiff, width, maxLines int) []string {
	if d.Diff == "" {
		return nil
	}
	path := diffPath(args)
	header := "  " + ToolDot(name) + " " + ToolHead(name, path, width)
	if stat := DiffStat(d); stat != "" {
		header += "  " + stat
	}
	if rows, ok := formatToolDiff(dropFileHeaderPair(d.Diff), width); ok {
		return append([]string{header}, rows...)
	}
	return append([]string{header}, diffBody(d, path, width, maxLines)...)
}

// dropFileHeaderPair removes a leading "--- "/"+++ " pair, the same pair the
// built-in body drops positionally. The external formatter would otherwise
// re-emit that raw header — including an absolute host path in the label — which
// the card header already names.
func dropFileHeaderPair(diff string) string {
	lines := strings.SplitN(diff, "\n", 3)
	if len(lines) == 3 && strings.HasPrefix(lines[0], "--- ") && strings.HasPrefix(lines[1], "+++ ") {
		return lines[2]
	}
	return diff
}

// diffPreviewFold is on in production; the preview benchmark turns it off to
// price the whole-body highlight the fold replaced.
var diffPreviewFold = true

// diffBody renders the hunks with a line-number gutter, dropping the file and
// "@@" headers (a dim "⋮" marks each hunk jump) and folding past maxLines to a
// "+N more" footer. path selects the syntax lexer.
func diffBody(d event.FileDiff, path string, width, maxLines int) []string {
	if d.Diff == "" {
		return nil
	}
	// A diff that already carries colour (a formatter's output pasted back, a
	// file whose own content is coloured) must not be re-parsed: the SGR-prefixed
	// lines would be mis-read and the colours dropped, so show it verbatim.
	if hasSGR(d.Diff) {
		return verbatimDiffBody(d.Diff, width, maxLines)
	}
	src := strings.Split(strings.TrimRight(d.Diff, "\n"), "\n")
	// Drop the "--- a/… / +++ b/…" header pair positionally — matching the prefix
	// on every line would eat real content (a deleted SQL "-- x" renders "--- x",
	// an added "++ y" renders "+++ y").
	if len(src) >= 2 && strings.HasPrefix(src[0], "--- ") && strings.HasPrefix(src[1], "+++ ") {
		src = src[2:]
	}
	gw := gutterWidth(src)

	// The fold is known before any row is laid out: colourise only the rows kept
	// and count the rest. A colourised row costs ~1.4 ms (chroma via regexp2), so
	// a folded preview must not pay to highlight the tail it discards.
	keep, folded := 0, 0
	if diffPreviewFold {
		keep, folded = foldPreview(diffRowCount(src), maxLines)
	}

	var rows []string
	oldNo, newNo, hunks := 0, 0, 0
	for _, ln := range src {
		if ln == "" {
			continue
		}
		if diffPreviewFold && len(rows) >= keep {
			break
		}
		switch ln[0] {
		case '@':
			if m := hunkRE.FindStringSubmatch(ln); m != nil {
				oldNo, newNo = atoi(m[1]), atoi(m[3])
			}
			if hunks > 0 {
				rows = append(rows, "  "+Dim("⋮"))
			}
			hunks++
		case '+':
			rows = append(rows, diffBar('+', ln[1:], path, width, bgSGR(activeTheme.DiffAddBG), fgSGR(activeTheme.Success), newNo, gw))
			newNo++
		case '-':
			rows = append(rows, diffBar('-', ln[1:], path, width, bgSGR(activeTheme.DiffDelBG), fgSGR(activeTheme.Err), oldNo, gw))
			oldNo++
		case '\\':
			rows = append(rows, "  "+Dim(clampPlain(ln, width-2)))
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

	if !diffPreviewFold {
		rows, folded = foldRows(rows, maxLines)
	}
	if folded > 0 {
		rows = append(rows, "  "+Dim(fmt.Sprintf(i18n.M.DiffFoldedFmt, folded)))
	}
	return rows
}

// foldRows drops a fully laid-out body's tail past maxLines, returning the kept
// rows and how many it hid. The seam's slow path: production counts the fold
// from the source and never lays the tail out.
func foldRows(rows []string, maxLines int) ([]string, int) {
	if maxLines <= 0 || len(rows) <= maxLines {
		return rows, 0
	}
	folded := len(rows) - (maxLines - 1)
	return rows[:maxLines-1], folded
}

// foldPreview returns how many rows to draw and how many the fold hides for a
// body of total rows: the first maxLines-1 rows and the count of the rest, or
// every row and no fold when the body fits.
func foldPreview(total, maxLines int) (keep, folded int) {
	if maxLines <= 0 || total <= maxLines {
		return total, 0
	}
	keep = maxLines - 1
	return keep, total - keep
}

// diffRowCount is how many rows diffBody draws for src: one per non-blank line
// that is not a "@@ " header, plus the "⋮" separator each hunk header after the
// first adds.
func diffRowCount(src []string) int {
	total, hunks := 0, 0
	for _, ln := range src {
		switch {
		case ln == "":
		case ln[0] == '@':
			if hunks > 0 {
				total++
			}
			hunks++
		default:
			total++
		}
	}
	return total
}

// hasSGR reports whether s carries an ANSI CSI/SGR introducer. A diff already
// containing one was colourised externally and must not be re-parsed.
func hasSGR(s string) bool {
	return strings.Contains(s, "\x1b[")
}

// verbatimDiffBody renders an externally colourised diff as-is: no gutter, no
// background bars, no header-drop, no syntax re-highlight — just the lines,
// width-clamped and sanitised so a hostile payload cannot drive the terminal.
func verbatimDiffBody(diff string, width, maxLines int) []string {
	lines := strings.Split(strings.TrimRight(diff, "\n"), "\n")
	total := 0
	for _, ln := range lines {
		if ln != "" {
			total++
		}
	}
	keep, folded := 0, 0
	if diffPreviewFold {
		keep, folded = foldPreview(total, maxLines)
	}
	rows := make([]string, 0, keep)
	for _, ln := range lines {
		if ln == "" {
			continue
		}
		if diffPreviewFold && len(rows) >= keep {
			break
		}
		rows = append(rows, "  "+clampPlain(sgrOnly(ln), max(width-2, 1)))
	}
	if !diffPreviewFold {
		rows, folded = foldRows(rows, maxLines)
	}
	if folded > 0 {
		rows = append(rows, "  "+Dim(fmt.Sprintf(i18n.M.DiffFoldedFmt, folded)))
	}
	return rows
}

// sgrOnly keeps SGR (colour/style) escape sequences and drops every other
// control sequence — OSC (clipboard pokes), cursor moves, non-SGR CSI — so an
// externally rendered diff can colour the terminal without hijacking it.
func sgrOnly(s string) string {
	if !strings.ContainsRune(s, 0x1b) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] != 0x1b {
			b.WriteByte(s[i])
			i++
			continue
		}
		if i+1 >= len(s) {
			break
		}
		switch s[i+1] {
		case '[': // CSI … final byte in 0x40–0x7e
			j := i + 2
			for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
				j++
			}
			if j >= len(s) {
				return b.String() // unterminated: drop the tail
			}
			if s[j] == 'm' {
				b.WriteString(s[i : j+1])
			}
			i = j + 1
		case ']': // OSC … BEL or ST
			j := i + 2
			for j < len(s) {
				if s[j] == 0x07 {
					j++
					break
				}
				if s[j] == 0x1b && j+1 < len(s) && s[j+1] == '\\' {
					j += 2
					break
				}
				j++
			}
			i = j
		default: // other escape: drop ESC and its introducer byte
			i += 2
		}
	}
	return b.String()
}

// diffBar draws one added/removed row on a full-width coloured background. The
// bg is re-applied after every chroma reset — \033[0m would otherwise end the
// bar mid-line — and padded to the bar width so it runs edge to edge.
func diffBar(sign byte, code, path string, width int, bg, signFg string, lineNo, gw int) string {
	gutter := Dim(lpad(strconv.Itoa(lineNo), gw))
	barW := max(width-2-gw-1, 4)
	if !colorOn() {
		return "  " + gutter + " " + string(sign) + " " + clampPlain(code, barW-2)
	}
	hl := reapplyBG(highlightClamped(code, path, barW-2), bg)
	pad := max(barW-2-VisibleWidth(hl), 0)
	return "  " + gutter + " " + bg + signFg + string(sign) + ansiReset + bg + " " + hl + strings.Repeat(" ", pad) + ansiReset
}

// diffContext draws an unchanged line: the gutter, no background, code aligned
// under the +/- rows' code column.
func diffContext(code, path string, width, lineNo, gw int) string {
	gutter := Dim(lpad(strconv.Itoa(lineNo), gw))
	return "  " + gutter + "   " + highlightClamped(code, path, width-5-gw)
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

// highlightClamped highlights the whole line, then clamps it to w columns with
// every escape sequence kept. Clamping first would cut a token in two — a line
// ending inside an open string lexes the lone quote as an Error token, and the
// light theme's Error background then paints over the diff bar's own colour.
func highlightClamped(code, path string, w int) string {
	if w < 1 {
		w = 1
	}
	code = ExpandTabs(code)
	if !colorOn() {
		return Truncate(code, w, "")
	}
	return Truncate(highlightCode(path, code), w, "")
}

func clampPlain(s string, w int) string {
	if w < 1 {
		w = 1
	}
	return Truncate(ExpandTabs(s), w, "")
}

// ExpandTabs replaces tabs with spaces to the next tabWidth stop. A literal tab
// has zero StringWidth but the terminal advances it to a tab stop, so leaving
// tabs in a background-bar row overflows the bar — expand them so the measured
// width matches what's drawn.
func ExpandTabs(s string) string {
	if !strings.ContainsRune(s, '\t') {
		return s
	}
	var b strings.Builder
	col := 0
	for _, r := range s {
		if r == '\t' {
			n := tabWidth - col%tabWidth
			for range n {
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
	if diffChromaFmt.Format(&b, activeDiffChromaStyle(), it) != nil {
		return code
	}
	return strings.TrimRight(b.String(), "\n")
}
