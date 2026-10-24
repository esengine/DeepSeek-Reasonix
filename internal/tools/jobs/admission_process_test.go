package jobs

import (
	"context"
	"io"
	"os"
	"os/exec"
	"testing"
	"time"

	"reasonix/internal/base/proc"
	"reasonix/internal/contract/event"
)

func TestSealJoinsOSWaitAfterJobCallbackReturned(t *testing.T) {
	for _, background := range []bool{false, true} {
		name := "foreground"
		if background {
			name = "background"
		}
		t.Run(name, func(t *testing.T) {
			m := NewManager(event.Discard)
			t.Cleanup(m.Close)
			entered, release, returned := make(chan struct{}), make(chan struct{}), make(chan struct{})
			ctx, cancel := context.WithCancel(WithExecutionOwner(t.Context(), m))
			defer cancel()
			exe, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			run := func(runCtx context.Context, _ io.Writer) (string, error) {
				defer close(returned)
				cmd := exec.CommandContext(runCtx, exe, "-test.run=^$")
				_, err := proc.RunCommand(runCtx, cmd, proc.RunOptions{Track: true, CancelWaitGrace: time.Millisecond,
					WaitScope: func() error { close(entered); <-release; return nil }})
				return "", err
			}
			if background {
				if _, err := m.StartOwnedForSession("owner", "task", "wait", run); err != nil {
					t.Fatal(err)
				}
			} else {
				go func() { _, _ = run(ctx, io.Discard) }()
			}
			<-entered
			cancel()
			m.Cancel()
			<-returned
			done := m.Seal()
			select {
			case <-done:
				t.Error("callback return released an unfinished physical OS wait")
			default:
			}
			close(release)
			<-done
		})
	}
}
