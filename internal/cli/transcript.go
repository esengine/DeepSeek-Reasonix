package cli

import (
	"encoding/base64"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/atotto/clipboard"
	"github.com/charmbracelet/x/ansi"

	"reasonix/internal/provider"
)

type transcriptSourceKind uint8

const (
	transcriptSourceFixed transcriptSourceKind = iota
	transcriptSourceMarkdown
	transcriptSourceUser
	transcriptSourceReasoning
	transcriptSourceToolCard
	transcriptSourceBanner
	transcriptSourceReplayBundle
	transcriptSourceTurnReceipt
)

// transcriptSource retains only the semantic inputs needed to reproduce a
// width-dependent transcript block. It deliberately sits beside []string
// instead of replacing it: the rendered slice remains the fast path for every
// frame and preserves the many index-based live tool/reasoning updates.
type transcriptSource struct {
	kind     transcriptSourceKind
	raw      string
	aux      string
	planMode bool
	maxLines int
	history  []provider.Message
}

func (m *chatTUI) ensureTranscriptSources() {
	if len(m.transcriptSources) > len(m.transcript) {
		m.transcriptSources = m.transcriptSources[:len(m.transcript)]
	}
	for len(m.transcriptSources) < len(m.transcript) {
		m.transcriptSources = append(m.transcriptSources, transcriptSource{kind: transcriptSourceFixed})
	}
}

func (m *chatTUI) appendTranscriptBlock(rendered string, source transcriptSource) {
	m.ensureTranscriptSources()
	m.transcript = append(m.transcript, rendered)
	m.transcriptSources = append(m.transcriptSources, source)
}

func (m *chatTUI) setTranscriptBlock(index int, rendered string, source transcriptSource) {
	if index < 0 || index >= len(m.transcript) {
		return
	}
	m.ensureTranscriptSources()
	m.transcript[index] = rendered
	m.transcriptSources[index] = source
}

func (m *chatTUI) removeTranscriptBlock(index int) {
	if index < 0 || index >= len(m.transcript) {
		return
	}
	m.ensureTranscriptSources()
	m.transcript = append(m.transcript[:index], m.transcript[index+1:]...)
	m.transcriptSources = append(m.transcriptSources[:index], m.transcriptSources[index+1:]...)
}

func (m *chatTUI) truncateTranscriptBlocks(length int) {
	length = min(max(length, 0), len(m.transcript))
	m.ensureTranscriptSources()
	m.transcript = m.transcript[:length]
	m.transcriptSources = m.transcriptSources[:length]
}

func (m *chatTUI) renderTranscriptSource(source transcriptSource, terminalWidth int) string {
	contentWidth := transcriptContentWidth(terminalWidth, m.nativeScrollback)
	switch source.kind {
	case transcriptSourceMarkdown:
		return renderAssistantMarkdown(source.raw, contentWidth)
	case transcriptSourceUser:
		return renderUserBubble(source.raw, terminalWidth, source.planMode)
	case transcriptSourceReasoning:
		return reasoningBlock(source.raw, terminalWidth, source.maxLines)
	case transcriptSourceToolCard:
		return toolCard(source.raw, source.aux, terminalWidth)
	case transcriptSourceBanner:
		return strings.TrimRight(renderTUIBanner(m.label, source.raw, contentWidth), "\n")
	case transcriptSourceReplayBundle:
		return m.renderReplayBundle(source, contentWidth, renderAssistantMarkdown)
	case transcriptSourceTurnReceipt:
		return renderTurnReceiptBand(source.raw, contentWidth)
	default:
		return ""
	}
}

