package cli

import (
	"encoding/base64"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

const (
	copySpanStartPrefix = "\x1b]1337;reasonix-copy-span="
	copySpanEndPrefix   = "\x1b]1337;reasonix-copy-span-end="
	copySpanTerminator  = "\x07"
	copyOmitSpanID      = "gutter"
)

var copyOmitSpanStart = copySpanStartMarker(copyOmitSpanID, "")

func copySpanStartMarker(id, source string) string {
	encoded := base64.RawURLEncoding.EncodeToString([]byte(source))
	return copySpanStartPrefix + id + ";" + encoded + copySpanTerminator
}

func copySpanEndMarker(id string) string {
	return copySpanEndPrefix + id + copySpanTerminator
}

func copyOmitSpan(rendered string) string {
	return copyOmitSpanStart + rendered + copySpanEndMarker(copyOmitSpanID)
}

// buildCopyTranscript renders semantic Markdown only when a copy is requested.
// The visible text stays byte-for-byte equivalent after ANSI stripping, while
// copy spans retain math source and identify render-only decorations.
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
		var rendered string
		switch source.kind {
		case transcriptSourceMarkdown:
			rendered = renderAssistantMarkdownCopy(source.raw, contentWidth, strconv.Itoa(i))
		case transcriptSourceReplayBundle:
			rendered = m.renderReplayBundleCopy(source, contentWidth, strconv.Itoa(i))
		case transcriptSourceReasoning:
			rendered = reasoningBlockCopy(source.raw, contentWidth, source.maxLines)
		default:
			rendered = m.transcript[i]
			if source.copyRendered != "" {
				rendered = source.copyRendered
			}
		}
		markers += strings.Count(rendered, copySpanStartPrefix)
		b.WriteString(rendered)
	}
	return b.String(), markers, true
}

type copyMathSpan struct {
	start  int
	end    int
	id     string
	source string
}

type copyOmittedRange struct {
	start int
	end   int
}

type copyTranscriptLine struct {
	text string
	math []copyMathSpan
	omit []copyOmittedRange
}

type activeCopySpan struct {
	id     string
	source string
	start  int
}

