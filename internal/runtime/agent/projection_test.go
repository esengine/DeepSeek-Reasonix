package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reasonix/internal/state/sessionstore"
	"slices"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
)

func TestCompactionStateAtomicSaveLoad(t *testing.T) {
	dir := testenv.TempDir(t)
	path := filepath.Join(dir, "sess.jsonl")
	st := sessionstore.CompactionState{
		SchemaVersion:     sessionstore.CompactionStateSchemaCurrent,
		TranscriptVersion: 3,
		Projection: sessionstore.ContextProjection{
			Messages: []provider.Message{
				{Role: provider.RoleSystem, Content: "sys"},
				{Role: provider.RoleUser, Content: "summary-body"},
			},
			TranscriptVersion: 3,
			ProjectionVersion: 1,
			CoveredCount:      10,
			SummaryHash:       summaryContentHash("summary-body"),
			SourceTokens:      1000,
			ProjectionTokens:  200,
		},
		PromptCacheKey: "ws|sess|model",
		LastCacheState: CacheStateCold,
		Generation:     7,
		LastReceipt: &sessionstore.ContextMaintenanceReceipt{
			Status: "applied", Action: "summary", ProjectionVersion: 1,
			InputHash: "in", OutputHash: "out", SavedTokens: 800,
		},
	}
	if err := sessionstore.SaveCompactionState(path, st); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, ok, err := sessionstore.LoadCompactionState(path)
	if err != nil || !ok {
		t.Fatalf("load: ok=%v err=%v", ok, err)
	}
	if got.SchemaVersion != sessionstore.CompactionStateSchemaCurrent || got.TranscriptVersion != 3 {
		t.Fatalf("loaded state = %+v", got)
	}
	if len(got.Projection.Messages) != 2 || got.Projection.CoveredCount != 10 {
		t.Fatalf("projection = %+v", got.Projection)
	}
	if got.Generation != 7 || got.LastReceipt == nil || got.LastReceipt.OutputHash != "out" || got.LastReceipt.ProjectionVersion != 1 {
		t.Fatalf("v3 maintenance receipt not round-tripped: %+v", got)
	}
}

func TestLoadCompactionStateAcceptsLegacyV1(t *testing.T) {
	path := filepath.Join(testenv.TempDir(t), "legacy.jsonl")
	legacy := sessionstore.CompactionState{
		SchemaVersion:     sessionstore.CompactionStateSchemaV1,
		TranscriptVersion: 2,
		Projection: sessionstore.ContextProjection{
			Messages:          []provider.Message{{Role: provider.RoleUser, Content: "legacy summary"}},
			TranscriptVersion: 2,
			ProjectionVersion: 1,
			CoveredCount:      3,
		},
		LastTrigger: CompactionTriggerManual,
	}
	raw, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sessionstore.ContextStatePath(path), raw, 0o600); err != nil {
		t.Fatal(err)
	}

	got, ok, err := sessionstore.LoadCompactionState(path)
	if err != nil || !ok {
		t.Fatalf("load legacy V1: ok=%v err=%v", ok, err)
	}
	if got.SchemaVersion != sessionstore.CompactionStateSchemaV1 || got.Projection.Messages[0].Content != "legacy summary" {
		t.Fatalf("legacy state changed: %+v", got)
	}
}

