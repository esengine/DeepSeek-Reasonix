package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/provider"
)

// --- NewSession ---

func TestNewSessionEmpty(t *testing.T) {
	s := NewSession("")
	if len(s.Messages) != 0 {
		t.Errorf("empty session should have 0 messages, got %d", len(s.Messages))
	}
}

func TestNewSessionWithSystem(t *testing.T) {
	s := NewSession("You are a helpful assistant.")
	if len(s.Messages) != 1 {
		t.Fatalf("want 1 message, got %d", len(s.Messages))
	}
	if s.Messages[0].Role != provider.RoleSystem {
		t.Errorf("role = %q, want system", s.Messages[0].Role)
	}
	if s.Messages[0].Content != "You are a helpful assistant." {
		t.Errorf("content = %q", s.Messages[0].Content)
	}
}

// --- Session.Add ---

func TestSessionAdd(t *testing.T) {
	s := NewSession("")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "hello"})
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "hi there"})
	if len(s.Messages) != 2 {
		t.Fatalf("want 2 messages, got %d", len(s.Messages))
	}
	if s.Messages[0].Role != provider.RoleUser {
		t.Errorf("first role = %q", s.Messages[0].Role)
	}
	if s.Messages[1].Role != provider.RoleAssistant {
		t.Errorf("second role = %q", s.Messages[1].Role)
	}
}

// --- Session.HasContent ---

func TestHasContentEmpty(t *testing.T) {
	s := NewSession("")
	if s.HasContent() {
		t.Error("empty session should not have content")
	}
}

func TestHasContentSystemOnly(t *testing.T) {
	s := NewSession("system prompt")
	if s.HasContent() {
		t.Error("system-only session should not have content")
	}
}

func TestHasContentWithUser(t *testing.T) {
	s := NewSession("system")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "hello"})
	if !s.HasContent() {
		t.Error("session with user message should have content")
	}
}

func TestHasContentWithAssistant(t *testing.T) {
	s := NewSession("")
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "response"})
	if !s.HasContent() {
		t.Error("session with assistant message should have content")
	}
}

func TestHasContentWithTool(t *testing.T) {
	s := NewSession("")
	s.Add(provider.Message{Role: provider.RoleTool, Content: "result", ToolCallID: "tc1"})
	if !s.HasContent() {
		t.Error("session with tool message should have content")
	}
}

// --- Session.HasSystemMessage ---

func TestHasSystemMessageWithSystem(t *testing.T) {
	s := NewSession("system prompt")
	if !s.HasSystemMessage() {
		t.Error("session with system message should report HasSystemMessage true")
	}
}

func TestHasSystemMessageWithoutSystem(t *testing.T) {
	s := NewSession("")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "hello"})
	if s.HasSystemMessage() {
		t.Error("session without system message should report HasSystemMessage false")
	}
}

func TestHasSystemMessageAfterReplaceWithoutSystem(t *testing.T) {
	s := NewSession("system")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "kept"})
	// Replace with messages that have no system message — simulates a
	// compact/summarise path that failed to preserve the system prompt.
	s.Replace([]provider.Message{
		{Role: provider.RoleUser, Content: "replaced"},
	})
	if s.HasContent() {
		// HasContent returns true because the user message exists.
		if s.HasSystemMessage() {
			t.Error("session replaced without system message should report HasSystemMessage false")
		}
	} else {
		t.Error("session with user message should have content")
	}
}

func TestHasSystemMessageCompactedKeepsSystem(t *testing.T) {
	// This is the healthy path: compact preserves the system message at index 0.
	s := NewSession("system")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "answer"})
	s.Replace([]provider.Message{
		{Role: provider.RoleSystem, Content: "system"},
		{Role: provider.RoleUser, Content: "summary"},
	})
	if !s.HasSystemMessage() {
		t.Error("compacted session should still have system message at index 0")
	}
}

// --- Save / LoadSession round-trip ---

func TestSaveLoadSessionRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")

	s := NewSession("system prompt")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "hello"})
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "world"})
	if err := s.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(loaded.Messages) != 3 {
		t.Fatalf("want 3 messages, got %d", len(loaded.Messages))
	}
	if loaded.Messages[0].Content != "system prompt" {
		t.Errorf("system = %q", loaded.Messages[0].Content)
	}
	if loaded.Messages[1].Content != "hello" {
		t.Errorf("user = %q", loaded.Messages[1].Content)
	}
	if loaded.Messages[2].Content != "world" {
		t.Errorf("assistant = %q", loaded.Messages[2].Content)
	}
}

func TestSaveEmptyPath(t *testing.T) {
	s := NewSession("")
	if err := s.Save(""); err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestSaveCreatesDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "deep", "nested", "session.jsonl")
	s := NewSession("")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "test"})
	if err := s.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("session file should exist")
	}
}

func TestLoadSessionMissing(t *testing.T) {
	_, err := LoadSession("/nonexistent/session.jsonl")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !os.IsNotExist(err) {
		t.Errorf("error should be os.IsNotExist, got %v", err)
	}
}

func TestLoadSessionMalformed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.jsonl")
	os.WriteFile(path, []byte("not valid json\n"), 0o644)
	_, err := LoadSession(path)
	if err == nil {
		t.Fatal("expected error for malformed JSONL")
	}
	if !strings.Contains(err.Error(), "decode") {
		t.Errorf("error should mention decode: %v", err)
	}
}

// --- ListSessions ---

func TestListSessionsMissingDirReturnsNil(t *testing.T) {
	sessions, err := ListSessions("/nonexistent/dir")
	if err != nil {
		t.Fatalf("expected nil error for missing dir, got %v", err)
	}
	if sessions != nil {
		t.Errorf("expected nil sessions, got %v", sessions)
	}
}

func TestListSessionsEmptyDir(t *testing.T) {
	dir := t.TempDir()
	sessions, err := ListSessions(dir)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("want 0 sessions, got %d", len(sessions))
	}
}

func TestListSessionsSorted(t *testing.T) {
	dir := t.TempDir()
	// Create two sessions with different content.
	s1 := NewSession("")
	s1.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	s1.Save(filepath.Join(dir, "a.jsonl"))

	s2 := NewSession("")
	s2.Add(provider.Message{Role: provider.RoleUser, Content: "second"})
	s2.Save(filepath.Join(dir, "b.jsonl"))

	sessions, err := ListSessions(dir)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("want 2 sessions, got %d", len(sessions))
	}
	// Newest first.
	if sessions[0].ModTime.Before(sessions[1].ModTime) {
		t.Error("sessions should be sorted newest first")
	}
}

func TestListSessionsSkipsEmpty(t *testing.T) {
	dir := t.TempDir()
	// A session with only a system prompt (no user interaction) should be skipped.
	s := NewSession("system only")
	s.Save(filepath.Join(dir, "empty.jsonl"))

	sessions, err := ListSessions(dir)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("empty sessions should be skipped, got %d", len(sessions))
	}
}

func TestListSessionsSkipsNonJSONL(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("not a session"), 0o644)
	s := NewSession("")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "real"})
	s.Save(filepath.Join(dir, "real.jsonl"))

	sessions, err := ListSessions(dir)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(sessions) != 1 {
		t.Errorf("want 1 session, got %d", len(sessions))
	}
}

// --- previewSession ---

func TestPreviewSession(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	s := NewSession("system")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "Help me debug the auth module"})
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "Sure, let me look..."})
	s.Save(path)

	preview, turns := previewSession(path)
	if turns != 1 {
		t.Errorf("turns = %d, want 1", turns)
	}
	if !strings.Contains(preview, "debug") {
		t.Errorf("preview = %q", preview)
	}
}

func TestPreviewSessionStripsTransientReasoningLanguageBlock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	s := NewSession("system")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "<reasoning-language>\nVisible reasoning/thinking text preference: use Simplified Chinese.\n</reasoning-language>\n\nHelp me debug the auth module"})
	s.Save(path)

	preview, turns := previewSession(path)
	if turns != 1 {
		t.Errorf("turns = %d, want 1", turns)
	}
	if preview != "Help me debug the auth module" {
		t.Errorf("preview = %q, want user prompt", preview)
	}
}

