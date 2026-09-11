package agent

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"reasonix/internal/provider"
)

func TestLegacyMigrationKeepsUnknownOriginForFallback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.jsonl")
	content := CompletionValidationContinuationPrefix + " the last message did not deliver a self-contained final result."
	if err := os.WriteFile(path, []byte(`{"type":"user.message","text":`+strconv.Quote(content)+`}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	msgs, err := reconstructSession(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || msgs[0].Origin != "" || !IsHostGeneratedUserMessage(msgs[0]) {
		t.Fatalf("migrated legacy message = %+v, want unknown origin classified by fallback", msgs)
	}
}

func TestLegacyCoalescedSummaryKeepsUnknownTailOrigin(t *testing.T) {
	combined := formatSummaryMessage("prior context")
	combined.Content += "\n\n" + CompletionValidationContinuationPrefix + " the last message did not deliver a self-contained final result."
	_, tail, ok := splitLegacyCoalescedSummary(combined)
	if !ok || tail.Role != provider.RoleUser || tail.Origin != "" || !IsHostGeneratedUserMessage(tail) {
		t.Fatalf("split legacy tail = %+v, ok=%v, want unknown origin with fallback", tail, ok)
	}
}
