package termrender

import (
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"

	"github.com/charmbracelet/x/ansi"
)

func TestAssistantMarkdownHasIdentityAndIndentedBody(t *testing.T) {
	defer restoreThemeForTest(activeColorProfile, activeTheme)
	activeColorProfile = colorprofile.NoTTY
	ConfigureTheme("dark")

	rendered := AssistantBlock("A concise answer that wraps across the available width.", 32, false)
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
		if width := VisibleWidth(line); width > 32 {
			t.Fatalf("assistant row %d width = %d, want <= 32: %q", i+2, width, line)
		}
	}
}
