package builtin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/contract/provider"
	"reasonix/internal/safety/evidence"
)

func TestAllCommandHintsCutsLongCommandOnRuneBoundary(t *testing.T) {
	// 80 bytes into this command falls inside a three-byte character, and
	// %q would render the split bytes as \x escapes in the hint.
	cmd := `echo "` + strings.Repeat("测试", 20) + `"`
	args, err := json.Marshal(map[string]string{"command": cmd})
	if err != nil {
		t.Fatal(err)
	}
	msgs := []provider.Message{{
		Role:      provider.RoleAssistant,
		ToolCalls: []provider.ToolCall{{ID: "c1", Name: "bash", Arguments: string(args)}},
	}}
	ctx := evidence.WithSessionMessages(context.Background(), func() []provider.Message { return msgs })

	hint := allCommandHints(ctx, nil)
	if !strings.Contains(hint, "echo") {
		t.Fatalf("hint does not list the command: %q", hint)
	}
	if strings.Contains(hint, `\x`) {
		t.Fatalf("hint split a character: %s", hint)
	}
}
