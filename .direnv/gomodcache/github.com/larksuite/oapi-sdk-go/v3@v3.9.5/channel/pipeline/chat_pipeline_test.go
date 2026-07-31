package pipeline

import (
	"context"
	"github.com/larksuite/oapi-sdk-go/v3/channel/types"
	"sync"
	"testing"
	"time"
)

func TestChatPipeline(t *testing.T) {
	cfg := types.BatchConfig{
		DelayMs:            50 * time.Millisecond,
		LongThresholdChars: 1000,
		LongDelayMs:        100 * time.Millisecond,
		MaxMessages:        3,
		MaxChars:           4000,
	}

	cp := NewChatPipeline(cfg, false)
	defer cp.Dispose()

	var mu sync.Mutex
	var dispatches []*types.BatchedDispatch

	handler := func(ctx context.Context, dispatch *types.BatchedDispatch) error {
		mu.Lock()
		dispatches = append(dispatches, dispatch)
		mu.Unlock()
		return nil
	}

	ctx := context.Background()

	msg1 := &types.NormalizedMessage{MessageID: "1", Content: "Hello"}
	msg2 := &types.NormalizedMessage{MessageID: "2", Content: "World"}

	cp.Push(ctx, msg1, handler)
	cp.Push(ctx, msg2, handler)

	// Should not be flushed yet
	mu.Lock()
	if len(dispatches) != 0 {
		t.Error("Expected no dispatches yet")
	}
	mu.Unlock()

	// Wait for delay
	time.Sleep(80 * time.Millisecond)

	mu.Lock()
	if len(dispatches) != 1 {
		t.Fatalf("Expected 1 dispatch, got %d", len(dispatches))
	}
	d := dispatches[0]
	if d.Message.Content != "Hello\n\nWorld" {
		t.Errorf("Unexpected merged content: %s", d.Message.Content)
	}
	if len(d.SourceIDs) != 2 {
		t.Errorf("Expected 2 source IDs, got %d", len(d.SourceIDs))
	}
	mu.Unlock()
}

func TestChatPipelineManager(t *testing.T) {
	cfg := types.BatchConfig{
		DelayMs:     50 * time.Millisecond,
		MaxMessages: 2,
	}

	cpm := NewChatPipelineManager(cfg)
	defer cpm.Dispose()

	var mu sync.Mutex
	var runCount int

	ctx := context.Background()
	msg := &types.NormalizedMessage{MessageID: "1", Content: "Msg"}

	handler := func(ctx context.Context, dispatch *types.BatchedDispatch) error {
		return nil
	}

	cpm.Push(ctx, "scope1", msg, handler)
	cpm.Push(ctx, "scope2", msg, handler)

	err := cpm.Run(ctx, "scope1", func() error {
		mu.Lock()
		runCount++
		mu.Unlock()
		return nil
	})

	if err != nil {
		t.Error(err)
	}

	mu.Lock()
	if runCount != 1 {
		t.Errorf("Expected runCount 1, got %d", runCount)
	}
	mu.Unlock()
}