func (m chatTUI) renderReplayBundle(
	source transcriptSource,
	contentWidth int,
	renderAssistant func(string, int) string,
) string {
	var b strings.Builder
	b.WriteString(renderTUIBanner(m.label, source.raw, contentWidth))
	for _, section := range replaySectionsForWithAssistantRenderer(
		source.history,
		contentWidth,
		renderAssistant,
	) {
		b.WriteString(section)
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m chatTUI) renderReplayBundleCopy(
	source transcriptSource,
	contentWidth int,
	prefix string,
) string {
	assistantIndex := 0
	return m.renderReplayBundle(source, contentWidth, func(raw string, width int) string {
		messagePrefix := prefix + "-" + strconv.Itoa(assistantIndex)
		assistantIndex++
		return renderAssistantMarkdownCopy(raw, width, messagePrefix)
	})
}

const assistantTranscriptIndent = "  "

// renderAssistantMarkdown gives assistant prose the same explicit transcript
// identity that user, reasoning, tool, and receipt blocks already have. The
// body keeps a restrained two-cell gutter instead of using a heavy card, and
// rendering at the reduced width keeps every indented row inside the viewport.
func renderAssistantMarkdown(raw string, contentWidth int) string {
	contentWidth = max(contentWidth, 1)
	indent := assistantTranscriptIndent
	if contentWidth <= visibleWidth(indent) {
		indent = ""
	}
	bodyWidth := max(contentWidth-visibleWidth(indent), 1)
	renderer := newMarkdownRenderer(bodyWidth)
	rendered := renderer.Render(raw)
	if rendered == "" {
		rendered = raw
	}
	body := strings.TrimRight(rendered, "\n")
	header := indent + accent("◆") + " " + bold("Reasonix")
	if body == "" {
		return header
	}
	return header + "\n\n" + indentTranscriptBlock(body, indent)
}

// renderAssistantMarkdownCopy mirrors renderAssistantMarkdown's visible output
// and adds zero-width math markers for on-demand clipboard reconstruction.
func renderAssistantMarkdownCopy(raw string, contentWidth int, prefix string) string {
	contentWidth = max(contentWidth, 1)
	indent := assistantTranscriptIndent
	if contentWidth <= visibleWidth(indent) {
		indent = ""
	}
	bodyWidth := max(contentWidth-visibleWidth(indent), 1)
	renderer := newMarkdownRenderer(bodyWidth)
	rendered := renderer.RenderCopy(raw, prefix)
	if rendered == "" {
		rendered = raw
	}
	body := strings.TrimRight(rendered, "\n")
	header := indent + accent("◆") + " " + bold("Reasonix")
	if body == "" {
		return header
	}
	return header + "\n\n" + indentTranscriptBlock(body, indent)
}

func indentTranscriptBlock(block, indent string) string {
	if indent == "" || block == "" {
		return block
	}
	lines := strings.Split(block, "\n")
	for i, line := range lines {
		if line != "" {
			lines[i] = indent + line
		}
	}
	return strings.Join(lines, "\n")
}

func renderTurnReceiptBand(receipt string, contentWidth int) string {
	if strings.TrimSpace(ansi.Strip(receipt)) == "" {
		return ""
	}
	contentWidth = max(contentWidth, 1)
	if contentWidth <= visibleWidth(statusFooterIndent) {
		rule := themeFg(activeCLITheme.border, strings.Repeat("─", contentWidth))
		return rule + "\n" + wrapTranscript(receipt, contentWidth)
	}
	indent := statusFooterIndent
	innerWidth := contentWidth - visibleWidth(indent)
	rule := indent + themeFg(activeCLITheme.border, strings.Repeat("─", innerWidth))
	body := wrapTranscript(receipt, contentWidth)
	return rule + "\n" + body
}

func (m *chatTUI) reflowTranscript(terminalWidth int) {
	m.ensureTranscriptSources()
	for i, source := range m.transcriptSources {
		if source.kind == transcriptSourceFixed {
			continue
		}
		m.transcript[i] = m.renderTranscriptSource(source, terminalWidth)
	}
}

func (m *chatTUI) commitTranscriptSource(source transcriptSource) {
	rendered := m.renderTranscriptSource(source, m.width)
	*m.pendingCommit = append(*m.pendingCommit, rendered)
	m.appendTranscriptBlock(rendered, source)
}

const (
	copyMathStartPrefix = "\x1b]1337;reasonix-copy-math="
	copyMathEndPrefix   = "\x1b]1337;reasonix-copy-math-end="
	copyMathTerminator  = "\x07"
)

func copyMathStartMarker(id, source string) string {
	encoded := base64.RawURLEncoding.EncodeToString([]byte(source))
	return copyMathStartPrefix + id + ";" + encoded + copyMathTerminator
}

func copyMathEndMarker(id string) string {
	return copyMathEndPrefix + id + copyMathTerminator
}

// buildCopyTranscript renders semantic Markdown only when a copy is requested.
// The visible text stays byte-for-byte equivalent after ANSI stripping, while
// math markers retain the source needed to map display cells back to LaTeX.
func (m chatTUI) buildCopyTranscript(contentWidth int) (string, int, bool) {
	if len(m.transcriptSources) != len(m.transcript) {
		return "", 0, false
	}
	var b strings.Builder
	markers := 0
	for i, source := range m.transcriptSources {
		if i > 0 {
			b.WriteByte('\n')
		}
		switch source.kind {
		case transcriptSourceMarkdown:
			rendered := renderAssistantMarkdownCopy(source.raw, contentWidth, strconv.Itoa(i))
			markers += strings.Count(rendered, copyMathStartPrefix)
			b.WriteString(rendered)
		case transcriptSourceReplayBundle:
			rendered := m.renderReplayBundleCopy(source, contentWidth, strconv.Itoa(i))
			markers += strings.Count(rendered, copyMathStartPrefix)
			b.WriteString(rendered)
		default:
			b.WriteString(m.transcript[i])
		}
	}
	return b.String(), markers, true
}

// transcriptResizeAnchor identifies the transcript block at the top of the
// viewport plus the relative row within it. Reflow can change a block's line
// count, so preserving a raw Y offset would jump to unrelated content.
type transcriptResizeAnchor struct {
	block    int
	fraction float64
	valid    bool
}

func captureTranscriptResizeAnchor(blocks []string, width, yOffset int) transcriptResizeAnchor {
	if width <= 0 || len(blocks) == 0 {
		return transcriptResizeAnchor{}
	}
	remaining := max(yOffset, 0)
	for i, block := range blocks {
		lines := transcriptBlockLineCount(block, width)
		if remaining < lines {
			fraction := 0.0
			if lines > 1 {
				fraction = float64(remaining) / float64(lines-1)
			}
			return transcriptResizeAnchor{block: i, fraction: fraction, valid: true}
		}
		remaining -= lines
	}
	return transcriptResizeAnchor{block: len(blocks) - 1, fraction: 1, valid: true}
}

func (a transcriptResizeAnchor) yOffset(blocks []string, width int) int {
	if !a.valid || len(blocks) == 0 || width <= 0 {
		return 0
	}
	block := min(max(a.block, 0), len(blocks)-1)
	offset := 0
	for i := 0; i < block; i++ {
		offset += transcriptBlockLineCount(blocks[i], width)
	}
	lines := transcriptBlockLineCount(blocks[block], width)
	if lines > 1 {
		offset += int(math.Round(a.fraction * float64(lines-1)))
	}
	return offset
}

func transcriptBlockLineCount(block string, width int) int {
	return strings.Count(wrapTranscript(block, width), "\n") + 1
}

// wrapTranscript wraps the joined transcript to width for the viewport, keeping
// SGR balanced across wrap points. ansi.Hardwrap leaves a style that spans a
// break open at the line end (e.g. a wrapped dim link tail), which bleeds the
// attribute into the padding and the next row on stricter terminals (Warp).
// lipgloss closes the active style at each line end and reopens it at the next.
func wrapTranscript(s string, width int) string {
	if width <= 0 {
		return s
	}
	return lipgloss.NewStyle().Width(width).Render(s)
}

type clipboardCopyMsg struct {
	text       string
	err        error
	osc52      bool
	statusHint bool
	seq        int
}

var writeNativeClipboardText = clipboard.WriteAll

func remoteClipboardSession() bool {
	return os.Getenv("SSH_CONNECTION") != "" || os.Getenv("SSH_CLIENT") != "" || os.Getenv("SSH_TTY") != ""
}

// copyToClipboard prefers the operating system clipboard in a local session,
// where success can be verified (pbcopy on macOS, the selected Wayland/X11
// utility on Linux, and the Win32 clipboard on Windows). SSH cannot reliably
// reach the user's local desktop clipboard, so it deliberately falls back to
// OSC 52. A failed local write also falls back, but the UI labels that path as
// an unverified terminal request rather than claiming a successful copy.
func copyToClipboard(text string) tea.Cmd {
	return copyToClipboardWithStatus(text, 0, false)
}

func copyToClipboardWithStatus(text string, seq int, statusHint bool) tea.Cmd {
	return func() tea.Msg {
		if remoteClipboardSession() {
			return clipboardCopyMsg{text: text, osc52: true, statusHint: statusHint, seq: seq}
		}
		return clipboardCopyMsg{
			text:       text,
			err:        writeNativeClipboardText(text),
			statusHint: statusHint,
			seq:        seq,
		}
	}
}

// copyNoticeTTL is how long the "copied to clipboard" status-line hint stays
// visible after a selection copy (mouse drag, right-click, or Ctrl+C) before
// copyNoticeExpireMsg clears it.
const copyNoticeTTL = 1500 * time.Millisecond

// copyNoticeExpireMsg clears the transient copy notice — but only if seq still
// matches m.copyNoticeSeq, so an older copy's timer can't stomp a newer notice
// (e.g. drag-copy immediately followed by a right-click re-copy).
type copyNoticeExpireMsg struct{ seq int }

// copySelectionWithNotice copies text to the clipboard and arms the status-line
// "copied to clipboard" hint, bumping copyNoticeSeq so any in-flight expiry tick
// from a prior copy is superseded rather than racing this one.
func (m *chatTUI) copySelectionWithNotice(text string) tea.Cmd {
	m.copyNoticeSeq++
	seq := m.copyNoticeSeq
	return copyToClipboardWithStatus(text, seq, true)
}

func copyNoticeExpire(seq int) tea.Cmd {
	return tea.Tick(copyNoticeTTL, func(time.Time) tea.Msg {
		return copyNoticeExpireMsg{seq: seq}
	})
}

// autoScrollMsg drives one step of edge-drag scrolling while a selection is held
// against the top or bottom of the transcript.
type autoScrollMsg struct{}

func autoScrollTick() tea.Cmd {
	return tea.Tick(80*time.Millisecond, func(time.Time) tea.Msg { return autoScrollMsg{} })
}

// edgeScrollDir reports the auto-scroll direction for a drag at screen row y in
// a viewport of `height` rows: -1 at the top edge, +1 at the bottom, 0 between.
func edgeScrollDir(y, height int) int {
	switch {
	case y <= 0:
		return -1
	case y >= height-1:
		return 1
	default:
		return 0
	}
}

// selPos is a caret position in the wrapped transcript: a content-line index
// (absolute, scroll-independent) and a visual column.
type selPos struct{ line, col int }

// selection is the live left-drag text selection over the transcript. anchor is
// where the drag began, head where it currently is; active gates rendering and
// copy. Coordinates are absolute content lines so scrolling never moves them.
type selection struct {
	active       bool
	anchor, head selPos
}

func (s selection) ordered() (start, end selPos) {
	if s.anchor.line > s.head.line || (s.anchor.line == s.head.line && s.anchor.col > s.head.col) {
		return s.head, s.anchor
	}
	return s.anchor, s.head
}

func (s selection) empty() bool { return s.anchor == s.head }

var (
	selStyle         = lipgloss.NewStyle().Reverse(true)
	scrollThumbStyle lipgloss.Style
	scrollTrackStyle lipgloss.Style
)

// renderTranscript draws the viewport's visible window with a scrollbar in the
// last column and the active selection reverse-highlighted. The content lines
// (m.wrappedLines) are already padded to cw by wrapTranscript, so this stays
// cheap per frame — important because a drag re-renders on every mouse move.
func (m chatTUI) renderTranscript() string {
	h := m.viewport.Height()
	if h <= 0 {
		return ""
	}
	cw := m.viewport.Width() // content width; the scrollbar occupies one more column
	lines := m.wrappedLines
	total := len(lines)
	yoff := m.viewport.YOffset()
	start, end := m.sel.ordered()
	thumbStart, thumbSize := scrollbarThumb(h, yoff, total)
	blank := strings.Repeat(" ", cw)

	rows := make([]string, h)
	bar := make([]string, h)
	for r := 0; r < h; r++ {
		idx := yoff + r
		line := blank // off-content rows fill to width
		if idx >= 0 && idx < total {
			line = lines[idx] // already cw-wide from wrapTranscript
		}
		if m.sel.active && !m.sel.empty() {
			if lo, hi, ok := selSpan(idx, start, end, cw); ok {
				line = lipgloss.StyleRanges(line, lipgloss.NewRange(lo, hi, selStyle))
			}
		}
		rows[r] = line
		bar[r] = scrollbarCell(r, total, h, thumbStart, thumbSize)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, strings.Join(rows, "\n"), strings.Join(bar, "\n"))
}

// selSpan returns the [lo, hi) visual-column span of the selection on content
// line idx (false when the line is outside the selection). cw bounds the span
// so a multi-line selection highlights through the right edge.
func selSpan(idx int, start, end selPos, cw int) (lo, hi int, ok bool) {
	if idx < start.line || idx > end.line {
		return 0, 0, false
	}
	lo, hi = 0, cw
	if idx == start.line {
		lo = start.col
	}
	if idx == end.line {
		hi = end.col
	}
	if hi > cw {
		hi = cw
	}
	if lo >= hi {
		return 0, 0, false
	}
	return lo, hi, true
}

type copyMathSpan struct {
	start  int
	end    int
	id     string
	source string
}

type copyTranscriptLine struct {
	text string
	math []copyMathSpan
}

type activeCopyMath struct {
	id     string
	source string
	start  int
}

func parseCopyTranscript(wrapped string) ([]copyTranscriptLine, int, bool) {
	rawLines := strings.Split(wrapped, "\n")
	lines := make([]copyTranscriptLine, 0, len(rawLines))
	var active *activeCopyMath
	parsedMarkers := 0

	for _, raw := range rawLines {
		var clean strings.Builder
		var spans []copyMathSpan
		column := 0
		position := 0

		for position < len(raw) {
			startAt := strings.Index(raw[position:], copyMathStartPrefix)
			endAt := strings.Index(raw[position:], copyMathEndPrefix)
			if startAt >= 0 {
				startAt += position
			}
			if endAt >= 0 {
				endAt += position
			}

			markerAt := -1
			isStart := false
			switch {
			case startAt >= 0 && (endAt < 0 || startAt < endAt):
				markerAt, isStart = startAt, true
			case endAt >= 0:
				markerAt = endAt
			}
			if markerAt < 0 {
				chunk := raw[position:]
				clean.WriteString(chunk)
				column += ansi.StringWidth(chunk)
				break
			}

			chunk := raw[position:markerAt]
			clean.WriteString(chunk)
			column += ansi.StringWidth(chunk)

			prefix := copyMathEndPrefix
			if isStart {
				prefix = copyMathStartPrefix
			}
			payloadStart := markerAt + len(prefix)
			terminatorAt := strings.Index(raw[payloadStart:], copyMathTerminator)
			if terminatorAt < 0 {
				return nil, 0, false
			}
			terminatorAt += payloadStart
			payload := raw[payloadStart:terminatorAt]
			position = terminatorAt + len(copyMathTerminator)

			if isStart {
				parts := strings.SplitN(payload, ";", 2)
				if len(parts) != 2 || active != nil {
					return nil, 0, false
				}
				decoded, err := base64.RawURLEncoding.DecodeString(parts[1])
				if err != nil {
					return nil, 0, false
				}
				active = &activeCopyMath{id: parts[0], source: string(decoded), start: column}
				parsedMarkers++
				continue
			}

			if active == nil || active.id != payload {
				return nil, 0, false
			}
			spans = append(spans, copyMathSpan{
				start: active.start, end: column, id: active.id, source: active.source,
			})
			active = nil
		}

		if active != nil {
			spans = append(spans, copyMathSpan{
				start: active.start, end: column, id: active.id, source: active.source,
			})
			active.start = 0
		}
		lines = append(lines, copyTranscriptLine{text: clean.String(), math: spans})
	}
	if active != nil {
		return nil, 0, false
	}
	return lines, parsedMarkers, true
}

func (m chatTUI) copyTranscriptLines() ([]copyTranscriptLine, bool) {
	contentWidth := m.viewport.Width()
	marked, expectedMarkers, ok := m.buildCopyTranscript(contentWidth)
	if !ok {
		return nil, false
	}
	lines, parsedMarkers, ok := parseCopyTranscript(wrapTranscript(marked, contentWidth))
	if !ok || parsedMarkers != expectedMarkers || len(lines) != len(m.wrappedLines) {
		return nil, false
	}
	for i := range lines {
		if ansi.Strip(lines[i].text) != ansi.Strip(m.wrappedLines[i]) {
			return nil, false
		}
	}
	return lines, true
}

func selectedDisplayText(lines []string, start, end selPos) string {
	var out []string
	for idx := start.line; idx <= end.line && idx < len(lines); idx++ {
		lo, hi := 0, ansi.StringWidth(lines[idx])
		if idx == start.line {
			lo = start.col
		}
		if idx == end.line {
			hi = end.col
		}
		out = append(out, strings.TrimRight(ansi.Strip(ansi.Cut(lines[idx], lo, hi)), " "))
	}
	return strings.Join(out, "\n")
}

func selectedCopyText(lines []copyTranscriptLine, start, end selPos) string {
	seen := make(map[string]bool)
	var out []string
	for idx := start.line; idx <= end.line && idx < len(lines); idx++ {
		line := lines[idx]
		lo, hi := 0, ansi.StringWidth(line.text)
		if idx == start.line {
			lo = start.col
		}
		if idx == end.line {
			hi = end.col
		}

		var selected strings.Builder
		cursor := lo
		touchedMath := false
		for _, span := range line.math {
			if span.end <= lo || span.start >= hi {
				continue
			}
			touchedMath = true
			if span.start > cursor {
				selected.WriteString(ansi.Strip(ansi.Cut(line.text, cursor, min(span.start, hi))))
			}
			if !seen[span.id] {
				selected.WriteString(span.source)
				seen[span.id] = true
			}
			cursor = max(cursor, min(span.end, hi))
		}
		if cursor < hi {
			selected.WriteString(ansi.Strip(ansi.Cut(line.text, cursor, hi)))
		}
		if selected.Len() == 0 && touchedMath {
			continue
		}
		out = append(out, strings.TrimRight(selected.String(), " "))
	}
	return strings.Join(out, "\n")
}

// selectedText is the plain text of the active display-cell selection. Math is
// reconstructed on demand from semantic transcript sources; if the marked copy
// rendition ever diverges from the visible transcript, the safe fallback keeps
// the exact displayed text rather than applying mismatched coordinates.
func (m chatTUI) selectedText() string {
	if !m.sel.active || m.sel.empty() {
		return ""
	}
	start, end := m.sel.ordered()
	if lines, ok := m.copyTranscriptLines(); ok {
		return selectedCopyText(lines, start, end)
	}
	return selectedDisplayText(m.wrappedLines, start, end)
}

// scrollbarThumb returns the thumb's [start, start+size) row span for a viewport
// of `height` rows showing `total` content lines scrolled to `yoff`.
func scrollbarThumb(height, yoff, total int) (start, size int) {
	if total <= height {
		return 0, 0 // no overflow → no thumb
	}
	size = height * height / total
	if size < 1 {
		size = 1
	}
	maxYoff := total - height
	start = yoff * (height - size) / maxYoff
	if start > height-size {
		start = height - size
	}
	return start, size
}

func scrollbarYOffset(height, row, total, grabOffset int) int {
	if total <= height {
		return 0
	}
	_, thumbSize := scrollbarThumb(height, 0, total)
	maxTop := height - thumbSize
	if maxTop <= 0 {
		return 0
	}
	top := row - grabOffset
	if top < 0 {
		top = 0
	}
	if top > maxTop {
		top = maxTop
	}
	maxYoff := total - height
	return (top*maxYoff + maxTop/2) / maxTop
}

func scrollbarCell(row, total, height, thumbStart, thumbSize int) string {
	if total <= height {
		return " "
	}
	if row >= thumbStart && row < thumbStart+thumbSize {
		return scrollThumbStyle.Render("█")
	}
	return scrollTrackStyle.Render("│")
}

func (m chatTUI) inScrollbar(x, y int) bool {
	if m.nativeScrollback {
		return false
	}
	h := m.viewport.Height()
	return h > 0 && y >= 0 && y < h && x == m.viewport.Width() && len(m.wrappedLines) > h
}

func (m chatTUI) scrollbarGrabRowOffset(row int) int {
	thumbStart, thumbSize := scrollbarThumb(m.viewport.Height(), m.viewport.YOffset(), len(m.wrappedLines))
	if row >= thumbStart && row < thumbStart+thumbSize {
		return row - thumbStart
	}
	return thumbSize / 2
}

func (m *chatTUI) dragScrollbar(row int) {
	m.viewport.SetYOffset(scrollbarYOffset(m.viewport.Height(), row, len(m.wrappedLines), m.scrollbarGrabOffset))
}

// transcriptCaret maps a screen cell (x, y) in the transcript region to an
// absolute content position, clamping to the visible window.
func (m chatTUI) transcriptCaret(x, y int) selPos {
	h := m.viewport.Height()
	if y < 0 {
		y = 0
	}
	if y > h-1 {
		y = h - 1
	}
	if x < 0 {
		x = 0
	}
	if cw := m.viewport.Width(); x > cw {
		x = cw
	}
	return selPos{line: m.viewport.YOffset() + y, col: x}
}
