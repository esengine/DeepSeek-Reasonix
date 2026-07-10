package agent

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/provider"
	"reasonix/internal/store"
)

// touch sets a file's mtime to t. Used by the listing-order test so it
// doesn't have to sleep between Saves.
func touch(path string, t time.Time) error {
	return os.Chtimes(path, t, t)
}

// TestSaveLoadRoundTrip is the contract `reasonix --resume` depends on: a
// session written to disk reloads byte-for-byte, including tool calls and
// reasoning content (which the model wants to keep across resumes for cache
// hits on thinking-mode providers).
func TestSaveLoadRoundTrip(t *testing.T) {
	s := NewSession("you are reasonix")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "find the bug"})
	s.Add(provider.Message{
		Role:             provider.RoleAssistant,
		Content:          "Let me check.",
		ReasoningContent: "I should look at main.go first.",
		ToolCalls: []provider.ToolCall{{
			ID: "call_1", Name: "read_file", Arguments: `{"path":"main.go"}`,
		}},
	})
	s.Add(provider.Message{
		Role: provider.RoleTool, Name: "read_file", ToolCallID: "call_1",
		Content: "package main\nfunc main() {}\n",
	})
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "It's fine."})

	path := filepath.Join(t.TempDir(), "s.jsonl")
	if err := s.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if got, want := len(loaded.Messages), len(s.Messages); got != want {
		t.Fatalf("message count after round-trip = %d, want %d", got, want)
	}
	for i, m := range s.Messages {
		if loaded.Messages[i].Role != m.Role {
			t.Errorf("message %d role mismatch", i)
		}
		if loaded.Messages[i].Content != m.Content {
			t.Errorf("message %d content mismatch", i)
		}
		if loaded.Messages[i].ReasoningContent != m.ReasoningContent {
			t.Errorf("message %d reasoning mismatch", i)
		}
		if len(loaded.Messages[i].ToolCalls) != len(m.ToolCalls) {
			t.Errorf("message %d tool_calls count mismatch", i)
		}
	}
}

func TestSaveRedactsSecretsOnDisk(t *testing.T) {
	secret := "sk-real-secret-value-123456"
	s := NewSession("sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "inspect"})
	s.Add(provider.Message{
		Role:       provider.RoleTool,
		Name:       "bash",
		ToolCallID: "call_1",
		Content:    "DEEPSEEK_API_KEY=" + secret + "\n",
	})

	path := filepath.Join(t.TempDir(), "redacted.jsonl")
	if err := s.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read saved session: %v", err)
	}
	if strings.Contains(string(body), secret) {
		t.Fatalf("session persisted raw secret:\n%s", body)
	}
	if !strings.Contains(string(body), "DEEPSEEK_API_KEY=sk-rea") {
		t.Fatalf("session did not persist a masked credential marker:\n%s", body)
	}
}

// TestSaveRedactionKeepsSnapshotBaselineStable is the digest-consistency
// contract behind redact-on-save: because Redact is idempotent, a loaded
// (already-redacted) transcript re-saves as the exact same bytes, so
// SaveSnapshot must take the up-to-date path — no rewrite, no revision bump,
// and above all no bogus snapshot conflict / recovery fork (#6083's failure
// shape). Appending after the resave must land as a plain append.
func TestSaveRedactionKeepsSnapshotBaselineStable(t *testing.T) {
	secret := "sk-real-secret-value-123456"
	s := NewSession("sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "inspect"})
	s.Add(provider.Message{
		Role:       provider.RoleTool,
		Name:       "bash",
		ToolCallID: "call_1",
		Content:    "DEEPSEEK_API_KEY=" + secret + "\n",
	})

	path := filepath.Join(t.TempDir(), "stable.jsonl")
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("first SaveSnapshot: %v", err)
	}
	rev1, _, err := sessionContentRevision(path)
	if err != nil {
		t.Fatalf("read revision: %v", err)
	}

	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	// Re-saving the loaded transcript re-runs RedactMessages over content that
	// is already masked; idempotence means identical bytes and the up-to-date
	// skip, not a conflict or a revision bump.
	if err := loaded.SaveSnapshot(path); err != nil {
		t.Fatalf("resave of loaded redacted session: %v", err)
	}
	rev2, _, err := sessionContentRevision(path)
	if err != nil {
		t.Fatalf("read revision after resave: %v", err)
	}
	if rev1 != rev2 {
		t.Fatalf("no-op resave bumped revision %d -> %d; redaction broke digest stability", rev1, rev2)
	}

	// A continued conversation still append-saves cleanly on top.
	loaded.Add(provider.Message{Role: provider.RoleAssistant, Content: "done"})
	if err := loaded.SaveSnapshot(path); err != nil {
		t.Fatalf("append snapshot after redacted round-trip: %v", err)
	}
	reloaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := len(reloaded.Messages); got != 4 {
		t.Fatalf("message count after append = %d, want 4", got)
	}
}

func TestSaveLoadLargeMessage(t *testing.T) {
	s := NewSession("sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "run it"})
	// A bash result can exceed any line-buffer cap; Save must round-trip it.
	big := strings.Repeat("x", 5*1024*1024)
	s.Add(provider.Message{Role: provider.RoleTool, Name: "bash", ToolCallID: "c1", Content: big})

	path := filepath.Join(t.TempDir(), "big.jsonl")
	if err := s.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession of a session with a >4MiB message: %v", err)
	}
	if len(loaded.Messages) != 3 {
		t.Fatalf("message count = %d, want 3", len(loaded.Messages))
	}
	if loaded.Messages[2].Content != big {
		t.Errorf("large content not round-tripped (got %d bytes, want %d)", len(loaded.Messages[2].Content), len(big))
	}
}

func TestSaveSnapshotRejectsStalePrefixOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	current := NewSession("sys")
	current.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	current.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	current.Add(provider.Message{Role: provider.RoleUser, Content: "second"})
	current.Add(provider.Message{Role: provider.RoleAssistant, Content: "two"})
	if err := current.Save(path); err != nil {
		t.Fatalf("Save current: %v", err)
	}

	stale := NewSession("sys")
	stale.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	stale.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	if err := stale.SaveSnapshot(path); !errors.Is(err, ErrSessionSnapshotConflict) {
		t.Fatalf("SaveSnapshot stale prefix err = %v, want ErrSessionSnapshotConflict", err)
	}

	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if got := len(loaded.Messages); got != 5 {
		t.Fatalf("message count after stale snapshot = %d, want 5", got)
	}
	if got := loaded.Messages[4].Content; got != "two" {
		t.Fatalf("last message after stale snapshot = %q, want %q", got, "two")
	}
}

func TestSaveSnapshotAllowsAppendFromDiskPrefix(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	base := NewSession("sys")
	base.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	if err := base.Save(path); err != nil {
		t.Fatalf("Save base: %v", err)
	}

	next := NewSession("sys")
	next.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	next.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	if err := next.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot append: %v", err)
	}

	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if got := len(loaded.Messages); got != 3 {
		t.Fatalf("message count after append snapshot = %d, want 3", got)
	}
}

// TestSaveSnapshotAppendsAcrossInterruptedToolCallTail is the mid-turn autosave
// shape from the field: a snapshot lands between an assistant tool call and its
// still-running result, so the transcript on disk ends with a dangling call
// that LoadSession answers with a fabricated placeholder. The live session then
// records the real result and keeps going. The next snapshot is a pure append
// over the bytes on disk and must land as one — not collide with the
// placeholder, misread the turn as divergence, and fork a recovery branch.
func TestSaveSnapshotAppendsAcrossInterruptedToolCallTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	s := NewSession("sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "run the build"})
	s.Add(provider.Message{
		Role: provider.RoleAssistant, Content: "Running it.",
		ToolCalls: []provider.ToolCall{{ID: "call_1", Name: "bash", Arguments: `{"cmd":"make"}`}},
	})
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("mid-turn SaveSnapshot: %v", err)
	}

	s.Add(provider.Message{Role: provider.RoleTool, Name: "bash", ToolCallID: "call_1", Content: "ok"})
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "Build passed."})
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot after tool result: %v", err)
	}

	replay, err := replaySessionEventLog(SessionEventLogPath(path))
	if err != nil {
		t.Fatalf("replay event log: %v", err)
	}
	if replay.damaged {
		t.Fatal("event log damaged after appending over an interrupted tool tail")
	}
	if replay.records != 2 {
		t.Fatalf("event log records = %d, want 2 (bootstrap replace + append)", replay.records)
	}
	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if got := len(loaded.Messages); got != 5 {
		t.Fatalf("message count after append snapshot = %d, want 5", got)
	}
	if got := loaded.Messages[3].Content; got != "ok" {
		t.Fatalf("tool result after round-trip = %q, want %q", got, "ok")
	}
	if loaded.normalizedDirty {
		t.Fatal("transcript still needs repair after appending the real tool result")
	}
}

