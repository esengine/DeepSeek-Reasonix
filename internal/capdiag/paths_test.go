package capdiag

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestURLHostOnly(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"whitespace", "   \t ", ""},
		{"https path query fragment", "https://example.com/path?q=1#frag", "example.com"},
		{"https bare", "https://example.com", "example.com"},
		{"userinfo dropped", "https://user:pass@example.com/path", "example.com"},
		{"userinfo with port", "https://user:pass@example.com:8443/path", "example.com:8443"},
		{"explicit default port kept", "https://example.com:443", "example.com:443"},
		{"ipv4 with port", "http://127.0.0.1:8080/", "127.0.0.1:8080"},
		{"ipv6 bracketed", "http://[::1]:8080/", "[::1]:8080"},
		{"ipv6 no port", "https://[2001:db8::1]", "[2001:db8::1]"},
		{"non-http scheme", "ftp://example.com/file", "example.com"},
		{"scheme-less host", "example.com", "<url>"},
		{"host with colon no scheme", "example.com:8080", "<url>"},
		{"path only", "/just/a/path", "<url>"},
		{"scheme only", "https://", "<url>"},
		{"invalid percent encoding", "https://%zz", "<url>"},
		{"spaces in scheme", "ht tp://example.com", "<url>"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := urlHostOnly(tc.in); got != tc.want {
				t.Errorf("urlHostOnly(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestTransportOf(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty defaults to stdio", "", "stdio"},
		{"whitespace defaults to stdio", "   ", "stdio"},
		{"stdio", "stdio", "stdio"},
		{"stdio case and space", " STDIO ", "stdio"},
		{"http", "http", "http"},
		{"http case", "HTTP", "http"},
		{"http trimmed", " http ", "http"},
		{"streamable-http", "streamable-http", "http"},
		{"streamable_http", "streamable_http", "http"},
		{"streamable-http case", "Streamable-HTTP", "http"},
		{"sse", "sse", "sse"},
		{"sse case", "SSE", "sse"},
		{"unknown passes through", "ws", "ws"},
		{"unknown normalized to lower", "WebSocket", "websocket"},
		{"unknown close to stdio", "stdio2", "stdio2"},
		{"unknown non-transport", "unknown", "unknown"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := transportOf(tc.in); got != tc.want {
				t.Errorf("transportOf(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestIsValidTransport(t *testing.T) {
	valid := []string{"", "   ", "stdio", " STDIO ", "http", "HTTP", "streamable-http", "streamable_http", "sse", "SSE", " sse "}
	for _, in := range valid {
		if !isValidTransport(in) {
			t.Errorf("isValidTransport(%q) = false, want true", in)
		}
	}
	// Empty/whitespace normalize to the default stdio transport, so they are
	// valid here; collect.go guards empties separately before flagging.
	invalid := []string{"ws", "websocket", "unknown", "stdio2", "stdio-extra", "http2"}
	for _, in := range invalid {
		if isValidTransport(in) {
			t.Errorf("isValidTransport(%q) = true, want false", in)
		}
	}
}

func TestSortedKeys(t *testing.T) {
	t.Run("nil map returns nil", func(t *testing.T) {
		if got := sortedKeys(nil); got != nil {
			t.Fatalf("sortedKeys(nil) = %v, want nil", got)
		}
	})
	t.Run("empty map returns nil", func(t *testing.T) {
		if got := sortedKeys(map[string]string{}); got != nil {
			t.Fatalf("sortedKeys(empty) = %v, want nil", got)
		}
	})
	t.Run("single key", func(t *testing.T) {
		if got := sortedKeys(map[string]string{"k": "v"}); !reflect.DeepEqual(got, []string{"k"}) {
			t.Fatalf("got %v, want [k]", got)
		}
	})
	t.Run("already sorted", func(t *testing.T) {
		got := sortedKeys(map[string]string{"a": "1", "b": "2", "c": "3"})
		if want := []string{"a", "b", "c"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("got %v, want %v", got, want)
		}
	})
	t.Run("reversed order", func(t *testing.T) {
		got := sortedKeys(map[string]string{"z": "1", "y": "2", "x": "3", "w": "4", "v": "5", "u": "6"})
		if want := []string{"u", "v", "w", "x", "y", "z"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("got %v, want %v", got, want)
		}
	})
	t.Run("mixed case byte order", func(t *testing.T) {
		got := sortedKeys(map[string]string{"b": "1", "B": "2", "a": "3", "A": "4"})
		// Byte-wise: 'A'(65) < 'B'(66) < 'a'(97) < 'b'(98).
		if want := []string{"A", "B", "a", "b"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("got %v, want %v", got, want)
		}
	})
	t.Run("blank keys skipped", func(t *testing.T) {
		got := sortedKeys(map[string]string{"b": "1", "": "empty", "  ": "spaces", "\t": "tab", "a": "2"})
		if want := []string{"a", "b"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("got %v, want %v", got, want)
		}
	})
}

func TestGuessMCPSource(t *testing.T) {
	t.Run("no mcp json falls back to toml", func(t *testing.T) {
		root := t.TempDir()
		if got := guessMCPSource(root, "server"); got != "toml" {
			t.Fatalf("got %q, want toml", got)
		}
	})
	t.Run("mcp json entry matched", func(t *testing.T) {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, ".mcp.json"), []byte(`{
  "mcpServers": {
    "server": {"url": "http://localhost:8080/sse"},
    "other": {"command": "npx"}
  }
}`), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := guessMCPSource(root, "server"); got != "mcp_json" {
			t.Fatalf("got %q, want mcp_json", got)
		}
	})
	t.Run("name absent in mcp json", func(t *testing.T) {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, ".mcp.json"), []byte(`{"mcpServers": {"other": {"command": "npx"}}}`), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := guessMCPSource(root, "server"); got != "toml" {
			t.Fatalf("got %q, want toml", got)
		}
	})
	t.Run("quote-bounded match only", func(t *testing.T) {
		// The substring check is `"`+name+`"`, so a longer name must not match.
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, ".mcp.json"), []byte(`{"mcpServers": {"server-extra": {"command": "npx"}}}`), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := guessMCPSource(root, "server"); got != "toml" {
			t.Fatalf("got %q, want toml (name prefix must not match)", got)
		}
	})
	t.Run("literal match not regex", func(t *testing.T) {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, ".mcp.json"), []byte(`{"mcpServers": {"a.b+c": {"command": "npx"}}}`), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := guessMCPSource(root, "a.b+c"); got != "mcp_json" {
			t.Fatalf("got %q, want mcp_json (plain substring match)", got)
		}
	})
	t.Run("unreadable mcp json falls back to toml", func(t *testing.T) {
		root := t.TempDir()
		// A directory cannot be read as a file; ReadFile errors.
		if err := os.Mkdir(filepath.Join(root, ".mcp.json"), 0o755); err != nil {
			t.Fatal(err)
		}
		if got := guessMCPSource(root, "server"); got != "toml" {
			t.Fatalf("got %q, want toml", got)
		}
	})
}
