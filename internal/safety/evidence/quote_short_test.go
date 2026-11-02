package evidence

import (
	"strconv"
	"strings"
	"testing"
)

func TestQuoteShortCutsOnRuneBoundary(t *testing.T) {
	// 80 bytes into this command falls inside a three-byte character, and
	// strconv.Quote would render the split bytes as \x escapes.
	cmd := `echo "` + strings.Repeat("测试", 20) + `"`
	got := quoteShort(cmd)
	if strings.Contains(got, `\x`) {
		t.Fatalf("quoteShort split a character: %s", got)
	}
	unquoted, err := strconv.Unquote(got)
	if err != nil {
		t.Fatalf("quoteShort result does not unquote: %v", err)
	}
	if !strings.HasSuffix(unquoted, "…") || !strings.HasPrefix(cmd, strings.TrimSuffix(unquoted, "…")) {
		t.Fatalf("quoteShort = %s, want a prefix of the command ending in …", got)
	}
	if len(strings.TrimSuffix(unquoted, "…")) > 80 {
		t.Fatalf("quoteShort kept more than 80 bytes: %s", got)
	}
}

func TestQuoteShortKeepsShortASCII(t *testing.T) {
	if got := quoteShort("  go test ./...  "); got != `"go test ./..."` {
		t.Fatalf("quoteShort = %s", got)
	}
}
