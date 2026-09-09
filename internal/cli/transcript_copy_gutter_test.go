package cli

import (
	"bytes"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"reasonix/internal/provider"
)

func transcriptSelectionModel(source transcriptSource) chatTUI {
	m := newTestChatTUI()
	contentWidth := transcriptContentWidth(m.width, m.nativeScrollback)
	m.viewport.SetWidth(contentWidth)
	rendered := m.renderTranscriptSource(source, m.width)
	m.transcript = []string{rendered}
	m.transcriptSources = []transcriptSource{source}
	m.wrappedLines = wrapBlockLines(rendered, contentWidth)
	return m
}

func transcriptLineContaining(t *testing.T, lines []string, needle string) int {
	t.Helper()
	for i, line := range lines {
		if strings.Contains(ansi.Strip(line), needle) {
			return i
		}
	}
	t.Fatalf("rendered transcript did not contain %q:\n%s", needle, ansi.Strip(strings.Join(lines, "\n")))
	return -1
}

func selectTranscriptLines(m *chatTUI, first, last int) string {
	m.sel = selection{
		active: true,
		anchor: selPos{line: first},
		head:   selPos{line: last, col: ansi.StringWidth(m.wrappedLines[last])},
	}
	return m.selectedText()
}

func TestSelectedTextStripsRenderedMarkdownGutters(t *testing.T) {
	defer restoreThemeForTest(activeColorProfile, activeCLITheme)
	activeColorProfile = colorprofile.TrueColor
	configureCLITheme("dark")

	m := transcriptSelectionModel(transcriptSource{
		kind: transcriptSourceMarkdown,
		raw:  "```go\nconst x = 1\nif x > 0 {\n```",
	})
	first := transcriptLineContaining(t, m.wrappedLines, "│ const x = 1")
	last := transcriptLineContaining(t, m.wrappedLines, "│ if x > 0 {")
	if got, want := selectTranscriptLines(&m, first, last), "const x = 1\nif x > 0 {"; got != want {
		t.Fatalf("fenced selection = %q, want %q", got, want)
	}
	plain := ansi.Strip(m.wrappedLines[first])
	contentCol := ansi.StringWidth(strings.Split(plain, "const x")[0])
	m.sel = selection{
		active: true,
		anchor: selPos{line: first, col: contentCol},
		head:   selPos{line: first, col: contentCol + ansi.StringWidth("const x")},
	}
	if got, want := m.selectedText(), "const x"; got != want {
		t.Fatalf("mid-line fenced selection = %q, want %q", got, want)
	}

	m = transcriptSelectionModel(transcriptSource{kind: transcriptSourceMarkdown, raw: "> quoted"})
	line := transcriptLineContaining(t, m.wrappedLines, "▎ quoted")
	if got, want := selectTranscriptLines(&m, line, line), "quoted"; got != want {
		t.Fatalf("blockquote selection = %q, want %q", got, want)
	}
}

func TestSelectedTextPreservesLiteralGutterGlyphs(t *testing.T) {
	defer restoreThemeForTest(activeColorProfile, activeCLITheme)
	activeColorProfile = colorprofile.NoTTY
	configureCLITheme("dark")

	for _, literal := range []string{"│ literal", "▎ literal", "⎿ literal", "| table | row |"} {
		m := transcriptSelectionModel(transcriptSource{kind: transcriptSourceMarkdown, raw: literal})
		line := transcriptLineContaining(t, m.wrappedLines, literal)
		plain := strings.TrimRight(ansi.Strip(m.wrappedLines[line]), " ")
		if got := selectTranscriptLines(&m, line, line); got != plain {
			t.Fatalf("full-line selection for %q = %q, want displayed %q", literal, got, plain)
		}
		start := ansi.StringWidth(strings.Split(plain, literal)[0])
		m.sel = selection{
			active: true,
			anchor: selPos{line: line, col: start},
			head:   selPos{line: line, col: start + ansi.StringWidth(literal)},
		}
		if got := m.selectedText(); got != literal {
			t.Fatalf("literal selection for %q = %q", literal, got)
		}
	}

	m := newTestChatTUI()
	m.wrappedLines = []string{"  │ fallback literal"}
	if got, want := selectTranscriptLines(&m, 0, 0), "  │ fallback literal"; got != want {
		t.Fatalf("display fallback = %q, want %q", got, want)
	}
}

func TestSelectedTextStripsWholeConnectorGutter(t *testing.T) {
	defer restoreThemeForTest(activeColorProfile, activeCLITheme)
	activeColorProfile = colorprofile.NoTTY
	configureCLITheme("dark")

	m := newTestChatTUI()
	contentWidth := transcriptContentWidth(m.width, m.nativeScrollback)
	m.viewport.SetWidth(contentWidth)
	m.beginToolRunning("shell-copy-test")
	m.streamToolOutput("shell-copy-test", "first\nsecond\n│ literal output")
	m.wrappedLines = wrapBlockLines(m.transcript[0], contentWidth)
	if got, want := selectTranscriptLines(&m, 0, 2), "first\nsecond\n│ literal output"; got != want {
		t.Fatalf("connector selection = %q, want %q", got, want)
	}
}

func TestSelectedTextStripsReplayReasoningConnector(t *testing.T) {
	defer restoreThemeForTest(activeColorProfile, activeCLITheme)
	activeColorProfile = colorprofile.NoTTY
	configureCLITheme("dark")

	m := transcriptSelectionModel(transcriptSource{
		kind: transcriptSourceReplayBundle,
		history: []provider.Message{{
			Role: provider.RoleAssistant, ReasoningContent: "first\nsecond",
		}},
	})
	first := transcriptLineContaining(t, m.wrappedLines, "⎿  first")
	last := transcriptLineContaining(t, m.wrappedLines, "second")
	if got, want := selectTranscriptLines(&m, first, last), "first\nsecond"; got != want {
		t.Fatalf("replayed reasoning selection = %q, want %q", got, want)
	}
}

func TestNativeClipboardPassesCJKUTF8Unchanged(t *testing.T) {
	t.Setenv("SSH_CONNECTION", "")
	t.Setenv("SSH_CLIENT", "")
	t.Setenv("SSH_TTY", "")
	previous := writeNativeClipboardText
	defer func() { writeNativeClipboardText = previous }()

	want := "这是中文复制验证 │ ▎ ⎿"
	var written []byte
	writeNativeClipboardText = func(text string) error {
		written = append([]byte(nil), []byte(text)...)
		return nil
	}
	msg := copyToClipboard(want)().(clipboardCopyMsg)
	if msg.err != nil || msg.osc52 {
		t.Fatalf("native clipboard result = %+v", msg)
	}
	if !utf8.Valid(written) || !bytes.Equal(written, []byte(want)) {
		t.Fatalf("native clipboard bytes = %q, want unchanged UTF-8 %q", written, []byte(want))
	}
}
