package cli

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// mdRenderer turns the model's markdown answer into ANSI-styled terminal text
// using the brand palette. It implements only the constructs a chat-style
// model reliably emits — headings, paragraphs, lists, fenced code, blockquotes,
// strong/em/code-spans, links, thematic breaks — and degrades to plain text
// for anything else. Word-wrapping respects CJK widths and skips over ANSI
// SGR codes when counting columns.
type mdRenderer struct {
	md    goldmark.Markdown
	width int
}

func newMarkdownRenderer(width int) *mdRenderer {
	if width <= 0 {
		width = 80
	}
	// Enable the GFM table extension so | header | rows | get parsed into
	// a Table node rather than falling through as a literal text block.
	return &mdRenderer{
		md: goldmark.New(
			goldmark.WithExtensions(extension.Table),
			goldmark.WithParserOptions(
				parser.WithInlineParsers(util.Prioritized(&mathParser{}, 150)),
			),
		),
		width: width,
	}
}

func italic(s string) string {
	if !colorEnabled {
		return s
	}
	return "\033[3m" + s + "\033[0m"
}

// Render parses input as markdown and returns ANSI-styled output with a
// trailing newline. Empty input returns an empty string so callers can
// reliably distinguish "nothing to draw" from "draw a blank line".
func (r *mdRenderer) Render(input string) string {
	if strings.TrimSpace(input) == "" {
		return ""
	}
	input = fixCJKEmphasis(normalizeMath(input))
	src := []byte(input)
	doc := r.md.Parser().Parse(text.NewReader(src))
	var buf strings.Builder
	r.renderBlocks(&buf, doc, src, 0)
	out := strings.TrimRight(buf.String(), "\n")
	if out == "" {
		return ""
	}
	return out + "\n"
}

// fixCJKEmphasis works around goldmark's CommonMark parser not recognising
// CJK punctuation as Unicode punctuation: a closing ** is only right-flanking
// when the char before it is punctuation, so **X，**Y (， = U+FF0C) is not bold.
// Inserting a space after such a closer fixes the flanking. The space must go
// only on a *closer* — putting it after an opener (，**X** → ，** X**) would
// instead break the left-flanking — so emphasis open/close is tracked by a
// running toggle. Inline code spans and fenced blocks are passed through so
// literal ** inside code is never touched.
func fixCJKEmphasis(s string) string {
	runes := []rune(s)
	n := len(runes)
	var b strings.Builder
	b.Grow(len(s) + 16)

	inFenced := false   // inside ``` fenced code block
	inCode := false     // inside ` inline code span
	inEmphasis := false // between an opening ** and its closer

	for i := 0; i < n; i++ {
		r := runes[i]

		// Fenced code block: ``` toggles in/out.
		if r == '`' && i+2 < n && runes[i+1] == '`' && runes[i+2] == '`' {
			inFenced = !inFenced
			b.WriteString("```")
			i += 2
			continue
		}
		// Inline code span: ` toggles in/out (but not inside fenced blocks).
		if r == '`' && !inFenced {
			inCode = !inCode
			b.WriteRune(r)
			continue
		}
		// Inside code — pass through verbatim.
		if inCode || inFenced {
			b.WriteRune(r)
			continue
		}
		// Emphasis cannot span a hard line break; reset so an unclosed ** on a
		// previous line can't make the next line's opener look like a closer.
		if r == '\n' {
			inEmphasis = false
			b.WriteRune(r)
			continue
		}

		if r == '*' && i+1 < n && runes[i+1] == '*' {
			b.WriteString("**")
			i++
			inEmphasis = !inEmphasis

			// Only a closer (emphasis just ended) hugging CJK punctuation needs
			// the trailing space; the same space after an opener would break it.
			if !inEmphasis && i >= 2 && !isSpace(runes[i-2]) && isCJKPunct(runes[i-2]) {
				b.WriteByte(' ')
			}
			continue
		}

		b.WriteRune(r)
	}
	return b.String()
}

