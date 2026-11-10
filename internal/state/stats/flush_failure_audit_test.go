package stats

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
)

// TestAuditFlushReportsWriteFailure pins the contract Flush advertises: a caller
// that gates a deterministic read on Flush is entitled to a non-nil error when
// the write did not happen. The injected failure is a plain filesystem one; the
// stats directory path exists as a regular file, so every Append fails and
// recordDispatcher.run discards the error that should reach Flush.
func TestAuditFlushReportsWriteFailure(t *testing.T) {
	parent := testenv.TempDir(t)
	notADir := filepath.Join(parent, "stats-is-a-file")
	if err := os.WriteFile(notADir, []byte("occupied"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Sanity: Append itself must report the failure, so the red result below
	// isolates the dispatcher/Flush boundary and not the writer.
	w := NewWriter(notADir)
	if err := w.Append(record{Timestamp: time.Now(), Source: "desktop", Turn: true}); err == nil {
		t.Fatalf("Append into a path that is a regular file returned nil error")
	}

	r := NewRecorder(&spySink{}, notADir, "desktop")
	r.Emit(usageEvent("deepseek/deepseek-v4-pro", 100, 50, 0, 0, 0, 150))
	r.Emit(turnEvent())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := r.Flush(ctx); err == nil {
		entries, err := os.ReadDir(notADir)
		if err == nil {
			t.Fatalf("records were actually written despite the failure injection: %v", entries)
		}
		t.Fatalf("Flush returned nil after every append failed: the caller believes records accepted by the queue are on disk, but %s is still a regular file and no .jsonl exists", notADir)
	}
}
