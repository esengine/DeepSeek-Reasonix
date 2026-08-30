package historycatalog

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/provider"
)

func TestResolveMaxBytes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		option       int64
		configuredMB int
		want         int64
	}{
		{"explicit option wins", 12345, 512, 12345},
		{"config max_mb", 0, 512, 512 << 20},
		{"built-in default", 0, 0, DefaultMaxBytes},
		{"negative config ignored", 0, -10, DefaultMaxBytes},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := resolveMaxBytes(tt.option, tt.configuredMB); got != tt.want {
				t.Fatalf("resolveMaxBytes(%d,%d)=%d, want %d", tt.option, tt.configuredMB, got, tt.want)
			}
		})
	}
	if DefaultMaxBytes != 256<<20 {
		t.Fatalf("DefaultMaxBytes=%d, want 256MiB", DefaultMaxBytes)
	}
}

// saveToolSession writes a session with exchanges tool calls, each carrying a
// 32KB output, so one session's index footprint is meaningful next to the
// database's fixed page overhead.
func saveToolSession(t *testing.T, path, marker string, exchanges int) {
	t.Helper()
	messages := []provider.Message{{Role: provider.RoleUser, Content: "question about " + marker}}
	for i := range exchanges {
		id := fmt.Sprintf("call-%d", i)
		content := id + " " + strings.Repeat("payload ", 4096)
		if i == 0 {
			// Keep the marker in exactly one document so search-hit counts are
			// per-session, not per-exchange.
			content = id + " " + marker + " " + strings.Repeat("payload ", 4096)
		}
		messages = append(messages,
			provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: id, Name: "bash", Arguments: `{"cmd":"echo ` + id + `"}`}}},
			provider.Message{Role: provider.RoleTool, ToolCallID: id, Name: "bash", Content: content})
	}
	saveMessages(t, path, messages...)
}

// setSessionModTime back-dates the session file and its sidecars so
// last_activity_at ordering is deterministic.
func setSessionModTime(t *testing.T, path string, mod time.Time) {
	t.Helper()
	stem := strings.TrimSuffix(path, filepath.Ext(path))
	matches, err := filepath.Glob(stem + "*")
	if err != nil {
		t.Fatal(err)
	}
	for _, match := range matches {
		if err := os.Chtimes(match, mod, mod); err != nil {
			t.Fatal(err)
		}
	}
}

func toolOutputHits(t *testing.T, catalog *Catalog, root, marker string) int {
	t.Helper()
	result, err := catalog.Search(context.Background(), SearchRequest{Query: marker, Kinds: []string{"tool_output"}, Roots: []string{root}})
	if err != nil {
		t.Fatal(err)
	}
	return len(result.Items)
}

func sourceHealth(t *testing.T, catalog *Catalog, path string) string {
	t.Helper()
	var health string
	if err := catalog.db.QueryRow(`SELECT health FROM history_sources WHERE path=?`, path).Scan(&health); err != nil {
		t.Fatal(err)
	}
	return health
}

// checkpoint folds the WAL so file-size-based caps are measured post-fold.
func checkpoint(t *testing.T, catalog *Catalog) {
	t.Helper()
	if _, err := catalog.db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		t.Fatal(err)
	}
}

// appendToSession grows an on-disk session so its content fingerprint changes.
func appendToSession(t *testing.T, path, marker string) {
	t.Helper()
	session, err := agent.LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	session.Add(provider.Message{Role: provider.RoleUser, Content: "more about " + marker})
	if err := session.SaveSnapshot(path); err != nil {
		t.Fatal(err)
	}
}

func TestGovernSizeUnderCapIsNoOp(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	root := t.TempDir()
	path := filepath.Join(root, "alpha.jsonl")
	saveToolSession(t, path, "alphamarker", 4)
	catalog, err := Open(ctx, Options{Path: filepath.Join(t.TempDir(), "history.sqlite"), MaxBytes: 1 << 30})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	if err := catalog.ReconcileRoot(ctx, Root{Path: root, Scope: "global"}); err != nil {
		t.Fatal(err)
	}
	catalog.governSize(ctx)
	if hits := toolOutputHits(t, catalog, root, "alphamarker"); hits != 1 {
		t.Fatalf("under-cap governSize dropped rows: hits=%d", hits)
	}
	if health := sourceHealth(t, catalog, path); health != "ok" {
		t.Fatalf("health=%q, want ok", health)
	}
}

