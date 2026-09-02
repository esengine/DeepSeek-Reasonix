package sessioncatalog

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"reasonix/internal/agent"
)

// BenchmarkRepairWave1056 measures the persistent due query, 64-row result
// batches, aggregate publication, and per-directory reconcile coalescing. The
// filesystem decode is replaced by a deterministic result so this benchmark
// isolates catalog scheduling cost.
func BenchmarkRepairWave1056(b *testing.B) {
	ctx := context.Background()
	dir := b.TempDir()
	catalog, err := Open(ctx, Options{InMemory: true, DisableRepair: true})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = catalog.Close(context.Background()) })
	records := make([]SessionRecord, 1056)
	for i := range records {
		records[i] = SessionRecord{
			Path: filepath.Join(dir, fmt.Sprintf("%04d.jsonl", i)), Directory: dir, Scope: "global",
			TurnsState: TurnsUnknown, Health: HealthOK, LastActivityAt: int64(1056 - i),
		}
	}
	if _, err := catalog.upsertSessionsWithNotification(ctx, records, nil, "seed", false, upsertDirectoryProjection); err != nil {
		b.Fatal(err)
	}
	catalog.testRepairSessionHook = func(context.Context, string) (agent.SessionListingRepairResult, error) {
		return agent.SessionListingRepairResult{Status: agent.SessionListingRepairApplied, Preview: "ok", Turns: 1}, nil
	}
	lock := catalog.directoryLock(dir)
	lock.Lock()
	defer lock.Unlock()

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		b.StopTimer()
		if _, err := catalog.db.ExecContext(ctx, `UPDATE catalog_sessions SET turns_state='unknown',repair_state='pending',
			repair_attempts=0,repair_retry_at=0,repair_error_kind=''`); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
		catalog.runRepairWave(ctx)
	}
}
