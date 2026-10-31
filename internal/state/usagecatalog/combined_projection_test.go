package usagecatalog

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestCombinedProjectionDoesNotReuseEitherVersionEightLayout(t *testing.T) {
	for _, column := range []string{"agent", "incomplete_reason"} {
		t.Run(column, func(t *testing.T) {
			cache := testenv.TempDir(t)
			t.Setenv("REASONIX_CACHE_HOME", cache)
			old := filepath.Join(cache, "usage-catalog", "v8.sqlite")
			if err := os.MkdirAll(filepath.Dir(old), 0o700); err != nil {
				t.Fatal(err)
			}
			db, err := sql.Open("sqlite", old)
			if err != nil {
				t.Fatal(err)
			}
			_, err = db.Exec("CREATE TABLE usage_records(" + column + " TEXT)")
			if closeErr := db.Close(); err != nil || closeErr != nil {
				t.Fatalf("old projection fixture: %v, %v", err, closeErr)
			}
			path := DefaultPath()
			if path == old {
				t.Fatal("combined layout reused an incompatible v8 projection")
			}
			ctx := context.Background()
			catalog, err := Open(ctx, path)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = catalog.Close(ctx) })
			dir := testenv.TempDir(t)
			day := "2026-09-01"
			file := filepath.Join(dir, day+".jsonl")
			line := []byte(`{"ts":"2026-09-01T01:00:00Z","source":"serve","agent":"a-1","model":"relay/m","total":42,"cost_complete":false,"incomplete_reason":"no_price"}` + "\n")
			if err := os.WriteFile(file, line, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := catalog.ReconcileFile(ctx, file, day); err != nil {
				t.Fatal(err)
			}
			agents, err := catalog.QueryAgents(ctx, day, day, "serve")
			if err != nil || len(agents) != 1 || agents[0].Agent != "a-1" || agents[0].Total != 42 {
				t.Fatalf("rebuilt agents: %+v, %v", agents, err)
			}
			calls, reasons, err := catalog.UnpricedInRange(ctx, day, day, "serve")
			if err != nil || calls != 1 || !reflect.DeepEqual(reasons, []string{"no_price"}) {
				t.Fatalf("rebuilt unpriced calls: %d, %v, %v", calls, reasons, err)
			}
			if after, err := os.ReadFile(file); err != nil || string(after) != string(line) {
				t.Fatalf("rebuild changed the authoritative history: %q, %v", after, err)
			}
		})
	}
}
