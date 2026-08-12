package agent

import (
	"errors"
	"path/filepath"
	"testing"

	"reasonix/internal/provider"
)

// Identity must survive a write/load round-trip even when tool output carries
// invalid UTF-8: encoding/json escapes each raw invalid byte as \ufffd and
// loading turns it into the U+FFFD rune, so the live transcript (raw bytes)
// and its reloaded copy must still compare equal.
func TestSessionIdentityStableAcrossInvalidUTF8RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	mojibake := "tool output \xff\xfe mojibake"
	s := NewSession("sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: mojibake, ReasoningContent: "thinking \xff bad"})
	if err := s.Save(path); err != nil {
		t.Fatalf("Save with invalid UTF-8: %v", err)
	}

	// The live session still holds the raw bytes; the reloaded copy holds the
	// U+FFFD rune. Identity must agree so the next save is a clean append.
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "second"})
	err := s.SaveSnapshot(path)
	if errors.Is(err, ErrSessionSnapshotConflict) {
		t.Fatalf("SaveSnapshot after invalid-UTF-8 save conflicted: %v", err)
	}
	if err != nil {
		t.Fatalf("SaveSnapshot after invalid-UTF-8 save: %v", err)
	}

	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	memDigest, err := digestSessionMessages(s.Snapshot())
	if err != nil {
		t.Fatalf("digest in-memory messages: %v", err)
	}
	diskDigest, err := digestSessionMessages(loaded.Snapshot())
	if err != nil {
		t.Fatalf("digest loaded messages: %v", err)
	}
	if memDigest != diskDigest {
		t.Fatalf("transcript identity drifted across round-trip: memory=%s disk=%s", digestString(memDigest), digestString(diskDigest))
	}
}

// The identity round-trip must match what the JSON encoder itself produces, so
// invalid bytes in nested fields (tool execution, tool-call arguments) cannot
// reintroduce drift without a message-level change being caught here.
func TestMessageForSessionIdentityNormalizesNestedInvalidUTF8(t *testing.T) {
	execution := &provider.ToolExecution{OutputTail: "tail \xff\xfe"}
	m := provider.Message{
		Role:          provider.RoleTool,
		Content:       "result \xff",
		ToolExecution: execution,
	}
	got := messageForSessionIdentity(m)
	if got.Content != "result \ufffd" {
		t.Fatalf("Content = %q, want normalized", got.Content)
	}
	if got.ToolExecution.OutputTail != "tail \ufffd\ufffd" {
		t.Fatalf("OutputTail = %q, want per-byte normalized", got.ToolExecution.OutputTail)
	}
	if got.ToolExecution != execution {
		t.Fatalf("identity must not alias the caller's ToolExecution")
	}
}