func TestEvictOldestBatchEvictsLeastRecentlyActiveFirst(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	root := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "history.sqlite")
	now := time.Now()
	paths := map[string]string{}
	for i, marker := range []string{"alpha", "beta", "gamma"} {
		path := filepath.Join(root, marker+".jsonl")
		saveToolSession(t, path, marker+"marker", 4)
		setSessionModTime(t, path, now.Add(time.Duration(i-3)*time.Hour))
		paths[marker] = path
	}
	catalog, err := Open(ctx, Options{Path: dbPath, MaxBytes: 1 << 30})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	if err := catalog.ReconcileRoot(ctx, Root{Path: root, Scope: "global"}); err != nil {
		t.Fatal(err)
	}
	evicted, err := catalog.evictOldestBatch(ctx, historyDBFileSize(dbPath), 1)
	if err != nil {
		t.Fatal(err)
	}
	if evicted != 1 {
		t.Fatalf("evicted=%d, want exactly 1 for a 1-byte overage", evicted)
	}
	if hits := toolOutputHits(t, catalog, root, "alphamarker"); hits != 0 {
		t.Fatalf("oldest session still searchable: hits=%d", hits)
	}
	for _, marker := range []string{"beta", "gamma"} {
		if hits := toolOutputHits(t, catalog, root, marker+"marker"); hits != 1 {
			t.Fatalf("%s wrongly evicted: hits=%d", marker, hits)
		}
	}
	if health := sourceHealth(t, catalog, paths["alpha"]); health != "evicted" {
		t.Fatalf("evicted source health=%q, want evicted", health)
	}
	if _, err := os.Stat(paths["alpha"]); err != nil {
		t.Fatalf("eviction touched the source session file: %v", err)
	}
}

func TestGovernSizeEvictsDownToTarget(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	root := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "history.sqlite")
	now := time.Now()
	markers := []string{"alpha", "beta", "gamma", "delta"}
	for i, marker := range markers {
		path := filepath.Join(root, marker+".jsonl")
		saveToolSession(t, path, marker+"marker", 24)
		setSessionModTime(t, path, now.Add(time.Duration(i-len(markers))*time.Hour))
	}
	catalog, err := Open(ctx, Options{Path: dbPath, MaxBytes: 1 << 30})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	if err := catalog.ReconcileRoot(ctx, Root{Path: root, Scope: "global"}); err != nil {
		t.Fatal(err)
	}
	checkpoint(t, catalog)
	size := historyDBFileSize(dbPath)
	catalog.opts.MaxBytes = size / 2 // over cap, well under the 2x rebuild threshold
	catalog.governSize(ctx)

	target := (size / 2) * evictTargetPercent / 100
	final := historyDBFileSize(dbPath)
	evictedCount := 0
	for i, marker := range markers {
		path := filepath.Join(root, marker+".jsonl")
		if sourceHealth(t, catalog, path) == "evicted" {
			// Eviction must be a prefix of the last-activity-ascending order.
			for j := range i {
				prev := filepath.Join(root, markers[j]+".jsonl")
				if sourceHealth(t, catalog, prev) != "evicted" {
					t.Fatalf("%s evicted while older %s survived", marker, markers[j])
				}
			}
			evictedCount++
			if hits := toolOutputHits(t, catalog, root, marker+"marker"); hits != 0 {
				t.Fatalf("evicted %s still searchable", marker)
			}
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("eviction touched source file %s: %v", marker, err)
			}
		} else if hits := toolOutputHits(t, catalog, root, marker+"marker"); hits != 1 {
			t.Fatalf("retained %s not searchable: hits=%d", marker, hits)
		}
	}
	if evictedCount == 0 {
		t.Fatal("over-cap governSize evicted nothing")
	}
	if final > target && evictedCount < len(markers) {
		t.Fatalf("size=%d still above target=%d with sessions left", final, target)
	}
	if final >= size {
		t.Fatalf("eviction reclaimed nothing: before=%d after=%d", size, final)
	}
}

func TestGovernSizeFarOverCapWipesProjection(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	root := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "history.sqlite")
	alphaPath := filepath.Join(root, "alpha.jsonl")
	saveToolSession(t, alphaPath, "alphamarker", 4)
	catalog, err := Open(ctx, Options{Path: dbPath, MaxBytes: 1 << 30})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	if err := catalog.ReconcileRoot(ctx, Root{Path: root, Scope: "global"}); err != nil {
		t.Fatal(err)
	}
	checkpoint(t, catalog)
	size := historyDBFileSize(dbPath)
	catalog.opts.MaxBytes = size / 4 // beyond the 2x rebuild threshold
	catalog.governSize(ctx)
	for _, table := range []string{"history_fts", "history_documents", "history_sources", "history_roots"} {
		var count int
		if err := catalog.db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("%s still has %d rows after oversize wipe", table, count)
		}
	}
	if _, err := os.Stat(alphaPath); err != nil {
		t.Fatalf("wipe touched the source session file: %v", err)
	}
}

