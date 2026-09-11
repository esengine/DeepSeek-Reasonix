package agent

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

func TestCoveredPrefixHashIgnoresLocalToolRawContent(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "call-1", Name: "read", Arguments: `{}`}}},
		{Role: provider.RoleTool, ToolCallID: "call-1", Name: "read", Content: "bounded", RawContent: "full result A"},
	}
	first := coveredPrefixHash(msgs, len(msgs))
	msgs[1].RawContent = "full result B"
	if second := coveredPrefixHash(msgs, len(msgs)); second != first {
		t.Fatal("local RawContent edit changed the provider-visible covered-prefix hash")
	}
	msgs[1].Content = "different bounded result"
	if second := coveredPrefixHash(msgs, len(msgs)); second == first {
		t.Fatal("provider-visible Content edit did not invalidate the covered-prefix hash")
	}
}

func TestLoadProjectionSidecarMigratesPromotedV3ToolHash(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "system"},
		{Role: provider.RoleUser, Content: "task"},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "call-1", Name: "read", Arguments: `{}`}}},
		{Role: provider.RoleTool, ToolCallID: "call-1", Name: "read", Content: "bounded", RawContent: "full result"},
	}
	legacyHash := promotedCoveredPrefixHash(msgs, len(msgs))
	if legacyHash == coveredPrefixHash(msgs, len(msgs)) {
		t.Fatal("fixture does not distinguish promoted and bounded hashes")
	}
	if err := SaveCompactionState(path, CompactionState{
		SchemaVersion:  compactionStateSchemaV3,
		PromptCacheKey: promptCacheKey("ws", BranchID(path), "model"),
		LastReceipt: &ContextMaintenanceReceipt{
			Status: "applied", Action: "prune", CoveredPrefixHash: legacyHash,
		},
		Projection: ContextProjection{
			Messages:          []provider.Message{{Role: provider.RoleSystem, Content: "system"}, formatSummaryMessage("summary")},
			CoveredCount:      len(msgs),
			CoveredPrefixHash: legacyHash,
		},
	}); err != nil {
		t.Fatal(err)
	}
	sess := &Session{Messages: msgs}
	a := New(nil, nil, sess, Options{SessionPath: path, WorkspaceID: "ws", ModelRef: "model"}, event.Discard)
	want := coveredPrefixHash(msgs, len(msgs))
	if got := a.sess.compactionState.Projection.CoveredPrefixHash; got != want {
		t.Fatalf("loaded hash = %q, want migrated %q", got, want)
	}
	disk, ok, err := LoadCompactionState(path)
	if err != nil || !ok {
		t.Fatalf("load migrated sidecar: ok=%v err=%v", ok, err)
	}
	if got := disk.Projection.CoveredPrefixHash; got != want {
		t.Fatalf("persisted hash = %q, want %q", got, want)
	}
	if disk.LastReceipt == nil || disk.LastReceipt.CoveredPrefixHash != want {
		t.Fatalf("receipt hash was not normalized: %+v", disk.LastReceipt)
	}
	mutated := append([]provider.Message(nil), msgs...)
	mutated[len(mutated)-1].RawContent = "changed after migration"
	if !projectionValid(a.sess.compactionState, mutated, a.currentPromptCacheKey()) {
		t.Fatal("local RawContent edit invalidated a bounded provider projection")
	}
}

