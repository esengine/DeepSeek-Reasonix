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
			rendered = reasoningBlockCopy(source.raw, m.width, source.maxLines)
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

func reasoningBlockLines(raw string, width, maxLines int) []string {
	w := max(width-len([]rune(connector)), 8)
	var lines []string
	for ln := range strings.SplitSeq(strings.TrimRight(raw, "\n"), "\n") {
		for wl := range strings.SplitSeq(ansi.Wrap(expandTabs(ln), w, ""), "\n") {
			lines = append(lines, dim(wl))
		}
	}
	if maxLines > 0 && len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	return lines
}

func reasoningBlockCopy(raw string, width, maxLines int) string {
	return connectorBlockCopy(reasoningBlockLines(raw, width, maxLines))
}
