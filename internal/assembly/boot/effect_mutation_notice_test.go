package boot

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/eventwire"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
	"reasonix/internal/state/mutation"
)

func TestEffectMutationWaitIdentityReachesFrontend(t *testing.T) {
	var current *pathLeaseProvider
	provider.Register("mutation-notice-effect", func(provider.Config) (provider.Provider, error) { return current, nil })
	for _, cancelled := range []bool{false, true} {
		name := "acquired"
		if cancelled {
			name = "cancelled"
		}
		t.Run(name, func(t *testing.T) {
			home, root := statedBootHome(t), t.TempDir()
			writeFile(t, root, "reasonix.toml", `default_model="test-model"
[codegraph]
enabled=false
[agent]
collaboration="shared"
[[providers]]
name="test-model"
kind="mutation-notice-effect"
model="x"
`)
			mirrorToUserConfigAt(t, home, root)
			approveWorkspaceAt(t, home, root)
			p := &pathLeaseProvider{path: "shared.txt", written: make(chan struct{}), resume: make(chan struct{})}
			close(p.resume)
			current = p
			notices := make(chan eventwire.Event, 8)
			c, err := Build(t.Context(), Options{Home: home, WorkspaceRoot: root, Sink: event.FuncSink(func(e event.Event) {
				if e.Kind == event.Notice && e.WorkspaceLease != nil {
					notices <- eventwire.ToWire(e)
				}
			})})
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			c.EnsureSessionPath()
			c.SetToolApprovalMode(control.ToolApprovalYolo)
			other, err := mutation.New(root, config.WorkspaceLeaseDir(), nil)
			if err != nil {
				t.Fatal(err)
			}
			held, err := other.Acquire(t.Context(), mutation.Intent{Effect: mutation.NamedPaths,
				Paths: []string{filepath.Join(root, p.path)}, SessionID: "holder", ExecutionID: "holder-child"})
			if err != nil {
				t.Fatal(err)
			}
			defer held.End(mutation.NotExecuted)
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- c.RunTurn(ctx, "Write the fixture") }()
			defer func() { cancel(); <-done }()
			read := func() eventwire.Event {
				t.Helper()
				select {
				case e := <-notices:
					return e
				case <-ctx.Done():
					t.Fatal("missing wait notice")
					return eventwire.Event{}
				}
			}
			start := read()
			w := start.WorkspaceLease
			if start.Code != event.NoticeCodeWorkspaceLease || w.TransactionID == "" || w.ExecutionID == "" || w.CallID != "write" || w.SessionID == "" || w.StartedAt == "" {
				t.Fatalf("missing execution identity: %+v", w)
			}
			if w.HolderTransactionID != held.ID || w.HolderSessionID != "holder" || w.HolderExecutionID != "holder-child" || len(w.Paths) != 1 {
				t.Fatalf("missing holder: %+v", w)
			}
			if cancelled {
				cancel()
			} else {
				held.End(mutation.NotExecuted)
			}
			var end eventwire.Event
			select {
			case end = <-notices:
			case <-time.After(5 * time.Second):
				t.Fatal("wait never closed")
			}
			if end.WorkspaceLease.TransactionID != w.TransactionID {
				t.Fatalf("unmatched close: %+v", end)
			}
			if cancelled {
				if end.Code != event.NoticeCodeWorkspaceLeaseAbandoned || end.WorkspaceLease.Reason != "cancelled" {
					t.Fatalf("wrong cancellation: %+v", end)
				}
			} else if end.Code != event.NoticeCodeWorkspaceLeaseResumed {
				t.Fatalf("wrong acquisition: %+v", end)
			}
		})
	}
}
