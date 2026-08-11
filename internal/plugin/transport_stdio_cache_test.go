package plugin

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestCachedShellPATHMemoizesNonEmpty(t *testing.T) {
	calls := 0
	probe := cachedShellPATH(func(context.Context) string {
		calls++
		return "/opt/homebrew/bin"
	})
	for range 3 {
		if got := probe(context.Background()); got != "/opt/homebrew/bin" {
			t.Fatalf("probe() = %q", got)
		}
	}
	if calls != 1 {
		t.Errorf("probe ran %d times, want 1 (memoized)", calls)
	}
}

func TestCachedShellPATHMemoizesCompletedEmptyProbe(t *testing.T) {
	calls := 0
	probe := cachedShellPATH(func(context.Context) string {
		calls++
		return ""
	})
	// A probe that ran to completion and found nothing is still an answer: a
	// host without a usable login shell must not re-run the (up to 6s) probe on
	// every stdio spawn.
	for range 3 {
		if got := probe(context.Background()); got != "" {
			t.Fatalf("probe() = %q, want empty", got)
		}
	}
	if calls != 1 {
		t.Errorf("probe ran %d times, want 1 (empty result memoized)", calls)
	}
}

func TestCachedShellPATHRetriesAfterCancelledProbe(t *testing.T) {
	calls := 0
	probe := cachedShellPATH(func(ctx context.Context) string {
		calls++
		if ctx.Err() != nil {
			return ""
		}
		return "/usr/local/bin"
	})

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if got := probe(cancelled); got != "" {
		t.Fatalf("probe(cancelled) = %q, want empty", got)
	}
	// The empty result came from the aborted caller, not the host, so it must
	// not have been cached: a fresh context probes again.
	if got := probe(context.Background()); got != "/usr/local/bin" {
		t.Fatalf("probe() after cancelled attempt = %q, want /usr/local/bin", got)
	}
	if calls != 2 {
		t.Errorf("probe ran %d times, want 2", calls)
	}
}

func TestCachedShellPATHConcurrentSpawnsShareOneProbe(t *testing.T) {
	var (
		mu      sync.Mutex
		calls   int
		started = make(chan struct{})
		release = make(chan struct{})
	)
	probe := cachedShellPATH(func(context.Context) string {
		mu.Lock()
		calls++
		if calls == 1 {
			close(started)
		}
		mu.Unlock()
		<-release
		return "/opt/homebrew/bin"
	})

	const n = 8
	results := make(chan string, n)
	for range n {
		go func() { results <- probe(context.Background()) }()
	}

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("no probe started")
	}
	// Give the remaining goroutines a moment to reach the cache; they must park
	// on the in-flight probe rather than each spawning login shells of their own.
	time.Sleep(50 * time.Millisecond)
	close(release)

	for i := range n {
		select {
		case got := <-results:
			if got != "/opt/homebrew/bin" {
				t.Fatalf("result %d = %q", i, got)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("probe call did not return; waiters stuck behind the in-flight probe")
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if calls != 1 {
		t.Errorf("probe ran %d times, want 1 (shared in-flight probe)", calls)
	}
}

func TestCachedShellPATHWaiterUnblocksOnItsOwnCancel(t *testing.T) {
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	probe := cachedShellPATH(func(context.Context) string {
		started <- struct{}{}
		<-release
		return ""
	})

	go probe(context.Background())
	select {
	case <-started: // the background call now owns the in-flight probe
	case <-time.After(5 * time.Second):
		t.Fatal("probe never started")
	}

	waiterCtx, cancel := context.WithCancel(context.Background())
	done := make(chan string, 1)
	go func() { done <- probe(waiterCtx) }()

	time.Sleep(50 * time.Millisecond) // let the waiter park on the in-flight probe
	cancel()

	select {
	case got := <-done:
		if got != "" {
			t.Fatalf("cancelled waiter = %q, want empty", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled waiter stayed blocked on another spawn's probe")
	}
}