// isCJKPunct reports whether r is a CJK full-width punctuation character.
// These are not classified as Unicode punctuation by the CommonMark spec,
// which breaks the "right-flanking delimiter run" check for emphasis.
func isCJKPunct(r rune) bool {
	if r <= 0x7F {
		return false // ASCII punctuation is handled correctly by CommonMark
	}
	// Fast path: common CJK punctuation ranges.
	switch {
	case r >= 0x3000 && r <= 0x303F: // CJK Symbols and Punctuation (。、etc.)
		return true
	case r >= 0xFF01 && r <= 0xFF0F: // Fullwidth Forms I (! " # $ etc.)
		return true
	case r >= 0xFF1A && r <= 0xFF20: // Fullwidth Forms II (: ; < = etc.)
		return true
	case r >= 0xFF3B && r <= 0xFF3F: // Fullwidth Forms III ([ \ ] ^ _)
		return true
	case r >= 0xFF5B && r <= 0xFF65: // Fullwidth Forms IV ({ | } ~ etc.)
		return true
	}
	// Fallback: any non-ASCII punctuation (e.g. Tibetan, Armenian).
	return unicode.IsPunct(r)
}

// isSpace reports whether r is a whitespace character.
func isSpace(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n' || r == '\r'
}

func (r *mdRenderer) renderBlocks(buf *strings.Builder, parent ast.Node, src []byte, indent int) {
	for c := parent.FirstChild(); c != nil; c = c.NextSibling() {
		r.renderBlock(buf, c, src, indent)
	}
}

func (r *mdRenderer) renderBlock(buf *strings.Builder, node ast.Node, src []byte, indent int) {
	switch n := node.(type) {
	case *ast.Heading:
		r.renderHeading(buf, n, src, indent)
	case *ast.Paragraph:
		r.renderParagraph(buf, n, src, indent)
	case *ast.TextBlock:
		// TextBlock is goldmark's container for tight-list-item inline content
		// (no trailing blank). Treat it like a paragraph but skip the spacer.
		r.renderTextBlock(buf, n, src, indent)
	case *ast.List:
		r.renderList(buf, n, src, indent)
	case *ast.FencedCodeBlock, *ast.CodeBlock:
		r.renderFenced(buf, n, src, indent)
	case *ast.Blockquote:
		r.renderBlockquote(buf, n, src, indent)
	case *extast.Table:
		r.renderTable(buf, n, src, indent)
	case *ast.ThematicBreak:
		w := r.width - indent
		if w < 8 {
			w = 8
		}
		buf.WriteString(strings.Repeat(" ", indent))
		buf.WriteString(dim(strings.Repeat("─", w)))
		buf.WriteString("\n\n")
	default:
		// Unknown block: drop into children rather than dropping content.
		r.renderBlocks(buf, node, src, indent)
	}
}

func (r *mdRenderer) renderHeading(buf *strings.Builder, n *ast.Heading, src []byte, indent int) {
	inline := r.collectInline(n, src)
	buf.WriteString(strings.Repeat(" ", indent))
	buf.WriteString(bold(accent(inline)))
	buf.WriteString("\n")
	// Level-1 headings get an accent underline; deeper levels rely on
	// bold+colour alone so the hierarchy reads at a glance without piling
	// on visual weight on every "###" in a long response.
	if n.Level == 1 {
		buf.WriteString(strings.Repeat(" ", indent))
		buf.WriteString(accent(strings.Repeat("─", visibleWidth(inline))))
		buf.WriteString("\n")
	}
	buf.WriteString("\n")
}

func (r *mdRenderer) renderParagraph(buf *strings.Builder, n *ast.Paragraph, src []byte, indent int) {
	r.renderInlineBlock(buf, n, src, indent, true)
}

func (r *mdRenderer) renderTextBlock(buf *strings.Builder, n *ast.TextBlock, src []byte, indent int) {
	r.renderInlineBlock(buf, n, src, indent, false)
}

func (r *mdRenderer) renderInlineBlock(buf *strings.Builder, n ast.Node, src []byte, indent int, trailingBlank bool) {
	inline := r.collectInline(n, src)
	prefix := strings.Repeat(" ", indent)
	wrapped := wrapAnsi(inline, r.width-indent)
	for _, line := range strings.Split(wrapped, "\n") {
		buf.WriteString(prefix)
		buf.WriteString(line)
		buf.WriteString("\n")
	}
	if trailingBlank {
		buf.WriteString("\n")
	}
}

