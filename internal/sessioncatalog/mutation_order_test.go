package sessioncatalog

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"reasonix/internal/agent"
)

func TestNewerRemovalGenerationSurvivesOlderRecreation(t *testing.T) {
	ctx := context.Background()
	catalog, err := Open(ctx, Options{InMemory: true, DisableRepair: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	record := SessionRecord{
		Path: "/sessions/race.jsonl", Directory: "/sessions", Scope: "global", TopicID: "topic",
		Preview: "initial", TurnsState: TurnsValid, Health: HealthOK,
	}
	if err := catalog.UpsertSession(ctx, record); err != nil {
		t.Fatal(err)
	}
	pathKey := catalog.pathKey(record.Path)
	oldRemoval := catalog.mutationSeq.Add(1)
	catalog.removedPaths.Store(pathKey, oldRemoval)
	record.Preview = "stale recreation"
	record.enqueueSequence = catalog.mutationSeq.Add(1)

	loaded := make(chan struct{})
	resume := make(chan struct{})
	var once sync.Once
	catalog.testPathMutationLoadedHook = func(key string) {
		if key == pathKey {
			once.Do(func() {
				close(loaded)
				<-resume
			})
		}
	}
	done := make(chan error, 1)
	go func() { done <- catalog.upsertSessions(ctx, []SessionRecord{record}, nil, "test") }()
	<-loaded
	newRemoval := catalog.mutationSeq.Add(1)
	catalog.removedPaths.Store(pathKey, newRemoval)
	close(resume)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	catalog.testPathMutationLoadedHook = nil

	removedAt, ok := catalog.removedPaths.Load(pathKey)
	if !ok || removedAt != newRemoval {
		t.Fatalf("removal generation = %v, %v, want %d", removedAt, ok, newRemoval)
	}
	var preview string
	if err := catalog.db.QueryRowContext(ctx, `SELECT preview FROM catalog_sessions WHERE path_key=?`, pathKey).Scan(&preview); err != nil {
		t.Fatal(err)
	}
	if preview != "initial" {
		t.Fatalf("older recreation committed preview %q", preview)
	}
}

func TestStaleExactIndexAndReconcileKeepRemovalTombstone(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := agent.SaveBranchMeta(path, agent.BranchMeta{
		Scope: "global", TopicID: "topic", Preview: "initial",
		SchemaVersion: agent.BranchMetaCountsVersion, Turns: 1,
	}); err != nil {
		t.Fatal(err)
	}
	catalog, err := Open(ctx, Options{Path: filepath.Join(t.TempDir(), "catalog.sqlite"), DisableRepair: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	target := DirectoryTarget{Path: dir, Scope: "global"}
	if err := catalog.ReconcileDirectory(ctx, target); err != nil {
		t.Fatal(err)
	}
	if err := agent.UpdateBranchMeta(path, false, func(meta *agent.BranchMeta) error {
		meta.Preview = "stale projection should never commit"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	staleExactSequence := catalog.mutationSeq.Add(1)
	staleScanSequence := catalog.mutationSeq.Add(1)
	pathKey := catalog.pathKey(path)
	removalSequence := catalog.mutationSeq.Add(1)
	catalog.removedPaths.Store(pathKey, removalSequence)

	if err := catalog.indexSessionPath(ctx, target, path, staleExactSequence); err != nil {
		t.Fatal(err)
	}
	if err := catalog.reconcileDirectory(ctx, target, staleScanSequence); err != nil {
		t.Fatal(err)
	}
	if removedAt, ok := catalog.removedPaths.Load(pathKey); !ok || removedAt != removalSequence {
		t.Fatalf("stale writers cleared removal generation: %v, %v", removedAt, ok)
	}
	if _, ok, err := catalog.GetSession(ctx, path); err != nil || ok {
		t.Fatalf("tombstoned session became visible: ok=%v err=%v", ok, err)
	}

	if err := agent.UpdateBranchMeta(path, false, func(meta *agent.BranchMeta) error {
		meta.Preview = "authoritative recreation"
		meta.Turns = 2
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.indexSessionPath(ctx, target, path, catalog.mutationSeq.Add(1)); err != nil {
		t.Fatal(err)
	}
	if _, ok := catalog.removedPaths.Load(pathKey); ok {
		t.Fatal("newer authoritative recreation did not clear the older tombstone")
	}
	got, ok, err := catalog.GetSession(ctx, path)
	if err != nil || !ok || got.Preview != "authoritative recreation" || got.Turns != 2 {
		t.Fatalf("recreated session = %#v, %v, %v", got, ok, err)
	}
}

func TestExactIndexQueueRetainsNewestGeneration(t *testing.T) {
	catalog := &Catalog{
		pathCh:         make(chan sessionPathRequest, 1),
		directoryLocks: map[string]*sync.Mutex{},
		stop:           make(chan struct{}),
	}
	path := filepath.Join(string(filepath.Separator), "sessions", "session.jsonl")
	first := DirectoryTarget{Path: filepath.Dir(path), Scope: "global"}
	second := DirectoryTarget{Path: filepath.Dir(path), Scope: "project", WorkspaceRoot: "/workspace"}
	if !catalog.RequestIndexSession(first, path) || !catalog.RequestIndexSession(second, path) {
		t.Fatal("coalesced exact-index request was rejected")
	}
	queued, ok := catalog.pathQueued.Load(queuePathKey(path))
	if !ok {
		t.Fatal("latest exact-index request was not retained")
	}
	request := queued.(sessionPathRequest)
	if request.target.Scope != "project" || request.target.WorkspaceRoot != "/workspace" || request.sequence != 2 {
		t.Fatalf("coalesced request = %#v, want newest generation", request)
	}
	if len(catalog.pathCh) != 1 {
		t.Fatalf("coalesced queue signals = %d, want one", len(catalog.pathCh))
	}
}
