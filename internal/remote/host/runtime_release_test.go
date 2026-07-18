package host

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/remote/protocol"
)

func TestSubscribeAndIdleReleaseAreActorOrdered(t *testing.T) {
	manager, _ := newTestRuntimeManager(t, context.Background(), 16, 64)
	const attempts = 100
	for attempt := 0; attempt < attempts; attempt++ {
		runtime, err := manager.GetOrCreate(testTarget())
		if err != nil {
			t.Fatal(err)
		}
		attachment := testAttachment(uint64(attempt + 1))
		start := make(chan struct{})
		type subscribeResult struct {
			subscription Subscription
			err          error
		}
		subscribeDone := make(chan subscribeResult, 1)
		releaseDone := make(chan struct {
			disposition protocol.SessionCloseDisposition
			err         error
		}, 1)
		go func() {
			<-start
			subscription, subscribeErr := runtime.Subscribe(context.Background(), attachment, "")
			subscribeDone <- subscribeResult{subscription: subscription, err: subscribeErr}
		}()
		go func() {
			<-start
			disposition, releaseErr := manager.ReleaseIdleSession(testTarget())
			releaseDone <- struct {
				disposition protocol.SessionCloseDisposition
				err         error
			}{disposition: disposition, err: releaseErr}
		}()
		close(start)
		subscribed := <-subscribeDone
		released := <-releaseDone
		if released.err != nil {
			t.Fatalf("attempt %d release: %v", attempt, released.err)
		}

		switch released.disposition {
		case protocol.SessionReleased:
			if !errors.Is(subscribed.err, ErrRuntimeClosed) {
				t.Fatalf("attempt %d: release won but Subscribe error = %v", attempt, subscribed.err)
			}
			if _, exists := manager.Runtime(testTarget()); exists {
				t.Fatalf("attempt %d: released runtime remained registered", attempt)
			}
		case protocol.SessionRetainedActive:
			if subscribed.err != nil {
				t.Fatalf("attempt %d: Subscribe won but returned %v", attempt, subscribed.err)
			}
			if current, exists := manager.Runtime(testTarget()); !exists || current != runtime {
				t.Fatalf("attempt %d: subscribed runtime was not retained", attempt)
			}
			if err := runtime.Unsubscribe(context.Background(), attachment, subscribed.subscription.ID); err != nil {
				t.Fatalf("attempt %d unsubscribe: %v", attempt, err)
			}
			if disposition, err := manager.ReleaseIdleSession(testTarget()); err != nil || disposition != protocol.SessionReleased {
				t.Fatalf("attempt %d cleanup release = %q, %v", attempt, disposition, err)
			}
		default:
			t.Fatalf("attempt %d unexpected release disposition %q", attempt, released.disposition)
		}
	}
}

func TestSubmitAndIdleReleaseAreActorOrdered(t *testing.T) {
	manager, factory := newTestRuntimeManager(t, context.Background(), 16, 64)
	const attempts = 100
	for attempt := 0; attempt < attempts; attempt++ {
		runtime, err := manager.GetOrCreate(testTarget())
		if err != nil {
			t.Fatal(err)
		}
		controller := factory.controller(attempt)
		start := make(chan struct{})
		type submitResult struct {
			result SubmitResult
			err    error
		}
		submitDone := make(chan submitResult, 1)
		releaseDone := make(chan struct {
			disposition protocol.SessionCloseDisposition
			err         error
		}, 1)
		go func() {
			<-start
			result, submitErr := runtime.Submit(context.Background(), fmt.Sprintf("turn-%d", attempt))
			submitDone <- submitResult{result: result, err: submitErr}
		}()
		go func() {
			<-start
			disposition, releaseErr := manager.ReleaseIdleSession(testTarget())
			releaseDone <- struct {
				disposition protocol.SessionCloseDisposition
				err         error
			}{disposition: disposition, err: releaseErr}
		}()
		close(start)
		submitted := <-submitDone
		released := <-releaseDone
		if released.err != nil {
			t.Fatalf("attempt %d release: %v", attempt, released.err)
		}

		switch released.disposition {
		case protocol.SessionReleased:
			if !errors.Is(submitted.err, ErrRuntimeClosed) || submitted.result != (SubmitResult{}) {
				t.Fatalf("attempt %d: release won but Submit = %+v, %v", attempt, submitted.result, submitted.err)
			}
		case protocol.SessionRetainedActive:
			if submitted.err != nil || submitted.result.TurnID == "" {
				t.Fatalf("attempt %d: Submit won but result = %+v, %v", attempt, submitted.result, submitted.err)
			}
			controller.releaseTurn()
			select {
			case <-controller.finished:
			case <-time.After(2 * time.Second):
				t.Fatalf("attempt %d retained turn did not finish", attempt)
			}
			if _, err := runtime.Snapshot(context.Background()); err != nil {
				t.Fatalf("attempt %d completion barrier: %v", attempt, err)
			}
			if disposition, err := manager.ReleaseIdleSession(testTarget()); err != nil || disposition != protocol.SessionReleased {
				t.Fatalf("attempt %d cleanup release = %q, %v", attempt, disposition, err)
			}
		default:
			t.Fatalf("attempt %d unexpected release disposition %q", attempt, released.disposition)
		}
	}
}

