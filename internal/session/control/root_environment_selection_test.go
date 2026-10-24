package control

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/runtime/rootenv"
	"reasonix/internal/state/sessionstore"
)

func TestRootEnvironmentPreparationRequiresFreshGatedRuntime(t *testing.T) {
	for _, kind := range []string{"fresh", "already-started", "history", "new-generation", "external-runtime"} {
		t.Run(kind, func(t *testing.T) {
			sentinel := errors.New("preparation fixture")
			calls := 0
			policy := &RootEnvironmentPolicy{Generation: 1, Prepare: func(context.Context, string) (*rootenv.Environment, *config.Config, error) {
				calls++
				return nil, nil, sentinel
			}}
			if kind == "external-runtime" {
				policy.Block = func() error { return errors.New("active external process") }
			}
			session := sessionstore.NewSession("BASE")
			c := New(Options{Executor: agent.New(nil, nil, session, agent.Options{}, event.Discard), RuntimeGeneration: 1, RootEnvironmentPolicy: policy, Sink: event.Discard})
			defer c.Close()
			path := filepath.Join(t.TempDir(), "chat.jsonl")
			lease, err := sessionstore.TryAcquireSessionLease(path)
			if err != nil {
				t.Fatal(err)
			}
			defer lease.Release()
			c.SetSessionPath(path)
			if err := c.BindSessionWriteAuthority(lease); err != nil {
				t.Fatal(err)
			}
			if kind == "already-started" {
				c.startedOnce = true
			}
			if kind == "history" {
				session.Add(provider.Message{Role: provider.RoleUser, Content: "earlier work"})
			}
			if kind == "new-generation" {
				c.SetRuntimeGeneration(2)
			}
			if _, _, err := c.PrepareRootEnvironment(t.Context()); err == nil || calls != 0 {
				t.Fatalf("ungated preparation ran: %v %d", err, calls)
			}
			release, err := c.BeginRuntimeReplacement()
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			_, _, err = c.PrepareRootEnvironment(t.Context())
			if kind == "fresh" {
				if !errors.Is(err, sentinel) || calls != 1 {
					t.Fatalf("fresh preparation = %v, calls=%d", err, calls)
				}
			} else if err == nil || calls != 0 {
				t.Fatalf("unsafe preparation allowed: %v, calls=%d", err, calls)
			}
		})
	}
}