func TestPreviewSessionStripsTransientResponseLanguageBlock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	s := NewSession("system")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "<response-language>\nFinal answer language preference: use English.\n</response-language>\n\nHelp me debug the auth module"})
	s.Save(path)

	preview, turns := previewSession(path)
	if turns != 1 {
		t.Errorf("turns = %d, want 1", turns)
	}
	if preview != "Help me debug the auth module" {
		t.Errorf("preview = %q, want user prompt", preview)
	}
}

func TestPreviewSessionLongMessage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	s := NewSession("")
	s.Add(provider.Message{Role: provider.RoleUser, Content: strings.Repeat("a", 200)})
	s.Save(path)

	preview, _ := previewSession(path)
	if len([]rune(preview)) > 80 {
		t.Errorf("preview should be capped at 80 runes, got %d", len([]rune(preview)))
	}
	if !strings.HasSuffix(preview, "…") {
		t.Errorf("truncated preview should end with …, got %q", preview)
	}
}

func TestPreviewSessionMalformed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.jsonl")
	os.WriteFile(path, []byte("not json\n"), 0o644)
	preview, turns := previewSession(path)
	if turns != 0 {
		t.Errorf("turns = %d, want 0", turns)
	}
	if preview != "" {
		t.Errorf("preview = %q, want empty", preview)
	}
}

// --- NewSessionPath ---

func TestNewSessionPath(t *testing.T) {
	dir := t.TempDir()
	path := NewSessionPath(dir, "deepseek-chat")
	if !strings.HasSuffix(path, ".jsonl") {
		t.Errorf("should end with .jsonl: %s", path)
	}
	if !strings.Contains(path, "deepseek-chat") {
		t.Errorf("should contain model name: %s", path)
	}
	if !strings.HasPrefix(path, dir) {
		t.Errorf("should be under dir: %s", path)
	}
}

func TestNewSessionPathSanitizesSlashes(t *testing.T) {
	path := NewSessionPath("/dir", "provider/model")
	base := filepath.Base(path)
	if strings.Contains(base, "/") {
		t.Errorf("filename should not contain /: %s", base)
	}
	if !strings.Contains(base, "provider-model") {
		t.Errorf("slashes should be replaced: %s", base)
	}
}

func TestNewSessionPathSanitizesWindowsReservedPunctuation(t *testing.T) {
	dir := t.TempDir()
	path := NewSessionPath(dir, `nemotron-3-nano:30b<>"|?*`)
	base := filepath.Base(path)
	if strings.ContainsAny(base, `:<>"|?*`) {
		t.Fatalf("filename contains Windows-reserved punctuation: %s", base)
	}
	if !strings.Contains(base, "nemotron-3-nano-30b") {
		t.Fatalf("colon should be replaced without hiding the model hint: %s", base)
	}

	s := NewSession("")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "hello"})
	if err := s.Save(path); err != nil {
		t.Fatalf("save session with sanitized model filename: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("stat saved session: %v", err)
	}
}

func TestNewSessionPathEmptyModel(t *testing.T) {
	path := NewSessionPath("/dir", "")
	if !strings.Contains(path, "session") {
		t.Errorf("empty model should use 'session' fallback: %s", path)
	}
}

// --- rewrite-save baseline ---

