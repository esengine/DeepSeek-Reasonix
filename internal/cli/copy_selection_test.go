package cli

import (
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
)

// selectionCopyTestUI builds a chatTUI whose transcript is a set of assistant
// markdown blocks rendered at the given terminal width, with the wrap cache
// populated exactly as the viewport would see it.
func selectionCopyTestUI(t *testing.T, width int, raws ...string) chatTUI {
	t.Helper()
	defer restoreThemeForTest(activeColorProfile, activeCLITheme)
	activeColorProfile = colorprofile.NoTTY
	configureCLITheme("dark")

	m := newTestChatTUI()
	m.width = width + 4
	contentWidth := transcriptContentWidth(m.width, m.nativeScrollback)
	m.viewport.SetWidth(contentWidth)
	for _, raw := range raws {
		source := transcriptSource{kind: transcriptSourceMarkdown, raw: raw}
		m.transcript = append(m.transcript, m.renderTranscriptSource(source, m.width))
		m.transcriptSources = append(m.transcriptSources, source)
	}
	for i := range m.transcript {
		m.wrapBlockLines = append(m.wrapBlockLines, wrapBlockLines(m.transcript[i], contentWidth))
	}
	m.wrappedLines = flattenBlockWraps(m.wrapBlockLines)
	return m
}

func findDisplayLine(m chatTUI, substr string) int {
	for i, line := range m.wrappedLines {
		if strings.Contains(ansi.Strip(line), substr) {
			return i
		}
	}
	return -1
}

func selectAll(m *chatTUI) {
	last := len(m.wrappedLines) - 1
	m.sel = selection{
		active: true,
		anchor: selPos{line: 0, col: 0},
		head:   selPos{line: last, col: ansi.StringWidth(m.wrappedLines[last])},
	}
}

// A long command wrapped across several display rows must copy as a single
// logical line — no display wrap newline may reach the clipboard.
func TestCopySelectionLongCommandStaysSingleLine(t *testing.T) {
	cmd := "npx skills add @stablyai/skills --agent cursor --agent codex --agent claude-code --agent grok --agent reasonix -y"
	for _, width := range []int{40, 50, 160} {
		m := selectionCopyTestUI(t, width, cmd)
		startLine, endLine := findDisplayLine(m, "npx skills"), findDisplayLine(m, "reasonix -y")
		if startLine < 0 || endLine < 0 {
			t.Fatalf("width %d: command rows not found", width)
		}
		if endLine > startLine {
			// Wrapped: selecting the whole command must yield one logical line.
			m.sel = selection{
				active: true,
				anchor: selPos{line: startLine, col: 0},
				head:   selPos{line: endLine, col: ansi.StringWidth(m.wrappedLines[endLine])},
			}
		} else {
			m.sel = selection{active: true, anchor: selPos{line: startLine, col: 0}, head: selPos{line: endLine, col: ansi.StringWidth(m.wrappedLines[endLine])}}
		}
		got := m.selectedText()
		if strings.Count(got, "\n") != 0 {
			t.Errorf("width %d: command copied with %d newlines: %q", width, strings.Count(got, "\n"), got)
		}
		if !strings.Contains(got, cmd) {
			t.Errorf("width %d: copied command = %q, want to contain %q", width, got, cmd)
		}
	}
}

// Real newlines inside a fenced code block are source semantics and must be
// preserved; only display wrap newlines are removed.
func TestCopySelectionPreservesFenceNewlines(t *testing.T) {
	m := selectionCopyTestUI(t, 40, "Here is the command:\n\n```\ncat <<'EOF' > /tmp/x\necho hello\nEOF\n```\n\nDone.")
	startLine := findDisplayLine(m, "cat <<")
	endLine := -1
	for i, line := range m.wrappedLines {
		if strings.TrimRight(ansi.Strip(line), " ") == "  │ EOF" {
			endLine = i
		}
	}
	if startLine < 0 || endLine < 0 {
		t.Fatalf("fence rows not found:\n%s", m.wrappedContentString())
	}
	m.sel = selection{
		active: true,
		anchor: selPos{line: startLine, col: 0},
		head:   selPos{line: endLine, col: ansi.StringWidth(m.wrappedLines[endLine])},
	}
	got := m.selectedText()
	for _, want := range []string{"cat <<'EOF' > /tmp/x", "echo hello", "EOF"} {
		if !strings.Contains(got, want) {
			t.Errorf("fence copy missing %q:\n%q", want, got)
		}
	}
	if got := strings.Count(got, "\n"); got != 2 {
		t.Errorf("fence copy has %d newlines, want 2 (one per real code line break):\n%q", got, got)
	}
}

// Partial-row selection maps display columns back to unwrapped source text.
func TestCopySelectionPartialRow(t *testing.T) {
	cmd := "npx skills add @stablyai/skills --agent cursor --agent codex --agent claude-code --agent grok --agent reasonix -y"
	m := selectionCopyTestUI(t, 40, cmd)
	line := findDisplayLine(m, "codex")
	if line < 0 {
		t.Fatalf("codex display row not found:\n%s", m.wrappedContentString())
	}
	plain := ansi.Strip(m.wrappedLines[line])
	lo := strings.Index(plain, "codex")
	if lo < 0 {
		t.Fatalf("codex not in row %q", plain)
	}
	m.sel = selection{active: true, anchor: selPos{line: line, col: lo}, head: selPos{line: line, col: lo + len("codex")}}
	if got, want := m.selectedText(), "codex"; got != want {
		t.Errorf("partial selection = %q, want %q", got, want)
	}
}

// A selection spanning two transcript blocks keeps both blocks' content.
func TestCopySelectionAcrossBlocks(t *testing.T) {
	m := selectionCopyTestUI(t, 40, "Block one content.", "Block two content.")
	selectAll(&m)
	got := m.selectedText()
	for _, want := range []string{"◆ Reasonix", "Block one content.", "Block two content."} {
		if !strings.Contains(got, want) {
			t.Errorf("cross-block copy missing %q:\n%q", want, got)
		}
	}
}

// Paragraph spacing and list structure survive the copy rebuild.
func TestCopySelectionKeepsStructure(t *testing.T) {
	m := selectionCopyTestUI(t, 40, "First para.\n\n- item one\n- item two\n\nSecond para.")
	selectAll(&m)
	got := m.selectedText()
	for _, want := range []string{"First para.", "• item one", "• item two", "Second para."} {
		if !strings.Contains(got, want) {
			t.Errorf("structure copy missing %q:\n%q", want, got)
		}
	}
	if !strings.Contains(got, "First para.\n\n") || !strings.Contains(got, "item two\n\n  Second para.") {
		t.Errorf("paragraph blank lines lost:\n%q", got)
	}
}

// Without transcript sources the semantic rebuild is unavailable and the
// fallback still yields the exact displayed rows.
func TestCopySelectionFallbackDisplayRows(t *testing.T) {
	m := newTestChatTUI()
	m.wrappedLines = []string{"hello world", "second line", "third row"}
	m.sel = selection{active: true, anchor: selPos{line: 0, col: 6}, head: selPos{line: 2, col: 5}}
	if got, want := m.selectedText(), "world\nsecond line\nthird"; got != want {
		t.Errorf("fallback selection = %q, want %q", got, want)
	}
}
