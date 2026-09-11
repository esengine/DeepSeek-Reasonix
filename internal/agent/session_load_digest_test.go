package agent

import (
	"crypto/sha256"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/provider"
)

// legacyDigestSessionMessages is the pre-fusion reference implementation: a
// full re-serialize pass over the final message slice. The incremental hasher
// fed during decode must reproduce it byte-for-byte.
func legacyDigestSessionMessages(msgs []provider.Message) ([sha256.Size]byte, error) {
	h := sha256.New()
	for _, m := range msgs {
		m = messageForSessionIdentity(m)
		b, err := json.Marshal(m)
		if err != nil {
			return [sha256.Size]byte{}, err
		}
		if _, err := h.Write(b); err != nil {
			return [sha256.Size]byte{}, err
		}
		if _, err := h.Write([]byte{'\n'}); err != nil {
			return [sha256.Size]byte{}, err
		}
	}
	var out [sha256.Size]byte
	copy(out[:], h.Sum(nil))
	return out, nil
}

// representativeSessionMessages mixes the message shapes a long real session
// carries: system prompt, plain text, multimodal user input, reasoning with
// provider signatures, tool calls and their results, and display timestamps
// (which the digest must keep ignoring).
func representativeSessionMessages() []provider.Message {
	return []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "prompt one", CreatedAt: 1720000000000},
		{Role: provider.RoleAssistant, Content: "thinking about it", ReasoningContent: "chain of thought", ReasoningID: "rs_1", ReasoningStatus: "completed", ReasoningSignature: "sig"},
		{Role: provider.RoleUser, Content: "with image", Images: []string{"data:image/png;base64,aGVsbG8="}, CreatedAt: 1720000001000},
		{Role: provider.RoleAssistant, Content: "", ToolCalls: []provider.ToolCall{
			{ID: "call_1", Name: "read_file", Arguments: `{"path":"a.go"}`},
		}},
		{Role: provider.RoleTool, ToolCallID: "call_1", Name: "read_file", Content: "package main"},
		{Role: provider.RoleAssistant, Content: "final answer", WorkDurationMs: 42},
	}
}

func writeLegacyJSONLSession(t *testing.T, path string, msgs []provider.Message) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		t.Fatalf("open jsonl: %v", err)
	}
	enc := json.NewEncoder(f)
	for _, m := range msgs {
		if err := enc.Encode(m); err != nil {
			f.Close()
			t.Fatalf("encode jsonl: %v", err)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close jsonl: %v", err)
	}
}

// loadAndDigestSessionMessages loads like LoadSession does and returns the
// decode-fused digest alongside the messages.
func loadAndDigestSessionMessages(path string) (msgs []provider.Message, fromEvents, damaged bool, digest [sha256.Size]byte, digestOK bool, err error) {
	hasher := newSessionTranscriptHasher()
	msgs, fromEvents, damaged, err = loadSessionMessagesWithLimits(path, defaultSessionReplayLimits, hasher)
	digest, digestOK = hasher.sum()
	return msgs, fromEvents, damaged, digest, digestOK, err
}

func TestLoadSessionMessagesWithDigestMatchesLegacyEventLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	s := NewSession("sys")
	for _, m := range representativeSessionMessages()[1:] {
		s.Add(m)
		// One save per message forces a replace record plus a chain of append
		// records, exercising the hasher across both event types.
		if err := s.SaveSnapshot(path); err != nil {
			t.Fatalf("SaveSnapshot: %v", err)
		}
	}

	msgs, fromEvents, damaged, digest, digestOK, err := loadAndDigestSessionMessages(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !fromEvents || damaged {
		t.Fatalf("fromEvents=%v damaged=%v, want event-log replay without damage", fromEvents, damaged)
	}
	if !digestOK {
		t.Fatal("digestOK = false, want true")
	}
	want, err := legacyDigestSessionMessages(msgs)
	if err != nil {
		t.Fatalf("legacy digest: %v", err)
	}
	if digest != want {
		t.Fatalf("incremental digest %x != legacy digest %x", digest, want)
	}
}

func TestLoadSessionMessagesWithDigestMatchesLegacyJSONL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	writeLegacyJSONLSession(t, path, representativeSessionMessages())

	msgs, fromEvents, damaged, digest, digestOK, err := loadAndDigestSessionMessages(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if fromEvents || damaged {
		t.Fatalf("fromEvents=%v damaged=%v, want plain jsonl load", fromEvents, damaged)
	}
	if !digestOK {
		t.Fatal("digestOK = false, want true")
	}
	want, err := legacyDigestSessionMessages(msgs)
	if err != nil {
		t.Fatalf("legacy digest: %v", err)
	}
	if digest != want {
		t.Fatalf("incremental digest %x != legacy digest %x", digest, want)
	}
}

