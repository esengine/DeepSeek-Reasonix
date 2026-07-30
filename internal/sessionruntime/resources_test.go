package sessionruntime

import (
	"context"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/jobs"
)

func TestResourcesRefcountDefersJobClose(t *testing.T) {
	t.Parallel()
	jm := jobs.NewManager(event.Discard, jobs.WithTeardownGrace(50*time.Millisecond))
	started := make(chan struct{})
	releaseJob := make(chan struct{})
	jm.Start("bash", "hold", func(ctx context.Context, out io.Writer) (string, error) {
		close(started)
		select {
		case <-releaseJob:
			return "ok", nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	})
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("job did not start")
	}

	res := New(Config{Jobs: jm})
	if !res.Retain() {
		t.Fatal("Retain failed")
	}
	if res.Refs() != 2 {
		t.Fatalf("refs = %d, want 2", res.Refs())
	}

	res.Release() // first controller leaves; job must keep running
	select {
	case <-res.Done():
		t.Fatal("resources finalized while a ref remained")
	case <-time.After(30 * time.Millisecond):
	}
	if len(jm.Running()) == 0 {
		t.Fatal("background job cancelled when a shared ref still held")
	}

	close(releaseJob)
	res.Release() // last ref
	select {
	case <-res.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("resources did not finalize after last release")
	}
	if !res.Closed() {
		t.Fatal("resources should be closed")
	}
	// Second release is a no-op.
	res.Release()
}

func TestResourcesRetainFailsAfterClose(t *testing.T) {
	t.Parallel()
	jm := jobs.NewManager(event.Discard, jobs.WithTeardownGrace(20*time.Millisecond))
	res := WrapJobs(jm)
	res.Release()
	select {
	case <-res.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("wrap-jobs resources never finalized")
	}
	if res.Retain() {
		t.Fatal("Retain after close should fail")
	}
}

func TestResourcesReleaseKeepsTeardownGraceBounded(t *testing.T) {
	t.Parallel()
	jm := jobs.NewManager(event.Discard, jobs.WithTeardownGrace(20*time.Millisecond))
	started := make(chan struct{})
	releaseJob := make(chan struct{})
	jm.Start("task", "non-cooperative", func(context.Context, io.Writer) (string, error) {
		close(started)
		<-releaseJob
		return "done", nil
	})
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("job did not start")
	}

	res := WrapJobs(jm)
	released := make(chan struct{})
	go func() {
		res.Release()
		close(released)
	}()
	select {
	case <-released:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Release exceeded the manager teardown grace")
	}
	select {
	case <-res.Done():
		t.Fatal("resources finalized before the non-cooperative job exited")
	default:
	}
	if res.Retain() {
		t.Fatal("closing resources must reject Retain")
	}

	close(releaseJob)
	select {
	case <-res.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("resources did not finalize after the job exited")
	}
}

func TestResourcesCompatibleWith(t *testing.T) {
	t.Parallel()
	res := New(Config{WorkspaceKey: "/ws", RuntimeKey: "delivery", ConfigKey: "cfg-a"})
	if !res.CompatibleWith("/ws", "delivery", "cfg-a") {
		t.Fatal("same keys should be compatible")
	}
	if res.CompatibleWith("/other", "delivery", "cfg-a") {
		t.Fatal("workspace mismatch should fail")
	}
	if res.CompatibleWith("/ws", "economy", "cfg-a") {
		t.Fatal("runtime mismatch should fail")
	}
	if res.CompatibleWith("/ws", "delivery", "cfg-b") {
		t.Fatal("resource configuration mismatch should fail")
	}
	if unkeyed := New(Config{WorkspaceKey: "/ws", RuntimeKey: "delivery"}); unkeyed.CompatibleWith("/ws", "delivery", "cfg-a") {
		t.Fatal("unkeyed resources must not satisfy an explicit configuration key")
	} else {
		unkeyed.Release()
		<-unkeyed.Done()
	}
	if !res.CompatibleWith("", "", "") {
		t.Fatal("empty expected keys should not block")
	}
	res.Release()
	<-res.Done()
	if res.CompatibleWith("/ws", "delivery", "cfg-a") {
		t.Fatal("closed resources must not be compatible")
	}
}

func TestJobsManagerCloseIdempotentAndDone(t *testing.T) {
	t.Parallel()
	jm := jobs.NewManager(event.Discard, jobs.WithTeardownGrace(20*time.Millisecond))
	var ran atomic.Int32
	jm.Start("bash", "quick", func(ctx context.Context, out io.Writer) (string, error) {
		ran.Add(1)
		return "ok", nil
	})
	jm.Close()
	jm.Close() // second close must not panic or hang
	select {
	case <-jm.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("Manager.Done never closed")
	}
	if ran.Load() != 1 {
		t.Fatalf("job runs = %d, want 1", ran.Load())
	}
}
