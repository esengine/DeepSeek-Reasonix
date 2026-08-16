package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"reasonix/internal/provider"
	"reasonix/internal/store"
)

// displayIndexTestMessages builds a multi-turn transcript exercising every
// classification the index records: plain turns, a tool call + result, an
// image attachment, a local-only message, a steer, and a synthetic user
// message.
func displayIndexTestMessages() []provider.Message {
	return []provider.Message{
		{Role: provider.RoleSystem, Content: "system prompt"},
		{Role: provider.RoleUser, Content: "first question"},
		{Role: provider.RoleAssistant, Content: "calling a tool", ToolCalls: []provider.ToolCall{
			{ID: "call_1", Name: "shell", Arguments: `{"cmd":"ls"}`},
		}},
		{Role: provider.RoleTool, ToolCallID: "call_1", Name: "shell", Content: "file.go"},
		{Role: provider.RoleAssistant, Content: "interrupted partial", LocalOnly: true},
		{Role: provider.RoleUser, Content: midTurnSteerMessage("hurry up")},
		{Role: provider.RoleUser, Content: "Plan approved — plan mode is off. Implement the plan now."},
		{Role: provider.RoleUser, Content: "second question", Images: []string{"data:image/png;base64,iVBORw0KGgo="}},
		{Role: provider.RoleAssistant, Content: "second answer"},
	}
}

func displayIndexTranscriptSize(t *testing.T, msgs []provider.Message) int64 {
	t.Helper()
	size := int64(0)
	for i, m := range msgs {
		b, err := json.Marshal(m)
		if err != nil {
			t.Fatalf("marshal message %d: %v", i, err)
		}
		size += int64(len(b)) + 1
	}
	return size
}

func TestBuildSessionDisplayIndexRoundTrip(t *testing.T) {
	msgs := displayIndexTestMessages()
	digest, err := digestSessionMessages(msgs)
	if err != nil {
		t.Fatalf("digestSessionMessages: %v", err)
	}
	idx := BuildSessionDisplayIndex(msgs, 7, true, digest)
	if idx == nil {
		t.Fatal("BuildSessionDisplayIndex returned nil")
	}
	if idx.MessageCount != len(msgs) || len(idx.Entries) != len(msgs) {
		t.Fatalf("message_count = %d, entries = %d, want %d", idx.MessageCount, len(idx.Entries), len(msgs))
	}
	if idx.AuthoredTurns != 2 {
		t.Fatalf("authored_turns = %d, want 2 (steer and synthetic messages are not turns)", idx.AuthoredTurns)
	}
	if idx.TranscriptSize != displayIndexTranscriptSize(t, msgs) {
		t.Fatalf("transcript_size = %d, want %d", idx.TranscriptSize, displayIndexTranscriptSize(t, msgs))
	}

	path := filepath.Join(t.TempDir(), "session.display-index.json")
	if err := WriteSessionDisplayIndex(path, idx); err != nil {
		t.Fatalf("WriteSessionDisplayIndex: %v", err)
	}
	loaded, err := LoadSessionDisplayIndex(path)
	if err != nil {
		t.Fatalf("LoadSessionDisplayIndex: %v", err)
	}
	if !reflect.DeepEqual(loaded, idx) {
		t.Fatalf("round trip mismatch:\n got %+v\nwant %+v", loaded, idx)
	}
	if !ValidateSessionDisplayIndex(loaded, 7, true, digest, idx.TranscriptSize) {
		t.Fatal("ValidateSessionDisplayIndex rejected a fresh index")
	}
}

func TestLoadSessionPreviewFromDisplayIndexReadsFirstAuthoredRange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	session := NewSession("system prompt")
	session.Add(provider.Message{Role: provider.RoleUser, Content: "first question"})
	session.Add(provider.Message{Role: provider.RoleAssistant, Content: strings.Repeat("answer", 10_000)})
	session.Add(provider.Message{Role: provider.RoleUser, Content: "second question"})
	if err := session.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot: %v", err)
	}

	preview, ok, err := LoadSessionPreviewFromDisplayIndex(path)
	if err != nil || !ok || preview != "first question" {
		t.Fatalf("preview = %q, ok=%v, err=%v", preview, ok, err)
	}
}

