package outbound

import (
	"reflect"
	"strings"
	"testing"
)

func TestSplitWithCodeFences(t *testing.T) {
	t.Run("short text", func(t *testing.T) {
		text := "Hello world"
		out := SplitWithCodeFences(text, 100)
		if !reflect.DeepEqual(out, []string{text}) {
			t.Errorf("Expected %v, got %v", []string{text}, out)
		}
	})

	t.Run("split plain text", func(t *testing.T) {
		text := strings.Repeat("A", 40)
		out := SplitWithCodeFences(text, 30)
		// Single long line won't be split inside SplitWithCodeFences
		if len(out) != 1 {
			t.Errorf("Expected 1 chunks, got %d", len(out))
		}
	})

	t.Run("split with code fences", func(t *testing.T) {
		text := "Line 1\n```go\nfunc main() {\n\tfmt.Println(\"Hello\")\n}\n```\nLine 2"
		out := SplitWithCodeFences(text, 40)

		if len(out) != 2 {
			t.Fatalf("Expected 2 chunks, got %d", len(out))
		}

		if !strings.HasSuffix(out[0], "```") {
			t.Errorf("First chunk should close the fence, got: %s", out[0])
		}

		if !strings.HasPrefix(out[1], "```go") {
			t.Errorf("Second chunk should reopen the fence, got: %s", out[1])
		}
	})

	t.Run("split with heading", func(t *testing.T) {
		text := strings.Repeat("A", 30) + "\n# Heading\nMore text"
		out := SplitWithCodeFences(text, 35)

		if len(out) != 2 {
			t.Fatalf("Expected 2 chunks, got %d", len(out))
		}

		if strings.Contains(out[0], "# Heading") {
			t.Errorf("First chunk should break before heading, got: %s", out[0])
		}
		if !strings.HasPrefix(out[1], "# Heading") {
			t.Errorf("Second chunk should start with heading, got: %s", out[1])
		}
	})
}
