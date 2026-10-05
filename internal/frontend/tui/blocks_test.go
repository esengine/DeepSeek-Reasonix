package tui

import "testing"

func TestSettledPrefixStopsAtFinishedBlocks(t *testing.T) {
	cases := []struct {
		name, text, settled string
	}{
		{"no blank line yet", "a paragraph still", ""},
		{"one finished paragraph", "first para\n\nsecond", "first para\n\n"},
		{"the last blank line wins", "a\n\nb\n\nc", "a\n\nb\n\n"},
		{"an open fence holds everything after it",
			"intro\n\n```go\nfunc a() {\n\n}\n", "intro\n\n"},
		{"a closed fence settles once a blank line follows it",
			"intro\n\n```go\nx\n\ny\n```\n\nafter", "intro\n\n```go\nx\n\ny\n```\n\n"},
		{"tilde fences count too", "~~~\na\n\nb\n", ""},
		{"a blank line without its newline is not finished", "a\n", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.text[:settledPrefix(c.text)]; got != c.settled {
				t.Fatalf("settled = %q, want %q", got, c.settled)
			}
		})
	}
}
