package control

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/checkpoint"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

type resultWritingRunner struct {
	session *agent.Session
	write   func() error
	started chan struct{}
	wait    bool
	failure error
}

func (r *resultWritingRunner) Run(ctx context.Context, input string) error {
	r.session.Add(provider.Message{Role: provider.RoleUser, Content: input, CreatedAt: time.Now().UnixMilli()})
	if err := r.write(); err != nil {
		return err
	}
	if r.started != nil {
		close(r.started)
	}
	if r.wait {
		<-ctx.Done()
		return ctx.Err()
	}
	return r.failure
}

func TestControllerFreezesTurnResultBeforeTerminalPublication(t *testing.T) {
	for _, mode := range []string{"success", "error", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			root, dir := t.TempDir(), t.TempDir()
			path := filepath.Join(root, "file.txt")
			if err := os.WriteFile(path, []byte("user dirty\nold\n"), 0644); err != nil {
				t.Fatal(err)
			}
			session := agent.NewSession("system")
			runner := &resultWritingRunner{session: session, started: make(chan struct{}), wait: mode == "cancel"}
			if mode == "error" {
				runner.failure = errors.New("provider failed after write")
			}
			executor := agent.New(nil, tool.NewRegistry(), session, agent.Options{}, event.Discard)
			events := make(chan event.Event, 2)
			c := New(Options{Runner: runner, Executor: executor, WorkspaceRoot: root, SessionDir: dir, SessionPath: filepath.Join(dir, "session.jsonl"), Sink: event.FuncSink(func(e event.Event) {
				if e.Kind == event.TurnDone {
					events <- e
				}
			})})
			t.Cleanup(c.Close)
			runner.write = func() error {
				store := c.checkpoints.storeRef()
				store.CaptureBefore("file.txt", checkpoint.CaptureBeforeOpts{})
				if err := os.WriteFile(path, []byte("user dirty\nnew\n"), 0644); err != nil {
					return err
				}
				store.CaptureAfter("file.txt", checkpoint.CaptureAfterOpts{})
				return nil
			}
			c.Send("update file")
			select {
			case <-runner.started:
			case <-time.After(5 * time.Second):
				t.Fatal("runner not started")
			}
			if mode == "cancel" {
				c.Cancel()
			}
			done := receiveCheckpointTurnDone(t, events)
			requireCheckpointTurn(t, done, 0)
			if done.Receipt == nil || done.Receipt.Diff == nil {
				t.Fatalf("terminal receipt missing: %+v", done)
			}
			diff := done.Receipt.Diff
			if diff.Coverage != "complete" || diff.Added != 1 || diff.Removed != 1 || len(diff.Files) != 1 || diff.Files[0].Patch != "" {
				t.Fatalf("summary: %+v", diff)
			}
			if mode == "cancel" && !done.Receipt.Interrupted {
				t.Fatal("cancel lost receipt interruption")
			}
			if err := os.WriteFile(path, []byte("external later write\n"), 0644); err != nil {
				t.Fatal(err)
			}
			frozen := c.CheckpointTurnChanges(0)
			if !strings.Contains(frozen.Files[0].Patch, "+new") || strings.Contains(frozen.Files[0].Patch, "external later") {
				t.Fatalf("result recomputed: %+v", frozen)
			}
		})
	}
}