func (r *mdRenderer) renderList(buf *strings.Builder, n *ast.List, src []byte, indent int) {
	idx := 1
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		item, ok := c.(*ast.ListItem)
		if !ok {
			continue
		}
		var marker string
		if n.IsOrdered() {
			marker = fmt.Sprintf("%d.", idx)
			idx++
		} else {
			marker = "•"
		}
		buf.WriteString(strings.Repeat(" ", indent))
		buf.WriteString(accent(marker) + " ")
		markerW := visibleWidth(marker) + 1

		first := item.FirstChild()
		// goldmark uses TextBlock for tight list items, Paragraph for loose
		// ones; treat both as the marker-line carrier so the inline content
		// lands next to the bullet either way.
		inlineHost := inlineCarrier(first)
		if inlineHost != nil {
			inline := r.collectInline(inlineHost, src)
			wrapped := wrapAnsi(inline, r.width-indent-markerW)
			lines := strings.Split(wrapped, "\n")
			buf.WriteString(lines[0] + "\n")
			for _, l := range lines[1:] {
				buf.WriteString(strings.Repeat(" ", indent+markerW))
				buf.WriteString(l + "\n")
			}
			for s := first.NextSibling(); s != nil; s = s.NextSibling() {
				r.renderBlock(buf, s, src, indent+markerW)
			}
		} else {
			buf.WriteString("\n")
			r.renderBlocks(buf, item, src, indent+2)
		}
	}
	buf.WriteString("\n")
}

func (r *mdRenderer) renderFenced(buf *strings.Builder, n ast.Node, src []byte, indent int) {
	prefix := strings.Repeat(" ", indent) + dim("│ ")
	for i := 0; i < n.Lines().Len(); i++ {
		l := n.Lines().At(i)
		line := strings.TrimRight(string(l.Value(src)), "\n")
		buf.WriteString(prefix)
		buf.WriteString(accent(line))
		buf.WriteString("\n")
	}
	buf.WriteString("\n")
}

func (r *mdRenderer) renderBlockquote(buf *strings.Builder, n *ast.Blockquote, src []byte, indent int) {
	var inner strings.Builder
	r.renderBlocks(&inner, n, src, 0)
	prefix := strings.Repeat(" ", indent) + dim("▎ ")
	for _, line := range strings.Split(strings.TrimRight(inner.String(), "\n"), "\n") {
		buf.WriteString(prefix)
		buf.WriteString(dim(line))
		buf.WriteString("\n")
	}
	buf.WriteString("\n")
}

// collectInline walks an inline subtree and returns its ANSI-styled flat text.
func (r *mdRenderer) collectInline(n ast.Node, src []byte) string {
	var b strings.Builder
	r.appendInline(&b, n, src)
	return b.String()
}

func (r *mdRenderer) appendInline(b *strings.Builder, n ast.Node, src []byte) {
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		switch v := c.(type) {
		case *ast.Text:
			b.Write(v.Segment.Value(src))
			switch {
			case v.HardLineBreak():
				b.WriteByte('\n')
			case v.SoftLineBreak():
				b.WriteByte(' ')
			}
		case *ast.Emphasis:
			var inner strings.Builder
			r.appendInline(&inner, v, src)
			if v.Level == 2 {
				b.WriteString(bold(inner.String()))
			} else {
				b.WriteString(italic(inner.String()))
			}
		case *ast.CodeSpan:
			var inner strings.Builder
			r.appendInline(&inner, v, src)
			b.WriteString(accent(inner.String()))
		case *ast.Link:
			var inner strings.Builder
			r.appendInline(&inner, v, src)
			b.WriteString(inner.String())
			b.WriteString(dim(" (" + string(v.Destination) + ")"))
		case *ast.AutoLink:
			b.WriteString(string(v.URL(src)))
		case *ast.RawHTML:
			// drop — rare in chat output and would print as literal escapes
		case *mathNode:
			b.WriteString(italic(v.value))
		case *ast.String:
			b.Write(v.Value)
		default:
			r.appendInline(b, c, src)
		}
	}
}

