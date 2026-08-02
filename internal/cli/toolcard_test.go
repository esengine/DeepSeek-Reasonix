package cli

import (
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
)

func TestToolCard(t *testing.T) {
	cases := []struct {
		name string
		args string
		want []string
		deny []string
	}{
		{"bash", `{"command":"npm test"}`, []string{"Bash", "npm test"}, nil},
		{"read_file", `{"path":"pkg/a.go"}`, []string{"Read", "pkg/a.go"}, nil},
		{"grep", `{"pattern":"TODO","path":"."}`, []string{"Search", "TODO"}, nil},
		{"wait", `{"job_ids":["bash-1","bash-2"],"timeout_seconds":300}`, []string{"Wait", "bash-1", "bash-2"}, []string{"timeout_seconds", "300", "job_ids"}},
		{"web_fetch", `{"url":"https://x.dev"}`, []string{"Fetch", "https://x.dev"}, nil},
		{"use_capability", `{"action":"call","capability_id":"mcp-tool:github/search_issues","arguments":{"query":"bug"}}`, []string{"MCP", "mcp-tool:github/search_issues"}, []string{`"arguments"`, `"query"`, "bug"}},
		{"use_capability", `{"action":"list"}`, []string{"MCP", "list"}, []string{"action"}},
	}
	for _, c := range cases {
		got := toolCard(c.name, c.args, 120)
		for _, w := range c.want {
			if !strings.Contains(got, w) {
				t.Errorf("%s: %q missing %q", c.name, got, w)
			}
		}
		for _, d := range c.deny {
			if strings.Contains(got, d) {
				t.Errorf("%s: %q should not contain raw arg %q", c.name, got, d)
			}
		}
	}
}

func TestToolCardUnknownFallsBackToName(t *testing.T) {
	if got := toolCard("frobnicate", `{}`, 80); !strings.Contains(got, "frobnicate") {
		t.Errorf("unknown tool should show its raw name, got %q", got)
	}
}

// TestToolCardTwoColumnFormat pins the DeepCode v2 card: "● Verb  arg" — the
// verb bold, two spaces, the arg in the theme's muted colour, no parentheses.
func TestToolCardTwoColumnFormat(t *testing.T) {
	defer restoreThemeForTest(activeColorProfile, activeCLITheme)
	activeColorProfile = colorprofile.ANSI256
	configureCLITheme("dark")

	got := toolCard("bash", `{"command":"npm test"}`, 120)
	plain := ansi.Strip(got)
	if !strings.Contains(plain, "Bash  npm test") {
		t.Errorf("card = %q, want verb and arg joined by two spaces", plain)
	}
	if strings.Contains(plain, "(") || strings.Contains(plain, ")") {
		t.Errorf("card keeps the old parenthesised arg: %q", plain)
	}
	if !strings.Contains(got, ansiBold) {
		t.Errorf("verb should be bold: %q", got)
	}
	if !strings.Contains(got, fgSGR(activeCLITheme.muted)) {
		t.Errorf("arg should wear the theme muted colour: %q", got)
	}
}

// TestToolCardNoColourDegradesToPlain proves the NO_COLOR path emits the bare
// two-column line with no escape sequences at all.
func TestToolCardNoColourDegradesToPlain(t *testing.T) {
	defer restoreThemeForTest(activeColorProfile, activeCLITheme)
	activeColorProfile = colorprofile.NoTTY
	configureCLITheme("dark")

	if got, want := toolCard("read_file", `{"path":"pkg/a.go"}`, 80), "  ● Read  pkg/a.go"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestToolCardClampsArgToWidth keeps a long arg inside the card's width budget
// so the transcript never overflows on reflow.
func TestToolCardClampsArgToWidth(t *testing.T) {
	defer restoreThemeForTest(activeColorProfile, activeCLITheme)
	activeColorProfile = colorprofile.NoTTY
	configureCLITheme("dark")

	args := `{"command":"` + strings.Repeat("x", 200) + `"}`
	for _, w := range []int{12, 20, 40, 80} {
		got := toolCard("bash", args, w)
		if width := ansi.StringWidth(got); width > w {
			t.Errorf("width %d: card renders %d columns wide: %q", w, width, got)
		}
	}
}
