package main

import (
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/event"
)

// TestStreamDeltaMergerCoalescesSameKind verifies consecutive same-kind deltas
// collapse into one wire event per flush window.
func TestStreamDeltaMergerCoalescesSameKind(t *testing.T) {
	var mu sync.Mutex
	var sent []event.Event
	m := &streamDeltaMerger{
		send: func(p streamDeltaPart) {
			kind, delta := p.kind, p.delta
			mu.Lock()
			sent = append(sent, event.Event{Kind: kind, Text: delta})
			mu.Unlock()
		},
	}
	for i := 0; i < 5; i++ {
		m.push(event.Text, "hello ", "", "")
	}
	m.flush()
	mu.Lock()
	defer mu.Unlock()
	if len(sent) != 1 {
		t.Fatalf("expected 1 merged event, got %d", len(sent))
	}
	if sent[0].Text != "hello hello hello hello hello " {
		t.Fatalf("unexpected merged text %q", sent[0].Text)
	}
}

// TestStreamDeltaMergerPreservesKindOrder verifies that interleaved
// reasoning/text deltas keep their causal order and never merge across kinds.
func TestStreamDeltaMergerPreservesKindOrder(t *testing.T) {
	var mu sync.Mutex
	var sent []event.Event
	m := &streamDeltaMerger{
		send: func(p streamDeltaPart) {
			kind, delta := p.kind, p.delta
			mu.Lock()
			sent = append(sent, event.Event{Kind: kind, Text: delta})
			mu.Unlock()
		},
	}
	m.push(event.Reasoning, "r1", "", "")
	m.push(event.Reasoning, "r2", "", "")
	m.push(event.Text, "t1", "", "")
	m.push(event.Reasoning, "r3", "", "")
	m.flush()
	mu.Lock()
	defer mu.Unlock()
	if len(sent) != 3 {
		t.Fatalf("expected 3 events (reasoning, text, reasoning), got %d: %+v", len(sent), sent)
	}
	if sent[0].Kind != event.Reasoning || sent[0].Text != "r1r2" {
		t.Fatalf("unexpected first event %+v", sent[0])
	}
	if sent[1].Kind != event.Text || sent[1].Text != "t1" {
		t.Fatalf("unexpected second event %+v", sent[1])
	}
	if sent[2].Kind != event.Reasoning || sent[2].Text != "r3" {
		t.Fatalf("unexpected third event %+v", sent[2])
	}
}

// TestStreamDeltaMergerKeepsEmptyDelta verifies that an empty text delta is
// still delivered: the frontend relies on it to complete live reasoning.
func TestStreamDeltaMergerKeepsEmptyDelta(t *testing.T) {
	var mu sync.Mutex
	var sent []event.Event
	m := &streamDeltaMerger{
		send: func(p streamDeltaPart) {
			kind, delta := p.kind, p.delta
			mu.Lock()
			sent = append(sent, event.Event{Kind: kind, Text: delta})
			mu.Unlock()
		},
	}
	m.push(event.Text, "", "", "")
	m.flush()
	mu.Lock()
	defer mu.Unlock()
	if len(sent) != 1 || sent[0].Text != "" {
		t.Fatalf("expected one empty text event, got %+v", sent)
	}
}

// TestStreamDeltaMergerTimerFlush verifies the window timer flushes without an
// explicit flush call.
func TestStreamDeltaMergerTimerFlush(t *testing.T) {
	var mu sync.Mutex
	var sent []event.Event
	done := make(chan struct{})
	m := &streamDeltaMerger{
		send: func(p streamDeltaPart) {
			kind, delta := p.kind, p.delta
			mu.Lock()
			sent = append(sent, event.Event{Kind: kind, Text: delta})
			mu.Unlock()
			select {
			case <-done:
			default:
				close(done)
			}
		},
	}
	m.push(event.Text, "streamed", "", "")
	select {
	case <-done:
	case <-time.After(2 * streamMergeWindow + 50*time.Millisecond):
		t.Fatal("timer never flushed pending delta")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(sent) != 1 || sent[0].Text != "streamed" {
		t.Fatalf("unexpected events %+v", sent)
	}
}

// TestStreamDeltaMergerIdleIsNoop verifies flush on an idle merger emits
// nothing and repeated flushes are safe.
func TestStreamDeltaMergerIdleIsNoop(t *testing.T) {
	m := &streamDeltaMerger{send: func(streamDeltaPart) { t.Fatal("unexpected send") }}
	m.flush()
	m.flush()
}

// TestStreamDeltaMergerConcurrentPushFlush verifies push/flush from concurrent
// goroutines never loses or reorders merged deltas.
func TestStreamDeltaMergerConcurrentPushFlush(t *testing.T) {
	var mu sync.Mutex
	var sent strings.Builder
	m := &streamDeltaMerger{
		send: func(p streamDeltaPart) {
			delta := p.delta
			mu.Lock()
			sent.WriteString(delta)
			mu.Unlock()
		},
	}
	var wg sync.WaitGroup
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				m.push(event.Text, "x", "", "")
				if i%10 == 0 {
					m.flush()
				}
			}
		}(g)
	}
	wg.Wait()
	m.flush()
	mu.Lock()
	defer mu.Unlock()
	if sent.Len() != 400 {
		t.Fatalf("lost deltas: expected 400 bytes, got %d", sent.Len())
	}
}
