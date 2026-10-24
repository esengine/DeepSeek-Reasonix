package delegation

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/writeclaim"
	"reasonix/internal/state/mutation"
)

type environmentSelectionProvider struct {
	started chan struct{}
	release chan struct{}
}

func (*environmentSelectionProvider) Name() string { return "auto-selection" }

func (p *environmentSelectionProvider) Stream(ctx context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	p.started <- struct{}{}
	select {
	case <-p.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	stream := make(chan provider.Chunk, 2)
	stream <- provider.Chunk{Type: provider.ChunkText, Text: "done"}
	stream <- provider.Chunk{Type: provider.ChunkDone}
	close(stream)
	return stream, nil
}

func TestConcurrentAutoWritersChooseAfterExecutionRegistration(t *testing.T) {
	root := t.TempDir()
	coordinator, err := mutation.New(root, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	p := &environmentSelectionProvider{started: make(chan struct{}, 2), release: make(chan struct{})}
	scheduler := writeclaim.NewSubagentScheduler(2, 2)
	parent := NewTaskToolWithOptions(TaskToolOptions{Provider: p, ParentRegistry: tool.NewRegistry(), MaxSteps: 4, SysPrompt: "sys"}).
		WithTranscripts(NewSubagentStore(t.TempDir()), root, "model", "").WithScheduler(scheduler).WithMutationCoordinator(coordinator)
	var preparations atomic.Int32
	parent.WithEnvironmentFactory("auto", func(_ context.Context, spec ProfileExecSpec, _ *EnvironmentSource) (*ExecutionEnvironment, error) {
		preparations.Add(1)
		child := NewTaskToolWithOptions(TaskToolOptions{Provider: p, ParentRegistry: tool.NewRegistry(), MaxSteps: 4, SysPrompt: "sys"}).
			WithTranscripts(NewSubagentStore(t.TempDir()), root, "model", "").WithMutationCoordinator(coordinator)
		return &ExecutionEnvironment{ID: "iso_selection", Task: child, Spec: spec}, nil
	})
	ctx, cancel := context.WithTimeout(testTaskContext(), 10*time.Second)
	defer cancel()
	admitted, start := make(chan struct{}, 2), make(chan struct{})
	done := make(chan error, 2)
	var running sync.WaitGroup
	t.Cleanup(func() { cancel(); running.Wait() })
	for range 2 {
		running.Go(func() {
			_, err := parent.RunProfileSpec(ctx, ProfileExecSpec{Task: TaskSpec{Objective: "work"}, Context: ContextRequest{Ephemeral: true}, Sched: SchedulerPolicy{OnStart: func() error {
				admitted <- struct{}{}
				select {
				case <-start:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			}}})
			done <- err
		})
	}
	for range 2 {
		select {
		case <-admitted:
		case err := <-done:
			t.Fatalf("call returned before admission: %v", err)
		case <-ctx.Done():
			t.Fatal("calls did not reach the deterministic admission boundary")
		}
	}
	close(start)
	for range 2 {
		select {
		case <-p.started:
		case <-ctx.Done():
			t.Fatal("both admitted calls did not execute")
		}
	}
	if preparations.Load() == 0 {
		t.Fatal("both simultaneous auto writers chose shared execution before either registered")
	}
	close(p.release)
	for range 2 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if scheduler.ActiveWriters() != 0 {
		t.Fatal("execution registration leaked after completion")
	}
	prepared := preparations.Load()
	if _, err := parent.RunProfileSpec(ctx, ProfileExecSpec{Task: TaskSpec{Objective: "single writer"}, Context: ContextRequest{Ephemeral: true}}); err != nil {
		t.Fatal(err)
	}
	if preparations.Load() != prepared {
		t.Fatal("a foreground singleton unnecessarily created an isolated environment")
	}
}
