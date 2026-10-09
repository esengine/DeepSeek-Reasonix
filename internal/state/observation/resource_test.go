package observation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/state/trustedstate"
)

type delayedStore struct {
	Store
	active atomic.Int64
	peak   atomic.Int64
	calls  atomic.Int64
	cancel context.CancelFunc
}

type memoryStore struct {
	mu      sync.Mutex
	objects map[trustedstate.Digest][]byte
}

func (s *memoryStore) PutIndexObject(b []byte) (trustedstate.Digest, error) {
	d := trustedstate.DigestOf(b)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.objects[d] = append([]byte(nil), b...)
	return d, nil
}

func (s *memoryStore) Object(d trustedstate.Digest) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.objects[d]
	if !ok {
		return nil, trustedstate.ErrNotFound
	}
	return append([]byte(nil), b...), nil
}

func (s *delayedStore) PutIndexObject(b []byte) (trustedstate.Digest, error) {
	s.calls.Add(1)
	n := s.active.Add(1)
	defer s.active.Add(-1)
	for old := s.peak.Load(); n > old; old = s.peak.Load() {
		if s.peak.CompareAndSwap(old, n) {
			break
		}
	}
	if s.cancel != nil {
		time.Sleep(10 * time.Millisecond)
		s.cancel()
	}
	time.Sleep(2 * time.Millisecond)
	return s.Store.PutIndexObject(b)
}

func resourceTree(t *testing.T, width, depth int) string {
	t.Helper()
	root := t.TempDir()
	for i := range width {
		path := filepath.Join(root, fmt.Sprintf("d%04d", i))
		for range depth {
			path = filepath.Join(path, "child")
		}
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, fmt.Sprintf("f%04d", i)), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestSnapshotResourceBound(t *testing.T) {
	root := resourceTree(t, 512, 4)
	s := &delayedStore{Store: &memoryStore{objects: make(map[trustedstate.Digest][]byte)}}
	o := NewObserver(s, DefaultPolicy())
	baseline := runtime.NumGoroutine()
	done := make(chan Snapshot, 1)
	go func() { done <- o.Take(t.Context(), root) }()
	peak := baseline
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case snap := <-done:
			if !snap.Complete || snap.Entries != 3072 {
				t.Fatalf("snapshot = %+v", snap)
			}
			if peak-baseline > readers+2 || s.peak.Load() > readers {
				t.Fatalf("peak goroutine growth = %d, concurrent puts = %d; limit = %d", peak-baseline, s.peak.Load(), readers)
			}
			if want := referenceTreeDigest(t, root); snap.Root != want {
				t.Fatalf("tree digest = %s, sequential reference = %s", snap.Root, want)
			}
			return
		case <-tick.C:
			peak = max(peak, runtime.NumGoroutine())
		}
	}
}

func referenceTreeDigest(t *testing.T, path string) string {
	t.Helper()
	entries, err := os.ReadDir(path)
	if err != nil {
		t.Fatal(err)
	}
	nodes := make([]node, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			t.Fatal(err)
		}
		n := node{Name: entry.Name(), Mode: uint32(info.Mode().Perm())}
		switch {
		case info.IsDir():
			n.Kind = "d"
			n.Ref = referenceTreeDigest(t, filepath.Join(path, n.Name))
		case info.Mode().IsRegular():
			n.Kind = "f"
			st := stampOf(info)
			n.Stamp = &st
		default:
			t.Fatalf("unexpected fixture mode: %v", info.Mode())
		}
		nodes = append(nodes, n)
	}
	b, err := json.Marshal(nodes)
	if err != nil {
		t.Fatal(err)
	}
	return string(trustedstate.DigestOf(b))
}

func TestCancellationDuringSnapshotStopsStoreWork(t *testing.T) {
	root := resourceTree(t, 256, 2)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	s := &delayedStore{Store: &memoryStore{objects: make(map[trustedstate.Digest][]byte)}, cancel: cancel}
	o := NewObserver(s, DefaultPolicy())
	snap := o.Take(ctx, root)
	if snap.Complete || snap.Incomplete != IncompleteCancelled {
		t.Fatalf("snapshot = %+v", snap)
	}
	if _, _, err := o.Changed(snap, snap, 10); !errors.Is(err, ErrIncomparable) {
		t.Fatalf("cancelled snapshot compared: %v", err)
	}
	if s.peak.Load() > readers {
		t.Fatalf("concurrent puts after cancellation = %d", s.peak.Load())
	}
	if s.calls.Load() > readers {
		t.Fatalf("store calls after cancellation began = %d, want <= %d in-flight calls", s.calls.Load(), readers)
	}
}

func TestDependencyTreeLimitEstablishesNothing(t *testing.T) {
	root := tree(t, map[string]string{"node_modules/a/x": "a", ".venv/b/x": "b", "target/c/x": "c"})
	o, _ := observer(t)
	complete := o.Take(t.Context(), root)
	if !complete.Complete || complete.Entries != 9 {
		t.Fatalf("dependency trees were excluded: %+v", complete)
	}
	p := DefaultPolicy()
	p.MaxEntries = 4
	o = NewObserver(o.store, p)
	short := o.Take(t.Context(), root)
	if short.Complete || short.Incomplete != IncompleteEntryLimit {
		t.Fatalf("snapshot = %+v", short)
	}
	if _, _, err := o.Changed(short, short, 10); !errors.Is(err, ErrIncomparable) {
		t.Fatalf("over-limit snapshot compared: %v", err)
	}
}

func TestConcurrentIdenticalNodesPublishCompleteSnapshot(t *testing.T) {
	root := t.TempDir()
	for i := range 128 {
		if err := os.Mkdir(filepath.Join(root, fmt.Sprintf("d%04d", i)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	s := &delayedStore{Store: trustedstate.Open(t.TempDir(), nil)}
	snap := NewObserver(s, DefaultPolicy()).Take(t.Context(), root)
	if !snap.Complete || s.peak.Load() > readers {
		t.Fatalf("snapshot = %+v, concurrent identical puts = %d", snap, s.peak.Load())
	}
}

func TestSnapshotCacheIsBoundedAcrossChanges(t *testing.T) {
	root := tree(t, map[string]string{"file": "x"})
	p := DefaultPolicy()
	p.MaxEntries = 4
	o := NewObserver(trustedstate.Open(t.TempDir(), nil), p)
	for i := range 20 {
		write(t, root, "file", string(make([]byte, i)))
		if snap := o.Take(t.Context(), root); !snap.Complete {
			t.Fatalf("snapshot = %+v", snap)
		}
	}
	if len(o.known) > p.MaxEntries {
		t.Fatalf("cached nodes = %d, limit = %d", len(o.known), p.MaxEntries)
	}
}