func TestSaveCompactionStateCreatesPreviousReaderBoundary(t *testing.T) {
	path := filepath.Join(testenv.TempDir(t), "current.jsonl")
	if err := sessionstore.SaveCompactionState(path, sessionstore.CompactionState{
		Projection: sessionstore.ContextProjection{
			Messages:     []provider.Message{{Role: provider.RoleUser, Content: "logical summary"}, {Role: provider.RoleUser, Content: "retained anchor"}},
			CoveredCount: 2,
		},
	}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(sessionstore.ContextStatePath(path))
	if err != nil {
		t.Fatal(err)
	}
	var header struct {
		SchemaVersion int `json:"schema_version"`
	}
	if err := json.Unmarshal(raw, &header); err != nil {
		t.Fatal(err)
	}
	if header.SchemaVersion != sessionstore.CompactionStateSchemaCurrent {
		t.Fatalf("written schema = %d, want %d", header.SchemaVersion, sessionstore.CompactionStateSchemaCurrent)
	}
	if previousCompactionReaderAccepts(raw) {
		t.Fatal("V1-only reader would accept a sidecar with V2 logical message invariants")
	}
	if _, ok, err := sessionstore.LoadCompactionState(path); err != nil || !ok {
		t.Fatalf("current reader rejected V2 sidecar: ok=%v err=%v", ok, err)
	}
}

func previousCompactionReaderAccepts(raw []byte) bool {
	var header struct {
		SchemaVersion int `json:"schema_version"`
	}
	if json.Unmarshal(raw, &header) != nil {
		return false
	}
	return header.SchemaVersion == 0 || header.SchemaVersion == sessionstore.CompactionStateSchemaV1
}

func TestCompactToProjectionLeavesCanonicalIntact(t *testing.T) {
	fp := &fakeProvider{reply: "GOAL: ship projection\nFACTS: keep path /tmp/x"}
	sess := sessionstore.NewSession("sys")
	// Build enough history that a fold is economical.
	for i := range 12 {
		sess.Add(provider.Message{Role: provider.RoleUser, Content: "user turn " + strings.Repeat("x", 80) + " " + string(rune('A'+i%26))})
		sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "assistant work " + strings.Repeat("y", 200)})
		sess.Add(provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "c" + string(rune('0'+i%10)), Name: "read", Arguments: `{"path":"f"}`}}})
		sess.Add(provider.Message{Role: provider.RoleTool, ToolCallID: "c" + string(rune('0'+i%10)), Name: "read", Content: strings.Repeat("tool-out-", 40)})
	}
	before := append([]provider.Message(nil), sess.Messages...)
	dir := testenv.TempDir(t)
	sessionPath := filepath.Join(dir, "s.jsonl")
	a := New(fp, nil, sess, Options{
		ContextWindow: 50_000,
		CompactRatio:  0.85,
		SessionPath:   sessionPath,
		ModelRef:      "test/model",
	}, event.Discard)

	if _, err := a.CompactNow(context.Background(), CompactRequest{}); err != nil {
		t.Fatalf("CompactNow: %v", err)
	}
	after := sess.Snapshot()
	if len(after) != len(before) {
		t.Fatalf("canonical length changed: before=%d after=%d", len(before), len(after))
	}
	for i := range before {
		if before[i].Content != after[i].Content || before[i].Role != after[i].Role {
			t.Fatalf("canonical message %d changed", i)
		}
	}
	if len(a.sess.win.compactionState.Projection.Messages) == 0 {
		t.Fatal("expected projection messages")
	}
	// Projection must be shorter than canonical.
	if a.window().estimatedPromptTokens(a.sess.win.compactionState.Projection.Messages) >= a.window().estimatedPromptTokens(before) {
		t.Fatalf("projection did not shrink: proj=%d src=%d",
			a.window().estimatedPromptTokens(a.sess.win.compactionState.Projection.Messages),
			a.window().estimatedPromptTokens(before))
	}
	// Sidecar must exist and reload with an applied summary receipt (v3 does not
	// persist the legacy last_mode field).
	st, ok, err := sessionstore.LoadCompactionState(sessionPath)
	if err != nil || !ok {
		t.Fatalf("reload sidecar: ok=%v err=%v", ok, err)
	}
	if st.LastReceipt == nil || st.LastReceipt.Status != "applied" || st.LastReceipt.Action != "summary" {
		t.Fatalf("last receipt = %+v, want applied summary", st.LastReceipt)
	}
	if st.Projection.ProjectionVersion == 0 {
		t.Fatal("reloaded projection version is zero")
	}
	// Model-visible must use projection.
	visible := a.window().modelVisibleMessages()
	if len(visible) == len(before) {
		t.Fatal("model-visible still full canonical")
	}
	if len(fp.got) != 0 {
		t.Fatalf("the fold made %d provider request(s); it archives instead", len(fp.got))
	}
}

// Checkpoint installation no longer writes archive copies — the canonical
// transcript is the lossless store. ArchiveDir misconfiguration must not block
// a successful summary install.
func TestArchiveDirIgnoredOnCheckpointInstall(t *testing.T) {
	fp := &fakeProvider{reply: "digest"}
	sess := sessionstore.NewSession("sys")
	big := strings.Repeat("assistant work detail ", 200)
	for range 6 {
		sess.Add(provider.Message{Role: provider.RoleUser, Content: "turn"})
		sess.Add(provider.Message{Role: provider.RoleAssistant, Content: big})
	}
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "next"})
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "ok"})
	// Point archive at a file path so MkdirAll/Create would fail if archives were written.
	badArchive := filepath.Join(testenv.TempDir(t), "not-a-dir")
	if err := writeFile(badArchive, []byte("x")); err != nil {
		t.Fatal(err)
	}
	a := New(fp, nil, sess, Options{
		ContextWindow: 50_000, CompactRatio: 0.85, ArchiveDir: badArchive,
	}, event.Discard)
	if _, err := a.CompactNow(context.Background(), CompactRequest{}); err != nil {
		t.Fatalf("CompactNow with unusable ArchiveDir: %v", err)
	}
	if len(a.sess.win.compactionState.Projection.Messages) == 0 {
		t.Fatal("expected projection despite unusable ArchiveDir")
	}
}

func writeFile(path string, b []byte) error {
	return os.WriteFile(path, b, 0o644)
}

// visibleContext is what the model actually sees: the frozen body spliced with
// the live canonical tail. Reading Projection.Messages instead would answer for
// the body alone, which stopped being the whole view once the tail left it.
func visibleContext(a *Agent) []provider.Message {
	if a == nil {
		return nil
	}
	return a.window().modelVisibleMessages()
}

func hasCompactionSummary(msgs []provider.Message) bool {
	return slices.ContainsFunc(msgs, isCompactionSummary)
}

func joinContents(msgs []provider.Message) string {
	var b strings.Builder
	for _, m := range msgs {
		b.WriteString(m.Content)
		b.WriteByte('\n')
	}
	return b.String()
}
