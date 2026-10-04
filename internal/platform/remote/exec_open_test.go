package remote

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeExecSession struct {
	closed     chan struct{}
	closeOnce  sync.Once
	closeCount atomic.Int32
}

func newFakeExecSession() *fakeExecSession {
	return &fakeExecSession{closed: make(chan struct{})}
}

func (s *fakeExecSession) Run(string) error           { return nil }
func (s *fakeExecSession) capture(_, _ *bytes.Buffer) {}
func (s *fakeExecSession) Close() error {
	s.closeCount.Add(1)
	s.closeOnce.Do(func() { close(s.closed) })
	return nil
}

type recordingSessionClient struct {
	newSession func() (execSession, error)
	closeCount atomic.Int32
}

func (c *recordingSessionClient) NewSession() (execSession, error) {
	return c.newSession()
}

func (c *recordingSessionClient) Close() error {
	c.closeCount.Add(1)
	return nil
}

type blockingSessionClient struct {
	started    chan struct{}
	closed     chan struct{}
	closeOnce  sync.Once
	closeCount atomic.Int32
}

func (c *blockingSessionClient) NewSession() (execSession, error) {
	close(c.started)
	<-c.closed
	return nil, errors.New("session client closed")
}

func (c *blockingSessionClient) Close() error {
	c.closeCount.Add(1)
	c.closeOnce.Do(func() { close(c.closed) })
	return nil
}

func TestOpenExecSessionDoesNotOpenOrCloseClientWhenContextAlreadyDone(t *testing.T) {
	var opened atomic.Int32
	client := &recordingSessionClient{newSession: func() (execSession, error) {
		opened.Add(1)
		return newFakeExecSession(), nil
	}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, stopClose, err := openExecSession(ctx, client, client.NewSession)
	defer stopClose()
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("openExecSession error = %v, want context.Canceled", err)
	}
	if got := opened.Load(); got != 0 {
		t.Fatalf("NewSession calls = %d, want 0", got)
	}
	if got := client.closeCount.Load(); got != 0 {
		t.Fatalf("client Close calls = %d, want 0", got)
	}
}

func TestOpenExecSessionCancellationUnblocksNewSession(t *testing.T) {
	client := &blockingSessionClient{
		started: make(chan struct{}),
		closed:  make(chan struct{}),
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, stopClose, err := openExecSession(ctx, client, client.NewSession)
		if stopClose != nil {
			stopClose()
		}
		done <- err
	}()

	select {
	case <-client.started:
	case <-time.After(2 * time.Second):
		t.Fatal("NewSession was not called")
	}
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("openExecSession error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancellation did not unblock NewSession")
	}
	if got := client.closeCount.Load(); got == 0 {
		t.Fatal("client was not closed to unblock an in-flight NewSession")
	}
}

func TestOpenExecSessionReadyCancellationClosesSessionNotClient(t *testing.T) {
	session := newFakeExecSession()
	client := &recordingSessionClient{newSession: func() (execSession, error) {
		return session, nil
	}}
	ctx, cancel := context.WithCancel(context.Background())

	opened, stopClose, err := openExecSession(ctx, client, client.NewSession)
	defer stopClose()
	if err != nil {
		t.Fatalf("openExecSession: %v", err)
	}
	if opened != session {
		t.Fatal("openExecSession returned a different session")
	}

	cancel()
	select {
	case <-session.closed:
	case <-time.After(2 * time.Second):
		t.Fatal("ready session was not closed after cancellation")
	}
	if got := client.closeCount.Load(); got != 0 {
		t.Fatalf("client Close calls = %d, want 0 after the session became ready", got)
	}
}
