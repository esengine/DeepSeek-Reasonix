package cli

import (
	"strings"
	"testing"

	"reasonix/internal/checkpoint"
	"reasonix/internal/i18n"
)

func TestOneLine(t *testing.T) {
	i18n.DetectLanguage("en")
	t.Cleanup(func() { i18n.DetectLanguage("en") })

	if got := oneLine("", 10); got != "(empty)" {
		t.Fatalf("empty -> %q", got)
	}
	if got := oneLine("a\nb\nc", 10); strings.Contains(got, "\n") {
		t.Fatalf("oneLine kept a newline: %q", got)
	}
	if got := oneLine("this is a fairly long prompt", 8); len([]rune(got)) > 8 {
		t.Fatalf("oneLine did not truncate to width: %q", got)
	}
}

func TestRenderRewindSmoke(t *testing.T) {
	i18n.DetectLanguage("en")
	t.Cleanup(func() { i18n.DetectLanguage("en") })

	metas := []checkpoint.Meta{
		{Turn: 0, Prompt: "add the parser", Paths: []string{"a.go"}},
		{Turn: 1, Prompt: "fix the bug", Paths: []string{"b.go", "c.go"}},
	}
	// Stage 0: turn list.
	m := chatTUI{width: 80, rewind: &rewindPicker{metas: metas, sel: 1}}
	out := m.renderRewind()
	if out == "" || !strings.Contains(out, "Rewind") || !strings.Contains(out, "fix the bug") {
		t.Fatalf("stage-0 render missing content:\n%s", out)
	}
	// Stage 1: scope menu.
	m.rewind.stage = 1
	out = m.renderRewind()
	for _, want := range []string{"Restore to turn 2", "Code + conversation", "Conversation only", "Code only"} {
		if !strings.Contains(out, want) {
			t.Fatalf("stage-1 render missing %q:\n%s", want, out)
		}
	}
	// Closed picker renders nothing.
	m.rewind = nil
	if out := m.renderRewind(); out != "" {
		t.Fatalf("closed picker rendered %q", out)
	}
}
