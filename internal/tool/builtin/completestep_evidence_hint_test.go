package builtin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/evidence"
)

func TestCompleteStepVerificationWithoutCommandSuggestsOtherKinds(t *testing.T) {
	ctx := evidence.WithLedger(context.Background(), evidence.NewLedger())
	_, err := completeStep{}.Execute(ctx, json.RawMessage(`{
		"step":"Remove debug files",
		"result":"debug files removed from git",
		"evidence":[{"kind":"verification","summary":"git commit abc123 updated .gitignore"}]
	}`))
	if err == nil {
		t.Fatal("verification evidence without a command should be rejected")
	}
	got := err.Error()
	for _, want := range []string{"verification command is required", `"files"`, `"diff"`, `"manual"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("error %q missing %q", got, want)
		}
	}
}