func TestSessionDisplayIndexOffsetsMatchTranscript(t *testing.T) {
	msgs := displayIndexTestMessages()
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := writeSessionMessages(path, msgs); err != nil {
		t.Fatalf("writeSessionMessages: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	digest, err := digestSessionMessages(msgs)
	if err != nil {
		t.Fatalf("digestSessionMessages: %v", err)
	}
	idx := BuildSessionDisplayIndex(msgs, 1, true, digest)
	if idx == nil {
		t.Fatal("BuildSessionDisplayIndex returned nil")
	}
	if int64(len(raw)) != idx.TranscriptSize {
		t.Fatalf("file size = %d, transcript_size = %d", len(raw), idx.TranscriptSize)
	}
	for _, entry := range idx.Entries {
		end := entry.Offset + entry.Length
		if end > int64(len(raw)) {
			t.Fatalf("entry %d range [%d,%d) exceeds file size %d", entry.Index, entry.Offset, end, len(raw))
		}
		line := raw[entry.Offset:end]
		if line[len(line)-1] != '\n' {
			t.Fatalf("entry %d line does not end with newline", entry.Index)
		}
		var m provider.Message
		if err := json.Unmarshal(line, &m); err != nil {
			t.Fatalf("entry %d line does not decode: %v", entry.Index, err)
		}
		if m.Role != msgs[entry.Index].Role {
			t.Errorf("entry %d role = %q, want %q", entry.Index, m.Role, msgs[entry.Index].Role)
		}
		if want := msgs[entry.Index].Content; len(want) > 0 && !strings.HasPrefix(m.Content, want[:min(len(want), 16)]) {
			t.Errorf("entry %d content = %q, want prefix of %q", entry.Index, m.Content, want)
		}
	}
	// Spot-check the classification flags.
	wantFlags := map[int]DisplayIndexEntry{
		1: {Role: provider.RoleUser, AuthoredTurn: 1, StartsTurn: true},
		2: {Role: provider.RoleAssistant, AuthoredTurn: 1, HasToolCalls: true},
		3: {Role: provider.RoleTool, AuthoredTurn: 1, ToolResult: true},
		4: {Role: provider.RoleAssistant, AuthoredTurn: 1, LocalOnly: true},
		5: {Role: provider.RoleUser, AuthoredTurn: 1, Steer: true},
		6: {Role: provider.RoleUser, AuthoredTurn: 1, Synthetic: true},
		7: {Role: provider.RoleUser, AuthoredTurn: 2, StartsTurn: true, HasImages: true},
		8: {Role: provider.RoleAssistant, AuthoredTurn: 2},
	}
	for i, want := range wantFlags {
		got := idx.Entries[i]
		if got.Role != want.Role || got.AuthoredTurn != want.AuthoredTurn ||
			got.StartsTurn != want.StartsTurn || got.HasToolCalls != want.HasToolCalls ||
			got.ToolResult != want.ToolResult || got.LocalOnly != want.LocalOnly ||
			got.Steer != want.Steer || got.Synthetic != want.Synthetic || got.HasImages != want.HasImages {
			t.Errorf("entry %d = %+v, want flags %+v", i, got, want)
		}
	}
}

func TestSessionDisplayIndexIncrementalAppend(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	base := NewSession("sys")
	base.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	if err := base.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot base: %v", err)
	}
	indexPath := store.SessionDisplayIndex(path)
	before, err := LoadSessionDisplayIndex(indexPath)
	if err != nil {
		t.Fatalf("LoadSessionDisplayIndex before append: %v", err)
	}
	if before.MessageCount != 2 {
		t.Fatalf("message_count before append = %d, want 2", before.MessageCount)
	}

	next, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	next.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	next.Add(provider.Message{Role: provider.RoleUser, Content: "second"})
	if err := next.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot append: %v", err)
	}
	after, err := LoadSessionDisplayIndex(indexPath)
	if err != nil {
		t.Fatalf("LoadSessionDisplayIndex after append: %v", err)
	}
	if after.MessageCount != 4 {
		t.Fatalf("message_count after append = %d, want 4", after.MessageCount)
	}
	if after.Revision != before.Revision+1 {
		t.Fatalf("revision = %d, want base %d + 1", after.Revision, before.Revision)
	}
	if !reflect.DeepEqual(after.Entries[:before.MessageCount], before.Entries) {
		t.Fatalf("prefix entries changed across append:\nbefore %+v\nafter  %+v", before.Entries, after.Entries[:before.MessageCount])
	}
	if after.Entries[3].AuthoredTurn != 2 || !after.Entries[3].StartsTurn {
		t.Fatalf("appended user entry = %+v, want authored_turn 2 starting the turn", after.Entries[3])
	}
	msgs, _, _, err := loadSessionMessages(path)
	if err != nil {
		t.Fatalf("loadSessionMessages: %v", err)
	}
	digest, err := digestSessionMessages(msgs)
	if err != nil {
		t.Fatalf("digestSessionMessages: %v", err)
	}
	if !ValidateSessionDisplayIndex(after, after.Revision, true, digest, after.TranscriptSize) {
		t.Fatal("appended index does not validate against the persisted transcript")
	}
}