// TestSaveSnapshotAppendsAcrossPartiallyAnsweredMultiToolCallTail covers the
// same raw-prefix fallback when a multi-call assistant turn already has some
// tool results on disk. LoadSession fabricates placeholders only for the still
// unanswered calls; the live session later appends the real remaining results.
func TestSaveSnapshotAppendsAcrossPartiallyAnsweredMultiToolCallTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	s := NewSession("sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "inspect and test"})
	s.Add(provider.Message{
		Role: provider.RoleAssistant, Content: "I will run a few checks.",
		ToolCalls: []provider.ToolCall{
			{ID: "call_read", Name: "read_file", Arguments: `{"path":"main.go"}`},
			{ID: "call_test", Name: "bash", Arguments: `{"cmd":"go test ./..."}`},
			{ID: "call_status", Name: "bash", Arguments: `{"cmd":"git status --short"}`},
		},
	})
	s.Add(provider.Message{Role: provider.RoleTool, Name: "read_file", ToolCallID: "call_read", Content: "package main"})
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("partial multi-tool SaveSnapshot: %v", err)
	}

	loadedPartial, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession partial: %v", err)
	}
	if !loadedPartial.normalizedDirty {
		t.Fatal("partial multi-tool load should need placeholder repair")
	}

	s.Add(provider.Message{Role: provider.RoleTool, Name: "bash", ToolCallID: "call_test", Content: "ok"})
	s.Add(provider.Message{Role: provider.RoleTool, Name: "bash", ToolCallID: "call_status", Content: "clean"})
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "All checks passed."})
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot after remaining multi-tool results: %v", err)
	}

	events := readSessionEventsForTest(t, path)
	if len(events) != 2 || events[1].Type != sessionEventTypeAppend {
		t.Fatalf("events after multi-tool append = %+v, want trailing append", events)
	}
	if events[1].MessageIndex != 4 || len(events[1].Messages) != 3 {
		t.Fatalf("multi-tool append event index=%d len=%d, want index 4 len 3", events[1].MessageIndex, len(events[1].Messages))
	}
	replay, err := replaySessionEventLog(SessionEventLogPath(path))
	if err != nil {
		t.Fatalf("replay event log: %v", err)
	}
	if replay.damaged {
		t.Fatal("event log damaged after appending remaining multi-tool results")
	}
	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession final: %v", err)
	}
	if got := len(loaded.Messages); got != 7 {
		t.Fatalf("message count after multi-tool append = %d, want 7", got)
	}
	if got := loaded.Messages[4].Content; got != "ok" {
		t.Fatalf("second tool result after round-trip = %q, want ok", got)
	}
	if got := loaded.Messages[5].Content; got != "clean" {
		t.Fatalf("third tool result after round-trip = %q, want clean", got)
	}
	if loaded.normalizedDirty {
		t.Fatal("multi-tool transcript still needs repair after real results landed")
	}
}

// TestSaveSnapshotUnchangedInterruptedToolCallTailIsNoOp covers the turn that
// stays interrupted (cancel, crash recovery with nothing new in memory):
// re-snapshotting the exact bytes on disk must be a no-op, not a stale-prefix
// conflict against the placeholder the load-time repair fabricated.
func TestSaveSnapshotUnchangedInterruptedToolCallTailIsNoOp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	s := NewSession("sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "run the build"})
	s.Add(provider.Message{
		Role: provider.RoleAssistant, Content: "Running it.",
		ToolCalls: []provider.ToolCall{{ID: "call_1", Name: "bash", Arguments: `{"cmd":"make"}`}},
	})
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("mid-turn SaveSnapshot: %v", err)
	}
	logBefore, err := os.ReadFile(SessionEventLogPath(path))
	if err != nil {
		t.Fatalf("ReadFile event log: %v", err)
	}

	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot unchanged: %v", err)
	}
	logAfter, err := os.ReadFile(SessionEventLogPath(path))
	if err != nil {
		t.Fatalf("ReadFile event log after no-op snapshot: %v", err)
	}
	if string(logBefore) != string(logAfter) {
		t.Fatal("no-op snapshot rewrote the event log")
	}
	if revision, _, err := sessionContentRevision(path); err != nil || revision != 1 {
		t.Fatalf("revision after no-op snapshot = %d (err %v), want 1", revision, err)
	}
}

// TestSaveRewriteOwnedAcrossInterruptedToolCallTail: compaction rewrites the
// in-memory history while the transcript on disk still ends with the dangling
// call a mid-turn snapshot left behind. Ownership is anchored on the raw bytes
// this session wrote; the placeholder fabricated on load must not revoke it.
func TestSaveRewriteOwnedAcrossInterruptedToolCallTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	s := NewSession("sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "run the build"})
	s.Add(provider.Message{
		Role: provider.RoleAssistant, Content: "Running it.",
		ToolCalls: []provider.ToolCall{{ID: "call_1", Name: "bash", Arguments: `{"cmd":"make"}`}},
	})
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("mid-turn SaveSnapshot: %v", err)
	}

	s.Replace([]provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "[compacted] run the build"},
	})
	if err := s.SaveRewrite(path); err != nil {
		t.Fatalf("SaveRewrite after compaction: %v", err)
	}

	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if got := len(loaded.Messages); got != 2 {
		t.Fatalf("message count after owned rewrite = %d, want 2", got)
	}
	if got := loaded.Messages[1].Content; got != "[compacted] run the build" {
		t.Fatalf("compacted message after round-trip = %q", got)
	}
}

// TestSaveSnapshotAfterDirtyResumeKeepsEventChainReplayable: a session resumed
// from an interrupted tool tail carries the load-time repair in memory, so its
// transcript is one message longer than what the event log replays. The next
// snapshot must not take the append shortcut with that inflated index — the
// chain-broken append event would be discarded on replay, silently dropping
// the whole new turn from disk. It must fall back to a full rewrite that
// persists the repair and the new turn together.
func TestSaveSnapshotAfterDirtyResumeKeepsEventChainReplayable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	s := NewSession("sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "run the build"})
	s.Add(provider.Message{
		Role: provider.RoleAssistant, Content: "Running it.",
		ToolCalls: []provider.ToolCall{{ID: "call_1", Name: "bash", Arguments: `{"cmd":"make"}`}},
	})
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("mid-turn SaveSnapshot: %v", err)
	}

	// Crash + reopen: the resume load answers the dangling call with a
	// placeholder, then the user runs another turn.
	resumed, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession resume: %v", err)
	}
	if !resumed.normalizedDirty {
		t.Fatal("resume load should carry a pending repair for the dangling call")
	}
	resumed.Add(provider.Message{Role: provider.RoleUser, Content: "try again"})
	resumed.Add(provider.Message{Role: provider.RoleAssistant, Content: "done"})
	if err := resumed.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot after dirty resume: %v", err)
	}

	replay, err := replaySessionEventLog(SessionEventLogPath(path))
	if err != nil {
		t.Fatalf("replay event log: %v", err)
	}
	if replay.damaged {
		t.Fatal("event log chain broken by the post-resume snapshot")
	}
	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession reload: %v", err)
	}
	if got := len(loaded.Messages); got != 6 {
		t.Fatalf("message count after post-resume snapshot = %d, want 6", got)
	}
	if got := loaded.Messages[3].ToolCallID; got != "call_1" {
		t.Fatalf("placeholder tool result not persisted; message 3 tool_call_id = %q", got)
	}
	if got := loaded.Messages[5].Content; got != "done" {
		t.Fatalf("new turn after round-trip = %q, want %q", got, "done")
	}
}

// TestSaveSnapshotAfterDirtyResumeWithTruncatedToolArgsPersistsRepair covers a
// same-length load-time repair: truncated tool-call JSON is fixed in memory, but
// appending against the repaired view would leave the broken arguments on disk.
// The snapshot must rewrite so the repair and new turn persist together.
func TestSaveSnapshotAfterDirtyResumeWithTruncatedToolArgsPersistsRepair(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	s := NewSession("sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "run tests"})
	s.Add(provider.Message{
		Role:      provider.RoleAssistant,
		Content:   "Running tests.",
		ToolCalls: []provider.ToolCall{{ID: "call_1", Name: "bash", Arguments: `{"cmd":"go test ./...`}},
	})
	s.Add(provider.Message{Role: provider.RoleTool, Name: "bash", ToolCallID: "call_1", Content: "ok"})
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot with truncated args: %v", err)
	}

	resumed, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession resume: %v", err)
	}
	if !resumed.normalizedDirty {
		t.Fatal("resume load should carry a pending repair for truncated tool arguments")
	}
	const repairedArgs = `{"cmd":"go test ./..."}`
	if got := resumed.Messages[2].ToolCalls[0].Arguments; got != repairedArgs {
		t.Fatalf("repaired args on resume = %q, want %q", got, repairedArgs)
	}

	resumed.Add(provider.Message{Role: provider.RoleUser, Content: "summarize"})
	resumed.Add(provider.Message{Role: provider.RoleAssistant, Content: "Tests passed."})
	if err := resumed.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot after truncated-args resume: %v", err)
	}

	events := readSessionEventsForTest(t, path)
	if len(events) != 2 || events[1].Type != sessionEventTypeReplace || events[1].Reason != "snapshot" {
		t.Fatalf("events after truncated-args repair = %+v, want trailing snapshot replace", events)
	}
	replay, err := replaySessionEventLog(SessionEventLogPath(path))
	if err != nil {
		t.Fatalf("replay event log: %v", err)
	}
	if replay.damaged {
		t.Fatal("event log damaged after truncated-args repair rewrite")
	}
	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession reload: %v", err)
	}
	if loaded.normalizedDirty {
		t.Fatal("truncated-args repair was not persisted")
	}
	if got := loaded.Messages[2].ToolCalls[0].Arguments; got != repairedArgs {
		t.Fatalf("persisted args = %q, want %q", got, repairedArgs)
	}
	if got := loaded.Messages[5].Content; got != "Tests passed." {
		t.Fatalf("new turn after round-trip = %q, want %q", got, "Tests passed.")
	}
}

