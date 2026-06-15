package cli

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"reasonix/internal/event"
)

// TestMathStreamingEndToEnd drives the real streaming path (ingestEvent →
// streamAnswer/flushableMarkdownPrefix → renderer.Render → commitPending) with
// the math chunked mid-formula across event.Text boundaries, the way a model
// actually streams tokens. It proves a half-written $...$ / $$...$$ is never
// flushed as raw LaTeX and the committed transcript shows finished Unicode.
func TestMathStreamingEndToEnd(t *testing.T) {
	colorEnabled = false
	m := newTestChatTUI()

	chunks := []string{
		`Euler's identity is $e^{i\`,
		"pi} + 1 = 0$, a classic.\n\nThe Basel sum:\n$$\\sum_{n=1}",
		`}^{\infty} \frac{1}{n^2}`,
		" = \\frac{\\pi^2}{6}$$\n\nThat is all.",
	}
	for _, c := range chunks {
		m.ingestEvent(event.Event{Kind: event.Text, Text: c})
		mid := strings.Join(m.transcript, "\n")
		for _, leak := range []string{`\pi`, `\sum`, `\frac`, `\infty`, `$e^{`, `$$\`} {
			if strings.Contains(mid, leak) {
				t.Fatalf("raw LaTeX %q leaked into a mid-stream flush: %q", leak, mid)
			}
		}
	}

	m.ingestEvent(event.Event{Kind: event.Message})

	out := strings.Join(m.transcript, "\n")
	for _, want := range []string{"e^(iπ) + 1 = 0", "∑", "1/(n²)", "(π²)/6", "That is all."} {
		if !strings.Contains(out, want) {
			t.Errorf("final transcript missing %q:\n%s", want, out)
		}
	}
	for _, leak := range []string{`\`, "$$", "$e"} {
		if strings.Contains(out, leak) {
			t.Errorf("final transcript still contains raw %q:\n%s", leak, out)
		}
	}
}

// TestMathStreamingStyledPath repeats the commit with colour on so the real
// ANSI-styled output is exercised, then asserts the Unicode survives a strip.
func TestMathStreamingStyledPath(t *testing.T) {
	colorEnabled = true
	defer func() { colorEnabled = false }()
	m := newTestChatTUI()

	m.ingestEvent(event.Event{Kind: event.Text, Text: `The relation $a^2 + b^2 = c^2$ holds.`})
	m.ingestEvent(event.Event{Kind: event.Message})

	out := strings.Join(m.transcript, "\n")
	if !strings.Contains(out, "\x1b[") {
		t.Errorf("expected ANSI escapes in coloured output, got %q", out)
	}
	if stripped := ansi.Strip(out); !strings.Contains(stripped, "a² + b² = c²") {
		t.Errorf("math missing after ANSI strip: %q", stripped)
	}
}
