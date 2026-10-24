package boot

import (
	"context"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
)

func TestSharedTransactionsReleaseWhileModelIsThinking(t *testing.T) {
	home, root := statedBootHome(t), t.TempDir()
	writeFile(t, root, "reasonix.toml", `
default_model = "test-model"
[codegraph]
enabled = false
[agent]
collaboration = "shared"
[[providers]]
name = "test-model"
kind = "mutation-effect"
model = "x"
`)
	mirrorToUserConfigAt(t, home, root)
	approveWorkspaceAt(t, home, root)
	first := &pathLeaseProvider{path: "shared.txt", written: make(chan struct{}), resume: make(chan struct{})}
	second := &pathLeaseProvider{path: "shared.txt", written: make(chan struct{}), resume: make(chan struct{})}
	current := first
	provider.Register("mutation-effect", func(provider.Config) (provider.Provider, error) { return current, nil })
	build := func() *control.Controller {
		c, err := Build(t.Context(), Options{Home: home, WorkspaceRoot: root, Sink: event.Discard})
		if err != nil {
			t.Fatal(err)
		}
		c.SetToolApprovalMode(control.ToolApprovalYolo)
		t.Cleanup(func() { c.Close() })
		return c
	}
	a := build()
	current = second
	b := build()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	doneA, doneB := make(chan struct{}), make(chan struct{})
	defer func() { cancel(); close(first.resume); close(second.resume); <-doneA; <-doneB }()
	go func() { defer close(doneA); _ = a.Run(ctx, "create shared.txt") }()
	go func() {
		defer close(doneB)
		select {
		case <-first.written:
		case <-ctx.Done():
			return
		}
		_ = b.Run(ctx, "create shared.txt")
	}()
	select {
	case <-second.written:
	case <-ctx.Done():
		t.Fatal("finished write blocked a second session during model thinking")
	}
	if a.WorkspaceLeaseState().Acquired || b.WorkspaceLeaseState().Acquired {
		t.Fatal("a shared-mode turn retained a legacy claim")
	}
}