func TestLoadProjectionSidecarNormalizesPromotedToolBody(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	const fullSentinel = "FULL-PROMOTED-RESULT-MUST-NOT-REPLAY"
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "system"},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "call-1", Name: "read", Arguments: `{}`}}},
		{Role: provider.RoleTool, ToolCallID: "call-1", Name: "read", Content: "bounded", RawContent: fullSentinel},
	}
	legacyHash := promotedCoveredPrefixHash(msgs, len(msgs))
	if err := SaveCompactionState(path, CompactionState{
		SchemaVersion:  compactionStateSchemaV3,
		PromptCacheKey: promptCacheKey("ws", BranchID(path), "model"),
		LastReceipt: &ContextMaintenanceReceipt{
			Status: "applied", Action: "prune", CoveredPrefixHash: legacyHash,
		},
		Projection: ContextProjection{
			Messages: []provider.Message{
				{Role: provider.RoleSystem, Content: "system"},
				{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "call-1", Name: "read", Arguments: `{}`}}},
				{Role: provider.RoleTool, ToolCallID: "call-1", Name: "read", Content: fullSentinel},
			},
			CoveredCount:      len(msgs),
			CoveredPrefixHash: legacyHash,
		},
	}); err != nil {
		t.Fatal(err)
	}

	a := New(nil, nil, &Session{Messages: msgs}, Options{SessionPath: path, WorkspaceID: "ws", ModelRef: "model"}, event.Discard)
	projection := a.sess.compactionState.Projection
	if len(projection.Messages) != 3 || projection.Messages[2].Content != "bounded" {
		t.Fatalf("loaded projection tool body = %+v, want bounded Content", projection.Messages)
	}
	visible := modelInputMessages(modelVisibleFromProjection(projection, msgs))
	if encoded := projectionJSON(t, visible); strings.Contains(encoded, fullSentinel) {
		t.Fatalf("provider-visible migrated projection retained full result: %s", encoded)
	}

	disk, ok, err := LoadCompactionState(path)
	if err != nil || !ok {
		t.Fatalf("load normalized sidecar: ok=%v err=%v", ok, err)
	}
	if len(disk.Projection.Messages) != 3 || disk.Projection.Messages[2].Content != "bounded" ||
		disk.Projection.Messages[2].RawContent != "" || disk.Projection.Messages[2].ProviderContent != "" {
		t.Fatalf("persisted projection tool body was not normalized: %+v", disk.Projection.Messages)
	}
}

func TestLoadProjectionSidecarDropsUnverifiablePromotedToolBody(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	msgs := []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "call-1", Name: "read", Arguments: `{}`}}},
		{Role: provider.RoleTool, ToolCallID: "call-1", Name: "read", Content: "bounded", RawContent: "canonical full result"},
	}
	legacyHash := promotedCoveredPrefixHash(msgs, len(msgs))
	if err := SaveCompactionState(path, CompactionState{
		PromptCacheKey: promptCacheKey("ws", BranchID(path), "model"),
		LastReceipt:    &ContextMaintenanceReceipt{Status: "applied", Action: "prune", CoveredPrefixHash: legacyHash},
		Projection: ContextProjection{
			Messages: []provider.Message{
				msgs[0],
				{Role: provider.RoleTool, ToolCallID: "call-1", Name: "read", Content: "different unverified full result"},
			},
			CoveredCount: len(msgs), CoveredPrefixHash: legacyHash,
		},
	}); err != nil {
		t.Fatal(err)
	}

	a := New(nil, nil, &Session{Messages: msgs}, Options{SessionPath: path, WorkspaceID: "ws", ModelRef: "model"}, event.Discard)
	if len(a.sess.compactionState.Projection.Messages) != 0 {
		t.Fatalf("unverifiable promoted projection survived load: %+v", a.sess.compactionState.Projection.Messages)
	}
	if a.sess.compactionState.LastReceipt == nil || a.sess.compactionState.LastReceipt.Action != "prune" {
		t.Fatalf("maintenance receipt was lost: %+v", a.sess.compactionState.LastReceipt)
	}
	if got := a.Session().Snapshot(); !reflect.DeepEqual(got, msgs) {
		t.Fatalf("canonical transcript changed: got=%+v want=%+v", got, msgs)
	}
}

