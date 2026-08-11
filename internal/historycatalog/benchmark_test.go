package historycatalog

import (
	"context"
	"fmt"
	"testing"
)

func BenchmarkWarmSearchTenThousandSources(b *testing.B) {
	ctx := context.Background()
	catalog, err := Open(ctx, Options{InMemory: true})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = catalog.Close(context.Background()) })
	tx, err := catalog.db.Begin()
	if err != nil {
		b.Fatal(err)
	}
	for i := range 10_000 {
		path := fmt.Sprintf("/sessions/session-%05d.jsonl", i)
		if _, err := tx.Exec(`INSERT INTO history_sources(path,root,source,scope,workspace_root,health) VALUES(?,?,?,?,?,'ok')`,
			path, "/sessions", "global", "global", ""); err != nil {
			b.Fatal(err)
		}
		result, err := tx.Exec(`INSERT INTO history_documents(source_path,message_index,part_index,role,kind,tool_name,token_count)
			VALUES(?,0,0,'user','user_text','',3)`, path)
		if err != nil {
			b.Fatal(err)
		}
		rowID, _ := result.LastInsertId()
		if _, err := tx.Exec(`INSERT INTO history_fts(rowid,terms) VALUES(?,?)`, rowID, fmt.Sprintf("shared marker item%d", i)); err != nil {
			b.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for range b.N {
		if _, err := catalog.Search(ctx, SearchRequest{Query: "shared marker", Roots: []string{"/sessions"}, Limit: 50}); err != nil {
			b.Fatal(err)
		}
	}
}
