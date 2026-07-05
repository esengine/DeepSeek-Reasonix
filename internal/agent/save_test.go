package agent

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/provider"
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

func TestSaveRewriteRejectsRevisionCASConflict(t *testing.T) {
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
	err = stale.SaveRewrite(path)
	if !errors.Is(err, ErrSessionSnapshotConflict) {
		t.Fatalf("SaveRewrite revision conflict err = %v, want ErrSessionSnapshotConflict", err)
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
