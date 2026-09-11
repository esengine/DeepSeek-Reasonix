package turnevent_test

import (
	"fmt"
	"path/filepath"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/turnevent"
)

type benchmarkLedgerSink struct {
	ledger    *turnevent.Ledger
	projected int
}

func (s *benchmarkLedgerSink) Emit(e event.Event) { _ = s.EmitChecked(e) }

func (s *benchmarkLedgerSink) EmitChecked(e event.Event) error {
	status := event.TurnInProgress
	if e.Kind == event.TurnDone {
		status = event.TurnCompleted
	}
	_, ok, err := s.ledger.Append(e, status)
	if err == nil && ok {
		s.projected++
	}
	return err
}

func BenchmarkCoalesceLedgerFrontend10000TextDeltas(b *testing.B) {
	root := b.TempDir()
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := range b.N {
		path := filepath.Join(root, fmt.Sprintf("text-%d.jsonl", iteration))
		ledger, err := turnevent.Open(path, "benchmark")
		if err != nil {
			b.Fatal(err)
		}
		if _, err := ledger.Begin(); err != nil {
			b.Fatal(err)
		}
		sink := &benchmarkLedgerSink{ledger: ledger}
		stream := event.Coalesce(sink, event.DefaultStreamDeltaWindow)
		for range 10_000 {
			stream.Emit(event.Event{Kind: event.Text, Text: "x"})
		}
		if err := event.EmitChecked(stream, event.Event{Kind: event.TurnDone}); err != nil {
			b.Fatal(err)
		}
		if sink.projected < 2 || sink.projected >= 10_000 {
			b.Fatalf("projected records = %d, want coalesced stream plus terminal", sink.projected)
		}
	}
}

func BenchmarkLedger10000ToolProgressPersistentHandle(b *testing.B) {
	root := b.TempDir()
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := range b.N {
		path := filepath.Join(root, fmt.Sprintf("progress-%d.jsonl", iteration))
		ledger, err := turnevent.Open(path, "benchmark")
		if err != nil {
			b.Fatal(err)
		}
		if _, err := ledger.Begin(); err != nil {
			b.Fatal(err)
		}
		for i := range 10_000 {
			if _, ok, err := ledger.Append(event.Event{Kind: event.ToolProgress, Tool: event.Tool{ID: "tool", Name: "bash", Output: "tick"}}, event.TurnInProgress); err != nil || !ok {
				b.Fatalf("progress %d: ok=%v err=%v", i, ok, err)
			}
		}
		if _, ok, err := ledger.Append(event.Event{Kind: event.TurnDone}, event.TurnCompleted); err != nil || !ok {
			b.Fatalf("terminal: ok=%v err=%v", ok, err)
		}
		metrics := ledger.MetricsSnapshot()
		if metrics.OpenCount != 1 || metrics.SyncCount != 1 || metrics.CloseCount != 1 {
			b.Fatalf("WAL lifecycle = %d/%d/%d, want 1/1/1", metrics.OpenCount, metrics.SyncCount, metrics.CloseCount)
		}
	}
}
