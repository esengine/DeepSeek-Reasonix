package delegation

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/writeclaim"
	"reasonix/internal/tools/jobs"
)

func TestEnvironmentPreparationWaitsForRootCapacity(t *testing.T) {
	for _, cancelQueued := range []bool{false, true} {
		name := "execute"
		if cancelQueued {
			name = "cancel"
		}
		t.Run(name, func(t *testing.T) {
			root, store := t.TempDir(), NewSubagentStore(t.TempDir())
			scheduler := writeclaim.NewSubagentScheduler(1, 1)
			release, err := scheduler.Acquire(t.Context(), writeclaim.AcquireRequest{})
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			p := &blockingProvider{started: make(chan struct{})}
			parent := NewTaskToolWithOptions(TaskToolOptions{Provider: p, ParentRegistry: tool.NewRegistry(), MaxSteps: 20, SysPrompt: "sys"}).WithTranscripts(store, root, "model", "high").WithScheduler(scheduler)
			var preparations, finishes atomic.Int32
			parent.WithEnvironmentFactory("shared", func(_ context.Context, spec ProfileExecSpec, _ *EnvironmentSource) (*ExecutionEnvironment, error) {
				preparations.Add(1)
				child := NewTaskToolWithOptions(TaskToolOptions{Provider: p, ParentRegistry: tool.NewRegistry(), MaxSteps: 20, SysPrompt: "sys"}).WithTranscripts(store, root, "model", "high")
				return &ExecutionEnvironment{ID: "iso_candidate", Task: child, Spec: spec, Finish: func(_ context.Context, out string, err error) (string, error) { finishes.Add(1); return out, err }}, nil
			})
			jm := jobs.NewManager(event.Discard)
			defer jm.Close()
			ctx := jobs.WithSession(jobs.WithManager(testTaskContext(), jm), "parent-session")
			type result struct {
				out string
				err error
			}
			done := make(chan result, 1)
			go func() {
				out, err := parent.Execute(ctx, json.RawMessage(`{"prompt":"work","isolation":"worktree","run_in_background":true}`))
				done <- result{out, err}
			}()
			var started result
			select {
			case started = <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("background submission waited for capacity")
			}
			if started.err != nil {
				t.Fatal(started.err)
			}
			if preparations.Load() != 0 {
				t.Fatal("queued task prepared an environment before capacity admission")
			}
			jobID := extractJobID(started.out)
			if jobID == "" {
				t.Fatalf("missing background identity: %s", started.out)
			}
			if cancelQueued {
				if !jm.KillForSession("parent-session", jobID) {
					t.Fatal("queued cancellation failed")
				}
				jm.Close()
				if preparations.Load() != 0 || finishes.Load() != 0 {
					t.Fatal("cancelled queue allocated an environment")
				}
				return
			}
			release()
			results, _ := jm.WaitForSession(ctx, "parent-session", []string{jobID}, jobs.WaitOptions{Timeout: 5 * time.Second})
			if len(results) != 1 || results[0].Status != jobs.Done {
				t.Fatalf("job: %+v", results)
			}
			if preparations.Load() != 1 || finishes.Load() != 1 {
				t.Fatalf("environment lifecycle counts: %d/%d", preparations.Load(), finishes.Load())
			}
		})
	}
}