func parseCopyTranscript(wrapped string) ([]copyTranscriptLine, int, bool) {
	rawLines := strings.Split(wrapped, "\n")
	lines := make([]copyTranscriptLine, 0, len(rawLines))
	var active *activeCopySpan
	parsedMarkers := 0

	for _, raw := range rawLines {
		var clean strings.Builder
		var math []copyMathSpan
		var omit []copyOmittedRange
		column := 0
		position := 0

		for position < len(raw) {
			startAt := strings.Index(raw[position:], copySpanStartPrefix)
			endAt := strings.Index(raw[position:], copySpanEndPrefix)
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

			prefix := copySpanEndPrefix
			if isStart {
				prefix = copySpanStartPrefix
			}
			payloadStart := markerAt + len(prefix)
			terminatorAt := strings.Index(raw[payloadStart:], copySpanTerminator)
			if terminatorAt < 0 {
				return nil, 0, false
			}
			terminatorAt += payloadStart
			payload := raw[payloadStart:terminatorAt]
			position = terminatorAt + len(copySpanTerminator)

			if isStart {
				parts := strings.SplitN(payload, ";", 2)
				if len(parts) != 2 || active != nil {
					return nil, 0, false
				}
				decoded, err := base64.RawURLEncoding.DecodeString(parts[1])
				if err != nil {
					return nil, 0, false
				}
				active = &activeCopySpan{id: parts[0], source: string(decoded), start: column}
				parsedMarkers++
				continue
			}

			if active == nil || active.id != payload {
				return nil, 0, false
			}
			if active.source == "" {
				omit = append(omit, copyOmittedRange{start: active.start, end: column})
			} else {
				math = append(math, copyMathSpan{
					start: active.start, end: column, id: active.id, source: active.source,
				})
			}
			active = nil
		}

		if active != nil {
			if active.source == "" {
				omit = append(omit, copyOmittedRange{start: active.start, end: column})
			} else {
				math = append(math, copyMathSpan{
					start: active.start, end: column, id: active.id, source: active.source,
				})
			}
			active.start = 0
		}
		lines = append(lines, copyTranscriptLine{text: clean.String(), math: math, omit: omit})
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
		for _, span := range line.omit {
			if span.end <= lo {
				continue
			}
			if span.start > lo {
				break
			}
			lo = min(span.end, hi)
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

// commitConnectorBlock stores a copy-only rendition beside the visible fixed
// block so selection copy omits generated cells without guessing from text.
func (m *chatTUI) commitConnectorBlock(lines []string) {
	rendered := connectorBlock(lines)
	*m.pendingCommit = append(*m.pendingCommit, rendered)
	m.appendTranscriptBlock(rendered, transcriptSource{
		kind:         transcriptSourceFixed,
		copyRendered: connectorBlockCopy(lines),
	})
}

func (m *chatTUI) rewriteConnectorBlock(index int, lines []string) {
	if index < 0 || index >= len(m.transcript) {
		return
	}
	m.ensureTranscriptSources()
	source := m.transcriptSources[index]
	source.copyRendered = connectorBlockCopy(lines)
	m.setTranscriptBlock(index, connectorBlock(lines), source)
}

// thinkingWrapStep is how far a soft-wrapped continuation sits left of its
// line's first word. The two-column step marks the line start without spending
// a blank row, keeping the thinking block dense.
const thinkingWrapStep = 2

// reasoningRows renders raw thinking text into dim, width-wrapped rows. The
// goal is information density: the thinking log streams into a tail-following
// viewport, so spending a blank row on every paragraph break and wrapping full
// rows one column short of the edge made a long chain of thought scroll past
// quickly and read as sparse. Dropping the blank rows and packing every row to
// the window edge fits more thinking per screen, so it scrolls more slowly.
//
// The gutter prefix is baked in per line: the block's first line gets the "⎿"
// connector, every later source line aligns its first word under the
// connector's text, and soft-wrapped overflow steps thinkingWrapStep columns
// left. Each source line therefore reads as its own line — a list item or a new
// sentence is not mistaken for a continuation — while a line's overflow still
// marks the paragraph start above it. Wrapping accounts for each row's own
// indent so no row exceeds width.
//
// Source blank lines separate prose paragraphs: the blank row is dropped between
// two paragraphs. It is also dropped around a fenced code block — the fence's
// own ``` delimiters already break the flow — but kept inside the fence, where a
// blank line carries meaning.
//
// copyMode wraps each generated gutter in a copy-omit span so selection copy
// keeps only the model's text (mirrors connectorBlockCopy).
func reasoningRows(raw string, width, maxLines int, copyMode bool) []string {
	paraIndent := len([]rune(connector)) // first word aligns under the connector text
	wrapIndent := max(paraIndent-thinkingWrapStep, 0)
	paraW := max(width-paraIndent, 8)
	wrapW := max(width-wrapIndent, 8)
	paraPrefix := strings.Repeat(" ", paraIndent)
	wrapPrefix := strings.Repeat(" ", wrapIndent)

	var rows []string
	emit := func(prefix, text string) {
		if copyMode {
			prefix = copyOmitSpan(prefix)
		}
		rows = append(rows, prefix+dim(text))
	}

	firstLine := true
	inFence := false
	for ln := range strings.SplitSeq(strings.TrimRight(raw, "\n"), "\n") {
		trimmed := strings.TrimSpace(ln)
		blank := trimmed == ""
		fence := strings.HasPrefix(trimmed, "```")

		if blank {
			if inFence { // blank inside a fence: keep it
				rows = append(rows, "")
			}
			continue
		}

		prefix := paraPrefix
		if firstLine {
			prefix = dim(connector)
		}
		for i, wl := range wrapHanging(expandTabs(ln), paraW, wrapW) {
			if i == 0 {
				emit(prefix, wl)
			} else {
				emit(wrapPrefix, wl)
			}
		}
		firstLine = false
		if fence {
			inFence = !inFence
		}
	}

	if maxLines > 0 && len(rows) > maxLines {
		rows = rows[len(rows)-maxLines:]
	}
	return rows
}

// wrapHanging wraps s so its first row fits firstW columns and every later row
// fits contW (contW >= firstW), breaking at spaces. ansi.Wrap can only take one
// width per call, so the line is wrapped at firstW, then its rejoined tail is
// re-flowed at the wider contW — a plain re-wrap of the tail would preserve the
// first pass's breaks. The two-column step is small enough that this stays cheap.
func wrapHanging(s string, firstW, contW int) []string {
	first := ansi.Wrap(s, max(firstW, 1), "")
	i := strings.IndexByte(first, '\n')
	if i < 0 {
		return []string{first}
	}
	rest := strings.ReplaceAll(first[i+1:], "\n", " ")
	return append([]string{first[:i]}, strings.Split(ansi.Wrap(rest, max(contW, 1), ""), "\n")...)
}

func reasoningBlockCopy(raw string, width, maxLines int) string {
	return strings.Join(reasoningRows(raw, width, maxLines, true), "\n")
}