// TestSaveRecoveryBranchNotNeededWhenRawTranscriptCoversSnapshot: the
// recovery-needed check must also judge coverage against the pre-repair bytes.
// Here the stored transcript equals the snapshot exactly, but normalization
// backfills an empty tool-call name on load; that repair must not make the
// disk look like it fails to cover the snapshot and fork a pointless recovery.
func TestSaveRecoveryBranchNotNeededWhenRawTranscriptCoversSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	s := NewSession("sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "run the build"})
	s.Add(provider.Message{
		Role: provider.RoleAssistant, Content: "Running it.",
		ToolCalls: []provider.ToolCall{{ID: "call_1", Name: "", Arguments: `{"cmd":"make"}`}},
	})
	s.Add(provider.Message{Role: provider.RoleTool, Name: "bash", ToolCallID: "call_1", Content: "ok"})
	if err := s.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	_, err := s.SaveRecoveryBranch(RecoveryBranchOptions{OriginalPath: path})
	if !errors.Is(err, ErrSessionRecoveryNotNeeded) {
		t.Fatalf("SaveRecoveryBranch err = %v, want ErrSessionRecoveryNotNeeded", err)
	}
}

func TestSaveSnapshotAppendsWithoutReplacingPrefixFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	base := NewSession("sys")
	base.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	if err := base.Save(path); err != nil {
		t.Fatalf("Save base: %v", err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat before append: %v", err)
	}

	next := NewSession("sys")
	next.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	next.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	if err := next.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot append: %v", err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat after append: %v", err)
	}
	if !os.SameFile(before, after) {
		t.Fatal("SaveSnapshot replaced the session file; want append-in-place for disk-prefix snapshots")
	}
}

func TestSaveSnapshotAppendsToEventLogWithoutChangingCheckpoint(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	base := NewSession("sys")
	base.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	if err := base.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot base: %v", err)
	}
	checkpointBefore, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile checkpoint before append: %v", err)
	}

	next, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession base: %v", err)
	}
	next.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	if err := next.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot append: %v", err)
	}

	checkpointAfter, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile checkpoint after append: %v", err)
	}
	if string(checkpointAfter) != string(checkpointBefore) {
		t.Fatalf("checkpoint changed after append-only snapshot:\nbefore=%s\nafter=%s", checkpointBefore, checkpointAfter)
	}
	events := readSessionEventsForTest(t, path)
	if len(events) != 2 {
		t.Fatalf("event count = %d, want replace + append", len(events))
	}
	if events[0].Type != sessionEventTypeReplace || events[1].Type != sessionEventTypeAppend {
		t.Fatalf("event types = %q, %q; want replace, append", events[0].Type, events[1].Type)
	}
	if events[1].MessageIndex != 2 || len(events[1].Messages) != 1 || events[1].Messages[0].Content != "one" {
		t.Fatalf("append event = %+v, want assistant suffix at index 2", events[1])
	}
	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession after append: %v", err)
	}
	if got := loaded.Messages[len(loaded.Messages)-1].Content; got != "one" {
		t.Fatalf("loaded tail = %q, want one", got)
	}
}

func TestSaveRewriteAppendsReplaceEventAndRefreshesCheckpoint(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	base := NewSession("sys")
	base.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	base.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	if err := base.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot base: %v", err)
	}

	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession base: %v", err)
	}
	loaded.Replace([]provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "rewound"},
	})
	if err := loaded.SaveRewrite(path); err != nil {
		t.Fatalf("SaveRewrite: %v", err)
	}
	// Rewrites refresh the compatibility checkpoint so direct .jsonl readers
	// and older binaries stay bounded-stale instead of frozen at first save.
	anchor, err := loadSessionMessagesFromJSONL(path)
	if err != nil {
		t.Fatalf("read checkpoint after rewrite: %v", err)
	}
	if len(anchor) != 2 || anchor[1].Content != "rewound" {
		t.Fatalf("checkpoint after rewrite = %+v, want refreshed rewound transcript", anchor)
	}
	events := readSessionEventsForTest(t, path)
	if len(events) != 2 || events[1].Type != sessionEventTypeReplace || events[1].Reason != "rewrite" {
		t.Fatalf("events after rewrite = %+v, want trailing rewrite replace", events)
	}
	reloaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession after rewrite: %v", err)
	}
	if len(reloaded.Messages) != 2 || reloaded.Messages[1].Content != "rewound" {
		t.Fatalf("replayed rewrite messages = %+v", reloaded.Messages)
	}
}

func TestSaveSnapshotMigratesLegacyJSONLToEventLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.jsonl")
	if err := os.WriteFile(path, []byte(`{"role":"system","content":"sys"}`+"\n"+`{"role":"user","content":"legacy"}`+"\n"), 0o644); err != nil {
		t.Fatalf("write legacy jsonl: %v", err)
	}
	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession legacy: %v", err)
	}
	loaded.Add(provider.Message{Role: provider.RoleAssistant, Content: "migrated"})
	if err := loaded.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot legacy append: %v", err)
	}
	events := readSessionEventsForTest(t, path)
	if len(events) != 1 || events[0].Type != sessionEventTypeReplace {
		t.Fatalf("legacy migration events = %+v, want one replace seed", events)
	}
	reloaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession migrated: %v", err)
	}
	if got := reloaded.Messages[len(reloaded.Messages)-1].Content; got != "migrated" {
		t.Fatalf("migrated tail = %q, want migrated", got)
	}
	if _, err := os.Stat(SessionEventIndexPath(path)); err != nil {
		t.Fatalf("event index missing: %v", err)
	}
}

func TestSaveSnapshotAllowsAppendAfterSystemPromptRefresh(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	base := NewSession("old sys")
	base.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	if err := base.Save(path); err != nil {
		t.Fatalf("Save base: %v", err)
	}

	next := NewSession("new sys")
	next.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	next.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	if err := next.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot after system refresh: %v", err)
	}

	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if got := len(loaded.Messages); got != 3 {
		t.Fatalf("message count after system refresh append = %d, want 3", got)
	}
	if got := loaded.Messages[0].Content; got != "new sys" {
		t.Fatalf("system prompt after refresh = %q, want %q", got, "new sys")
	}
}

func TestSaveSnapshotRecordsRevisionAndMetaUpdatesPreserveIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	base := NewSession("sys")
	base.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	if err := base.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot base: %v", err)
	}
	meta, ok, err := LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta base ok=%v err=%v", ok, err)
	}
	if meta.Revision != 1 || meta.ContentDigest == "" || meta.WriterID == "" {
		t.Fatalf("base persistence meta = %+v, want revision/digest/writer", meta)
	}

	if err := UpdateSessionMeta(path, "model-a", "first", 1, true); err != nil {
		t.Fatalf("UpdateSessionMeta: %v", err)
	}
	refreshed, ok, err := LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta refreshed ok=%v err=%v", ok, err)
	}
	if refreshed.Revision != meta.Revision || refreshed.ContentDigest != meta.ContentDigest || refreshed.WriterID != meta.WriterID {
		t.Fatalf("listing meta update changed persistence fields: before=%+v after=%+v", meta, refreshed)
	}

	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	loaded.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	if err := loaded.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot append: %v", err)
	}
	advanced, ok, err := LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta advanced ok=%v err=%v", ok, err)
	}
	if advanced.Revision != refreshed.Revision+1 {
		t.Fatalf("revision after append = %d, want %d", advanced.Revision, refreshed.Revision+1)
	}
	if advanced.ContentDigest == refreshed.ContentDigest {
		t.Fatalf("content digest did not change after append: %q", advanced.ContentDigest)
	}
}

func TestSaveSnapshotSameContentSkipsRevisionBump(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	s := NewSession("sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot base: %v", err)
	}
	before, ok, err := LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta base ok=%v err=%v", ok, err)
	}

	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot same content: %v", err)
	}
	after, ok, err := LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta after no-op ok=%v err=%v", ok, err)
	}
	if after.Revision != before.Revision || after.ContentDigest != before.ContentDigest || after.WriterID != before.WriterID {
		t.Fatalf("same-content snapshot changed persistence meta: before=%+v after=%+v", before, after)
	}

	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot append after no-op: %v", err)
	}
	advanced, ok, err := LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta advanced ok=%v err=%v", ok, err)
	}
	if advanced.Revision != before.Revision+1 {
		t.Fatalf("revision after append = %d, want %d", advanced.Revision, before.Revision+1)
	}
}