func TestLoadSessionMessagesWithDigestCoversOnlyCleanPrefix(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	sessionWithTurns(t, path, 2)

	logPath := SessionEventLogPath(path)
	f, err := os.OpenFile(logPath, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatalf("open log: %v", err)
	}
	if _, err := f.Write([]byte(`{"schema_version":1,"type":"append","message_index":5,"mess`)); err != nil {
		t.Fatalf("write torn tail: %v", err)
	}
	f.Close()

	msgs, _, damaged, digest, digestOK, err := loadAndDigestSessionMessages(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !damaged {
		t.Fatal("damaged = false, want true for torn tail")
	}
	if !digestOK {
		t.Fatal("digestOK = false, want true")
	}
	// The digest must cover exactly the replayable prefix the caller received,
	// never bytes from the torn record.
	want, err := legacyDigestSessionMessages(msgs)
	if err != nil {
		t.Fatalf("legacy digest: %v", err)
	}
	if digest != want {
		t.Fatalf("incremental digest %x != legacy digest %x", digest, want)
	}
}

func TestLoadSessionDigestFastPathKeepsPersistedBaseline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	s := NewSession("sys")
	for _, m := range representativeSessionMessages()[1:] {
		s.Add(m)
	}
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot: %v", err)
	}

	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if loaded.normalizedDirty {
		t.Fatal("normalizedDirty = true for a well-formed session, want false")
	}
	// The baseline anchored during load must equal the legacy digest of the
	// loaded transcript; otherwise change detection (HasUnsavedChanges) would
	// report phantom writes or miss real ones.
	want, err := legacyDigestSessionMessages(loaded.Snapshot())
	if err != nil {
		t.Fatalf("legacy digest: %v", err)
	}
	if !loaded.persisted.ok || loaded.persisted.digest != want {
		t.Fatalf("persisted baseline digest %x (ok=%v) != legacy digest %x", loaded.persisted.digest, loaded.persisted.ok, want)
	}
	if loaded.HasUnsavedChanges(path) {
		t.Fatal("HasUnsavedChanges = true right after a clean load, want false")
	}
}

func TestLoadSessionDigestRecomputedAfterNormalizationRepair(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	// Dangling tool call: normalization fabricates a placeholder tool result,
	// so the persisted baseline must be the digest of the repaired transcript.
	writeLegacyJSONLSession(t, path, []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "run it"},
		{Role: provider.RoleAssistant, Content: "", ToolCalls: []provider.ToolCall{
			{ID: "call_1", Name: "read_file", Arguments: `{"path":"a.go"}`},
		}},
	})

	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if !loaded.normalizedDirty {
		t.Fatal("normalizedDirty = false, want true for a dangling tool call")
	}
	want, err := legacyDigestSessionMessages(loaded.Snapshot())
	if err != nil {
		t.Fatalf("legacy digest: %v", err)
	}
	if !loaded.persisted.ok || loaded.persisted.digest != want {
		t.Fatalf("persisted baseline digest %x (ok=%v) != legacy digest of repaired transcript %x", loaded.persisted.digest, loaded.persisted.ok, want)
	}
	rawWant, err := legacyDigestSessionMessages(loaded.rawMessages)
	if err != nil {
		t.Fatalf("legacy raw digest: %v", err)
	}
	if loaded.persisted.digest == rawWant {
		t.Fatal("persisted baseline still hashes the pre-repair transcript")
	}
}

func TestLoadSessionDisplayMessagesDigestMatchesLegacy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	s := NewSession("sys")
	for _, m := range representativeSessionMessages()[1:] {
		s.Add(m)
	}
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot: %v", err)
	}

	msgs, state, clean, err := LoadSessionDisplayMessages(path)
	if err != nil {
		t.Fatalf("LoadSessionDisplayMessages: %v", err)
	}
	if !clean {
		t.Fatal("clean = false, want true")
	}
	want, err := legacyDigestSessionMessages(msgs)
	if err != nil {
		t.Fatalf("legacy digest: %v", err)
	}
	if state.Digest != want || state.DigestHex != digestString(want) {
		t.Fatalf("display digest %s != legacy digest %s", state.DigestHex, digestString(want))
	}
}

// BenchmarkLoadSessionDigestFusion contrasts the old load shape (decode pass +
// separate full re-serialize digest pass) with the fused decode-time digest on
// a synthetic long session with realistic tool-output sizes.
func BenchmarkLoadSessionDigestFusion(b *testing.B) {
	path := filepath.Join(b.TempDir(), "session.jsonl")
	s := NewSession("sys")
	bigResult := strings.Repeat("package main // line of tool output\n", 128) // ~4KB
	for range 400 {
		for _, m := range representativeSessionMessages()[1:] {
			if m.Role == provider.RoleTool {
				m.Content = bigResult
			}
			s.Add(m)
		}
	}
	if err := s.SaveSnapshot(path); err != nil {
		b.Fatalf("SaveSnapshot: %v", err)
	}

	b.Run("digest_pass_only", func(b *testing.B) {
		msgs, _, _, err := loadSessionMessages(path)
		if err != nil {
			b.Fatalf("loadSessionMessages: %v", err)
		}
		b.ResetTimer()
		for b.Loop() {
			if _, err := digestSessionMessages(msgs); err != nil {
				b.Fatalf("digestSessionMessages: %v", err)
			}
		}
	})
	b.Run("separate_digest_pass", func(b *testing.B) {
		for b.Loop() {
			msgs, _, _, err := loadSessionMessages(path)
			if err != nil {
				b.Fatalf("loadSessionMessages: %v", err)
			}
			if _, err := digestSessionMessages(msgs); err != nil {
				b.Fatalf("digestSessionMessages: %v", err)
			}
		}
	})
	b.Run("fused_decode_digest", func(b *testing.B) {
		for b.Loop() {
			if _, _, _, _, ok, err := loadAndDigestSessionMessages(path); err != nil || !ok {
				b.Fatalf("fused load: ok=%v err=%v", ok, err)
			}
		}
	})
}
