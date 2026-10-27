package delegation

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/state/checkpoint"
	"reasonix/internal/state/sessionstore"
	"reasonix/internal/tools/jobs"
)

func TestBackgroundNoLaunchFinalizesOwnedResources(t *testing.T) {
	for _, fleet := range []bool{false, true} {
		name := "task"
		if fleet {
			name = "fleet"
		}
		for _, invalid := range []bool{false, true} {
			reason := "closed_manager"
			if invalid {
				reason = "invalid_session"
			}
			t.Run(name+"/"+reason, func(t *testing.T) {
				root := t.TempDir()
				store := checkpoint.New("", root)
				observer := checkpoint.NewMutationObserver(checkpoint.ObserverOptions{Store: store})
				prov := &blockingProvider{started: make(chan struct{})}
				transcripts := mustSubagentStore(t)
				task := NewTaskTool(prov, nil, tool.NewRegistry(), 20, 0, 0, 0, 0, "", "sys", nil, 0, "", "", nil).
					WithTranscripts(transcripts, root, "base", "high").WithMutationObserver(observer)
				var target tool.Tool = task
				args := json.RawMessage(`{"prompt":"work","run_in_background":true}`)
				if fleet {
					target = NewFleetTool(task)
					args = json.RawMessage(`{"run_in_background":true,"tasks":[{"prompt":"one","write_paths":["one"]},{"prompt":"two","write_paths":["two"]}]}`)
				}
				manager := jobs.NewManager(event.Discard)
				defer manager.Close()
				sink := &recordSink{}
				session := "closed-session"
				if invalid {
					session = "invalid/session"
				} else {
					manager.Close()
				}
				ctx := jobs.WithManager(agent.WithCallContext(context.Background(), "no-launch", sink, nil, false), manager)
				ctx = jobs.WithSession(agent.WithParentSession(ctx, "parent-session"), session)
				_, _ = target.Execute(ctx, args)
				writers := observer.ActiveWriters()
				for _, w := range writers {
					defer observer.UnregisterWriter(w.ID)
				}
				if len(writers) != 0 || store.Barrier().HasWriters() {
					t.Errorf("job never invoked its closure but writer remains: %+v", writers)
				}
				transcripts.mu.Lock()
				locked, live := len(transcripts.locked), len(transcripts.live)
				transcripts.mu.Unlock()
				if locked != 0 || live != 0 {
					t.Errorf("no-launch transcript retained: locked=%d live=%d", locked, live)
				}
				wantPhase, wantMeta := subagentPhaseCancelled, sessionstore.SubagentCancelled
				if invalid {
					wantPhase, wantMeta = subagentPhaseFailed, sessionstore.SubagentFailed
				}
				terminals := 0
				for _, e := range sink.kinds(event.ToolProgress) {
					if progressName(e) == event.SubagentProgressStatusName && isTerminalStatusOutput(progressOutput(e)) {
						terminals++
						if progressOutput(e) != string(wantPhase) {
							t.Errorf("no-launch progress phase = %s", progressOutput(e))
						}
					}
				}
				if terminals != 1 {
					t.Errorf("no-launch terminal progress events = %d", terminals)
				}
				if !fleet {
					entries, err := os.ReadDir(transcripts.dir)
					if err != nil {
						t.Fatal(err)
					}
					found := 0
					for _, entry := range entries {
						if strings.HasSuffix(entry.Name(), ".meta.json") {
							found++
							meta, err := transcripts.LoadMeta(strings.TrimSuffix(entry.Name(), ".meta.json"))
							if err != nil || meta.Status != wantMeta {
								t.Errorf("no-launch persisted status = %s, %v", meta.Status, err)
							}
						}
					}
					if found != 1 {
						t.Errorf("no-launch transcript metadata records = %d", found)
					}
				}
				select {
				case <-prov.started:
					t.Error("no-launch job reached its provider")
				default:
				}
			})
		}
	}
}