// renderTable lays out a GFM table as terminal columns separated by dim
// "│" rails with a "─┼─" rule under the header. Column widths auto-fit the
// widest cell in each column and are capped to a fair share of the terminal
// width so a wide table can't push the input off-screen. Long cells are
// wrapped across multiple visual rows (the whole logical row inflates to
// the tallest cell), not truncated, so no content is lost. Alignment is
// left-only — Markdown's ":---:" hints are read but not honoured yet.
func (r *mdRenderer) renderTable(buf *strings.Builder, n *extast.Table, src []byte, indent int) {
	var header []string
	var rows [][]string

	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		switch row := c.(type) {
		case *extast.TableHeader:
			header = r.collectCells(row, src)
		case *extast.TableRow:
			rows = append(rows, r.collectCells(row, src))
		}
	}
	if len(header) == 0 && len(rows) == 0 {
		return
	}

	cols := len(header)
	for _, row := range rows {
		if len(row) > cols {
			cols = len(row)
		}
	}
	if cols == 0 {
		return
	}

	// Initial widths fit the widest cell content per column.
	widths := make([]int, cols)
	pick := func(i, w int) {
		if i < cols && w > widths[i] {
			widths[i] = w
		}
	}
	for i, h := range header {
		pick(i, visibleWidth(h))
	}
	for _, row := range rows {
		for i, c := range row {
			pick(i, visibleWidth(c))
		}
	}

	// Cap each column so the whole table fits the terminal: total = sum of
	// widths + separators (3 chars each) + indent. Distribute the budget
	// proportionally to the natural widths so columns with rich content
	// keep more space than narrow ones.
	available := r.width - indent - 3*(cols-1)
	if available < cols*3 {
		available = cols * 3
	}
	total := 0
	for _, w := range widths {
		total += w
	}
	if total > available {
		for i := range widths {
			widths[i] = widths[i] * available / total
			if widths[i] < 3 {
				widths[i] = 3
			}
		}
	}

	prefix := strings.Repeat(" ", indent)
	sep := dim(" │ ")

	if len(header) > 0 {
		r.renderTableRow(buf, prefix, sep, header, widths, true)
		buf.WriteString(prefix)
		for i := range widths {
			if i > 0 {
				buf.WriteString(dim("─┼─"))
			}
			buf.WriteString(dim(strings.Repeat("─", widths[i])))
		}
		buf.WriteByte('\n')
	}
	for _, row := range rows {
		r.renderTableRow(buf, prefix, sep, row, widths, false)
	}
	buf.WriteByte('\n')
}

// renderTableRow lays out one logical row across multiple visual rows when
// any cell wraps. wrapAnsi handles per-cell word + hard-break wrapping; the
// row's visual height = max wrapped lines across all cells. Cells that ran
// out of content get padded with spaces so the rail "│" stays aligned.
func (r *mdRenderer) renderTableRow(buf *strings.Builder, prefix, sep string, cells []string, widths []int, isHeader bool) {
	cols := len(widths)
	wrapped := make([][]string, cols)
	maxLines := 1
	for i := 0; i < cols; i++ {
		var text string
		if i < len(cells) {
			text = cells[i]
		}
		wrapped[i] = strings.Split(wrapAnsi(text, widths[i]), "\n")
		if len(wrapped[i]) > maxLines {
			maxLines = len(wrapped[i])
		}
	}
	for line := 0; line < maxLines; line++ {
		buf.WriteString(prefix)
		for i := 0; i < cols; i++ {
			if i > 0 {
				buf.WriteString(sep)
			}
			var cell string
			if line < len(wrapped[i]) {
				cell = wrapped[i][line]
			}
			padded := padRight(cell, widths[i])
			if isHeader {
				padded = bold(padded)
			}
			buf.WriteString(padded)
		}
		buf.WriteByte('\n')
	}
}

// collectCells walks a TableHeader / TableRow node and pulls each TableCell's
// inline content as an ANSI-styled string. Non-cell children are ignored.
func (r *mdRenderer) collectCells(parent ast.Node, src []byte) []string {
	var out []string
	for c := parent.FirstChild(); c != nil; c = c.NextSibling() {
		if cell, ok := c.(*extast.TableCell); ok {
			out = append(out, strings.TrimSpace(r.collectInline(cell, src)))
		}
	}
	return out
}

// inlineCarrier returns n when it's a paragraph or text-block (both hold
// inline runs), else nil. Used by list rendering so the marker line gets the
// inline content regardless of whether the list is tight or loose.
func inlineCarrier(n ast.Node) ast.Node {
	switch n.(type) {
	case *ast.Paragraph, *ast.TextBlock:
		return n
	}
	return nil
}

// wrapAnsi word-wraps text to width columns, hard-breaking any single word too
// wide to fit on its own line — the path CJK takes, having no inter-word spaces.
// ANSI SGR escapes are preserved and counted as zero width; wide chars count as
// two columns. Thin wrapper over x/ansi's Wrap (already in the dep tree).
func wrapAnsi(text string, width int) string {
	if width < 4 {
		width = 4
	}
	return ansi.Wrap(text, width, "")
}
