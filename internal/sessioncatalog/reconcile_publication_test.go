package sessioncatalog

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/agent"
)

func TestReconcilePublishesOnlyCompletedDirectorySnapshot(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()
	const sessionCount = 65
	for i := range sessionCount {
		path := filepath.Join(dir, fmt.Sprintf("chat-%03d.jsonl", i))
		if err := os.WriteFile(path, []byte(`{"role":"user","content":"hi"}`+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := agent.SaveBranchMeta(path, agent.BranchMeta{
			Scope:         "project",
			WorkspaceRoot: "/workspace",
			TopicID:       fmt.Sprintf("topic-%03d", i),
			TopicTitle:    fmt.Sprintf("Topic %03d", i),
			SchemaVersion: agent.BranchMetaCountsVersion,
			Turns:         1,
		}); err != nil {
			t.Fatal(err)
		}
	}

	events := make(chan string, sessionCount+1)
	catalog, err := Open(ctx, Options{
		InMemory:      true,
		DisableRepair: true,
		OnRevision: func(_ uint64, _ []string, reason string) {
			events <- reason
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })

	if err := catalog.ReconcileDirectory(ctx, DirectoryTarget{
		Path:          dir,
		Scope:         "project",
		WorkspaceRoot: "/workspace",
	}); err != nil {
		t.Fatal(err)
	}
	close(events)
	published := make([]string, 0, len(events))
	for reason := range events {
		published = append(published, reason)
	}
	if len(published) != 1 || published[0] != "reconcile_complete" {
		t.Fatalf("published reasons = %q, want only reconcile_complete", published)
	}

	page, err := catalog.ListTopics(ctx, TopicPageRequest{
		Scope:         "project",
		WorkspaceRoot: "/workspace",
		Limit:         sessionCount,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != sessionCount {
		t.Fatalf("final page items = %d, want %d", len(page.Items), sessionCount)
	}
}

func TestReconcileBatchBoundaryIsOneAtomicSnapshot(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	const sessionCount = 65
	paths := make([]string, 0, sessionCount)
	for i := range sessionCount {
		path := filepath.Join(dir, fmt.Sprintf("chat-%03d.jsonl", i))
		paths = append(paths, path)
		if err := os.WriteFile(path, []byte(`{"role":"user","content":"hi"}`+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := agent.SaveBranchMeta(path, agent.BranchMeta{
			Scope: "global", TopicID: fmt.Sprintf("old-%03d", i), TopicTitle: fmt.Sprintf("Old %03d", i),
			SchemaVersion: agent.BranchMetaCountsVersion, Turns: 1,
		}); err != nil {
			t.Fatal(err)
		}
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
	for i, path := range paths {
		if err := agent.UpdateBranchMeta(path, false, func(meta *agent.BranchMeta) error {
			meta.TopicID = fmt.Sprintf("new-%03d", i)
			meta.TopicTitle = fmt.Sprintf("New %03d", i)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	paused := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	catalog.testReconcileBatchHook = func(processed int) {
		if processed == 64 {
			once.Do(func() {
				close(paused)
				<-release
			})
		}
	}
	done := make(chan error, 1)
	go func() { done <- catalog.ReconcileDirectory(ctx, target) }()
	select {
	case <-paused:
	case <-time.After(5 * time.Second):
		t.Fatal("reconcile did not reach the first batch boundary")
	}
	page, err := catalog.ListTopics(ctx, TopicPageRequest{Scope: "global", Limit: sessionCount})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != sessionCount {
		t.Fatalf("uncommitted page size = %d, want old snapshot %d", len(page.Items), sessionCount)
	}
	for _, item := range page.Items {
		if !strings.HasPrefix(item.TopicID, "old-") {
			t.Fatalf("reader observed partial new projection at batch boundary: %q", item.TopicID)
		}
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	page, err = catalog.ListTopics(ctx, TopicPageRequest{Scope: "global", Limit: sessionCount})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != sessionCount {
		t.Fatalf("committed page size = %d, want %d", len(page.Items), sessionCount)
	}
	for _, item := range page.Items {
		if !strings.HasPrefix(item.TopicID, "new-") {
			t.Fatalf("reader retained old projection after commit: %q", item.TopicID)
		}
	}
}

func TestReconcileSQLFailureRollsBackSnapshotAndRevision(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "chat.jsonl")
	if err := os.WriteFile(path, []byte(`{"role":"user","content":"hi"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := agent.SaveBranchMeta(path, agent.BranchMeta{
		Scope: "global", TopicID: "topic", TopicTitle: "Topic", Preview: "before",
		SchemaVersion: agent.BranchMetaCountsVersion, Turns: 1,
	}); err != nil {
		t.Fatal(err)
	}
	catalog, err := Open(ctx, Options{InMemory: true, DisableRepair: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	target := DirectoryTarget{Path: dir, Scope: "global"}
	if err := catalog.ReconcileDirectory(ctx, target); err != nil {
		t.Fatal(err)
	}
	revision := catalog.Status().Revision
	if err := agent.UpdateBranchMeta(path, false, func(meta *agent.BranchMeta) error {
		meta.Preview = "after"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.db.ExecContext(ctx, `CREATE TRIGGER fail_atomic_projection
        BEFORE UPDATE OF preview ON catalog_sessions
        BEGIN SELECT RAISE(FAIL, 'injected projection failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := catalog.ReconcileDirectory(ctx, target); err == nil {
		t.Fatal("ReconcileDirectory succeeded despite injected SQL failure")
	}
	got, ok, err := catalog.GetSession(ctx, path)
	if err != nil || !ok {
		t.Fatalf("GetSession: ok=%v err=%v", ok, err)
	}
	if got.Preview != "before" {
		t.Fatalf("rolled-back preview = %q, want before", got.Preview)
	}
	if gotRevision := catalog.Status().Revision; gotRevision != revision {
		t.Fatalf("revision after rollback = %d, want %d", gotRevision, revision)
	}
}

func TestExactIndexAndReconcileConvergeUnderDirectoryLock(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "chat.jsonl")
	if err := os.WriteFile(path, []byte(`{"role":"user","content":"hi"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := agent.SaveBranchMeta(path, agent.BranchMeta{
		Scope: "global", TopicID: "topic", TopicTitle: "Topic", Preview: "initial",
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
		meta.Preview = "reconcile snapshot"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	paused := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	catalog.testReconcileBatchHook = func(processed int) {
		if processed == 1 {
			once.Do(func() {
				close(paused)
				<-release
			})
		}
	}
	reconcileDone := make(chan error, 1)
	go func() { reconcileDone <- catalog.ReconcileDirectory(ctx, target) }()
	<-paused
	if err := agent.UpdateBranchMeta(path, false, func(meta *agent.BranchMeta) error {
		meta.Preview = "exact snapshot"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	exactStarted := make(chan struct{})
	exactDone := make(chan error, 1)
	go func() {
		close(exactStarted)
		exactDone <- catalog.IndexSessionPath(ctx, target, path)
	}()
	<-exactStarted
	select {
	case err := <-exactDone:
		t.Fatalf("exact index escaped directory lock before reconcile commit: %v", err)
	default:
	}
	close(release)
	if err := <-reconcileDone; err != nil {
		t.Fatal(err)
	}
	if err := <-exactDone; err != nil {
		t.Fatal(err)
	}
	if err := catalog.ReconcileDirectory(ctx, target); err != nil {
		t.Fatal(err)
	}
	record, ok, err := catalog.GetSession(ctx, path)
	if err != nil || !ok {
		t.Fatalf("GetSession: ok=%v err=%v", ok, err)
	}
	if record.Preview != "exact snapshot" || !record.OrdinaryVisible || record.TopicID != "topic" {
		t.Fatalf("final exact/reconcile projection = %+v", record)
	}
	page, err := catalog.ListTopics(ctx, TopicPageRequest{Scope: "global", Limit: 50})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("ListTopics after interleave: items=%+v err=%v", page.Items, err)
	}
}
