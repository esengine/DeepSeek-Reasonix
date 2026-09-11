package sessioncatalog

import "testing"

func TestReconcileTokenUsesLatestPreDispatchTargetOnce(t *testing.T) {
	catalog := &Catalog{
		reconcileCh: make(chan DirectoryTarget, 2), reconcileDirty: map[string]DirectoryTarget{}, stop: make(chan struct{}),
	}
	a := DirectoryTarget{Path: "/sessions", Scope: "global", mutationSeq: 1}
	b := DirectoryTarget{Path: "/sessions", Scope: "workspace", WorkspaceRoot: "/work", mutationSeq: 2}
	if !catalog.RequestReconcile(a) || !catalog.RequestReconcile(b) {
		t.Fatal("failed to queue targets")
	}
	token := <-catalog.reconcileCh
	got, ok := catalog.resolveReconcileToken(token)
	if !ok || got.Scope != b.Scope || got.WorkspaceRoot != b.WorkspaceRoot || got.mutationSeq <= a.mutationSeq {
		t.Fatalf("resolved target = %+v ok=%v, want latest B", got, ok)
	}
	if _, dirty := catalog.takeReconcileDirty(); dirty {
		t.Fatal("pre-dispatch dirty target remained queued")
	}
	select {
	case extra := <-catalog.reconcileCh:
		t.Fatalf("unexpected second channel token: %+v", extra)
	default:
	}
}

func TestReconcileRunningUpdatesKeepOnlyLatestFollowUp(t *testing.T) {
	catalog := &Catalog{reconcileDirty: map[string]DirectoryTarget{}}
	key := queuePathKey("/sessions")
	catalog.reconcileQueued.Store(key, DirectoryTarget{Path: "/sessions", mutationSeq: 1})
	catalog.markReconcileDirty(DirectoryTarget{Path: "/sessions", Scope: "global", mutationSeq: 2})
	catalog.markReconcileDirty(DirectoryTarget{Path: "/sessions", Scope: "workspace", WorkspaceRoot: "/latest", mutationSeq: 3})
	got, ok := catalog.takeReconcileDirty()
	if !ok || got.mutationSeq != 3 || got.WorkspaceRoot != "/latest" {
		t.Fatalf("follow-up = %+v ok=%v, want latest only", got, ok)
	}
	if _, ok := catalog.takeReconcileDirty(); ok {
		t.Fatal("more than one follow-up remained")
	}
}

func TestReconcileDirtyNeverDowngradesToLateOlderTarget(t *testing.T) {
	newer := DirectoryTarget{Path: "/sessions", Scope: "workspace", WorkspaceRoot: "/new", mutationSeq: 3}
	older := DirectoryTarget{Path: "/sessions", Scope: "global", mutationSeq: 2}
	for _, tc := range []struct {
		name      string
		seedDirty bool
	}{
		{name: "queued target"},
		{name: "dirty target", seedDirty: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			catalog := &Catalog{reconcileDirty: map[string]DirectoryTarget{}}
			key := queuePathKey(newer.Path)
			catalog.reconcileQueued.Store(key, newer)
			if tc.seedDirty {
				catalog.reconcileDirty[key] = newer
			}
			catalog.markReconcileDirty(older)
			got, ok := catalog.takeReconcileDirty()
			if !ok || got.mutationSeq != newer.mutationSeq || got.WorkspaceRoot != newer.WorkspaceRoot {
				t.Fatalf("dirty target = %+v ok=%v, want newer target", got, ok)
			}
		})
	}
}
