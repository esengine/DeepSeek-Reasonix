package serve

import (
	"strings"
	"testing"

	"reasonix/internal/contract/config"
)

// The launch line is what the user approves, so it is redacted, carries no env
// values, and cannot repaint the terminal or reorder itself with bidi marks.
func TestMcpLaunchLineIsRedactedAndPlain(t *testing.T) {
	got := launchText(config.PluginEntry{
		Command: "node",
		Args:    []string{"srv.js", "--token=abc123secret", "\x1b[2J\x1b]0;pwned\x07ok\r\u202eevil"},
		Env:     map[string]string{"API_KEY": "envsecret"},
	})
	for _, bad := range []string{"abc123secret", "envsecret", "\x1b", "\x07", "\r", "\u202e"} {
		if strings.Contains(got, bad) {
			t.Fatalf("launch line %q contains %q", got, bad)
		}
	}
	if !strings.HasPrefix(got, "node srv.js") {
		t.Fatalf("launch line = %q", got)
	}
	url := launchText(config.PluginEntry{URL: "https://user:pw@host.example/mcp?token=zzz"})
	if strings.Contains(url, "pw@") || strings.Contains(url, "zzz") {
		t.Fatalf("url not redacted: %q", url)
	}
	long := launchText(config.PluginEntry{Command: "node", Args: []string{strings.Repeat("a", 5000)}})
	if len([]rune(long)) > mcpLaunchTextLimit+1 {
		t.Fatalf("launch line not capped: %d runes", len([]rune(long)))
	}
}

func TestMcpLaunchRedactsKeysSplitByFormatCharacters(t *testing.T) {
	got := launchText(config.PluginEntry{
		Command: "node",
		Args:    []string{"--tok\u200ben=hidden123", "--pass\u202ewor\u2060d", "split\u200bvalue"},
	})
	if strings.Contains(got, "hidden123") {
		t.Fatalf("zero-width-split key leaked its value: %q", got)
	}
}
