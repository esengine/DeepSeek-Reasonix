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

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/i18n"
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
	transcriptSourceSubagentProgress
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
	// Wrap cache extends on next Update via append-only path.
}

func (m *chatTUI) setTranscriptBlock(index int, rendered string, source transcriptSource) {
	if index < 0 || index >= len(m.transcript) {
		return
	}
	m.ensureTranscriptSources()
	m.transcript[index] = rendered
	m.transcriptSources[index] = source
	// In-place rewrite: drop wrap from this block onward so the next sync
	// re-wraps the mutated block and everything after it.
	m.invalidateWrapFrom(index)
}

func (m *chatTUI) removeTranscriptBlock(index int) {
	if index < 0 || index >= len(m.transcript) {
		return
	}
	m.ensureTranscriptSources()
	m.transcript = append(m.transcript[:index], m.transcript[index+1:]...)
	m.transcriptSources = append(m.transcriptSources[:index], m.transcriptSources[index+1:]...)
	m.invalidateWrapFrom(index)
}

func (m *chatTUI) truncateTranscriptBlocks(length int) {
	length = min(max(length, 0), len(m.transcript))
	m.ensureTranscriptSources()
	m.transcript = m.transcript[:length]
	m.transcriptSources = m.transcriptSources[:length]
	m.invalidateWrapFrom(length)
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
	case transcriptSourceSubagentProgress:
		if sp := m.subagentProgress[source.raw]; sp != nil {
			return m.subagentProgressBlock(source.raw, sp)
		}
		return ""
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

// copyBlock is one transcript block's copy rendition: the semantic text (for
// markdown blocks, free of display wrap newlines) plus the display-row mapping
// used to translate a screen selection back to that text.
type copyBlock struct {
	text string
	rows []mdCopyRow
}

// textRows splits display text into its logical rows: every newline-terminated
// segment counts as a row (including blank rows), while a trailing newline is
// only a terminator and adds no row of its own.
func textRows(block string) []string {
	if block == "" {
		return []string{""}
	}
	return strings.Split(strings.TrimSuffix(block, "\n"), "\n")
}

// splitCopyRows maps display-rendered text onto 1:1 copy rows. Used for blocks
// whose only rendition is the wrapped display text (user bubbles, tool cards,
// reasoning, banners): their copy semantics are the display rows themselves.
func splitCopyRows(block string) []mdCopyRow {
	lines := textRows(block)
	rows := make([]mdCopyRow, len(lines))
	for i, l := range lines {
		rows[i] = mdCopyRow{text: l, rows: []string{l}, bases: []int{0}}
	}
	return rows
}

func (m chatTUI) renderReplayBundleCopy(
	source transcriptSource,
	contentWidth int,
	prefix string,
) copyBlock {
	var b strings.Builder
	var rows []mdCopyRow
	banner := renderTUIBanner(m.label, source.raw, contentWidth)
	b.WriteString(banner)
	rows = append(rows, splitCopyRows(banner)...)

	// Mirror replaySectionsForWithAssistantRenderer so the copy rendition and
	// its display mapping stay in lockstep; assistant markdown sections use the
	// unwrapped copy renderer, everything else its wrapped display text.
	assistantIndex := 0
	appendSection := func(text string) {
		b.WriteString(text)
		rows = append(rows, splitCopyRows(text)...)
	}
	appendAssistant := func(body string) {
		blk := renderAssistantMarkdownCopy(body, contentWidth, prefix+"-"+strconv.Itoa(assistantIndex))
		assistantIndex++
		b.WriteString(blk.text + "\n\n")
		rows = append(rows, blk.rows...)
		rows = append(rows, blankCopyRow())
	}
	for _, msg := range source.history {
		if msg.LocalOnly {
			if reasoning := strings.TrimSpace(msg.ReasoningContent); reasoning != "" {
				appendSection(dim("  ▎ "+i18n.M.ChatThinking) + "\n" + reasoningBlock(reasoning, contentWidth, 0) + "\n\n")
			}
			if body := strings.TrimSpace(msg.Content); body != "" {
				appendAssistant(body)
			}
			for _, call := range msg.ToolCalls {
				appendSection(toolCard(call.Name, "", contentWidth) + "\n\n")
			}
			if msg.InterruptedTurn != nil {
				appendSection("  · " + interruptedTurnDisplayNotice() + "\n\n")
			}
			continue
		}
		switch msg.Role {
		case provider.RoleUser:
			if steerText, isSteer := agent.SteerText(msg.Content); isSteer {
				appendSection("  ↪ " + steerText + "\n\n")
				continue
			}
			content := control.StripComposePrefixes(msg.Content)
			appendSection(renderUserBubble(content, contentWidth, false) + "\n\n")
		case provider.RoleAssistant:
			if reasoning := strings.TrimSpace(msg.ReasoningContent); reasoning != "" {
				appendSection(dim("  ▎ "+i18n.M.ChatThinking) + "\n" + reasoningBlock(reasoning, contentWidth, 0) + "\n\n")
			}
			if body := strings.TrimSpace(msg.Content); body != "" {
				appendAssistant(body)
			}
			for _, call := range msg.ToolCalls {
				appendSection(toolCard(call.Name, call.Arguments, contentWidth) + "\n\n")
			}
		}
	}
	text := strings.TrimRight(b.String(), "\n")
	if segs := strings.Count(text, "\n") + 1; len(rows) > segs {
		rows = rows[:segs]
	}
	return copyBlock{text: text, rows: rows}
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

// renderAssistantMarkdownCopy mirrors renderAssistantMarkdown's visible output,
// renders it without display wrap newlines, and adds zero-width math markers for
// on-demand clipboard reconstruction. The returned rows carry the display-layer
// expansion of each semantic row so selection coordinates still translate.
func renderAssistantMarkdownCopy(raw string, contentWidth int, prefix string) copyBlock {
	contentWidth = max(contentWidth, 1)
	indent := assistantTranscriptIndent
	if contentWidth <= visibleWidth(indent) {
		indent = ""
	}
	bodyWidth := max(contentWidth-visibleWidth(indent), 1)
	renderer := newMarkdownRenderer(bodyWidth)
	rendered, rows := renderer.RenderCopy(raw, prefix)
	if rendered == "" {
		rendered = raw
		rows = splitCopyRows(raw)
	}
	body := strings.TrimRight(rendered, "\n")
	if segs := strings.Count(body, "\n") + 1; len(rows) > segs {
		rows = rows[:segs]
	}
	header := indent + accent("◆") + " " + bold("Reasonix")
	if body == "" {
		return copyBlock{text: header, rows: []mdCopyRow{{text: header, rows: []string{header}, bases: []int{0}}}}
	}
	// Re-apply the two-cell gutter the display layer adds to non-blank rows;
	// the semantic-column offsets are unaffected (both sides shift equally).
	for i := range rows {
		if rows[i].text == "" {
			continue
		}
		rows[i].text = indent + rows[i].text
		for j := range rows[i].rows {
			if rows[i].rows[j] != "" {
				rows[i].rows[j] = indent + rows[i].rows[j]
			}
		}
	}
	all := make([]mdCopyRow, 0, len(rows)+2)
	all = append(all, mdCopyRow{text: header, rows: []string{header}, bases: []int{0}})
	all = append(all, blankCopyRow())
	all = append(all, rows...)
	return copyBlock{
		text: header + "\n\n" + indentTranscriptBlock(body, indent),
		rows: all,
	}
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
// The text is the copy source (no display wrap newlines for markdown blocks);
// rows track each semantic row's display expansion so a screen selection can be
// mapped back. math markers retain the source needed to reconstruct LaTeX.
func (m chatTUI) buildCopyTranscript(contentWidth int) (string, []mdCopyRow, int, bool) {
	if len(m.transcriptSources) != len(m.transcript) {
		return "", nil, 0, false
	}
	var b strings.Builder
	var rows []mdCopyRow
	markers := 0
	for i, source := range m.transcriptSources {
		if i > 0 {
			b.WriteByte('\n')
		}
		var blk copyBlock
		switch source.kind {
		case transcriptSourceMarkdown:
			blk = renderAssistantMarkdownCopy(source.raw, contentWidth, strconv.Itoa(i))
		case transcriptSourceReplayBundle:
			blk = m.renderReplayBundleCopy(source, contentWidth, strconv.Itoa(i))
		default:
			blk = copyBlock{text: m.transcript[i], rows: splitCopyRows(m.transcript[i])}
		}
		markers += strings.Count(blk.text, copyMathStartPrefix)
		b.WriteString(blk.text)
		rows = append(rows, blk.rows...)
	}
	return b.String(), rows, markers, true
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
	for i := range block {
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
	for r := range h {
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

// copyTranscriptLine is one semantic copy row: unwrapped source text (still
// carrying copy-math markers and ANSI), its in-row math spans, and the display
// rows it expands to (display[j] with semantic column offset bases[j]).
type copyTranscriptLine struct {
	text    string
	math    []copyMathSpan
	display []string
	bases   []int
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

// copyTranscriptLines builds the semantic copy rendition of the transcript and
// validates it against the wrapped display lines: row count and, per display
// row, the ANSI-stripped text must match exactly, so selection coordinates are
// trustworthy before selectedCopyText consumes them.
func (m chatTUI) copyTranscriptLines() ([]copyTranscriptLine, bool) {
	contentWidth := m.viewport.Width()
	marked, rows, expectedMarkers, ok := m.buildCopyTranscript(contentWidth)
	if !ok || len(rows) != strings.Count(marked, "\n")+1 {
		return nil, false
	}
	lines := make([]copyTranscriptLine, 0, len(rows))
	var display []string
	markers := 0
	for _, row := range rows {
		parsed, parsedMarkers, ok := parseCopyTranscript(row.text)
		if !ok || len(parsed) != 1 {
			return nil, false
		}
		markers += parsedMarkers
		dl, mdBases := expandCopyRow(row, contentWidth)
		bases := semanticBases(row.text, dl, mdBases)
		lines = append(lines, copyTranscriptLine{text: row.text, math: parsed[0].math, display: dl, bases: bases})
		display = append(display, dl...)
	}
	if markers != expectedMarkers || len(display) != len(m.wrappedLines) {
		return nil, false
	}
	for i := range display {
		if ansi.Strip(display[i]) != ansi.Strip(m.wrappedLines[i]) {
			return nil, false
		}
	}
	return lines, true
}

// expandCopyRow soft-wraps each recorded display row (the lipgloss pass the
// display layer applies after the md renderer), returning the viewport rows and
// their md-layer semantic column offsets (used when matching is ambiguous).
func expandCopyRow(row mdCopyRow, width int) ([]string, []int) {
	var out []string
	var bases []int
	for j, r := range row.rows {
		sub := strings.Split(wrapTranscript(r, width), "\n")
		acc := 0
		for _, s := range sub {
			out = append(out, s)
			bases = append(bases, row.bases[j]+acc)
			acc += ansi.StringWidth(s)
		}
	}
	return out, bases
}

// semanticBases returns, for every display row, the visual column at which the
// row's first character maps back into the semantic text. Word-wrapping drops
// the break-point space, so a plain width prefix sum is off by one per break;
// matching each row fragment against the unwrapped text absorbs that. The
// per-row indent the display layer repeats on every line is tolerated by
// trimming it off before matching and subtracting it from the offset. Rows
// whose fragment cannot be matched (e.g. padded table rails) fall back to the
// md-layer offset.
func semanticBases(text string, display []string, mdBases []int) []int {
	t := []rune(ansi.Strip(text))
	bases := make([]int, len(display))
	pos := 0
	for i, d := range display {
		frag := strings.TrimRight(ansi.Strip(d), " ")
		if frag == "" {
			bases[i] = pos
			continue
		}
		cand := []rune(frag)
		skip := 0
		if cand[0] == ' ' {
			trimmed := []rune(strings.TrimLeft(frag, " "))
			skip = len(cand) - len(trimmed)
			cand = trimmed
		}
		if idx := indexRunes(t, cand, pos); idx >= 0 {
			bases[i] = max(idx-skip, 0)
			pos = idx + len(cand)
			continue
		}
		bases[i] = mdBases[i]
	}
	return bases
}

// indexRunes finds sub in s at or after from, in rune space.
func indexRunes(s, sub []rune, from int) int {
	if from < 0 || from > len(s) {
		return -1
	}
	s = s[from:]
	for i := 0; i+len(sub) <= len(s); i++ {
		if string(s[i:i+len(sub)]) == string(sub) {
			return from + i
		}
	}
	return -1
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

// selectedCopyText maps the display-cell selection back to semantic rows: each
// selected viewport row resolves to a semantic row plus a column range, and the
// output is rebuilt from the unwrapped source text so no display wrap newline
// leaks into the clipboard.
func selectedCopyText(lines []copyTranscriptLine, start, end selPos) string {
	seen := make(map[string]bool)
	var out []string
	visLine := 0 // global display-line index of the first display row of lines[i]
	for _, line := range lines {
		firstVis := visLine
		lastVis := visLine + len(line.display) - 1
		if lastVis >= start.line && firstVis <= end.line && len(line.display) > 0 {
			width := ansi.StringWidth(line.text)
			var a, b int
			if start.line >= firstVis {
				a = line.bases[start.line-firstVis] + start.col
			}
			if end.line <= lastVis {
				b = line.bases[end.line-firstVis] + end.col
			} else {
				b = width
			}
			a = max(a, 0)
			b = min(b, width)
			if line.text == "" {
				out = append(out, "")
			} else if a < b || mathSpanOverlaps(line.math, a, b) {
				out = append(out, selectCopyRange(line, a, b, seen))
			}
		}
		visLine = lastVis + 1
	}
	return strings.Join(out, "\n")
}

func mathSpanOverlaps(spans []copyMathSpan, a, b int) bool {
	for _, span := range spans {
		if span.end > a && span.start < b {
			return true
		}
	}
	return false
}

func selectCopyRange(line copyTranscriptLine, a, b int, seen map[string]bool) string {
	var selected strings.Builder
	cursor := a
	touchedMath := false
	for _, span := range line.math {
		if span.end <= a || span.start >= b {
			continue
		}
		touchedMath = true
		if span.start > cursor {
			selected.WriteString(ansi.Strip(ansi.Cut(line.text, cursor, min(span.start, b))))
		}
		if !seen[span.id] {
			selected.WriteString(span.source)
			seen[span.id] = true
		}
		cursor = max(cursor, min(span.end, b))
	}
	if cursor < b {
		selected.WriteString(ansi.Strip(ansi.Cut(line.text, cursor, b)))
	}
	if selected.Len() == 0 && touchedMath {
		return ""
	}
	return strings.TrimRight(selected.String(), " ")
}

// selectedText is the plain text of the active display-cell selection. Math is
// reconstructed on demand from semantic transcript sources; if the marked copy
// rendition ever diverges from the visible transcript, the fallback rebuilds
// whole markdown blocks from their unwrapped source and only resorts to the
// exact displayed rows for partial blocks and non-markdown content.
func (m chatTUI) selectedText() string {
	if !m.sel.active || m.sel.empty() {
		return ""
	}
	start, end := m.sel.ordered()
	if lines, ok := m.copyTranscriptLines(); ok {
		return selectedCopyText(lines, start, end)
	}
	return m.selectedFallbackText(start, end)
}

func (m chatTUI) selectedFallbackText(start, end selPos) string {
	if len(m.wrapBlockLines) == 0 || len(m.transcriptSources) != len(m.wrapBlockLines) {
		return selectedDisplayText(m.wrappedLines, start, end)
	}
	contentWidth := m.viewport.Width()
	var out []string
	vis := 0
	for i := range m.wrapBlockLines {
		blockVis := len(m.wrapBlockLines[i])
		blockStart, blockEnd := vis, vis+blockVis-1
		if blockEnd >= start.line && blockStart <= end.line {
			inLo := max(start.line-blockStart, 0)
			inHi := min(end.line-blockStart, blockVis-1)
			full := inLo == 0 && inHi == blockVis-1
			if full && m.transcriptSources[i].kind == transcriptSourceMarkdown {
				blk := renderAssistantMarkdownCopy(m.transcriptSources[i].raw, contentWidth, strconv.Itoa(i))
				out = append(out, blk.text)
			} else {
				for idx := blockStart + inLo; idx <= blockStart+inHi && idx < len(m.wrappedLines); idx++ {
					lo, hi := 0, ansi.StringWidth(m.wrappedLines[idx])
					if idx == start.line {
						lo = start.col
					}
					if idx == end.line {
						hi = end.col
					}
					out = append(out, strings.TrimRight(ansi.Strip(ansi.Cut(m.wrappedLines[idx], lo, hi)), " "))
				}
			}
		}
		vis = blockEnd + 1
	}
	return strings.Join(out, "\n")
}

// scrollbarThumb returns the thumb's [start, start+size) row span for a viewport
// of `height` rows showing `total` content lines scrolled to `yoff`.
func scrollbarThumb(height, yoff, total int) (start, size int) {
	if total <= height {
		return 0, 0 // no overflow → no thumb
	}
	size = max(height*height/total, 1)
	maxYoff := total - height
	start = min(yoff*(height-size)/maxYoff, height-size)
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
	top := min(max(row-grabOffset, 0), maxTop)
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
	// Sync immediately so a streaming event between drag motions cannot see a
	// stale followTail and yank the reader back to the bottom (#6430/#6978).
	m.syncScrollModeAfterGesture()
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
