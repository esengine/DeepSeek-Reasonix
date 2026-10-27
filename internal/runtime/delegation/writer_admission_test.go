package delegation

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"testing/synctest"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/state/checkpoint"
	"reasonix/internal/tools/jobs"
)

func TestBackgroundDelegationWriterAdmissionCancellation(t *testing.T) {
	for _, fleet := range []bool{false, true} {
		name := "task"
		if fleet {
			name = "fleet"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			store := checkpoint.New("", root)
			observer := checkpoint.NewMutationObserver(checkpoint.ObserverOptions{Store: store})
			prov := &blockingProvider{started: make(chan struct{})}
			task := NewTaskTool(prov, nil, tool.NewRegistry(), 20, 0, 0, 0, 0, "", "sys", nil, 0, "", "", nil).
				WithTranscripts(mustSubagentStore(t), root, "base", "high").WithMutationObserver(observer)
			var target tool.Tool = task
			args := json.RawMessage(`{"prompt":"work","run_in_background":true}`)
			if fleet {
				target = NewFleetTool(task)
				args = json.RawMessage(`{"run_in_background":true,"tasks":[{"prompt":"one","write_paths":["one.txt"]},{"prompt":"two","write_paths":["two.txt"]}]}`)
			}
			synctest.Test(t, func(t *testing.T) {
				manager := jobs.NewManager(event.Discard)
				defer manager.Close()
				ctx := jobs.WithManager(agent.WithCallContext(context.Background(), "cancel-writer", event.Discard, nil, false), manager)
				ctx = jobs.WithSession(ctx, "writer-admission")
				if err := store.Barrier().EnterExclusive(); err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithCancel(ctx)
				defer cancel()
				done := make(chan error, 1)
				go func() { _, err := target.Execute(ctx, args); done <- err }()
				synctest.Wait()
				cancel()
				synctest.Wait()
				select {
				case err := <-done:
					if !errors.Is(err, context.Canceled) {
						t.Errorf("background admission error = %v", err)
					}
				default:
					t.Error("cancelled delegation remained behind checkpoint exclusive owner")
					store.Barrier().ExitExclusive()
					<-done
					return
				}
				if store.Barrier().HasWriters() || !store.Barrier().Busy() || len(observer.ActiveWriters()) != 0 {
					t.Error("cancelled delegation changed checkpoint ownership")
				}
				store.Barrier().ExitExclusive()
				select {
				case <-prov.started:
					t.Error("cancelled admission launched a child provider")
				default:
				}
			})
		})
	}
}