func TestSessionDisplayIndexRewriteInvalidates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	s := NewSession("sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot: %v", err)
	}
	indexPath := store.SessionDisplayIndex(path)
	stale, err := LoadSessionDisplayIndex(indexPath)
	if err != nil {
		t.Fatalf("LoadSessionDisplayIndex: %v", err)
	}

	// Rewind/compaction shape: the history shrinks, so revision and digest move.
	s.Rewrite(s.Messages[:2], "rewind")
	if err := s.SaveRewrite(path); err != nil {
		t.Fatalf("SaveRewrite: %v", err)
	}
	revision, _, err := sessionContentRevision(path)
	if err != nil {
		t.Fatalf("sessionContentRevision: %v", err)
	}
	digest, err := digestSessionMessages(s.Messages[:2])
	if err != nil {
		t.Fatalf("digestSessionMessages: %v", err)
	}
	if ValidateSessionDisplayIndex(stale, revision, true, digest, displayIndexTranscriptSize(t, s.Messages[:2])) {
		t.Fatal("stale index still validates after rewrite")
	}
	fresh, err := LoadSessionDisplayIndex(indexPath)
	if err != nil {
		t.Fatalf("LoadSessionDisplayIndex after rewrite: %v", err)
	}
	if fresh.MessageCount != 2 {
		t.Fatalf("message_count after rewrite = %d, want 2", fresh.MessageCount)
	}
	if !ValidateSessionDisplayIndex(fresh, revision, true, digest, displayIndexTranscriptSize(t, s.Messages[:2])) {
		t.Fatal("rebuilt index does not validate after rewrite")
	}
}

