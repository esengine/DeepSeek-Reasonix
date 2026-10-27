package delegation

import (
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/state/checkpoint"
	"reasonix/internal/tools/jobs"
)

func TestJobFinalizerOwnsTheRegisteredWriterToken(t *testing.T) {
	for _, replace := range []bool{false, true} {
		t.Run(map[bool]string{false: "current", true: "replacement"}[replace], func(t *testing.T) {
			store := checkpoint.New("", t.TempDir())
			observer := checkpoint.NewMutationObserver(checkpoint.ObserverOptions{Store: store})
			release, err := observer.RegisterWriterContext(t.Context(), "writer", "background", 1)
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			manager := jobs.NewManager(event.Discard)
			defer manager.Close()
			finish, started, finalized := make(chan struct{}), make(chan struct{}), make(chan struct{})
			unblock := sync.OnceFunc(func() { close(finish) })
			defer unblock()
			manager.StartForSession("session", "task", "token lifetime", func(context.Context, io.Writer) (string, error) {
				close(started)
				<-finish
				return "done", nil
			}, jobCleanup(release), jobs.WithFinalizer(func(jobs.Finalization) error {
				close(finalized)
				return nil
			}))
			select {
			case <-started:
			case <-time.After(5 * time.Second):
				t.Fatal("job did not start")
			}
			if !store.Barrier().HasWriters() {
				t.Fatal("running job lost its writer before closure exit")
			}
			if replace {
				release()
				next, err := observer.RegisterWriterContext(t.Context(), "writer", "background", 2)
				if err != nil {
					t.Fatal(err)
				}
				defer next()
			}
			unblock()
			select {
			case <-finalized:
			case <-time.After(5 * time.Second):
				t.Fatal("job did not finalize")
			}
			writers := observer.ActiveWriters()
			if replace {
				if !store.Barrier().HasWriters() || len(writers) != 1 || writers[0].Turn != 2 {
					t.Fatalf("old finalizer retired replacement: %+v", writers)
				}
			} else if store.Barrier().HasWriters() || len(writers) != 0 {
				t.Fatalf("completed job retained its writer: %+v", writers)
			}
		})
	}
}
