package history

import (
	"fmt"
	"sync"
	"testing"

	"reasonix/internal/remote/protocol"
)

func TestConcurrentCapturePagingSweepAndRelease(t *testing.T) {
	store := newTestStore(t, Options{MaxSnapshots: 64})
	bindings := make([]Binding, 12)
	for index := range bindings {
		bindings[index] = testBinding(fmt.Sprintf("concurrent-%02d", index))
		if err := store.CaptureSnapshot(simpleCapture(bindings[index], 50)); err != nil {
			t.Fatal(err)
		}
	}

	const readers = 24
	errorsFound := make(chan error, readers+2)
	var wait sync.WaitGroup
	for reader := 0; reader < readers; reader++ {
		reader := reader
		wait.Add(1)
		go func() {
			defer wait.Done()
			for iteration := 0; iteration < 100; iteration++ {
				binding := bindings[(reader+iteration)%len(bindings)]
				turns := 1 + (reader+iteration)%protocol.HistoryMaxTurns
				page, err := store.latest(binding, turns)
				if err != nil {
					errorsFound <- fmt.Errorf("latest %s: %w", binding.SnapshotID, err)
					return
				}
				if page.TotalTurns != 50 || page.ActualTurns < 1 || page.ActualTurns > 50 {
					errorsFound <- fmt.Errorf("invalid page range: %#v", page)
					return
				}
				// Mutating a returned deep copy races only with this goroutine and
				// must never affect another reader's retained snapshot.
				if len(page.Messages) > 0 && page.Messages[0].Content != nil {
					*page.Messages[0].Content = "reader mutation"
				}
				if page.HasOlder {
					older, olderErr := store.older(binding, page.NextCursor, 7)
					if olderErr != nil {
						errorsFound <- fmt.Errorf("older %s: %w", binding.SnapshotID, olderErr)
						return
					}
					if older.EndTurn != page.StartTurn {
						errorsFound <- fmt.Errorf("cursor boundary = %d, want %d", older.EndTurn, page.StartTurn)
						return
					}
				}
				_ = store.Stats()
				store.Sweep()
			}
		}()
	}

	wait.Add(1)
	go func() {
		defer wait.Done()
		for iteration := 0; iteration < 100; iteration++ {
			binding := testBinding(fmt.Sprintf("transient-%03d", iteration))
			if err := store.CaptureSnapshot(simpleCapture(binding, 5)); err != nil {
				errorsFound <- fmt.Errorf("capture transient: %w", err)
				return
			}
			if _, err := store.latest(binding, 2); err != nil {
				errorsFound <- fmt.Errorf("page transient: %w", err)
				return
			}
			if !store.Release(binding) {
				errorsFound <- fmt.Errorf("release transient %s", binding.SnapshotID)
				return
			}
		}
	}()

	wait.Wait()
	close(errorsFound)
	for err := range errorsFound {
		t.Error(err)
	}
	if t.Failed() {
		return
	}
	for _, binding := range bindings {
		page, err := store.latest(binding, 1)
		if err != nil {
			t.Fatal(err)
		}
		if got := content(page.Messages[0]); got != "user-049" {
			t.Fatalf("retained snapshot %s was mutated: %q", binding.SnapshotID, got)
		}
	}
}
