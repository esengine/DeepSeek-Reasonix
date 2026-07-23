package main

import (
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

func TestDisplayTurnBufferPreservesStreamingReplacementAndTools(t *testing.T) {
	var buffer displayTurnBuffer
	recordHistoryDisplayEvent(&buffer, event.Event{Kind: event.Reasoning, Text: "draft reason "})
	recordHistoryDisplayEvent(&buffer, event.Event{Kind: event.Reasoning, Text: "continued"})
	recordHistoryDisplayEvent(&buffer, event.Event{Kind: event.Text, Text: "draft answer"})
	recordHistoryDisplayEvent(&buffer, event.Event{
		Kind:      event.Message,
		Text:      "final answer",
		Reasoning: "final reason",
		MemoryCitations: []provider.MemoryCitation{{
			ID: "memory-1", Source: "project",
		}},
	})
	recordHistoryDisplayEvent(&buffer, event.Event{Kind: event.ToolDispatch, Tool: event.Tool{
		ID: "call-1", Name: "read_file", Args: `{"path":"settings.json"}`,
	}})
	recordHistoryDisplayEvent(&buffer, event.Event{Kind: event.ToolResult, Tool: event.Tool{
		ID: "call-1", Output: "settings contents",
	}})

	got := buffer.materialize()
	if len(got) != 3 {
		t.Fatalf("messages = %d, want 3: %+v", len(got), got)
	}
	if got[0].Role != "assistant" || got[0].Content != "final answer" || got[0].Reasoning != "final reason" {
		t.Fatalf("stream replacement changed: %+v", got[0])
	}
	if len(got[0].MemoryCitations) != 1 || got[0].MemoryCitations[0].ID != "memory-1" {
		t.Fatalf("memory citations changed: %+v", got[0].MemoryCitations)
	}
	if got[1].Role != "assistant" || len(got[1].ToolCalls) != 1 || got[1].ToolCalls[0].ID != "call-1" || got[1].ToolCalls[0].Summary == "" {
		t.Fatalf("tool call changed: %+v", got[1])
	}
	if got[2].Role != "tool" || got[2].ToolCallID != "call-1" || got[2].ToolName != "read_file" {
		t.Fatalf("tool result changed: %+v", got[2])
	}
}

func TestDisplayTurnBufferStreamingAllocationsStayNearLinear(t *testing.T) {
	const (
		chunks    = 2_000
		chunkSize = 32
	)
	chunk := strings.Repeat("x", chunkSize)
	result := testing.Benchmark(func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			var buffer displayTurnBuffer
			for part := 0; part < chunks; part++ {
				recordHistoryDisplayEvent(&buffer, event.Event{Kind: event.Text, Text: chunk})
			}
			messages := buffer.materialize()
			if len(messages) != 1 || len(messages[0].Content) != chunks*chunkSize {
				b.Fatalf("materialized display length changed")
			}
		}
	})

	// Repeated string concatenation allocated roughly one full growing prefix
	// per chunk (>69 MiB for this 64 KiB stream). Keep a generous ceiling for
	// platform/runtime variance while pinning the intended near-linear shape.
	if got, max := result.AllocedBytesPerOp(), int64(chunks*chunkSize*16); got > max {
		t.Fatalf("stream allocated %d bytes/op, want <= %d (%s)", got, max, result.String())
	}
	if got := result.AllocsPerOp(); got > 100 {
		t.Fatalf("stream allocated %d objects/op, want <= 100 (%s)", got, result.String())
	}
	t.Logf("64 KiB stream: %d bytes/op, %d allocs/op", result.AllocedBytesPerOp(), result.AllocsPerOp())
}

func TestPendingDisplayWriteRetriesWithoutDroppingTurn(t *testing.T) {
	state := &tabDisplayState{}
	var attempts atomic.Int32
	persisted := make(chan struct{})
	write := &pendingDisplayWrite{
		dir:         "sessions",
		sessionPath: "sessions/session.jsonl",
		userContent: "prompt",
		messages:    []HistoryMessage{{Role: "assistant", Content: "partial answer"}},
		persist: func(_, _, _ string, messages []HistoryMessage) error {
			attempt := attempts.Add(1)
			if len(messages) != 1 || messages[0].Content != "partial answer" {
				return errors.New("queued turn changed")
			}
			if attempt < 3 {
				return errors.New("temporary lock contention")
			}
			close(persisted)
			return nil
		},
	}
	persistOrEnqueueDisplayWrite(state, write)
	select {
	case <-persisted:
	case <-time.After(3 * time.Second):
		t.Fatal("pending display write was not retried")
	}
	deadline := time.Now().Add(time.Second)
	for {
		state.mu.Lock()
		pending := len(state.pendingWrites)
		running := state.persistRunning
		state.mu.Unlock()
		if pending == 0 && !running {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("retry worker did not drain: pending=%d running=%v", pending, running)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := attempts.Load(); got != 3 {
		t.Fatalf("persist attempts = %d, want 3", got)
	}
}
