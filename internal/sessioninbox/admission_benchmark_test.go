package sessioninbox

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestAdmittedItemsRejectUserCRUD(t *testing.T) {
	dir := t.TempDir()
	session := filepath.Join(dir, "s.jsonl")
	_ = os.WriteFile(session, []byte("{}\n"), 0o644)
	s, err := Open(session, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	rec, _ := s.Enqueue(EnqueueRequest{Envelope: PromptEnvelope{SubmitText: "running"}})
	if err := s.SetState(rec.ItemID, StateRunning, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateItem(rec.ItemID, PromptEnvelope{SubmitText: "changed"}); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("update running item error = %v, want ErrInvalidState", err)
	}
	if err := s.MoveItem(rec.ItemID, 0); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("move running item error = %v, want ErrInvalidState", err)
	}
	if err := s.DeleteItem(rec.ItemID); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("delete running item error = %v, want ErrInvalidState", err)
	}
	if err := s.RetryItem(rec.ItemID); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("retry running item error = %v, want ErrInvalidState", err)
	}
	if err := s.AckDequeue(rec.ItemID); err != nil {
		t.Fatalf("durable completion could not ack running item: %v", err)
	}
}

func BenchmarkThirtyOneMiBItemsRetainedHeap(b *testing.B) {
	body := strings.Repeat("x", 1<<20)
	var retainedTotal uint64
	for range b.N {
		b.StopTimer()
		session := filepath.Join(b.TempDir(), "s.jsonl")
		s, err := Open(session, Limits{})
		if err != nil {
			b.Fatal(err)
		}
		runtime.GC()
		var before runtime.MemStats
		runtime.ReadMemStats(&before)

		b.StartTimer()
		for range 30 {
			if _, err := s.Enqueue(EnqueueRequest{
				Intent: IntentFollowup,
				Envelope: PromptEnvelope{
					DisplayText: "one MiB body",
					SubmitText:  body,
				},
			}); err != nil {
				b.Fatal(err)
			}
		}
		b.StopTimer()

		runtime.GC()
		var after runtime.MemStats
		runtime.ReadMemStats(&after)
		if after.HeapAlloc > before.HeapAlloc {
			retainedTotal += after.HeapAlloc - before.HeapAlloc
		}
		s.Close()
	}
	b.ReportMetric(float64(retainedTotal)/float64(b.N), "B/retained-op")
}
