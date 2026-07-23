package cli

import (
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"reasonix/internal/provider"
)

func TestAssistantMarkdownHasIdentityAndIndentedBody(t *testing.T) {
	defer restoreThemeForTest(colorEnabled, activeCLITheme)
	colorEnabled = false
	configureCLITheme("dark")

	rendered := renderAssistantMarkdown("A concise answer that wraps across the available width.", 32)
	lines := strings.Split(ansi.Strip(rendered), "\n")
	if len(lines) < 4 {
		t.Fatalf("assistant block should contain a header, gap, and wrapped body:\n%s", rendered)
	}
	if lines[0] != "  ◆ Reasonix" {
		t.Fatalf("assistant header = %q, want %q", lines[0], "  ◆ Reasonix")
	}
	if lines[1] != "" {
		t.Fatalf("assistant header/body separator = %q, want blank row", lines[1])
	}
	for i, line := range lines[2:] {
		if line != "" && !strings.HasPrefix(line, assistantTranscriptIndent) {
			t.Fatalf("assistant body row %d lacks the two-cell gutter: %q", i+2, line)
		}
		if width := visibleWidth(line); width > 32 {
			t.Fatalf("assistant row %d width = %d, want <= 32: %q", i+2, width, line)
		}
	}
}

func TestReplaySectionsKeepAssistantIdentity(t *testing.T) {
	defer restoreThemeForTest(colorEnabled, activeCLITheme)
	colorEnabled = false
	configureCLITheme("dark")

	sections := replaySectionsFor([]provider.Message{
		{Role: provider.RoleUser, Content: "Which version?"},
		{Role: provider.RoleAssistant, Content: "Version 1.2.3"},
	}, 48)
	if len(sections) != 2 {
		t.Fatalf("replay sections = %d, want user and assistant", len(sections))
	}
	if plain := ansi.Strip(sections[1]); !strings.HasPrefix(plain, "  ◆ Reasonix\n\n  Version 1.2.3") {
		t.Fatalf("replayed assistant answer lost its identity: %q", plain)
	}
}

func TestReplaySectionsRestoreInterruptedLocalOutput(t *testing.T) {
	defer restoreThemeForTest(colorEnabled, activeCLITheme)
	colorEnabled = false
	configureCLITheme("dark")

	sections := replaySectionsFor([]provider.Message{
		{Role: provider.RoleUser, Content: "change config"},
		{
			Role: provider.RoleTool, ToolCallID: provider.LocalOnlyToolID, Name: provider.LocalOnlyToolName,
			LocalOnly: true, Content: "partial answer", ReasoningContent: "checking config",
			ToolCalls:       []provider.ToolCall{{ID: "p1", Name: "write_file"}},
			InterruptedTurn: &provider.InterruptedTurnRecovery{Pending: true},
		},
	}, 64)
	plain := ansi.Strip(strings.Join(sections, ""))
	for _, want := range []string{"change config", "checking config", "partial answer", "Write", "bounded recovery summary"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("replayed interrupted history missing %q:\n%s", want, plain)
		}
	}
}

func TestScrollbarThumb(t *testing.T) {
	if _, size := scrollbarThumb(10, 0, 5); size != 0 {
		t.Errorf("content within viewport should have no thumb, got size %d", size)
	}
	if start, _ := scrollbarThumb(10, 0, 100); start != 0 {
		t.Errorf("at top the thumb starts at row 0, got %d", start)
	}
	const h, total = 10, 100
	if start, size := scrollbarThumb(h, total-h, total); start+size != h {
		t.Errorf("at bottom the thumb reaches the last row: start=%d size=%d h=%d", start, size, h)
	}
}

func TestEdgeScrollDir(t *testing.T) {
	const h = 10
	if got := edgeScrollDir(0, h); got != -1 {
		t.Errorf("top edge dir = %d, want -1", got)
	}
	if got := edgeScrollDir(h-1, h); got != 1 {
		t.Errorf("bottom edge dir = %d, want 1", got)
	}
	if got := edgeScrollDir(h/2, h); got != 0 {
		t.Errorf("middle dir = %d, want 0", got)
	}
}

func TestSelSpan(t *testing.T) {
	start, end, cw := selPos{line: 1, col: 3}, selPos{line: 3, col: 5}, 20
	for _, tc := range []struct {
		idx         int
		wantOK      bool
		wantLo, wHi int
	}{
		{0, false, 0, 0}, // above
		{1, true, 3, cw}, // first line: anchor col → right edge
		{2, true, 0, cw}, // middle line: full width
		{3, true, 0, 5},  // last line: left edge → head col
		{4, false, 0, 0}, // below
	} {
		lo, hi, ok := selSpan(tc.idx, start, end, cw)
		if ok != tc.wantOK || (ok && (lo != tc.wantLo || hi != tc.wHi)) {
			t.Errorf("selSpan(%d) = (%d,%d,%v), want (%d,%d,%v)", tc.idx, lo, hi, ok, tc.wantLo, tc.wHi, tc.wantOK)
		}
	}
}

func TestSelectedTextMultiLine(t *testing.T) {
	m := newTestChatTUI()
	m.wrappedLines = []string{"hello world", "second line", "third row"}
	m.sel = selection{active: true, anchor: selPos{line: 0, col: 6}, head: selPos{line: 2, col: 5}}

	if got, want := m.selectedText(), "world\nsecond line\nthird"; got != want {
		t.Errorf("selectedText() = %q, want %q", got, want)
	}

	// A zero-width selection (plain click) copies nothing.
	m.sel = selection{active: true, anchor: selPos{line: 0, col: 3}, head: selPos{line: 0, col: 3}}
	if got := m.selectedText(); got != "" {
		t.Errorf("empty selection should yield no text, got %q", got)
	}
}

func TestCopyToClipboard(t *testing.T) {
	t.Setenv("SSH_CONNECTION", "")
	t.Setenv("SSH_CLIENT", "")
	t.Setenv("SSH_TTY", "")
	previous := writeNativeClipboardText
	t.Cleanup(func() { writeNativeClipboardText = previous })

	var written string
	writeNativeClipboardText = func(text string) error {
		written = text
		return nil
	}
	message := copyToClipboard("hello")()
	got, ok := message.(clipboardCopyMsg)
	if !ok {
		t.Fatalf("copyToClipboard returned %T, want clipboardCopyMsg", message)
	}
	if written != "hello" || got.text != "hello" || got.err != nil || got.osc52 {
		t.Fatalf("native clipboard result = %+v, written %q", got, written)
	}

	wantErr := errors.New("clipboard unavailable")
	writeNativeClipboardText = func(string) error { return wantErr }
	got = copyToClipboard("fallback")().(clipboardCopyMsg)
	if !errors.Is(got.err, wantErr) || got.osc52 {
		t.Fatalf("failed native clipboard result = %+v", got)
	}

	t.Setenv("SSH_CONNECTION", "host 22 client 1234")
	writeNativeClipboardText = func(string) error {
		t.Fatal("SSH copy must not write the remote host's native clipboard")
		return nil
	}
	got = copyToClipboard("remote")().(clipboardCopyMsg)
	if !got.osc52 || got.text != "remote" {
		t.Fatalf("SSH clipboard result = %+v, want OSC 52", got)
	}
}
