package extension

import (
	"context"
	"errors"
	"io"
	"testing"
)

type nopCloser struct{}

func (nopCloser) Close() error { return nil }

type errCloser struct{}

func (errCloser) Close() error { return errors.New("close failed") }

func TestTrackResourceHelpers(t *testing.T) {
	s := NewEffectScope(3)
	if err := TrackMCPClient(s, "demo", nopCloser{}); err != nil {
		t.Fatal(err)
	}
	if err := TrackUIHub(s, 3); err != nil {
		t.Fatal(err)
	}
	cancelled := false
	if err := TrackProviderStream(s, "s1", func() { cancelled = true }, nil); err != nil {
		t.Fatal(err)
	}
	if err := TrackWatcher(s, "w1", func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := TrackEventSubscription(s, "e1", func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := TrackBackgroundJob(s, "j1", func() {}, nil); err != nil {
		t.Fatal(err)
	}
	if err := TrackControllerCleanup(s, "c1", func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := TrackIrreversible(s, "irr-1", "test"); err != nil {
		t.Fatal(err)
	}
	if err := s.Dispose(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !cancelled {
		t.Fatal("expected provider stream cancel")
	}
	if _, ok := DefaultReceiptStore.Get("irr-1"); !ok {
		t.Fatal("expected irreversible receipt")
	}
}

func TestTrackMCPClientNilSafe(t *testing.T) {
	if err := TrackMCPClient(nil, "x", nopCloser{}); err != nil {
		t.Fatal(err)
	}
	var c io.Closer
	s := NewEffectScope(1)
	if err := TrackMCPClient(s, "x", c); err != nil {
		t.Fatal(err)
	}
}

func TestTrackMCPClientDisposeError(t *testing.T) {
	s := NewEffectScope(1)
	_ = TrackMCPClient(s, "bad", errCloser{})
	if err := s.Dispose(context.Background()); err == nil {
		t.Fatal("expected dispose error")
	}
}
