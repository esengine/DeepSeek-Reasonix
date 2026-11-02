package recovery

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestArgsSummaryCutsOnRuneBoundary(t *testing.T) {
	// 20 bytes into this command falls inside a three-byte character.
	cmd := "echo 'a" + strings.Repeat("测试", 10) + "'"
	args, _ := json.Marshal(map[string]string{"command": cmd})
	got := ArgsSummary(args, 20)
	if !utf8.ValidString(got) {
		t.Fatalf("ArgsSummary split a character: %q", got)
	}
	kept := strings.TrimSuffix(got, "…")
	if kept == got || !strings.HasPrefix(cmd, kept) || len(kept) > 20 || len(kept) < 18 {
		t.Fatalf("ArgsSummary = %q, want a prefix of the command just under 20 bytes ending in …", got)
	}
}

func TestArgsSummaryKeepsShortCommand(t *testing.T) {
	args, _ := json.Marshal(map[string]string{"command": "  go   test ./...  "})
	if got := ArgsSummary(args, 20); got != "go test ./..." {
		t.Fatalf("ArgsSummary = %q", got)
	}
}
