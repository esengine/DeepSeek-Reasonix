package boot

import (
	"context"
	"sync"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
	"reasonix/internal/state/executionpresence"
	"reasonix/internal/state/mutation"
	"reasonix/internal/state/sessionstore"
)

func bindPresenceSession(t *testing.T, controller *control.Controller) *sessionstore.SessionLease {
	t.Helper()
	controller.EnsureSessionPath()
	lease, err := sessionstore.TryAcquireSessionLease(controller.SessionPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(lease.Release)
	if err := controller.BindSessionWriteAuthority(lease); err != nil {
		t.Fatal(err)
	}
	return lease
}

type presenceWaitingProvider struct {
	entered chan struct{}
	leave   chan struct{}
	started sync.Once
}

func (*presenceWaitingProvider) Name() string { return "presence-wait" }

func (p *presenceWaitingProvider) Stream(ctx context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 2)
	p.started.Do(func() { close(p.entered) })
	go func() {
		defer close(ch)
		select {
		case <-p.leave:
		case <-ctx.Done():
			return
		}
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "done"}
		ch <- provider.Chunk{Type: provider.ChunkDone}
	}()
	return ch, nil
}

func TestEffectActiveModelRegistersWriterWithoutHoldingFileLease(t *testing.T) {
	home, root := statedBootHome(t), testenv.TempDir(t)
	p := &presenceWaitingProvider{entered: make(chan struct{}), leave: make(chan struct{})}
	provider.Register("boot-model-presence", func(provider.Config) (provider.Provider, error) { return p, nil })
	writeFile(t, root, "reasonix.toml", `
default_model = "test-model"
[agent]
system_prompt = "BASE"
collaboration = "shared"
[codegraph]
enabled = false
[[providers]]
name = "test-model"
kind = "boot-model-presence"
model = "x"
`)
	mirrorToUserConfigAt(t, home, root)
	b := newBuilder()
	built, err := b.build(t.Context(), Options{Home: home, WorkspaceRoot: root, Sink: event.Discard})
	if err != nil {
		t.Fatal(err)
	}
	defer built.Controller.Close()
	bindPresenceSession(t, built.Controller)
	done := make(chan error, 1)
	finished := make(chan struct{})
	var leaveOnce sync.Once
	defer func() { leaveOnce.Do(func() { close(p.leave) }); <-finished }()
	go func() { defer close(finished); done <- built.Controller.Run(t.Context(), "think") }()
	select {
	case <-p.entered:
	case <-t.Context().Done():
		t.Fatal(t.Context().Err())
	}
	store, err := executionpresence.New(root, config.WorkspaceLeaseDir())
	if err != nil {
		t.Fatal(err)
	}
	reservation, err := store.Reserve(t.Context(), executionpresence.Request{Strategy: executionpresence.Auto, Writer: true, SessionID: "other"})
	if err != nil {
		t.Fatal(err)
	}
	if reservation.Record().Selected != executionpresence.Isolated {
		t.Fatal("simultaneous chat did not see active writer")
	}
	reservation.Close()
	lease, err := b.session.mutations.Acquire(t.Context(), mutation.Intent{Effect: mutation.NamedPaths, Paths: []string{root}, CallID: "parallel-write"})
	if err != nil {
		t.Fatal("model thinking held file resources:", err)
	}
	lease.End(mutation.NotExecuted)
	leaveOnce.Do(func() { close(p.leave) })
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	reservation, err = store.Reserve(t.Context(), executionpresence.Request{Strategy: executionpresence.Auto, Writer: true, SessionID: "after"})
	if err != nil {
		t.Fatal(err)
	}
	defer reservation.Close()
	if reservation.Record().Selected != executionpresence.Shared {
		t.Fatal("completed model left writer presence")
	}
}
