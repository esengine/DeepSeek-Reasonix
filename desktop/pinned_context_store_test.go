package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/store"
)

func TestPinnedContextSidecarRoundTripAndCopy(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.jsonl")
	target := filepath.Join(dir, "target.jsonl")
	files := []string{"docs/b.md", "docs/a.md", "docs/b.md"}
	if err := savePinnedContextState(source, files); err != nil {
		t.Fatalf("save source: %v", err)
	}
	state, err := loadPinnedContextState(source)
	if err != nil {
		t.Fatalf("load source: %v", err)
	}
	want := []string{"docs/a.md", "docs/b.md"}
	if !reflect.DeepEqual(state.Files, want) {
		t.Fatalf("source files = %v, want stable sorted order %v", state.Files, want)
	}
	if state.SchemaVersion != pinnedContextSchemaVersion || state.SessionID != agent.BranchID(source) {
		t.Fatalf("source state = %+v", state)
	}
	if err := copyPinnedContextState(source, target); err != nil {
		t.Fatalf("copy sidecar: %v", err)
	}
	copied, err := loadPinnedContextState(target)
	if err != nil {
		t.Fatalf("load copied sidecar: %v", err)
	}
	if copied.SessionID != agent.BranchID(target) || !reflect.DeepEqual(copied.Files, want) {
		t.Fatalf("copied state = %+v", copied)
	}
}

func TestCopyPinnedContextStateCopiesEmptySourceSemantics(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.jsonl")
	target := filepath.Join(dir, "target.jsonl")
	if err := savePinnedContextState(target, []string{"stale.md"}); err != nil {
		t.Fatal(err)
	}
	if err := copyPinnedContextState(source, target); err != nil {
		t.Fatal(err)
	}
	state, err := loadPinnedContextState(target)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Files) != 0 {
		t.Fatalf("empty source left stale target pins: %v", state.Files)
	}
}

func TestPinnedContextSidecarRejectsCorruptionAndUnknownSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	sidecar := store.SessionPinnedContext(path)
	for _, test := range []struct {
		name string
		raw  string
	}{
		{name: "corrupt", raw: "{"},
		{name: "unknown schema", raw: `{"schemaVersion":2,"sessionId":"session","files":[]}`},
		{name: "wrong session", raw: `{"schemaVersion":1,"sessionId":"other","files":[]}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := os.WriteFile(sidecar, []byte(test.raw), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := loadPinnedContextState(path); err == nil {
				t.Fatal("invalid sidecar was accepted")
			}
		})
	}
}

func TestLegacyPinnedFilesMigrateOnlyWhenSidecarAbsent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := savePinnedContextState(path, []string{}); err != nil {
		t.Fatal(err)
	}
	state, err := loadOrMigratePinnedContextState(path, []string{"legacy.md"})
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Files) != 0 {
		t.Fatalf("legacy pins overwrote existing sidecar: %v", state.Files)
	}
	raw, err := os.ReadFile(store.SessionPinnedContext(path))
	if err != nil {
		t.Fatal(err)
	}
	var persisted pinnedContextState
	if err := json.Unmarshal(raw, &persisted); err != nil {
		t.Fatal(err)
	}
	if len(persisted.Files) != 0 {
		t.Fatalf("persisted files = %v, want empty", persisted.Files)
	}
}

func TestFailedLegacyMigrationRemainsRoundTrippable(t *testing.T) {
	root := t.TempDir()
	blockedParent := filepath.Join(root, "blocked")
	if err := os.WriteFile(blockedParent, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	sessionPath := filepath.Join(blockedParent, "session.jsonl")
	tab := &WorkspaceTab{ID: "tab", Scope: "project", SessionPath: sessionPath}
	restoreTabPinnedContext(tab, []string{"legacy.md"})
	if got := tab.GetPinnedFiles(); !reflect.DeepEqual(got, []string{"legacy.md"}) {
		t.Fatalf("visible legacy pins = %v", got)
	}
	app := &App{tabs: map[string]*WorkspaceTab{"tab": tab}, tabOrder: []string{"tab"}, activeTabID: "tab"}
	_, entries, _, _ := app.saveTabsCollectLocked()
	if len(entries) != 1 || !reflect.DeepEqual(entries[0].PinnedFiles, []string{"legacy.md"}) {
		t.Fatalf("persisted tab entries = %#v, want pending legacy pins", entries)
	}

	if err := os.Remove(blockedParent); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(blockedParent, 0o755); err != nil {
		t.Fatal(err)
	}
	migratePendingLegacyPinnedFiles(tab, sessionPath)
	if pending := tab.pendingLegacyPinnedFilesForPersistence(); len(pending) != 0 {
		t.Fatalf("pending legacy pins after migration = %v", pending)
	}
	state, err := loadPinnedContextState(sessionPath)
	if err != nil || !reflect.DeepEqual(state.Files, []string{"legacy.md"}) {
		t.Fatalf("migrated sidecar = %+v, err = %v", state, err)
	}
}

func TestPathlessLegacyPinsRemainRoundTrippable(t *testing.T) {
	tab := &WorkspaceTab{ID: "tab", Scope: "project"}
	restoreTabPinnedContext(tab, []string{"legacy.md"})
	if got := tab.pendingLegacyPinnedFilesForPersistence(); !reflect.DeepEqual(got, []string{"legacy.md"}) {
		t.Fatalf("pending pathless legacy pins = %v", got)
	}
	sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
	prepareStartupPinnedContext(tab, sessionPath, "")
	state, err := loadPinnedContextState(sessionPath)
	if err != nil || !reflect.DeepEqual(state.Files, []string{"legacy.md"}) {
		t.Fatalf("startup migration state = %+v, err = %v", state, err)
	}
	if pending := tab.pendingLegacyPinnedFilesForPersistence(); len(pending) != 0 {
		t.Fatalf("pending legacy pins after startup migration = %v", pending)
	}
}