func TestSaveSnapshotSameContentByOtherRuntimeKeepsClonedBaselineWritable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	s := NewSession("sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	if err := s.Save(path); err != nil {
		t.Fatalf("Save base: %v", err)
	}

	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	resumed, ok := loaded.CloneWithMessagesIfCompatible(loaded.Snapshot())
	if !ok {
		t.Fatal("expected compatible clone")
	}

	// Another runtime autosaves the identical transcript (e.g. a shutdown
	// snapshot of an idle tab). It must not bump the revision, or the resumed
	// clone's baseline goes stale and its next append is misread as a
	// stale-runtime conflict.
	other, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession other: %v", err)
	}
	if err := other.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot other same content: %v", err)
	}

	resumed.Add(provider.Message{Role: provider.RoleUser, Content: "next"})
	if err := resumed.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot append after same-content autosave elsewhere: %v", err)
	}
	reloaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession appended: %v", err)
	}
	if got := reloaded.Messages[len(reloaded.Messages)-1].Content; got != "next" {
		t.Fatalf("tail after append = %q, want next", got)
	}
	if matches, err := filepath.Glob(filepath.Join(filepath.Dir(path), "*-recovery-*.jsonl")); err != nil || len(matches) != 0 {
		t.Fatalf("recovery branches after append = %v err=%v, want none", matches, err)
	}
}

func TestSaveSnapshotAllowsExactAppendFromStaleRevisionBaseline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	s := NewSession("sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot base: %v", err)
	}
	staleBaseline := s.persistState(path)

	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot prefix append: %v", err)
	}
	prefixMeta, ok, err := LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta prefix ok=%v err=%v", ok, err)
	}
	if prefixMeta.Revision == staleBaseline.revision {
		t.Fatalf("prefix revision did not advance: %d", prefixMeta.Revision)
	}

	s.Add(provider.Message{Role: provider.RoleUser, Content: "two"})
	s.setPersistedBaseline(path, staleBaseline.digest, staleBaseline.version, staleBaseline.revision, true, 0)
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot exact append from stale revision baseline: %v", err)
	}

	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession appended: %v", err)
	}
	if got := loaded.Messages[len(loaded.Messages)-1].Content; got != "two" {
		t.Fatalf("tail after stale-baseline append = %q, want two", got)
	}
	advancedMeta, ok, err := LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta advanced ok=%v err=%v", ok, err)
	}
	if advancedMeta.Revision != prefixMeta.Revision+1 {
		t.Fatalf("revision after stale-baseline append = %d, want %d", advancedMeta.Revision, prefixMeta.Revision+1)
	}
	if matches, err := filepath.Glob(filepath.Join(filepath.Dir(path), "*-recovery-*.jsonl")); err != nil || len(matches) != 0 {
		t.Fatalf("recovery branches after stale-baseline append = %v err=%v, want none", matches, err)
	}
}

func TestSaveSnapshotAllowsCompatibleSystemAppendFromStaleRevisionBaseline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	s := NewSession("sys v1")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot base: %v", err)
	}
	staleBaseline := s.persistState(path)

	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot prefix append: %v", err)
	}
	prefixMeta, ok, err := LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta prefix ok=%v err=%v", ok, err)
	}

	// A resume swapped the system prompt, then the turn appended a message —
	// while the persistence baseline still points at the first save.
	msgs := s.Snapshot()
	msgs[0] = provider.Message{Role: provider.RoleSystem, Content: "sys v2"}
	msgs = append(msgs, provider.Message{Role: provider.RoleUser, Content: "two"})
	s.Replace(msgs)
	s.setPersistedBaseline(path, staleBaseline.digest, staleBaseline.version, staleBaseline.revision, true, 0)
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot compatible-system append from stale baseline: %v", err)
	}

	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession appended: %v", err)
	}
	if got := loaded.Messages[0].Content; got != "sys v2" {
		t.Fatalf("system after compatible append = %q, want sys v2", got)
	}
	if got := loaded.Messages[len(loaded.Messages)-1].Content; got != "two" {
		t.Fatalf("tail after compatible append = %q, want two", got)
	}
	advancedMeta, ok, err := LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta advanced ok=%v err=%v", ok, err)
	}
	if advancedMeta.Revision != prefixMeta.Revision+1 {
		t.Fatalf("revision after compatible append = %d, want %d", advancedMeta.Revision, prefixMeta.Revision+1)
	}
	if matches, err := filepath.Glob(filepath.Join(filepath.Dir(path), "*-recovery-*.jsonl")); err != nil || len(matches) != 0 {
		t.Fatalf("recovery branches after compatible append = %v err=%v, want none", matches, err)
	}
}

func TestSaveSnapshotRefusesStaleBaselineAppendOverRewoundTranscript(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	s := NewSession("sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot base: %v", err)
	}
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot extend: %v", err)
	}

	// Another runtime rewinds the transcript below this session's baseline
	// (e.g. a cancelled turn truncated the partial assistant reply).
	other, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession other: %v", err)
	}
	other.Replace(other.Snapshot()[:2])
	if err := other.SaveRewrite(path); err != nil {
		t.Fatalf("SaveRewrite rewind: %v", err)
	}

	// Appending from the stale baseline would resurrect the rewound suffix.
	s.Add(provider.Message{Role: provider.RoleUser, Content: "two"})
	if err := s.SaveSnapshot(path); !errors.Is(err, ErrSessionSnapshotConflict) {
		t.Fatalf("SaveSnapshot over rewound transcript err = %v, want ErrSessionSnapshotConflict", err)
	}
	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession after refused append: %v", err)
	}
	if got := len(loaded.Messages); got != 2 {
		t.Fatalf("messages after refused append = %d, want rewound 2", got)
	}
}

func TestSaveRewriteAllowsOwnedRewriteAfterLedgerReset(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	s := NewSession("sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "partial turn"})
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot base: %v", err)
	}
	// The meta sidecar (revision ledger) is lost — e.g. swept by a cleanup
	// that deleted session-adjacent files. The transcript itself is intact.
	if err := os.Remove(BranchMetaPath(path)); err != nil {
		t.Fatalf("remove sidecar: %v", err)
	}

	// A cancelled turn strips the partial reply and flushes via SaveRewrite.
	msgs := s.Snapshot()
	s.Replace(msgs[:len(msgs)-1])
	if err := s.SaveRewrite(path); err != nil {
		t.Fatalf("SaveRewrite after ledger reset: %v", err)
	}

	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession truncated: %v", err)
	}
	if got := len(loaded.Messages); got != 2 {
		t.Fatalf("messages after owned rewrite = %d, want 2", got)
	}
	if got := loaded.Messages[len(loaded.Messages)-1].Content; got != "first" {
		t.Fatalf("tail after owned rewrite = %q, want first", got)
	}
	if matches, err := filepath.Glob(filepath.Join(filepath.Dir(path), "*-recovery-*.jsonl")); err != nil || len(matches) != 0 {
		t.Fatalf("recovery branches after owned rewrite = %v err=%v, want none", matches, err)
	}
}

func TestSaveSnapshotStillPersistsNormalizedRepair(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	mal := NewSession("sys")
	mal.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	// Unanswered tool call: LoadSession backfills a placeholder result, so the
	// loaded history digests equal to itself while the on-disk bytes differ.
	mal.Add(provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "tool-1", Name: "read_file", Arguments: "{}"}}})
	mal.Add(provider.Message{Role: provider.RoleAssistant, Content: "done"})
	if err := mal.Save(path); err != nil {
		t.Fatalf("Save malformed: %v", err)
	}
	base, ok, err := LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta base ok=%v err=%v", ok, err)
	}

	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if !loaded.normalizedDirty {
		t.Fatal("fixture did not trigger a load-time repair; adjust the malformed history")
	}
	if err := loaded.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot repaired history: %v", err)
	}
	repaired, ok, err := LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta repaired ok=%v err=%v", ok, err)
	}
	if repaired.Revision != base.Revision+1 {
		t.Fatalf("revision after repair save = %d, want %d", repaired.Revision, base.Revision+1)
	}
	reloaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession repaired: %v", err)
	}
	if reloaded.normalizedDirty {
		t.Fatal("repair did not persist: reloaded session is still normalized-dirty")
	}

	// With the repair on disk, the same snapshot is now a true no-op.
	if err := reloaded.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot post-repair: %v", err)
	}
	final, ok, err := LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta final ok=%v err=%v", ok, err)
	}
	if final.Revision != repaired.Revision {
		t.Fatalf("post-repair no-op bumped revision: %d, want %d", final.Revision, repaired.Revision)
	}
}

func TestSaveSnapshotRejectsStalePrefixAfterSystemPromptRefresh(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	current := NewSession("new sys")
	current.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	current.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	current.Add(provider.Message{Role: provider.RoleUser, Content: "second"})
	if err := current.Save(path); err != nil {
		t.Fatalf("Save current: %v", err)
	}

	stale := NewSession("old sys")
	stale.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	stale.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	if err := stale.SaveSnapshot(path); !errors.Is(err, ErrSessionSnapshotConflict) {
		t.Fatalf("SaveSnapshot stale after system refresh err = %v, want ErrSessionSnapshotConflict", err)
	}

	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if got := len(loaded.Messages); got != 4 {
		t.Fatalf("message count after stale system refresh snapshot = %d, want 4", got)
	}
	if got := loaded.Messages[3].Content; got != "second" {
		t.Fatalf("last message after stale system refresh snapshot = %q, want %q", got, "second")
	}
}

