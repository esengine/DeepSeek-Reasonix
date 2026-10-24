package boot

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/runtime/agent"
)

type delayedCapacityProvider struct {
	first, next chan struct{}
	input       chan provider.Chunk
	firstOnce   sync.Once
	nextOnce    sync.Once
}

func (*delayedCapacityProvider) Name() string { return "delayed-capacity-effect" }
func (p *delayedCapacityProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	if strings.Contains(firstUser(req), "BLOCKED_MODEL") {
		p.firstOnce.Do(func() { close(p.first) })
		return p.input, nil
	}
	p.nextOnce.Do(func() { close(p.next) })
	ch := make(chan provider.Chunk, 2)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "completed"}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func TestEffectModelCapacityWaitsForPhysicalSourceAfterCancellation(t *testing.T) {
	home, root := statedBootHome(t), t.TempDir()
	p := &delayedCapacityProvider{first: make(chan struct{}), next: make(chan struct{}), input: make(chan provider.Chunk)}
	provider.Register("delayed-capacity-effect", func(provider.Config) (provider.Provider, error) { return p, nil })
	writeFile(t, root, "reasonix.toml", `default_model="test-model"
[agent]
collaboration="shared"
max_subagent_concurrency=1
[codegraph]
enabled=false
[[providers]]
name="test-model"
kind="delayed-capacity-effect"
model="x"
`)
	mirrorToUserConfigAt(t, home, root)
	approveWorkspaceAt(t, home, root)
	waiting := make(chan event.Event, 8)
	b := newBuilder()
	built, err := b.build(t.Context(), Options{Home: home, WorkspaceRoot: root, Sink: event.FuncSink(func(e event.Event) {
		if e.WorkspaceLease != nil && e.WorkspaceLease.ResourceKind == "capacity" {
			waiting <- e
		}
	})})
	if err != nil {
		t.Fatal(err)
	}
	defer built.Controller.Close()
	if err := os.MkdirAll(b.session.dir, 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	first, cancelFirst := context.WithCancel(ctx)
	defer cancelFirst()
	firstDone, nextDone := make(chan error, 1), make(chan error, 1)
	run := func(ctx context.Context, id, prompt string, done chan<- error) {
		ctx = agent.WithParentSession(ctx, "root-owner")
		ctx = agent.WithCallContext(ctx, id, event.Discard, nil, false)
		args, _ := json.Marshal(map[string]any{"prompt": prompt, "read_only": true})
		_, err := b.tools.taskTool.Execute(ctx, args)
		done <- err
	}
	go run(first, "first", "BLOCKED_MODEL", firstDone)
	var closeOnce sync.Once
	closeInput := func() { closeOnce.Do(func() { close(p.input) }) }
	defer func() { cancel(); closeInput(); <-firstDone; <-nextDone }()
	go func() {
		select {
		case <-p.first:
		case <-ctx.Done():
			nextDone <- ctx.Err()
			return
		}
		run(ctx, "next", "NEXT_MODEL", nextDone)
	}()
	var open event.Event
	select {
	case open = <-waiting:
		if open.WorkspaceLease.State != "waiting" || open.WorkspaceLease.ExecutionID == "" || open.WorkspaceLease.HolderExecutionID == "" {
			t.Fatalf("capacity wait lost identities: %+v", open)
		}
	case <-ctx.Done():
		t.Fatal("second request bypassed model admission")
	}
	cancelFirst()
	select {
	case err := <-firstDone:
		firstDone <- err
		if err == nil {
			t.Fatal("cancelled request succeeded")
		}
	case <-ctx.Done():
		t.Fatal("logical cancellation waited for physical source")
	}
	select {
	case <-p.next:
		t.Fatal("second provider started while cancelled source remained open")
	default:
	}
	closeInput()
	select {
	case <-p.next:
	case <-ctx.Done():
		t.Fatal("source completion did not free model capacity")
	}
	select {
	case end := <-waiting:
		if end.WorkspaceLease.State != "acquired" || end.WorkspaceLease.TransactionID != open.WorkspaceLease.TransactionID {
			t.Fatalf("unclosed capacity wait: %+v", end)
		}
	case <-ctx.Done():
		t.Fatal("missing capacity acquisition event")
	}
}
