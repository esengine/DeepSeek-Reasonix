package store

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSessionStem(t *testing.T) {
	cases := []struct {
		path, want string
	}{
		{"/home/u/.reasonix/sessions/abc.jsonl", "/home/u/.reasonix/sessions/abc"},
		{"abc.jsonl", "abc"},
		{"abc.events.jsonl", "abc.events"},
		{"abc.jsonl.jsonl", "abc.jsonl"}, // strips exactly one suffix
		{"abc.json", "abc.json"},         // not a .jsonl suffix
		{"", ""},
	}
	for _, c := range cases {
		if got := sessionStem(c.path); got != c.want {
			t.Errorf("sessionStem(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}

func TestRemoteServeLockName(t *testing.T) {
	if got := RemoteServeLockName("home-dev-app"); got != "serve-home-dev-app.lock" {
		t.Errorf("lock name = %q, want %q", got, "serve-home-dev-app.lock")
	}
	if got := RemoteServeLockName(""); got != "serve-.lock" {
		t.Errorf("empty-slug lock name = %q, want %q", got, "serve-.lock")
	}
}

func TestBoundRemoteComponent(t *testing.T) {
	// At or under the budget the component passes through byte-identical.
	if got := boundRemoteComponent("home-dev-app", 180); got != "home-dev-app" {
		t.Errorf("short component = %q, want passthrough", got)
	}
	if got := boundRemoteComponent("", 180); got != "" {
		t.Errorf("empty component = %q, want empty", got)
	}
	if got := boundRemoteComponent("anything", 0); got != "anything" {
		t.Errorf("non-positive budget = %q, want passthrough", got)
	}

	// Over budget: truncated to a rune boundary with an FNV-1a hex suffix,
	// landing exactly at the budget (ASCII input truncates cleanly).
	long := strings.Repeat("verydeepdir/", 30) + "app"
	got := boundRemoteComponent(long, 100)
	if len(got) != 100 {
		t.Fatalf("bounded length = %d, want 100", len(got))
	}
	if !strings.HasPrefix(got, long[:83]) {
		t.Errorf("bounded component lost its readable prefix: %q", got)
	}
	for _, r := range got[len(got)-16:] {
		if !strings.ContainsRune("0123456789abcdef", r) {
			t.Errorf("hash suffix not hex: %q", got)
			break
		}
	}

	// Multibyte input truncates on a rune boundary, never mid-rune.
	wide := strings.Repeat("가", 100) // 300 bytes
	got = boundRemoteComponent(wide, 100)
	if len(got) > 100 || !utf8.ValidString(got) {
		t.Fatalf("wide component = %q (len %d), not valid UTF-8 within budget", got, len(got))
	}
}
