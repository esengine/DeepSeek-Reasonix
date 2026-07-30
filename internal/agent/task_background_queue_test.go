package agent

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/jobs"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// TestBackgroundTaskReturnsBeforeSlotFrees ensures run_in_background returns a
// job id even when the session concurrency pool is full, instead of blocking
// the parent tool call on Acquire.
func TestBackgroundTaskReturnsBeforeSlotFrees(t *testing.T) {
	root := t.TempDir()
	sched := NewSubagentScheduler(1, 1)
	// Hold the only slot.
	holdRelease, err := sched.Acquire(context.Background(), AcquireRequest{Writer: false})
	if err != nil {
		t.Fatal(err)
	}

	started := make(chan struct{})
	prov := &blockingProvider{started: started}
	jm := jobs.NewManager(event.Discard)
	defer jm.Close()
	ctx := jobs.WithManager(withCallContext(context.Background(), "bg", event.Discard, nil, false), jm)
	ctx = jobs.WithSession(ctx, "sess-bg")
	ctx = WithParentSession(ctx, "sess-bg")

	task := NewTaskTool(prov, nil, tool.NewRegistry(), 20, 0, 0, 0, 0, 0, 0, 0.0, "", "sys", nil, 0, "", "", nil).
		WithTranscripts(mustSubagentStore(t), root, "base", "high").
		WithScheduler(sched)

	done := make(chan string, 1)
	go func() {
		out, err := task.Execute(ctx, json.RawMessage(`{"prompt":"work","run_in_background":true,"description":"queued"}`))
		if err != nil {
			done <- "err:" + err.Error()
			return
		}
		done <- out
	}()

	var jobID string
	select {
	case out := <-done:
		if !strings.Contains(out, "Started background task") {
			t.Fatalf("want immediate job start, got %q", out)
		}
		if !strings.Contains(out, "queue") {
			t.Fatalf("want queue note in background start message, got %q", out)
		}
		jobID = extractJobID(out)
		if jobID == "" {
			t.Fatalf("no background job id in output: %q", out)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("background task blocked on concurrency slot instead of returning a job id")
	}

	// Free the slot so the background job can finish (and not leak).
	holdRelease()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("background job never acquired slot / started provider")
	}
	result := jm.WaitForSession(context.Background(), "sess-bg", []string{jobID}, 5)
	if len(result) != 1 || result[0].Status != jobs.Done {
		t.Fatalf("background job result = %+v, want one completed job", result)
	}
}

type blockingProvider struct {
	started chan struct{}
	once    sync.Once
}

func (p *blockingProvider) Name() string { return "blocking" }

func (p *blockingProvider) Stream(_ context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	p.once.Do(func() { close(p.started) })
	ch := make(chan provider.Chunk, 1)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "done"}
	close(ch)
	return ch, nil
}