func TestSaveRewriteAllowsRewriteOverSameContentForeignStamp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	base := NewSession("sys")
	base.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	base.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	if err := base.Save(path); err != nil {
		t.Fatalf("Save base: %v", err)
	}

	stale, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession stale: %v", err)
	}
	// Another runtime healed the ledger over identical content: the revision
	// advanced under a foreign writer id, but the recorded digest still
	// describes the exact bytes this session loaded and owns.
	meta, ok, err := LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta ok=%v err=%v", ok, err)
	}
	meta.Revision++
	meta.WriterID = "other-writer"
	if err := SaveBranchMetaPreserveUpdated(path, meta); err != nil {
		t.Fatalf("bump revision: %v", err)
	}

	stale.Replace([]provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "summarized first"},
	})
	if err := stale.SaveRewrite(path); err != nil {
		t.Fatalf("SaveRewrite over same-content stamp: %v", err)
	}

	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if got := loaded.Messages[len(loaded.Messages)-1].Content; got != "summarized first" {
		t.Fatalf("tail after owned rewrite = %q, want summarized first", got)
	}
	advanced, ok, err := LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta advanced ok=%v err=%v", ok, err)
	}
	if advanced.Revision != meta.Revision+1 {
		t.Fatalf("revision after rewrite = %d, want %d", advanced.Revision, meta.Revision+1)
	}
	if matches, err := filepath.Glob(filepath.Join(filepath.Dir(path), "*-recovery-*.jsonl")); err != nil || len(matches) != 0 {
		t.Fatalf("recovery branches after owned rewrite = %v err=%v, want none", matches, err)
	}
}

func TestSaveRewriteRejectsForeignStampForUnattributedBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	base := NewSession("sys")
	base.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	base.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	if err := base.Save(path); err != nil {
		t.Fatalf("Save base: %v", err)
	}

	stale, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession stale: %v", err)
	}
	// A foreign stamp whose digest disagrees with the on-disk transcript is
	// the aftermath of a save whose bytes and record split — the transcript
	// cannot be attributed, so the rewrite must fall to the conflict path.
	meta, ok, err := LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta ok=%v err=%v", ok, err)
	}
	meta.Revision++
	meta.WriterID = "other-writer"
	meta.ContentDigest = "0000000000000000000000000000000000000000000000000000000000000000"
	if err := SaveBranchMetaPreserveUpdated(path, meta); err != nil {
		t.Fatalf("stamp foreign digest: %v", err)
	}

	stale.Replace([]provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "summarized first"},
	})
	err = stale.SaveRewrite(path)
	if !errors.Is(err, ErrSessionSnapshotConflict) {
		t.Fatalf("SaveRewrite unattributed stamp err = %v, want ErrSessionSnapshotConflict", err)
	}
	var conflict *SessionSnapshotConflictError
	if !errors.As(err, &conflict) || conflict.Kind != SessionSnapshotConflictDiverged {
		t.Fatalf("conflict = %+v, want diverged revision conflict", conflict)
	}
	if conflict.BaseRevision != meta.Revision-1 || conflict.DiskRevision != meta.Revision {
		t.Fatalf("conflict revisions = base %d disk %d, want %d/%d",
			conflict.BaseRevision, conflict.DiskRevision, meta.Revision-1, meta.Revision)
	}

	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if got := loaded.Messages[len(loaded.Messages)-1].Content; got != "one" {
		t.Fatalf("tail after rejected rewrite = %q, want one", got)
	}
}

func TestSaveSnapshotRejectsOwnedNonPrefixRewrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	s := NewSession("sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	if err := s.Save(path); err != nil {
		t.Fatalf("Save base: %v", err)
	}

	s.Replace([]provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "summarized first"},
	})
	if err := s.SaveSnapshot(path); !errors.Is(err, ErrSessionSnapshotConflict) {
		t.Fatalf("SaveSnapshot owned rewrite err = %v, want ErrSessionSnapshotConflict", err)
	}

	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if got := len(loaded.Messages); got != 3 {
		t.Fatalf("message count after rejected snapshot rewrite = %d, want 3", got)
	}
	if got := loaded.Messages[2].Content; got != "one" {
		t.Fatalf("last message after rejected snapshot rewrite = %q, want %q", got, "one")
	}
}

func TestSaveRewriteAllowsOwnedNonPrefixRewrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	s := NewSession("sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	if err := s.Save(path); err != nil {
		t.Fatalf("Save base: %v", err)
	}

	s.Replace([]provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "summarized first"},
	})
	if err := s.SaveRewrite(path); err != nil {
		t.Fatalf("SaveRewrite owned rewrite: %v", err)
	}

	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if got := len(loaded.Messages); got != 2 {
		t.Fatalf("message count after rewrite = %d, want 2", got)
	}
	if got := loaded.Messages[1].Content; got != "summarized first" {
		t.Fatalf("rewritten content = %q, want %q", got, "summarized first")
	}
}

func TestCloneWithMessagesPreservesRewriteBaseline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	s := NewSession("old sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	s.Add(provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "tool-1", Name: "read_file", Arguments: "{}"}}})
	s.Add(provider.Message{Role: provider.RoleTool, ToolCallID: "tool-1", Name: "read_file", Content: strings.Repeat("detail ", 100)})
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "done"})
	if err := s.Save(path); err != nil {
		t.Fatalf("Save base: %v", err)
	}

	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	msgs := loaded.Snapshot()
	msgs[0].Content = "new sys"
	msgs[3].Content = "[elided tool result]"
	resumed := loaded.CloneWithMessages(msgs)
	if err := resumed.SaveRewrite(path); err != nil {
		t.Fatalf("SaveRewrite cloned resume rewrite: %v", err)
	}

	reloaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession rewritten: %v", err)
	}
	if got := reloaded.Messages[0].Content; got != "new sys" {
		t.Fatalf("system prompt after rewrite = %q, want new sys", got)
	}
	if got := reloaded.Messages[3].Content; got != "[elided tool result]" {
		t.Fatalf("tool result after rewrite = %q, want elided", got)
	}
}

func TestCloneWithMessagesIfCompatibleRejectsHistoryChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	s := NewSession("old sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	if err := s.Save(path); err != nil {
		t.Fatalf("Save base: %v", err)
	}

	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	systemOnly := loaded.Snapshot()
	systemOnly[0].Content = "new sys"
	if _, ok := loaded.CloneWithMessagesIfCompatible(systemOnly); !ok {
		t.Fatal("system-only change should be compatible")
	}

	changed := loaded.Snapshot()
	changed[2].Content = "rewritten assistant"
	if _, ok := loaded.CloneWithMessagesIfCompatible(changed); ok {
		t.Fatal("non-system history change should not preserve baseline")
	}
}

func TestSaveRewriteRejectsStalePrefixOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	current := NewSession("sys")
	current.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	current.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	current.Add(provider.Message{Role: provider.RoleUser, Content: "second"})
	if err := current.Save(path); err != nil {
		t.Fatalf("Save current: %v", err)
	}

	stale := NewSession("sys")
	stale.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	if err := stale.SaveRewrite(path); !errors.Is(err, ErrSessionSnapshotConflict) {
		t.Fatalf("SaveRewrite stale err = %v, want ErrSessionSnapshotConflict", err)
	}

	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if got := len(loaded.Messages); got != 4 {
		t.Fatalf("message count after stale rewrite = %d, want 4", got)
	}
	if got := loaded.Messages[3].Content; got != "second" {
		t.Fatalf("last message after stale rewrite = %q, want %q", got, "second")
	}
}

func TestSaveRecoveryBranchPersistsDivergedSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	current := NewSession("sys")
	current.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	current.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	current.Add(provider.Message{Role: provider.RoleUser, Content: "disk second"})
	if err := current.Save(path); err != nil {
		t.Fatalf("Save current: %v", err)
	}

	stale := NewSession("sys")
	stale.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	stale.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	stale.Add(provider.Message{Role: provider.RoleUser, Content: "local second"})
	if err := stale.SaveSnapshot(path); !errors.Is(err, ErrSessionSnapshotConflict) {
		t.Fatalf("SaveSnapshot stale err = %v, want ErrSessionSnapshotConflict", err)
	}

	info, err := stale.SaveRecoveryBranch(RecoveryBranchOptions{OriginalPath: path})
	if err != nil {
		t.Fatalf("SaveRecoveryBranch: %v", err)
	}
	if info.Path == "" || info.Path == path {
		t.Fatalf("recovery path = %q, want distinct path", info.Path)
	}
	if info.Turns != 2 || info.Preview != "first" {
		t.Fatalf("recovery preview/turns = %q/%d, want first/2", info.Preview, info.Turns)
	}
	recovered, err := LoadSession(info.Path)
	if err != nil {
		t.Fatalf("LoadSession recovery: %v", err)
	}
	if got := recovered.Messages[len(recovered.Messages)-1].Content; got != "local second" {
		t.Fatalf("recovery tail = %q, want local second", got)
	}
	original, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession original: %v", err)
	}
	if got := original.Messages[len(original.Messages)-1].Content; got != "disk second" {
		t.Fatalf("original tail = %q, want disk second", got)
	}
	meta, ok, err := LoadBranchMeta(info.Path)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta recovery ok=%v err=%v", ok, err)
	}
	if !meta.Recovered || meta.ParentID != BranchID(path) || meta.Name != RecoveryBranchDefaultName {
		t.Fatalf("recovery meta = %+v, want recovered parent/name", meta)
	}
	if meta.RecoveryDigest == "" || meta.SchemaVersion != BranchMetaCountsVersion {
		t.Fatalf("recovery digest/schema = %q/%d", meta.RecoveryDigest, meta.SchemaVersion)
	}
	if meta.Revision != 1 || meta.ContentDigest != meta.RecoveryDigest || meta.WriterID == "" {
		t.Fatalf("recovery persistence meta = %+v, want revision/content digest/writer", meta)
	}
}