func TestScanSessionDisplayIndexParity(t *testing.T) {
	msgs := displayIndexTestMessages()
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := writeSessionMessages(path, msgs); err != nil {
		t.Fatalf("writeSessionMessages: %v", err)
	}
	digest, err := digestSessionMessages(msgs)
	if err != nil {
		t.Fatalf("digestSessionMessages: %v", err)
	}
	built := BuildSessionDisplayIndex(msgs, 3, true, digest)
	if built == nil {
		t.Fatal("BuildSessionDisplayIndex returned nil")
	}
	scanned, err := ScanSessionDisplayIndex(path)
	if err != nil {
		t.Fatalf("ScanSessionDisplayIndex: %v", err)
	}
	if !reflect.DeepEqual(scanned.Entries, built.Entries) {
		t.Fatalf("scanner entries diverge from builder:\nscanned %+v\nbuilt   %+v", scanned.Entries, built.Entries)
	}
	if scanned.MessageCount != built.MessageCount ||
		scanned.AuthoredTurns != built.AuthoredTurns ||
		scanned.TranscriptSize != built.TranscriptSize ||
		scanned.ContentDigest != built.ContentDigest {
		t.Fatalf("scanner header = (%d, %d, %d, %q), want (%d, %d, %d, %q)",
			scanned.MessageCount, scanned.AuthoredTurns, scanned.TranscriptSize, scanned.ContentDigest,
			built.MessageCount, built.AuthoredTurns, built.TranscriptSize, built.ContentDigest)
	}
	if scanned.RevisionKnown {
		t.Fatal("scanned index must not claim a revision; the transcript does not carry one")
	}
	// A scanned index validates against the transcript it scanned.
	if !ValidateSessionDisplayIndex(scanned, 0, false, digest, built.TranscriptSize) {
		t.Fatal("scanned index does not validate against its own transcript")
	}
}

func TestScanSessionDisplayIndexRejectsUnboundedLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oversized.jsonl")
	// Keep the payload syntactically irrelevant: the scanner must reject the
	// record before json.Unmarshal gets a chance to materialize it.
	if err := os.WriteFile(path, append(make([]byte, sessionDisplayIndexMaxLineBytes+1), '\n'), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := ScanSessionDisplayIndex(path); err == nil {
		t.Fatal("ScanSessionDisplayIndex accepted a line over the safety limit")
	}
}

func TestLoadSessionDisplayIndexCorrupt(t *testing.T) {
	dir := t.TempDir()
	truncated := filepath.Join(dir, "truncated.display-index.json")
	if err := os.WriteFile(truncated, []byte(`{"schema_version":1,"revision":`), 0o600); err != nil {
		t.Fatalf("WriteFile truncated: %v", err)
	}
	if _, err := LoadSessionDisplayIndex(truncated); err == nil {
		t.Fatal("LoadSessionDisplayIndex accepted truncated JSON")
	}
	wrongSchema := filepath.Join(dir, "schema.display-index.json")
	if err := os.WriteFile(wrongSchema, []byte(`{"schema_version":999,"message_count":0,"entries":[]}`), 0o600); err != nil {
		t.Fatalf("WriteFile wrong schema: %v", err)
	}
	if _, err := LoadSessionDisplayIndex(wrongSchema); err == nil {
		t.Fatal("LoadSessionDisplayIndex accepted schema_version 999")
	}
	countMismatch := filepath.Join(dir, "count.display-index.json")
	if err := os.WriteFile(countMismatch, []byte(`{"schema_version":1,"message_count":2,"entries":[]}`), 0o600); err != nil {
		t.Fatalf("WriteFile count mismatch: %v", err)
	}
	if _, err := LoadSessionDisplayIndex(countMismatch); err == nil {
		t.Fatal("LoadSessionDisplayIndex accepted message_count/entries mismatch")
	}
	badRange := filepath.Join(dir, "range.display-index.json")
	if err := os.WriteFile(badRange, []byte(`{"schema_version":1,"transcript_size":10,"message_count":1,"entries":[{"index":0,"offset":1,"length":9}]}`), 0o600); err != nil {
		t.Fatalf("WriteFile bad range: %v", err)
	}
	if _, err := LoadSessionDisplayIndex(badRange); err == nil {
		t.Fatal("LoadSessionDisplayIndex accepted a non-contiguous offset range")
	}
}

