package browserhost

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/extension"
	"reasonix/internal/extension/protocol"
)

type fakeBackend struct {
	mu    sync.Mutex
	list  []protocol.BrowserTab
	open  protocol.BrowserTab
	block chan struct{}
}

func (f *fakeBackend) List(ctx context.Context) ([]protocol.BrowserTab, error) {
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]protocol.BrowserTab(nil), f.list...), nil
}

func (f *fakeBackend) Open(ctx context.Context, p protocol.BrowserTabOpenParams) (protocol.BrowserTab, error) {
	_ = ctx
	f.mu.Lock()
	defer f.mu.Unlock()
	f.open = protocol.BrowserTab{TabID: "t1", URL: p.URL, Title: "ok", Active: true, Generation: 1}
	return f.open, nil
}

func (f *fakeBackend) Snapshot(context.Context, protocol.BrowserTabSnapshotParams) (protocol.BrowserTabSnapshotResult, error) {
	return protocol.BrowserTabSnapshotResult{
		Tab:      protocol.BrowserTab{TabID: "t1", URL: "https://example.com/", Generation: 1},
		Origin:   "https://example.com",
		Snapshot: "root",
	}, nil
}

func (f *fakeBackend) Wait(context.Context, protocol.BrowserTabWaitParams) (protocol.BrowserTab, error) {
	return protocol.BrowserTab{TabID: "t1", URL: "https://example.com/", Generation: 1}, nil
}

func (f *fakeBackend) Act(context.Context, protocol.BrowserTabActParams) (protocol.BrowserTab, error) {
	return protocol.BrowserTab{TabID: "t1", URL: "https://example.com/", Generation: 1}, nil
}

func TestBindingRejectsStaleGeneration(t *testing.T) {
	owner := extension.NewRuntimeOwner()
	owner.Gate.Publish(1)
	b := NewBinding(BindingOptions{
		Backend:    &fakeBackend{list: []protocol.BrowserTab{{TabID: "a"}}},
		Owner:      owner,
		Generation: 2,
		PluginID:   "p",
	})
	_, err := b.List(context.Background(), protocol.BrowserTabListParams{})
	if err == nil {
		t.Fatal("expected stale_generation")
	}
	var pe *protocol.ProtocolError
	if !errors.As(err, &pe) || pe.Reason != protocol.ErrStaleGeneration {
		t.Fatalf("err = %v", err)
	}
}

func TestBindingAllowsPublishedGeneration(t *testing.T) {
	owner := extension.NewRuntimeOwner()
	owner.Gate.Publish(3)
	b := NewBinding(BindingOptions{
		Backend:    &fakeBackend{list: []protocol.BrowserTab{{TabID: "a", URL: "https://x/"}}},
		Owner:      owner,
		Generation: 3,
		PluginID:   "p",
	})
	res, err := b.List(context.Background(), protocol.BrowserTabListParams{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Tabs) != 1 || res.Tabs[0].TabID != "a" {
		t.Fatalf("tabs = %+v", res.Tabs)
	}
}

func TestBindingDisposeCancelsInFlight(t *testing.T) {
	owner := extension.NewRuntimeOwner()
	owner.Gate.Publish(1)
	block := make(chan struct{})
	b := NewBinding(BindingOptions{
		Backend:    &fakeBackend{block: block},
		Owner:      owner,
		Generation: 1,
		PluginID:   "p",
	})
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		close(started)
		_, err := b.List(context.Background(), protocol.BrowserTabListParams{})
		done <- err
	}()
	<-started
	// Barrier: wait until InFlight reflects the blocked call.
	deadline := time.Now().Add(2 * time.Second)
	for b.metrics.InFlight.Load() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("call never became in-flight")
		}
		time.Sleep(5 * time.Millisecond)
	}
	b.Dispose()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected cancel")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("call did not cancel")
	}
}

func TestOpenRecordsReceiptWithoutURL(t *testing.T) {
	owner := extension.NewRuntimeOwner()
	owner.Gate.Publish(1)
	b := NewBinding(BindingOptions{
		Backend:    &fakeBackend{},
		Owner:      owner,
		Generation: 1,
		PluginID:   "p",
	})
	_, err := b.Open(context.Background(), protocol.BrowserTabOpenParams{
		URL: "https://example.com/secret-path", Disposition: protocol.BrowserDispositionForeground,
	})
	if err != nil {
		t.Fatal(err)
	}
	receipts := owner.Receipts.ForGeneration(1)
	if len(receipts) == 0 {
		t.Fatal("expected irreversible receipt")
	}
	for _, r := range receipts {
		if r.Class != extension.Irreversible {
			t.Fatalf("class = %v", r.Class)
		}
		if strings.Contains(r.ID, "example.com") || strings.Contains(r.Error, "example.com") {
			t.Fatalf("receipt leaked URL: %+v", r)
		}
	}
}

func TestCapabilitySchemaHashStable(t *testing.T) {
	hash := SchemaHash()
	if hash == "" {
		t.Fatal("schema hash is empty")
	}
	if next := SchemaHash(); next != hash {
		t.Fatal("schema hash unstable")
	}
	cap := Capability()
	if err := cap.Validate(); err != nil {
		t.Fatal(err)
	}
	if cap.Key.String() != "reasonix/browser/companion" {
		t.Fatalf("key = %s", cap.Key)
	}
}
