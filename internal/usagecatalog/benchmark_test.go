package usagecatalog

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func BenchmarkWarmQueryTenYears(b *testing.B) {
	catalog, err := Open(context.Background(), filepath.Join(b.TempDir(), "usage.sqlite"))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = catalog.Close(context.Background()) })
	tx, err := catalog.db.Begin()
	if err != nil {
		b.Fatal(err)
	}
	start := time.Date(2016, 1, 1, 0, 0, 0, 0, time.UTC)
	for day := range 3_650 {
		date := start.AddDate(0, 0, day).Format("2006-01-02")
		for model := range 8 {
			ref := fmt.Sprintf("provider/model-%d", model)
			if _, err := tx.Exec(`INSERT INTO usage_rollups(day,source,model_ref,provider,prompt,completion,reasoning,cache_hit,cache_miss,total,requests,turns)
				VALUES(?,?,?,?,1,1,0,0,1,2,1,1)`, date, "desktop", ref, "provider"); err != nil {
				b.Fatal(err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for range b.N {
		if _, err := catalog.Query(context.Background(), "2016-01-01", "2025-12-28", "all"); err != nil {
			b.Fatal(err)
		}
	}
}
