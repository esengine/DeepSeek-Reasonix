package historycatalog

import (
	"context"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/provider"
)

// A rebuild must survive being opened: it indexed with this build's tokenizer,
// so the version check has nothing to reclaim. Before the stamp, every bump of
// TokenizerVersion silently wiped the projection a rebuild had just produced.
func TestRebuildSurvivesTheVersionCheck(t *testing.T) {
	ctx := context.Background()
	dir := testenv.TempDir(t)
	dbPath := filepath.Join(dir, "h.sqlite")
	root := testenv.TempDir(t)
	saveMessages(t, filepath.Join(root, "s.jsonl"), provider.Message{Role: provider.RoleUser, Content: "current phoenix marker"})

	if _, err := Rebuild(ctx, Options{Path: dbPath}, []Root{{Path: root, Source: "global", Scope: "global"}}); err != nil {
		t.Fatal(err)
	}
	c, err := Open(ctx, Options{Path: dbPath})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close(context.Background()) })
	var version int
	if err := c.db.QueryRowContext(ctx, `SELECT tokenizer_version FROM history_state WHERE id=1`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != TokenizerVersion {
		t.Fatalf("tokenizer_version = %d, want %d — the rebuild did not stamp what it built with", version, TokenizerVersion)
	}
	got, err := c.Search(ctx, SearchRequest{Query: "phoenix", Roots: []string{root}})
	if err != nil || len(got.Items) != 1 {
		t.Fatalf("search after rebuild = %#v err=%v, want the rebuilt row", got, err)
	}
}
