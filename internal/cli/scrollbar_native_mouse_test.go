package cli

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// TestNoScrollbarInNativeMouseMode proves the transcript drops its right-hand
// scrollbar (and its reserved column) when the terminal owns the mouse.
func TestNoScrollbarInNativeMouseMode(t *testing.T) {
	m := newTestChatTUI()
	m.nativeScrollback = false
	m.mouseCaptureOff = false
	if m.noScrollbar() {
		t.Fatal("capture-on alt-screen should keep the scrollbar")
	}
	if got, want := m.transcriptWidth(), m.width-1; got != want {
		t.Fatalf("transcriptWidth with scrollbar = %d, want %d", got, want)
	}

	m.mouseCaptureOff = true
	if !m.noScrollbar() {
		t.Fatal("native mouse mode should drop the scrollbar")
	}
	if got, want := m.transcriptWidth(), m.width; got != want {
		t.Fatalf("transcriptWidth without scrollbar = %d, want %d", got, want)
	}
}

// TestRenderTranscriptOmitsScrollbarInNativeMouseMode proves the rendered frame
// stops at the content width (no bar column) once mouse capture is off.
func TestRenderTranscriptOmitsScrollbarInNativeMouseMode(t *testing.T) {
	m := newTestChatTUI()
	m.nativeScrollback = false
	const cw = 20
	m.viewport.SetWidth(cw)
	m.viewport.SetHeight(4)
	for _, s := range []string{"one", "two", "three", "four", "five", "six"} {
		m.wrappedLines = append(m.wrappedLines, padRight(s, cw))
	}

	m.mouseCaptureOff = false
	withBar := m.renderTranscript()
	m.mouseCaptureOff = true
	withoutBar := m.renderTranscript()

	if w := visibleWidth(strings.Split(withBar, "\n")[0]); w != cw+1 {
		t.Fatalf("capture-on transcript first row width = %d, want content+scrollbar %d", w, cw+1)
	}
	if w := visibleWidth(strings.Split(withoutBar, "\n")[0]); w != cw {
		t.Fatalf("native-mouse transcript first row width = %d, want content width %d", w, cw)
	}
}

// TestNativeMouseOmitsCodeQuoteRail proves that when the terminal owns the
// viewport chrome, the "│" gutter before fenced code is dropped and its cell
// rendered as plain spaces instead.
func TestNativeMouseOmitsCodeQuoteRail(t *testing.T) {
	const code = "```go\nfunc main() {}\n```\n"

	withRail := ansi.Strip(renderAssistantMarkdown(code, 40, false))
	if !strings.Contains(withRail, "│ func main()") {
		t.Fatalf("capture-on code should keep its rail:\n%s", withRail)
	}

	withoutRail := ansi.Strip(renderAssistantMarkdown(code, 40, true))
	if strings.Contains(withoutRail, "│") {
		t.Fatalf("native-mouse code should drop the rail:\n%s", withoutRail)
	}
	if !strings.Contains(withoutRail, "    func main()") {
		t.Fatalf("native-mouse code should be indented by spaces:\n%s", withoutRail)
	}
}
