package imageinput

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

type fakeProvider struct {
	calls   atomic.Int32
	entered chan struct{}
	release chan struct{}
	fail    bool
	seen    chan provider.Request
}

func (*fakeProvider) Name() string { return "vision" }
func (p *fakeProvider) Stream(ctx context.Context, r provider.Request) (<-chan provider.Chunk, error) {
	p.calls.Add(1)
	if p.seen != nil {
		p.seen <- r
	}
	if p.entered != nil {
		p.entered <- struct{}{}
	}
	if p.fail {
		return nil, errors.New("test failure")
	}
	out := make(chan provider.Chunk, 2)
	go func() {
		defer close(out)
		if p.release != nil {
			select {
			case <-ctx.Done():
				return
			case <-p.release:
			}
		}
		out <- provider.Chunk{Type: provider.ChunkText, Text: "left red, right blue, OCR Z7"}
		out <- provider.Chunk{Type: provider.ChunkUsage, Usage: &provider.Usage{}}
	}()
	return out, nil
}
func service(p *fakeProvider) *Service {
	return New(Config{Model: "vision/model", Resolve: func(string) (provider.Provider, error) { return p, nil }})
}
func TestSerializedCacheAndCancellation(t *testing.T) {
	p := &fakeProvider{entered: make(chan struct{}, 2), release: make(chan struct{})}
	s := service(p)
	refs := []string{"data:image/png;base64,QUFB"}
	done := make(chan error, 2)
	go func() {
		_, err := s.Understand(context.Background(), "text/model", refs, nil, event.Discard)
		done <- err
	}()
	<-p.entered
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Understand(ctx, "text/model", refs, nil, event.Discard); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled queue: %v", err)
	}
	go func() {
		_, err := s.Understand(context.Background(), "text/model", refs, nil, event.Discard)
		done <- err
	}()
	close(p.release)
	for range 2 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if p.calls.Load() != 1 {
		t.Fatalf("calls %d", p.calls.Load())
	}
	_, err := s.Understand(context.Background(), "text/model", []string{"data:image/png;base64,QkJC"}, nil, event.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if p.calls.Load() != 2 {
		t.Fatal("changed bytes reused stale summary")
	}
}
func TestRestoredCacheAndMutableURL(t *testing.T) {
	p := &fakeProvider{}
	s := service(p)
	refs := []string{"data:image/png;base64,QUFB"}
	v, err := s.Understand(context.Background(), "text/model", refs, nil, event.Discard)
	if err != nil {
		t.Fatal(err)
	}
	restored := service(p)
	got, err := restored.Understand(context.Background(), "text/model", refs, func() []provider.Message { return []provider.Message{{Role: provider.RoleTool, VisionSummary: v}} }, event.Discard)
	if err != nil || got.Summary != v.Summary || p.calls.Load() != 1 {
		t.Fatalf("restored: %v %v", got, err)
	}
	for range 2 {
		_, err = restored.Understand(context.Background(), "text/model", []string{"https://example.test/mutable.png"}, nil, event.Discard)
		if err != nil {
			t.Fatal(err)
		}
	}
	if p.calls.Load() != 3 {
		t.Fatal("URL cache incorrectly reused")
	}
}
func TestSelectionFailuresAndProviderFileIsolation(t *testing.T) {
	p := &fakeProvider{}
	cfg := Config{Model: "auto", Select: func(current, mode string) (string, bool) {
		if current != "text/model" {
			t.Fatal(current)
		}
		return "vision/model", true
	}, Resolve: func(string) (provider.Provider, error) { return p, nil }}
	s := New(cfg)
	if _, err := s.Understand(context.Background(), "text/model", []string{"file-api-abc"}, nil, event.Discard); err == nil {
		t.Fatal("cross-provider file ID accepted")
	}
	cfg.Select = func(string, string) (string, bool) { return "", false }
	if _, err := New(cfg).Understand(context.Background(), "text/model", []string{"data:image/png;base64,QUFB"}, nil, event.Discard); err == nil {
		t.Fatal("missing auto candidate accepted")
	}
	if _, err := New(Config{}).Understand(context.Background(), "text/model", nil, nil, event.Discard); err == nil {
		t.Fatal("disabled accepted")
	}
}
func TestCancelInFlightDoesNotCache(t *testing.T) {
	p := &fakeProvider{entered: make(chan struct{}, 2), release: make(chan struct{})}
	s := service(p)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := s.Understand(ctx, "text/model", []string{"data:image/png;base64,QUFB"}, nil, event.Discard)
		done <- err
	}()
	<-p.entered
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if s.cached != nil {
		t.Fatal("canceled request cached")
	}
}

func TestCachedSummaryDoesNotEmitAdditionalUsage(t *testing.T) {
	p := &fakeProvider{}
	s := service(p)
	var usage atomic.Int32
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.Usage {
			if e.ModelRef != "vision/model" || e.UsageSource != event.UsageSourceClassifier {
				t.Errorf("usage attribution: %+v", e)
			}
			usage.Add(1)
		}
	})
	for range 2 {
		if _, err := s.Understand(context.Background(), "text/model", []string{"data:image/png;base64,QUFB"}, nil, sink); err != nil {
			t.Fatal(err)
		}
	}
	if usage.Load() != 1 {
		t.Fatalf("usage events=%d", usage.Load())
	}
}