func TestSaveRecoveryBranchSkipsPureStalePrefix(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	current := NewSession("sys")
	current.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	current.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	current.Add(provider.Message{Role: provider.RoleUser, Content: "disk second"})
	if err := current.Save(path); err != nil {
		t.Fatalf("Save current: %v", err)
	}

	stale := NewSession("sys")
	stale.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	stale.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	if _, err := stale.SaveRecoveryBranch(RecoveryBranchOptions{OriginalPath: path}); !errors.Is(err, ErrSessionRecoveryNotNeeded) {
		t.Fatalf("SaveRecoveryBranch stale prefix err = %v, want ErrSessionRecoveryNotNeeded", err)
	}
}

func divergedSessionPair(t *testing.T, dir, name string) (string, *Session) {
	t.Helper()
	path := filepath.Join(dir, name)
	current := NewSession("sys")
	current.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	current.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	current.Add(provider.Message{Role: provider.RoleUser, Content: "disk second"})
	if err := current.Save(path); err != nil {
		t.Fatalf("Save current: %v", err)
	}
	stale := NewSession("sys")
	stale.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	stale.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	stale.Add(provider.Message{Role: provider.RoleUser, Content: "local " + name})
	return path, stale
}

func stampRecoveryMeta(t *testing.T, path string, depth int) {
	t.Helper()
	meta, ok, err := LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta ok=%v err=%v", ok, err)
	}
	meta.Recovered = true
	meta.RecoveryDepth = depth
	if err := SaveBranchMeta(path, meta); err != nil {
		t.Fatalf("SaveBranchMeta: %v", err)
	}
}

func TestSaveRecoveryBranchStampsAndCapsChainDepth(t *testing.T) {
	dir := t.TempDir()

	// Forking from a normal session stamps depth 1.
	path, stale := divergedSessionPair(t, dir, "session.jsonl")
	info, err := stale.SaveRecoveryBranch(RecoveryBranchOptions{OriginalPath: path})
	if err != nil {
		t.Fatalf("SaveRecoveryBranch: %v", err)
	}
	if info.Meta.RecoveryDepth != 1 {
		t.Fatalf("first fork depth = %d, want 1", info.Meta.RecoveryDepth)
	}

	// Forking from a recovery branch increments the chain depth.
	deeper, staleDeeper := divergedSessionPair(t, dir, "deeper.jsonl")
	stampRecoveryMeta(t, deeper, 1)
	info, err = staleDeeper.SaveRecoveryBranch(RecoveryBranchOptions{OriginalPath: deeper})
	if err != nil {
		t.Fatalf("SaveRecoveryBranch from depth 1: %v", err)
	}
	if info.Meta.RecoveryDepth != 2 {
		t.Fatalf("nested fork depth = %d, want 2", info.Meta.RecoveryDepth)
	}

	// A legacy recovery meta without the depth field counts as depth 1.
	legacy, staleLegacy := divergedSessionPair(t, dir, "legacy.jsonl")
	stampRecoveryMeta(t, legacy, 0)
	info, err = staleLegacy.SaveRecoveryBranch(RecoveryBranchOptions{OriginalPath: legacy})
	if err != nil {
		t.Fatalf("SaveRecoveryBranch from legacy recovery: %v", err)
	}
	if info.Meta.RecoveryDepth != 2 {
		t.Fatalf("legacy nested fork depth = %d, want 2", info.Meta.RecoveryDepth)
	}

	// A parent at the cap refuses to fork deeper.
	capped, staleCapped := divergedSessionPair(t, dir, "capped.jsonl")
	stampRecoveryMeta(t, capped, SessionRecoveryMaxDepth)
	if _, err := staleCapped.SaveRecoveryBranch(RecoveryBranchOptions{OriginalPath: capped}); !errors.Is(err, ErrSessionRecoveryDepthExceeded) {
		t.Fatalf("SaveRecoveryBranch at cap err = %v, want ErrSessionRecoveryDepthExceeded", err)
	}
	forks, err := filepath.Glob(filepath.Join(dir, "capped-recovery-*.jsonl"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(forks) != 0 {
		t.Fatalf("capped parent still forked: %v", forks)
	}
}

func TestSaveRecoveryBranchDedupesByDigest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	current := NewSession("sys")
	current.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	current.Add(provider.Message{Role: provider.RoleAssistant, Content: "disk"})
	if err := current.Save(path); err != nil {
		t.Fatalf("Save current: %v", err)
	}

	stale := NewSession("sys")
	stale.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	stale.Add(provider.Message{Role: provider.RoleAssistant, Content: "local"})
	first, err := stale.SaveRecoveryBranch(RecoveryBranchOptions{OriginalPath: path})
	if err != nil {
		t.Fatalf("first SaveRecoveryBranch: %v", err)
	}
	second, err := stale.SaveRecoveryBranch(RecoveryBranchOptions{OriginalPath: path})
	if err != nil {
		t.Fatalf("second SaveRecoveryBranch: %v", err)
	}
	if second.Path != first.Path || !second.Existing {
		t.Fatalf("second recovery = %+v, want existing same path %q", second, first.Path)
	}
}

func TestSaveRecoveryBranchCompactsLongParentFilename(t *testing.T) {
	dir := t.TempDir()
	parentID := strings.Repeat("longparent-", 22)
	path := filepath.Join(dir, parentID+".jsonl")
	current := NewSession("sys")
	current.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	current.Add(provider.Message{Role: provider.RoleAssistant, Content: "disk"})
	if err := current.Save(path); err != nil {
		t.Fatalf("Save current: %v", err)
	}

	stale := NewSession("sys")
	stale.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	stale.Add(provider.Message{Role: provider.RoleAssistant, Content: "local"})
	info, err := stale.SaveRecoveryBranch(RecoveryBranchOptions{OriginalPath: path})
	if err != nil {
		t.Fatalf("SaveRecoveryBranch: %v", err)
	}
	base := filepath.Base(info.Path)
	if len(base) > 140 {
		t.Fatalf("recovery basename length = %d (%q), want bounded", len(base), base)
	}
	for _, suffix := range []string{".lock", ".lease.lock", ".lease.json", ".meta"} {
		if len(base+suffix) > 255 {
			t.Fatalf("recovery sidecar basename %q length = %d, want <= 255", base+suffix, len(base+suffix))
		}
	}
	meta, ok, err := LoadBranchMeta(info.Path)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta recovery ok=%v err=%v", ok, err)
	}
	if meta.ParentID != BranchID(path) {
		t.Fatalf("recovery parent = %q, want original branch id", meta.ParentID)
	}
}

func TestSaveRecoveryBranchDoesNotCascadeRecoveryFilename(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	current := NewSession("sys")
	current.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	current.Add(provider.Message{Role: provider.RoleAssistant, Content: "disk"})
	if err := current.Save(path); err != nil {
		t.Fatalf("Save current: %v", err)
	}

	local := NewSession("sys")
	local.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	local.Add(provider.Message{Role: provider.RoleAssistant, Content: "local"})
	first, err := local.SaveRecoveryBranch(RecoveryBranchOptions{OriginalPath: path})
	if err != nil {
		t.Fatalf("first SaveRecoveryBranch: %v", err)
	}

	recoveryDisk, err := LoadSession(first.Path)
	if err != nil {
		t.Fatalf("LoadSession recovery: %v", err)
	}
	recoveryDisk.Add(provider.Message{Role: provider.RoleUser, Content: "disk follow-up"})
	if err := recoveryDisk.SaveSnapshot(first.Path); err != nil {
		t.Fatalf("SaveSnapshot recovery disk: %v", err)
	}
	recoveryLocal := NewSession("sys")
	recoveryLocal.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	recoveryLocal.Add(provider.Message{Role: provider.RoleAssistant, Content: "local"})
	recoveryLocal.Add(provider.Message{Role: provider.RoleUser, Content: "local follow-up"})

	second, err := recoveryLocal.SaveRecoveryBranch(RecoveryBranchOptions{OriginalPath: first.Path})
	if err != nil {
		t.Fatalf("second SaveRecoveryBranch: %v", err)
	}
	base := filepath.Base(second.Path)
	if count := strings.Count(base, "-recovery-"); count != 1 {
		t.Fatalf("recovery basename = %q, contains %d recovery markers, want 1", base, count)
	}
	if len(base) > 140 {
		t.Fatalf("recovery basename length = %d (%q), want bounded", len(base), base)
	}
}