// TestNeedsRewriteSaveFollowsSaves pins the baseline's lifecycle on the
// session object itself: an in-memory rewrite demands a rewrite save, every
// full save re-anchors (including the plain force Save the depth-cap recovery
// path uses), and the baseline never moves backwards when a slower save
// reports an older capture.
func TestNeedsRewriteSaveFollowsSaves(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	s := NewSession("sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "hi"})
	if s.NeedsRewriteSave() {
		t.Fatal("fresh session should not need a rewrite save")
	}
	s.IncrementRewrite()
	if !s.NeedsRewriteSave() {
		t.Fatal("in-memory rewrite must demand a rewrite save")
	}
	if err := s.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if s.NeedsRewriteSave() {
		t.Fatal("force save must re-anchor the rewrite baseline")
	}
	s.IncrementRewrite()
	if err := s.SaveRewrite(path); err != nil {
		t.Fatalf("SaveRewrite: %v", err)
	}
	if s.NeedsRewriteSave() {
		t.Fatal("SaveRewrite must re-anchor the rewrite baseline")
	}

	// A slower save that captured an older rewriteVersion must not roll the
	// baseline back below what a faster save already persisted.
	digest, err := digestSessionMessages(s.Snapshot())
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	s.markPersisted(path, digest, 1, 1, 0)
	if s.NeedsRewriteSave() {
		t.Fatal("stale capture rolled the rewrite baseline backwards")
	}
}

func TestUpdateToolCallPreviewPersistsAfterMidTurnSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	s := NewSession("system")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "edit twice"})
	s.Add(provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{
		{ID: "c1", Name: "edit_file", Arguments: `{}`},
		{ID: "c2", Name: "edit_file", Arguments: `{}`},
	}})
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("mid-turn snapshot: %v", err)
	}

	refreshed := provider.ToolCall{ID: "c2", Diff: "@@ -1 +1 @@\n-ready\n+done\n", Added: 1, Removed: 1}
	if !s.UpdateToolCallPreview(refreshed) {
		t.Fatal("matching tool call was not updated")
	}
	s.Add(provider.Message{Role: provider.RoleTool, ToolCallID: "c1", Name: "edit_file", Content: "ready"})
	s.Add(provider.Message{Role: provider.RoleTool, ToolCallID: "c2", Name: "edit_file", Content: "done"})
	if !s.NeedsRewriteSave() {
		t.Fatal("mutating a snapshotted assistant message must require rewrite save")
	}
	if err := s.SaveRewrite(path); err != nil {
		t.Fatalf("rewrite refreshed preview: %v", err)
	}

	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	var got provider.ToolCall
	for _, msg := range loaded.Messages {
		for _, call := range msg.ToolCalls {
			if call.ID == "c2" {
				got = call
			}
		}
	}
	if got.Diff != refreshed.Diff || got.Added != 1 || got.Removed != 1 {
		t.Fatalf("persisted preview = %+v, want %+v", got, refreshed)
	}
}

func TestUpdateToolCallResolutionPersistsAfterMidTurnSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	s := NewSession("system")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "use MCP"})
	s.Add(provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
		ID: "c1", Name: "use_capability",
		Arguments: `{"action":"call","capability_id":"mcp-tool:db/write"}`,
	}}})
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("mid-turn snapshot: %v", err)
	}

	readOnly := false
	resolved := provider.ToolCall{
		ID: "c1", ResolvedName: "mcp__db__write",
		CapabilityID: "mcp-tool:db/write", ResolvedReadOnly: &readOnly,
	}
	if !s.UpdateToolCallResolution(resolved) {
		t.Fatal("matching tool call resolution was not updated")
	}
	s.Add(provider.Message{Role: provider.RoleTool, ToolCallID: "c1", Name: "use_capability", Content: "done"})
	if !s.NeedsRewriteSave() {
		t.Fatal("resolved metadata on a snapshotted assistant message must require rewrite save")
	}
	if err := s.SaveRewrite(path); err != nil {
		t.Fatalf("rewrite resolved metadata: %v", err)
	}

	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	got := loaded.Messages[2].ToolCalls[0]
	if got.ResolvedReadOnly == nil || *got.ResolvedReadOnly ||
		got.ResolvedName != resolved.ResolvedName || got.CapabilityID != resolved.CapabilityID {
		t.Fatalf("persisted resolved metadata = %+v, want %+v", got, resolved)
	}
}

// TestRewriteBaselineStaysWithClones: an unpersisted rewrite travels with the
// clone, and the source persisting later does not mark the clone's copy as
// saved — each session object owns its own baseline, so no swap can orphan or
// misattribute it.
func TestRewriteBaselineStaysWithClones(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	s := NewSession("sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "hi"})
	s.IncrementRewrite()
	clone := s.CloneWithMessages(s.Snapshot())
	if !clone.NeedsRewriteSave() {
		t.Fatal("clone must inherit the unpersisted rewrite")
	}
	if err := s.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if s.NeedsRewriteSave() {
		t.Fatal("source baseline not re-anchored by save")
	}
	if !clone.NeedsRewriteSave() {
		t.Fatal("saving the source must not mark the clone's rewrite persisted")
	}
}