func TestWorkspaceReleaseIsAllOrNothingAcrossEveryIdleAxis(t *testing.T) {
	manager, factory := newTestRuntimeManager(t, context.Background(), 16, 64)
	targetA := protocol.RuntimeTarget{WorkspaceID: "workspace-release", SessionID: "session-a"}
	targetB := protocol.RuntimeTarget{WorkspaceID: "workspace-release", SessionID: "session-b"}
	targetOther := protocol.RuntimeTarget{WorkspaceID: "workspace-other", SessionID: "session-c"}
	runtimeA, err := manager.GetOrCreate(targetA)
	if err != nil {
		t.Fatal(err)
	}
	runtimeB, err := manager.GetOrCreate(targetB)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.GetOrCreate(targetOther); err != nil {
		t.Fatal(err)
	}

	for _, target := range []protocol.RuntimeTarget{targetA, targetB, targetOther} {
		summary, exists := manager.SessionSummary(target)
		if !exists || summary.PendingPrompt || summary.ActiveJobs != 0 || summary.Running {
			t.Fatalf("initial summary for %+v = %+v, exists=%v", target, summary, exists)
		}
	}

	attachment := testAttachment(1)
	subscription, err := runtimeB.Subscribe(context.Background(), attachment, "")
	if err != nil {
		t.Fatal(err)
	}
	if !manager.WorkspaceInUse(targetA.WorkspaceID) {
		t.Fatal("workspace with a subscription reported idle")
	}
	manager.ReleaseIdleWorkspace(targetA.WorkspaceID)
	requireSameRuntime(t, manager, targetA, runtimeA)
	requireSameRuntime(t, manager, targetB, runtimeB)
	if closeCalls, _ := factory.controller(0).counts(); closeCalls != 0 {
		t.Fatalf("idle sibling was partially released: Close calls=%d", closeCalls)
	}
	if err := runtimeB.Unsubscribe(context.Background(), attachment, subscription.ID); err != nil {
		t.Fatal(err)
	}

	factory.controller(0).emit(event.Event{Kind: event.ApprovalRequest, Approval: event.Approval{
		ID: "controller-prompt-release", Tool: "bash", Subject: "go test ./...",
	}})
	if snapshot, err := runtimeA.Snapshot(context.Background()); err != nil || snapshot.PendingPrompt == nil {
		t.Fatalf("pending Prompt registration = %+v, %v", snapshot.PendingPrompt, err)
	}
	if !manager.WorkspaceInUse(targetA.WorkspaceID) {
		t.Fatal("workspace with an exact pending Prompt reported idle")
	}
	manager.ReleaseIdleWorkspace(targetA.WorkspaceID)
	requireSameRuntime(t, manager, targetA, runtimeA)
	requireSameRuntime(t, manager, targetB, runtimeB)
	factory.controller(0).emit(event.Event{Kind: event.TurnDone})
	if snapshot, err := runtimeA.Snapshot(context.Background()); err != nil || snapshot.PendingPrompt != nil {
		t.Fatalf("TurnDone did not invalidate pending Prompt: %+v, %v", snapshot.PendingPrompt, err)
	}

	if err := runtimeB.setActiveJobsState(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if !manager.WorkspaceInUse(targetA.WorkspaceID) {
		t.Fatal("workspace with an exact active job reported idle")
	}
	manager.ReleaseIdleWorkspace(targetA.WorkspaceID)
	requireSameRuntime(t, manager, targetA, runtimeA)
	requireSameRuntime(t, manager, targetB, runtimeB)
	if err := runtimeB.setActiveJobsState(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	if err := runtimeB.setActiveJobsState(context.Background(), -1); err == nil {
		t.Fatal("negative active job state was accepted")
	}

	if manager.WorkspaceInUse(targetA.WorkspaceID) {
		t.Fatal("fully idle workspace reported in use")
	}
	manager.ReleaseIdleWorkspace(targetA.WorkspaceID)
	if _, exists := manager.Runtime(targetA); exists {
		t.Fatal("idle runtime A was not released")
	}
	if _, exists := manager.Runtime(targetB); exists {
		t.Fatal("idle runtime B was not released")
	}
	if _, exists := manager.Runtime(targetOther); !exists {
		t.Fatal("another workspace runtime was released")
	}
	for index := 0; index < 2; index++ {
		if closeCalls, _ := factory.controller(index).counts(); closeCalls != 1 {
			t.Fatalf("workspace Controller %d Close calls = %d, want 1", index, closeCalls)
		}
	}
	if closeCalls, _ := factory.controller(2).counts(); closeCalls != 0 {
		t.Fatalf("other workspace Controller Close calls = %d", closeCalls)
	}
}

func TestReleasedRuntimeRecreationChangesEpochAndDropsLateEvents(t *testing.T) {
	manager, factory := newTestRuntimeManager(t, context.Background(), 16, 64)
	first, err := manager.GetOrCreate(testTarget())
	if err != nil {
		t.Fatal(err)
	}
	firstController := factory.controller(0)
	firstController.emit(event.Event{Kind: event.Notice, Text: "before-release"})
	if snapshot, err := first.Snapshot(context.Background()); err != nil || snapshot.BoundarySeq != 1 {
		t.Fatalf("first snapshot = %+v, %v", snapshot, err)
	}
	if disposition, err := manager.ReleaseIdleSession(testTarget()); err != nil || disposition != protocol.SessionReleased {
		t.Fatalf("first release = %q, %v", disposition, err)
	}
	if closeCalls, _ := firstController.counts(); closeCalls != 1 {
		t.Fatalf("released Controller Close calls = %d, want 1", closeCalls)
	}

	firstController.emit(event.Event{Kind: event.Notice, Text: "late-old-event"})
	replacement, err := manager.GetOrCreate(testTarget())
	if err != nil {
		t.Fatal(err)
	}
	if replacement == first || replacement.Epoch() == first.Epoch() {
		t.Fatalf("recreated runtime did not replace epoch %q", first.Epoch())
	}
	snapshot, err := replacement.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.BoundarySeq != 0 || len(snapshot.Events) != 0 {
		t.Fatalf("late old event entered recreated runtime: %+v", snapshot)
	}
	summary, exists := manager.SessionSummary(testTarget())
	if !exists || summary.RuntimeEpoch != replacement.Epoch() {
		t.Fatalf("replacement summary = %+v, exists=%v", summary, exists)
	}
	if disposition, err := manager.ReleaseIdleSession(testTarget()); err != nil || disposition != protocol.SessionReleased {
		t.Fatalf("replacement release = %q, %v", disposition, err)
	}
	if disposition, err := manager.ReleaseIdleSession(testTarget()); err != nil || disposition != protocol.SessionAlreadyClosed {
		t.Fatalf("already closed release = %q, %v", disposition, err)
	}
	if summary, exists := manager.SessionSummary(testTarget()); exists || summary != nil {
		t.Fatalf("released Session summary = %+v, exists=%v", summary, exists)
	}
}

func requireSameRuntime(t *testing.T, manager *RuntimeManager, target protocol.RuntimeTarget, want *SessionRuntime) {
	t.Helper()
	got, exists := manager.Runtime(target)
	if !exists || got != want {
		t.Fatalf("runtime %+v = %p, exists=%v; want %p", target, got, exists, want)
	}
}
