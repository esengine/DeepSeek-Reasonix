package imageinput

import (
	"context"
	"errors"
	"reasonix/internal/event"
	"testing"
	"time"
)

func TestImageDeadlineReleasesQueueWithoutCaching(t *testing.T) {
	p := &fakeProvider{entered: make(chan struct{}, 2), release: make(chan struct{})}
	s := service(p)
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := s.Understand(ctx, "text/model", []string{"data:image/png;base64,QUFB"}, nil, event.Discard)
		done <- err
	}()
	<-p.entered
	if err := <-done; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if len(s.cached) != 0 {
		t.Fatal("deadline result cached")
	}
	close(p.release)
	if _, err := s.Understand(context.Background(), "text/model", []string{"data:image/png;base64,QUFB"}, nil, event.Discard); err != nil {
		t.Fatal(err)
	}
	if p.calls.Load() != 2 {
		t.Fatal("request after deadline reused stale result")
	}
}

func TestWaitingImageCancellationLeavesActiveRequestRunning(t *testing.T) {
	p := &fakeProvider{entered: make(chan struct{}, 2), release: make(chan struct{})}
	s := service(p)
	done := make(chan error, 1)
	go func() {
		_, err := s.Understand(context.Background(), "text/model", []string{"data:image/png;base64,QUFB"}, nil, event.Discard)
		done <- err
	}()
	<-p.entered
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	if _, err := s.Understand(ctx, "text/model", []string{"data:image/png;base64,QUFB"}, nil, event.Discard); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	close(p.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if p.calls.Load() != 1 {
		t.Fatal("waiting cancellation dispatched another request")
	}
}
