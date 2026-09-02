package sessioncatalog

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRepairDrainEventuallyCompletesBeyondQueue(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(t.TempDir(), "catalog.sqlite")
	seed, err := Open(ctx, Options{Path: path, DisableRepair: true})
	if err != nil {
		t.Fatal(err)
	}
	const total = 8
	for i := range total {
		session := filepath.Join(dir, fmt.Sprintf("%02d.jsonl", i))
		if err := os.WriteFile(session, []byte(`{"role":"user","content":"turn"}`+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := seed.UpsertSession(ctx, SessionRecord{
			Path: session, Directory: dir, Scope: "global", TopicID: fmt.Sprintf("t%d", i),
			TurnsState: TurnsUnknown, Health: HealthOK, LastActivityAt: int64(i + 1),
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := seed.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	catalog, err := Open(ctx, Options{Path: path, QueueCapacity: 2, Now: time.Now})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	deadline := time.Now().Add(15 * time.Second)
	for {
		status := catalog.Status()
		if status.RepairPending == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("repair pending stuck at %d", status.RepairPending)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
