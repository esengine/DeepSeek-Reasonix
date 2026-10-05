package termrender

import (
	"encoding/base64"
	"strings"
)

const assistantTranscriptIndent = "  "

// AssistantBlock gives assistant prose the same explicit transcript
// identity that user, reasoning, tool, and receipt blocks already have. The
// body keeps a restrained two-cell gutter instead of using a heavy card, and
// rendering at the reduced width keeps every indented row inside the viewport.
func AssistantBlock(raw string, contentWidth int, hideRail bool) string {
	contentWidth = max(contentWidth, 1)
	indent := assistantTranscriptIndent
	if contentWidth <= VisibleWidth(indent) {
		indent = ""
	}
	bodyWidth := max(contentWidth-VisibleWidth(indent), 1)
	renderer := NewMarkdownRenderer(bodyWidth)
	renderer.hideRail = hideRail
	rendered := renderer.Render(raw)
	if rendered == "" {
		rendered = raw
	}
	body := strings.TrimRight(rendered, "\n")
	header := indent + Accent("◆") + " " + Bold("Reasonix")
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