func TestOpenTriggersBackgroundWipeWhenFarOverCap(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	root := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "history.sqlite")
	saveToolSession(t, filepath.Join(root, "alpha.jsonl"), "alphamarker", 4)
	catalog, err := Open(ctx, Options{Path: dbPath, MaxBytes: 1 << 30})
	if err != nil {
		t.Fatal(err)
	}
	if err := catalog.ReconcileRoot(ctx, Root{Path: root, Scope: "global"}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.Close(ctx); err != nil {
		t.Fatal(err)
	}
	size := historyDBFileSize(dbPath)
	rebuildDone := make(chan struct{}, 1)
	reopened, err := Open(ctx, Options{Path: dbPath, MaxBytes: size / 4, OnRevision: func(_ Status, _ []string, reason string) {
		if reason == "rebuild-oversize" {
			select {
			case rebuildDone <- struct{}{}:
			default:
			}
		}
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close(context.Background()) })
	select {
	case <-rebuildDone:
	case <-time.After(20 * time.Second):
		t.Fatal("background wipe did not publish rebuild completion within 20s")
	}
	// The wipe must be followed by a rescan that rebuilds the index (now with
	// truncated tool payloads) without blocking startup.
	if !reopened.RegisterRoot(Root{Path: root, Scope: "global"}) {
		t.Fatal("register rebuilt root was not queued")
	}
	flushCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if err := reopened.Flush(flushCtx); err != nil {
		t.Fatalf("flush rebuilt root: %v", err)
	}
	result, err := reopened.Search(ctx, SearchRequest{Query: "alphamarker", Kinds: []string{"tool_output"}, Roots: []string{root}})
	if err != nil || len(result.Items) != 1 {
		t.Fatalf("rebuild rescan result=%#v err=%v", result, err)
	}
}

func TestEvictedSessionStaysEvictedWhenUnchangedAndReindexesOnChange(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	root := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "history.sqlite")
	now := time.Now()
	alphaPath := filepath.Join(root, "alpha.jsonl")
	betaPath := filepath.Join(root, "beta.jsonl")
	saveToolSession(t, alphaPath, "alphamarker", 4)
	saveToolSession(t, betaPath, "betamarker", 4)
	setSessionModTime(t, alphaPath, now.Add(-2*time.Hour))
	setSessionModTime(t, betaPath, now.Add(-time.Hour))
	catalog, err := Open(ctx, Options{Path: dbPath, MaxBytes: 1 << 30})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	registered := Root{Path: root, Scope: "global"}
	if err := catalog.ReconcileRoot(ctx, registered); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.evictOldestBatch(ctx, historyDBFileSize(dbPath), 1); err != nil {
		t.Fatal(err)
	}
	if health := sourceHealth(t, catalog, alphaPath); health != "evicted" {
		t.Fatalf("alpha health=%q, want evicted", health)
	}

	// Touch beta so the root signature changes and both paths are revisited;
	// alpha is unchanged and must stay out of the index.
	appendToSession(t, betaPath, "betamarker")
	if err := catalog.ReconcileRoot(ctx, registered); err != nil {
		t.Fatal(err)
	}
	if health := sourceHealth(t, catalog, alphaPath); health != "evicted" {
		t.Fatalf("unchanged alpha resurrected: health=%q", health)
	}
	if hits := toolOutputHits(t, catalog, root, "alphamarker"); hits != 0 {
		t.Fatalf("unchanged alpha re-indexed: hits=%d", hits)
	}

	// A content change fully re-indexes an evicted session.
	appendToSession(t, alphaPath, "alphamarker")
	if err := catalog.indexPath(ctx, registered, alphaPath, 0, -1); err != nil {
		t.Fatal(err)
	}
	if health := sourceHealth(t, catalog, alphaPath); health != "ok" {
		t.Fatalf("changed alpha health=%q, want ok", health)
	}
	if hits := toolOutputHits(t, catalog, root, "alphamarker"); hits != 1 {
		t.Fatalf("changed alpha not re-indexed: hits=%d", hits)
	}
}