func TestRepairSessionDisplayReadModelFromAuthoritativeEventLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "repair.jsonl")
	s := NewSession("sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot base: %v", err)
	}
	oldModel, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read old model: %v", err)
	}
	oldIndex, err := os.ReadFile(store.SessionDisplayIndex(path))
	if err != nil {
		t.Fatalf("read old index: %v", err)
	}
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "new tail"})
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot tail: %v", err)
	}
	if err := os.WriteFile(path, oldModel, 0o600); err != nil {
		t.Fatalf("restore stale model: %v", err)
	}
	if err := os.WriteFile(store.SessionDisplayIndex(path), oldIndex, 0o600); err != nil {
		t.Fatalf("restore stale index: %v", err)
	}

	msgs, state, repairable, err := LoadSessionDisplayMessages(path)
	if err != nil || !repairable {
		t.Fatalf("LoadSessionDisplayMessages = (%d, %+v, %v, %v)", len(msgs), state, repairable, err)
	}
	if len(msgs) != 3 || msgs[2].Content != "new tail" {
		t.Fatalf("authoritative messages = %+v, want event-log tail", msgs)
	}
	if err := RepairSessionDisplayReadModel(path); err != nil {
		t.Fatalf("RepairSessionDisplayReadModel: %v", err)
	}
	repaired, err := loadSessionMessagesFromJSONL(path)
	if err != nil || !reflect.DeepEqual(repaired, msgs) {
		t.Fatalf("repaired model = %+v, err %v; want %+v", repaired, err, msgs)
	}
	idx, err := LoadSessionDisplayIndex(store.SessionDisplayIndex(path))
	if err != nil {
		t.Fatalf("LoadSessionDisplayIndex repaired: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !ValidateSessionDisplayIndex(idx, state.Revision, state.RevisionKnown, state.Digest, info.Size()) {
		t.Fatalf("repaired index does not match read model: %+v", idx)
	}
}

func TestValidateSessionDisplayIndexMismatch(t *testing.T) {
	msgs := displayIndexTestMessages()
	digest, err := digestSessionMessages(msgs)
	if err != nil {
		t.Fatalf("digestSessionMessages: %v", err)
	}
	size := displayIndexTranscriptSize(t, msgs)
	idx := BuildSessionDisplayIndex(msgs, 4, true, digest)
	if idx == nil {
		t.Fatal("BuildSessionDisplayIndex returned nil")
	}
	if ValidateSessionDisplayIndex(nil, 4, true, digest, size) {
		t.Fatal("nil index validated")
	}
	otherDigest, err := digestSessionMessages(msgs[:2])
	if err != nil {
		t.Fatalf("digestSessionMessages prefix: %v", err)
	}
	if ValidateSessionDisplayIndex(idx, 4, true, otherDigest, size) {
		t.Fatal("index validated against a foreign digest")
	}
	if ValidateSessionDisplayIndex(idx, 5, true, digest, size) {
		t.Fatal("index validated against a foreign revision")
	}
	if ValidateSessionDisplayIndex(idx, 4, true, digest, size-1) {
		t.Fatal("index validated against a foreign transcript size")
	}
	if ValidateSessionDisplayIndex(idx, 0, false, digest, size) {
		t.Fatal("index with a known revision validated as revision-unknown")
	}
}

func TestWriteSessionDisplayIndexPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not expose POSIX permission bits")
	}

	msgs := displayIndexTestMessages()
	digest, err := digestSessionMessages(msgs)
	if err != nil {
		t.Fatalf("digestSessionMessages: %v", err)
	}
	idx := BuildSessionDisplayIndex(msgs, 1, true, digest)
	if idx == nil {
		t.Fatal("BuildSessionDisplayIndex returned nil")
	}
	path := filepath.Join(t.TempDir(), "session.display-index.json")
	if err := WriteSessionDisplayIndex(path, idx); err != nil {
		t.Fatalf("WriteSessionDisplayIndex: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("permissions = %o, want 600", perm)
	}
	// Rewriting an existing index keeps the tight permissions.
	if err := WriteSessionDisplayIndex(path, idx); err != nil {
		t.Fatalf("WriteSessionDisplayIndex rewrite: %v", err)
	}
	info, err = os.Stat(path)
	if err != nil {
		t.Fatalf("Stat after rewrite: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("permissions after rewrite = %o, want 600", perm)
	}
}
