package main

import (
	"context"
	"errors"
	"testing"
)

func TestTopicMutationBudgetStartsAfterStorePreparation(t *testing.T) {
	isolateDesktopUserDirs(t)
	manager := newTopicStateManager()
	t.Cleanup(manager.close)
	root := t.TempDir()
	var operation context.Context
	manager.operationContext = func() (context.Context, context.CancelFunc) {
		// Simulate a cold-open phase consuming the pre-existing deadline. A
		// budget created before the store is prepared must already be expired.
		ctx, cancel := context.WithCancel(context.Background())
		operation = ctx
		if manager.scope(root).store == nil {
			cancel()
		}
		return ctx, cancel
	}
	if err := manager.setTitle(root, "topic-budget", "Prepared first", topicTitleSourceManual); err != nil {
		t.Fatalf("title mutation received a pre-preparation deadline: %v", err)
	}
	if operation == nil || !errors.Is(operation.Err(), context.Canceled) {
		t.Fatal("completed mutation did not release its context")
	}
	snapshot, err := manager.snapshot(root)
	if err != nil || snapshot.Records["topic-budget"].Title != "Prepared first" {
		t.Fatalf("title was not committed: %+v, %v", snapshot, err)
	}
}

func TestTopicMutationStillHonorsExpiredOperationBudget(t *testing.T) {
	isolateDesktopUserDirs(t)
	manager := newTopicStateManager()
	t.Cleanup(manager.close)
	manager.operationContext = func() (context.Context, context.CancelFunc) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		return ctx, cancel
	}
	root := t.TempDir()
	err := manager.setTitle(root, "cancelled", "must not commit", topicTitleSourceManual)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expired mutation budget was ignored: %v", err)
	}
	snapshot, err := manager.snapshot(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := snapshot.Records["cancelled"]; exists {
		t.Fatal("cancelled mutation committed a title")
	}
}
