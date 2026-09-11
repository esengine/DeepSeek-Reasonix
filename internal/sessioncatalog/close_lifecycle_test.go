package sessioncatalog

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCloseCanBeAwaitedAfterCanceledCaller(t *testing.T) {
	catalog, err := Open(context.Background(), Options{InMemory: true, DisableRepair: true})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := catalog.Close(canceled); err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("first close: %v", err)
	}
	ctx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	if err := catalog.Close(ctx); err != nil {
		t.Fatalf("await eventual close: %v", err)
	}
	if err := catalog.db.Ping(); err == nil {
		t.Fatal("database remained open after close completion")
	}
}
