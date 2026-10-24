package delegation

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/tool"
	"reasonix/internal/state/checkpoint"
	"reasonix/internal/tools/jobs"
)

func TestEnvironmentRejectedBackgroundStartReleasesRuntime(t *testing.T) {
	root := t.TempDir()
	transcripts := NewSubagentStore(t.TempDir())
	p := &blockingProvider{started: make(chan struct{})}
	parent := NewTaskToolWithOptions(TaskToolOptions{Provider: p, ParentRegistry: tool.NewRegistry(), MaxSteps: 20, SysPrompt: "sys"}).WithTranscripts(transcripts, root, "model", "high")
	finished := 0
	parent.WithEnvironmentFactory("shared", func(_ context.Context, spec ProfileExecSpec, _ *EnvironmentSource) (*ExecutionEnvironment, error) {
		child := NewTaskToolWithOptions(TaskToolOptions{Provider: p, ParentRegistry: tool.NewRegistry(), MaxSteps: 20, SysPrompt: "sys"}).WithTranscripts(transcripts, root, "model", "high")
		return &ExecutionEnvironment{ID: "iso_test", Task: child, Spec: spec, Finish: func(_ context.Context, answer string, err error) (string, error) { finished++; return answer, err }}, nil
	})
	jm := jobs.NewManager(event.Discard)
	jm.Close()
	ctx := jobs.WithSession(jobs.WithManager(testTaskContext(), jm), "parent-session")
	_, err := parent.Execute(ctx, json.RawMessage(`{"prompt":"work","isolation":"worktree","run_in_background":true}`))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("closed manager accepted work: %v", err)
	}
	if finished != 0 {
		t.Fatalf("rejected submission allocated an environment and finalized it %d times", finished)
	}
	transcripts.mu.Lock()
	defer transcripts.mu.Unlock()
	if len(transcripts.locked) != 0 {
		t.Fatalf("unstarted transcript still locked: %v", transcripts.locked)
	}
	select {
	case <-p.started:
		t.Fatal("unstarted job called provider")
	default:
	}
}

func TestFleetRejectedBackgroundStartReleasesWriterReservation(t *testing.T) {
	root := t.TempDir()
	observer := checkpoint.NewMutationObserver(checkpoint.ObserverOptions{Store: checkpoint.New("", root)})
	task := NewTaskToolWithOptions(TaskToolOptions{Provider: &mockProvider{name: "sub"}, ParentRegistry: tool.NewRegistry(), MaxSteps: 20, SysPrompt: "sys"}).
		WithTranscripts(NewSubagentStore(t.TempDir()), root, "model", "high").WithMutationObserver(observer)
	jm := jobs.NewManager(event.Discard)
	jm.Close()
	ctx := jobs.WithSession(jobs.WithManager(testTaskContext(), jm), "parent-session")
	_, err := NewFleetTool(task).Execute(ctx, json.RawMessage(`{"run_in_background":true,"tasks":[{"prompt":"first","write_paths":["first.txt"]},{"prompt":"second","write_paths":["second.txt"]}]}`))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("closed manager accepted fleet: %v", err)
	}
	if writers := observer.ActiveWriters(); len(writers) != 0 {
		t.Fatalf("unstarted fleet retained writers: %+v", writers)
	}
}