func TestCopyValidContextProjectionNormalizesPromotedToolBodyOrFailsClosed(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "call-1", Name: "read", Arguments: `{}`}}},
		{Role: provider.RoleTool, ToolCallID: "call-1", Name: "read", Content: "bounded", RawContent: "canonical full result"},
	}
	legacyHash := promotedCoveredPrefixHash(msgs, len(msgs))
	for _, tc := range []struct {
		name       string
		body       string
		wantCopied bool
	}{
		{name: "exact", body: msgs[1].RawContent, wantCopied: true},
		{name: "unverifiable", body: "different full result", wantCopied: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			original := filepath.Join(dir, "original.jsonl")
			target := filepath.Join(dir, "target.jsonl")
			if err := SaveCompactionState(original, CompactionState{
				Projection: ContextProjection{
					Messages: []provider.Message{
						msgs[0],
						{Role: provider.RoleTool, ToolCallID: "call-1", Name: "read", Content: tc.body},
					},
					CoveredCount: len(msgs), CoveredPrefixHash: legacyHash,
				},
				LastReceipt: &ContextMaintenanceReceipt{Status: "applied", CoveredPrefixHash: legacyHash},
			}); err != nil {
				t.Fatal(err)
			}
			copied, err := copyValidContextProjection(original, target, msgs)
			if err != nil || copied != tc.wantCopied {
				t.Fatalf("copy result: copied=%v err=%v want=%v", copied, err, tc.wantCopied)
			}
			if !copied {
				if _, ok, err := LoadCompactionState(target); err != nil || ok {
					t.Fatalf("unverifiable body created target sidecar: ok=%v err=%v", ok, err)
				}
				return
			}
			disk, ok, err := LoadCompactionState(target)
			if err != nil || !ok {
				t.Fatalf("load copied sidecar: ok=%v err=%v", ok, err)
			}
			if got := disk.Projection.Messages[1]; got.Content != msgs[1].Content || got.RawContent != "" || got.ProviderContent != "" {
				t.Fatalf("copied projection body = %+v, want bounded canonical Content", got)
			}
		})
	}
}

func TestNormalizePromotedProjectionToolBodiesRejectsAmbiguousDuplicateCallID(t *testing.T) {
	canonical := []provider.Message{
		{Role: provider.RoleTool, ToolCallID: "repeat", Name: "read", Content: "bounded-1", RawContent: "same promoted body"},
		{Role: provider.RoleTool, ToolCallID: "repeat", Name: "read", Content: "bounded-2", RawContent: "same promoted body"},
	}
	projection := []provider.Message{{
		Role: provider.RoleTool, ToolCallID: "repeat", Name: "read", Content: "same promoted body",
	}}
	if normalized, ok := normalizePromotedProjectionToolBodies(projection, canonical, len(canonical)); ok || normalized != nil {
		t.Fatalf("ambiguous duplicate was normalized: ok=%v messages=%+v", ok, normalized)
	}
}

func projectionJSON(t *testing.T, value any) string {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestLoadProjectionSidecarDropsUnverifiableBodyButKeepsReceipt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	msgs := []provider.Message{{Role: provider.RoleSystem, Content: "system"}, {Role: provider.RoleUser, Content: "task"}}
	if err := SaveCompactionState(path, CompactionState{
		SchemaVersion:  compactionStateSchemaV3,
		PromptCacheKey: promptCacheKey("ws", BranchID(path), "model"),
		Projection: ContextProjection{
			Messages:     []provider.Message{{Role: provider.RoleSystem, Content: "system"}, formatSummaryMessage("summary")},
			CoveredCount: len(msgs), CoveredPrefixHash: "unrelated-stale-hash",
		},
		LastReceipt: &ContextMaintenanceReceipt{Status: "applied", Action: "summary", CoveredPrefixHash: "unrelated-stale-hash"},
	}); err != nil {
		t.Fatal(err)
	}
	a := New(nil, nil, &Session{Messages: msgs}, Options{SessionPath: path, WorkspaceID: "ws", ModelRef: "model"}, event.Discard)
	if len(a.sess.compactionState.Projection.Messages) != 0 {
		t.Fatal("unverifiable projection body survived load")
	}
	if a.sess.compactionState.LastReceipt == nil || a.sess.compactionState.LastReceipt.Action != "summary" {
		t.Fatalf("maintenance receipt was lost: %+v", a.sess.compactionState.LastReceipt)
	}
	if got := a.Session().Snapshot(); !reflect.DeepEqual(got, msgs) {
		t.Fatalf("canonical transcript changed: got=%+v want=%+v", got, msgs)
	}
}
