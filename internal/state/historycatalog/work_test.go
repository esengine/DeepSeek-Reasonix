package historycatalog

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"

	"reasonix/internal/base/backgroundwork"
	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/provider"
	"reasonix/internal/state/sessionstore"
)

func TestInterruptedScanDoesNotMarkUnvisitedSourcesMissing(t *testing.T) {
	root := Root{Path: testenv.TempDir(t), Source: "project", Scope: "project"}
	root.WorkspaceRoot = root.Path
	a, b := filepath.Join(root.Path, "a.jsonl"), filepath.Join(root.Path, "b.jsonl")
	saveMessages(t, a, provider.Message{Role: provider.RoleUser, Content: "first marker"})
	saveMessages(t, b, provider.Message{Role: provider.RoleUser, Content: "retained marker"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var armed atomic.Bool
	c, err := Open(context.Background(), Options{InMemory: true, Work: backgroundwork.New(backgroundwork.Options{}), OnRevision: func(_ Status, _ []string, reason string) {
		if reason == "source-indexed" && armed.CompareAndSwap(true, false) {
			cancel()
		}
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close(context.Background()) })
	if err := c.ReconcileRoot(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	session, err := sessionstore.LoadSession(a)
	if err != nil {
		t.Fatal(err)
	}
	session.Rewrite([]provider.Message{{Role: provider.RoleUser, Content: "updated marker"}}, "test")
	if err := session.SaveRewrite(a); err != nil {
		t.Fatal(err)
	}
	armed.Store(true)
	if err := c.ReconcileRoot(ctx, root); !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v", err)
	}
	var health, state string
	var missing int64
	if err := c.db.QueryRow(`SELECT health,missing_since FROM history_sources WHERE path=?`, b).Scan(&health, &missing); err != nil {
		t.Fatal(err)
	}
	if health != "ok" || missing != 0 {
		t.Fatalf("health=%s missing=%d", health, missing)
	}
	if err := c.db.QueryRow(`SELECT state FROM history_roots WHERE path=?`, root.Path).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "scanning" {
		t.Fatalf("state=%s", state)
	}
	if err := c.ReconcileRoot(context.Background(), root); err != nil {
		t.Fatal(err)
	}
}

func TestCanceledIndexDoesNotClassifyHealthySourceAsCorrupt(t *testing.T) {
	c, err := Open(context.Background(), Options{InMemory: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close(context.Background()) })
	root := Root{Path: testenv.TempDir(t)}
	path := filepath.Join(root.Path, "history.jsonl")
	saveMessages(t, path, provider.Message{Role: provider.RoleUser, Content: "healthy history"})
	if err := c.indexPath(context.Background(), root, path, 0, -1); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.indexPathOnce(ctx, root, path, 0, -1); !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v", err)
	}
	var health string
	if err := c.db.QueryRow(`SELECT health FROM history_sources WHERE path=?`, path).Scan(&health); err != nil {
		t.Fatal(err)
	}
	if health != "ok" {
		t.Fatalf("health=%s", health)
	}
}