func TestReconcileSessionSidecarsRemovesUnlockedArtifacts(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(path, []byte(`{"role":"user","content":"hello"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, sidecar := range []string{path + ".lock", path + ".lease.lock", path + ".lease.json"} {
		if err := os.WriteFile(sidecar, []byte("{}\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", sidecar, err)
		}
	}

	if err := ReconcileSessionSidecars(dir); err != nil {
		t.Fatalf("ReconcileSessionSidecars: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("session transcript removed: %v", err)
	}
	for _, sidecar := range []string{path + ".lock", path + ".lease.lock", path + ".lease.json"} {
		if _, err := os.Stat(sidecar); !os.IsNotExist(err) {
			t.Fatalf("%s exists after sidecar cleanup (err=%v)", sidecar, err)
		}
	}
}

func TestReconcileSessionSidecarsKeepsLiveLocks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(path, []byte(`{"role":"user","content":"hello"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	unlock, err := lockSessionFile(path)
	if err != nil {
		t.Fatalf("lockSessionFile: %v", err)
	}
	defer unlock()
	lease, err := TryAcquireSessionLease(path)
	if err != nil {
		t.Fatalf("TryAcquireSessionLease: %v", err)
	}
	defer lease.Release()

	if err := ReconcileSessionSidecars(dir); err != nil {
		t.Fatalf("ReconcileSessionSidecars: %v", err)
	}
	for _, sidecar := range []string{path + ".lock", path + ".lease.lock", path + ".lease.json"} {
		if _, err := os.Stat(sidecar); err != nil {
			t.Fatalf("%s missing while lock is live: %v", sidecar, err)
		}
	}
}

// TestReconcileSessionSidecarsKeepsFlockOnlyLocks proves the file lock alone
// protects a writer from cleanup: CLI-style savers hold the .lock flock while
// writing without ever taking a session lease.
func TestReconcileSessionSidecarsKeepsFlockOnlyLocks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(path, []byte(`{"role":"user","content":"hello"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	unlock, err := lockSessionFile(path)
	if err != nil {
		t.Fatalf("lockSessionFile: %v", err)
	}
	if err := ReconcileSessionSidecars(dir); err != nil {
		t.Fatalf("ReconcileSessionSidecars: %v", err)
	}
	if _, err := os.Stat(path + ".lock"); err != nil {
		t.Fatalf(".lock removed while a lock-only writer holds it: %v", err)
	}
	unlock()
	if err := ReconcileSessionSidecars(dir); err != nil {
		t.Fatalf("ReconcileSessionSidecars after unlock: %v", err)
	}
	if _, err := os.Stat(path + ".lock"); !os.IsNotExist(err) {
		t.Fatalf(".lock survived cleanup after release (err=%v)", err)
	}
}

// TestReconcileSessionSidecarsRenamesOverlongSessionFiles covers the
// migration for transcripts left behind by the unbounded recovery cascade
// (#5923): names so long their lock/lease sidecars could not be created. The
// conversation bytes must survive under a bounded name, branch meta must move
// with its ID rewritten, and children must be re-parented onto the new ID.
func TestReconcileSessionSidecarsRenamesOverlongSessionFiles(t *testing.T) {
	dir := t.TempDir()
	longID := strings.Repeat("p", 240) // 246-byte basename: .lock fits, .lease.lock does not
	hugeID := strings.Repeat("q", 248) // 254-byte basename: no sidecar fits at all
	oldLong := filepath.Join(dir, longID+".jsonl")
	oldHuge := filepath.Join(dir, hugeID+".jsonl")
	content := `{"role":"system","content":"sys"}` + "\n" + `{"role":"user","content":"hello"}` + "\n"
	for _, p := range []string{oldLong, oldHuge} {
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(oldLong+".lock", []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SaveBranchMeta(oldLong, BranchMeta{Name: "长会话", ParentID: "root-branch"}); err != nil {
		t.Fatalf("SaveBranchMeta: %v", err)
	}
	childPath := filepath.Join(dir, "child.jsonl")
	if err := os.WriteFile(childPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SaveBranchMeta(childPath, BranchMeta{Name: "child", ParentID: longID}); err != nil {
		t.Fatalf("SaveBranchMeta child: %v", err)
	}

	if err := ReconcileSessionSidecars(dir); err != nil {
		t.Fatalf("ReconcileSessionSidecars: %v", err)
	}

	for _, gone := range []string{oldLong, oldHuge, oldLong + ".lock", oldLong + ".meta"} {
		if _, err := os.Stat(gone); !os.IsNotExist(err) {
			t.Fatalf("%s still present after rename (err=%v)", filepath.Base(gone), err)
		}
	}
	newLongID := recoveryParentStem(longID)
	newLong := filepath.Join(dir, newLongID+".jsonl")
	newHuge := filepath.Join(dir, recoveryParentStem(hugeID)+".jsonl")
	for _, p := range []string{newLong, newHuge} {
		if base := filepath.Base(p); len(base) > maxSessionBasenameBytes {
			t.Fatalf("renamed basename %q length %d exceeds bound %d", base, len(base), maxSessionBasenameBytes)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read renamed transcript: %v", err)
		}
		if string(b) != content {
			t.Fatalf("transcript content changed by rename: %q", b)
		}
	}
	meta, ok, err := LoadBranchMeta(newLong)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta renamed ok=%v err=%v", ok, err)
	}
	if meta.ID != newLongID {
		t.Fatalf("migrated meta ID = %q, want %q", meta.ID, newLongID)
	}
	if meta.Name != "长会话" || meta.ParentID != "root-branch" {
		t.Fatalf("migrated meta lost fields: %+v", meta)
	}
	childMeta, ok, err := LoadBranchMeta(childPath)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta child ok=%v err=%v", ok, err)
	}
	if childMeta.ParentID != newLongID {
		t.Fatalf("child ParentID = %q, want re-parented %q", childMeta.ParentID, newLongID)
	}

	before, err := filepath.Glob(filepath.Join(dir, "*"))
	if err != nil {
		t.Fatal(err)
	}
	if err := ReconcileSessionSidecars(dir); err != nil {
		t.Fatalf("ReconcileSessionSidecars rerun: %v", err)
	}
	after, err := filepath.Glob(filepath.Join(dir, "*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != len(after) {
		t.Fatalf("rerun changed the directory: before=%d entries, after=%d", len(before), len(after))
	}
}

// TestReconcileOverlongRenameStillReparentsWhenSidecarMigrationFails pins the
// point-of-no-return contract: once the transcript rename lands, the mapping
// must be committed — children re-parented, error surfaced as a warning —
// because the old name is gone and no later run can reconstruct it.
func TestReconcileOverlongRenameStillReparentsWhenSidecarMigrationFails(t *testing.T) {
	dir := t.TempDir()
	longID := strings.Repeat("m", 240)
	oldPath := filepath.Join(dir, longID+".jsonl")
	content := `{"role":"user","content":"hello"}` + "\n"
	if err := os.WriteFile(oldPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SaveBranchMeta(oldPath, BranchMeta{Name: "keep", ParentID: "root"}); err != nil {
		t.Fatalf("SaveBranchMeta: %v", err)
	}
	childPath := filepath.Join(dir, "child.jsonl")
	if err := os.WriteFile(childPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SaveBranchMeta(childPath, BranchMeta{Name: "child", ParentID: longID}); err != nil {
		t.Fatalf("SaveBranchMeta child: %v", err)
	}

	newID := recoveryParentStem(longID)
	newPath := filepath.Join(dir, newID+".jsonl")
	// Sabotage the meta migration: its destination path is a directory.
	if err := os.Mkdir(newPath+".meta", 0o755); err != nil {
		t.Fatal(err)
	}

	if err := ReconcileSessionSidecars(dir); err == nil {
		t.Fatal("expected the sabotaged meta migration to surface an error")
	}
	if _, err := os.Stat(newPath); err != nil {
		t.Fatalf("renamed transcript missing after partial failure: %v", err)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("old transcript still present (err=%v)", err)
	}
	childMeta, ok, err := LoadBranchMeta(childPath)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta child ok=%v err=%v", ok, err)
	}
	if childMeta.ParentID != newID {
		t.Fatalf("child ParentID = %q, want %q despite sidecar failure", childMeta.ParentID, newID)
	}
	// The old meta stays behind as the durable copy of the un-migrated fields.
	if _, err := os.Stat(oldPath + ".meta"); err != nil {
		t.Fatalf("old meta lost though its migration failed: %v", err)
	}
}

// TestListSessionsOrdersByMTime makes sure the picker shows the most
// recently used conversation first — that's what users reach for when they
// hit `reasonix --continue`.
func TestListSessionsOrdersByMTime(t *testing.T) {
	dir := t.TempDir()
	// Write two sessions with explicit mtimes so the order is deterministic.
	for _, name := range []string{"a.jsonl", "b.jsonl"} {
		s := NewSession("")
		s.Add(provider.Message{Role: provider.RoleUser, Content: "preview for " + name})
		if err := s.Save(filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	oldT := time.Now().Add(-1 * time.Hour)
	newT := time.Now()
	if err := touch(filepath.Join(dir, "a.jsonl"), oldT); err != nil {
		t.Fatal(err)
	}
	if err := touch(filepath.Join(dir, "b.jsonl"), newT); err != nil {
		t.Fatal(err)
	}

	got, err := ListSessions(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if !strings.HasSuffix(got[0].Path, "b.jsonl") {
		t.Errorf("first entry = %s, want the newer 'b.jsonl'", got[0].Path)
	}
	if got[0].Turns != 1 || got[0].Preview != "preview for b.jsonl" {
		t.Errorf("preview/turns wrong on newest: turns=%d preview=%q", got[0].Turns, got[0].Preview)
	}
}

func TestListSessionsIncludesCustomTitle(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "named.jsonl")
	s := NewSession("")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "first user prompt"})
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	if err := SaveBranchMetaPreserveUpdated(path, BranchMeta{
		TopicTitle:    "Topic title",
		CustomTitle:   "Custom session title",
		Preview:       "first user prompt",
		Turns:         1,
		SchemaVersion: BranchMetaCountsVersion,
	}); err != nil {
		t.Fatalf("SaveBranchMetaPreserveUpdated: %v", err)
	}

	got, err := ListSessions(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].CustomTitle != "Custom session title" {
		t.Fatalf("custom title = %q, want Custom session title", got[0].CustomTitle)
	}
	if got[0].TopicTitle != "Topic title" {
		t.Fatalf("topic title = %q, want Topic title", got[0].TopicTitle)
	}
}

func TestListSessionsSkipsCleanupPending(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pending.jsonl")
	s := NewSession("")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "preview"})
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	if err := MarkCleanupPending(path, "delete"); err != nil {
		t.Fatal(err)
	}
	if !IsCleanupPending(path) {
		t.Fatal("session should be marked cleanup-pending")
	}

	got, err := ListSessions(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("cleanup-pending session should be hidden, got %+v", got)
	}

	if err := ClearCleanupPending(path); err != nil {
		t.Fatal(err)
	}
	got, err = ListSessions(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Path != path {
		t.Fatalf("session should be visible after clearing marker, got %+v", got)
	}
}

func TestListSessionsOrdersByLastActivityMeta(t *testing.T) {
	dir := t.TempDir()
	aPath := filepath.Join(dir, "a.jsonl")
	bPath := filepath.Join(dir, "b.jsonl")
	for _, path := range []string{aPath, bPath} {
		s := NewSession("")
		s.Add(provider.Message{Role: provider.RoleUser, Content: "preview for " + filepath.Base(path)})
		if err := s.Save(path); err != nil {
			t.Fatal(err)
		}
	}

	now := time.Now().UTC()
	olderActivity := now.Add(-2 * time.Hour)
	newerActivity := now.Add(-1 * time.Hour)
	writeBranchMeta(t, aPath, now.Add(-24*time.Hour), newerActivity)
	writeBranchMeta(t, bPath, now.Add(-24*time.Hour), olderActivity)
	if err := touch(aPath, now.Add(-3*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := touch(bPath, now); err != nil {
		t.Fatal(err)
	}

	got, err := ListSessions(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Path != aPath {
		t.Fatalf("first entry = %s, want activity-newer a.jsonl despite older file mtime", got[0].Path)
	}
	if !got[0].LastActivityAt.Equal(newerActivity) || !got[0].ModTime.Equal(newerActivity) {
		t.Fatalf("activity fields = %s / %s, want %s", got[0].LastActivityAt, got[0].ModTime, newerActivity)
	}
}

func TestListSessionOrderIncludesEmptySessionsWithoutPreviewScan(t *testing.T) {
	dir := t.TempDir()
	emptyPath := filepath.Join(dir, "empty.jsonl")
	realPath := filepath.Join(dir, "real.jsonl")
	if err := os.WriteFile(emptyPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	s := NewSession("")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "real prompt"})
	if err := s.Save(realPath); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	writeBranchMeta(t, emptyPath, now, now.Add(time.Hour))
	writeBranchMeta(t, realPath, now, now)

	ordered, err := ListSessionOrder(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(ordered) != 2 {
		t.Fatalf("lightweight order len = %d, want 2", len(ordered))
	}
	if ordered[0].Path != emptyPath {
		t.Fatalf("lightweight order first = %s, want newer empty session %s", ordered[0].Path, emptyPath)
	}

	listed, err := ListSessions(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].Path != realPath {
		t.Fatalf("ListSessions = %+v, want only the non-empty real session", listed)
	}
}

func TestSessionListingsExposeRecoveryMetadata(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "recovered.jsonl")
	s := NewSession("")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "continued recovery"})
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}

	meta, ok, err := LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta: ok=%v err=%v", ok, err)
	}
	meta.Recovered = true
	meta.RecoveryDigest = strings.Repeat("a", 64)
	meta.ParentID = "parent"
	if err := SaveBranchMetaPreserveUpdated(path, meta); err != nil {
		t.Fatal(err)
	}

	ordered, err := ListSessionOrder(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(ordered) != 1 {
		t.Fatalf("ListSessionOrder len = %d, want 1", len(ordered))
	}
	if ordered[0].RecoveryDigest != meta.RecoveryDigest || ordered[0].ParentID != meta.ParentID {
		t.Fatalf("ordered recovery metadata = digest:%q parent:%q, want digest:%q parent:%q", ordered[0].RecoveryDigest, ordered[0].ParentID, meta.RecoveryDigest, meta.ParentID)
	}

	listed, err := ListSessions(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 {
		t.Fatalf("ListSessions len = %d, want 1", len(listed))
	}
	if listed[0].RecoveryDigest != meta.RecoveryDigest || listed[0].ParentID != meta.ParentID {
		t.Fatalf("listed recovery metadata = digest:%q parent:%q, want digest:%q parent:%q", listed[0].RecoveryDigest, listed[0].ParentID, meta.RecoveryDigest, meta.ParentID)
	}
}

func writeBranchMeta(t *testing.T, path string, createdAt, updatedAt time.Time) {
	t.Helper()
	meta := BranchMeta{
		ID:        BranchID(path),
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}
	b, err := json.Marshal(meta)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(BranchMetaPath(path), append(b, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestContinueSessionPathReusesPriorFile(t *testing.T) {
	prev := filepath.Join("sessions", "20260602-120000.000000000-deepseek.jsonl")
	if got := ContinueSessionPath(prev, "sessions", "other-model"); got != prev {
		t.Fatalf("carried conversation should keep its file %q, got %q", prev, got)
	}
}

func TestContinueSessionPathMintsFreshWhenNoPrior(t *testing.T) {
	dir := t.TempDir()
	got := ContinueSessionPath("", dir, "deepseek")
	if filepath.Dir(got) != dir || !strings.HasSuffix(got, ".jsonl") {
		t.Fatalf("fresh path = %q, want a .jsonl under %q", got, dir)
	}
}

func TestContinueSessionPathNoPersistence(t *testing.T) {
	if got := ContinueSessionPath("", "", "deepseek"); got != "" {
		t.Fatalf("no session dir should disable persistence, got %q", got)
	}
}

// TestListSessionsMissingDir returns nil + no error so callers can fall
// through to a fresh session without special-casing.
func TestListSessionsMissingDir(t *testing.T) {
	got, err := ListSessions(filepath.Join(t.TempDir(), "never-created"))
	if err != nil || got != nil {
		t.Errorf("missing dir = %v / %v, want nil/nil", got, err)
	}
}

func readSessionEventsForTest(t *testing.T, path string) []sessionEventRecord {
	t.Helper()
	f, err := os.Open(SessionEventLogPath(path))
	if err != nil {
		t.Fatalf("open event log: %v", err)
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	var out []sessionEventRecord
	for {
		var rec sessionEventRecord
		if err := dec.Decode(&rec); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			t.Fatalf("decode event log: %v", err)
		}
		out = append(out, rec)
	}
	return out
}

// TestReconcileOverlongMigratesDiagnosticSidecars pins two halves of one fix:
// overlong-name reconciliation must skip .events.jsonl / .conflicts.jsonl
// sidecars instead of renaming each into a fake session with fabricated meta,
// and the owning transcript's rename must carry those sidecars along instead
// of orphaning them under the retired stem.
func TestReconcileOverlongMigratesDiagnosticSidecars(t *testing.T) {
	dir := t.TempDir()
	// 236-byte transcript name: past the 224 reconcile bound, while its
	// .events.jsonl (243) and .conflicts.jsonl (246) still fit under 255 —
	// exactly the window where the old suffix filter mistook them for
	// overlong sessions.
	id := strings.Repeat("s", 230)
	oldPath := filepath.Join(dir, id+".jsonl")
	content := `{"role":"system","content":"sys"}` + "\n" + `{"role":"user","content":"hello"}` + "\n"
	if err := os.WriteFile(oldPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.SessionEventLog(oldPath),
		[]byte(`{"schema_version":1,"type":"replace","messages":[{"role":"user","content":"hello"}]}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.SessionConflictLog(oldPath),
		[]byte(`{"outcome":"forked_recovery_branch"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := ReconcileSessionSidecars(dir); err != nil {
		t.Fatalf("ReconcileSessionSidecars: %v", err)
	}

	newPath := filepath.Join(dir, recoveryParentStem(id)+".jsonl")
	if _, err := os.Stat(newPath); err != nil {
		t.Fatalf("renamed transcript missing: %v", err)
	}
	if _, err := os.Stat(store.SessionEventLog(newPath)); err != nil {
		t.Fatalf("event log not migrated with the rename: %v", err)
	}
	if _, err := os.Stat(store.SessionConflictLog(newPath)); err != nil {
		t.Fatalf("conflict log not migrated with the rename: %v", err)
	}
	for _, gone := range []string{oldPath, store.SessionEventLog(oldPath), store.SessionConflictLog(oldPath)} {
		if _, err := os.Stat(gone); !os.IsNotExist(err) {
			t.Fatalf("%s still present under the retired stem (err=%v)", filepath.Base(gone), err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if store.IsSessionTranscriptName(e.Name()) && e.Name() != filepath.Base(newPath) {
			t.Fatalf("reconcile fabricated an extra session from a sidecar: %s", e.Name())
		}
	}
}
