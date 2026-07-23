package remote

import (
	"context"
	"math/rand"
	"sync"
	"testing"
	"time"

	"reasonix/internal/remote/sshtest"
)

func deterministicRand() *rand.Rand { return rand.New(rand.NewSource(1)) }

// fakeClock is a controllable Clock. After() channels fire when advance() moves
// past their deadline.
type fakeClock struct {
	mu      sync.Mutex
	now     time.Time
	waiters []fakeWaiter
}

type fakeWaiter struct {
	at time.Time
	ch chan time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Unix(1_700_000_000, 0)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan time.Time, 1)
	if d <= 0 {
		ch <- c.now
		return ch
	}
	c.waiters = append(c.waiters, fakeWaiter{at: c.now.Add(d), ch: ch})
	return ch
}

// advance moves time forward, firing any waiters whose deadline is reached.
func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	now := c.now
	var remaining []fakeWaiter
	var fire []chan time.Time
	for _, w := range c.waiters {
		if !w.at.After(now) {
			fire = append(fire, w.ch)
		} else {
			remaining = append(remaining, w)
		}
	}
	c.waiters = remaining
	c.mu.Unlock()
	for _, ch := range fire {
		ch <- now
	}
}

func (c *fakeClock) pendingWaiters() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.waiters)
}

// TestReconnectAfterConnectionDrop verifies the supervisor detects a dropped
// connection and reconnects, emitting Connecting -> Connected -> Reconnecting
// -> Connected.
func TestReconnectAfterConnectionDrop(t *testing.T) {
	srv := sshtest.Start(t, sshtest.Options{Password: "x"})

	var mu sync.Mutex
	var states []Status
	host, _ := ResolveHost(nil, "test@"+srv.Addr, nil)
	c, err := New(Options{
		Host:     host,
		HostKeys: managedOnlyPolicy(t, true),
		Auth:     AuthOptions{DisableAgent: true, Password: func() (string, error) { return "x", nil }},
		// Real clock here: we rely on the actual keepalive to notice the drop
		// quickly, so keep intervals short.
		Keepalive: KeepalivePolicy{Interval: 50 * time.Millisecond, MaxMisses: 1, Timeout: 200 * time.Millisecond},
		Backoff:   BackoffPolicy{Initial: 10 * time.Millisecond, Max: 50 * time.Millisecond},
	})
	if err != nil {
		t.Fatal(err)
	}
	reconnected := make(chan struct{}, 1)
	connectedCount := 0
	c.Subscribe(func(ev StatusEvent) {
		mu.Lock()
		states = append(states, ev.Status)
		if ev.Status == StatusConnected {
			connectedCount++
			if connectedCount == 2 {
				select {
				case reconnected <- struct{}{}:
				default:
				}
			}
		}
		mu.Unlock()
	})

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer c.Close()

	// Drop every server-side connection to force a reconnect.
	srv.DropConnections()

	select {
	case <-reconnected:
	case <-time.After(10 * time.Second):
		t.Fatalf("never reconnected; states=%v", snapshot(&mu, &states))
	}

	got := snapshot(&mu, &states)
	if !containsStatus(got, StatusReconnecting) {
		t.Fatalf("no Reconnecting status observed: %v", got)
	}
}

// TestBackoffUsesClock drives the backoff purely through the fake clock: a
// failed first reconnect must wait on clock.After before retrying.
func TestBackoffSleepHonorsContextCancel(t *testing.T) {
	clock := newFakeClock()
	c := &Client{
		opts:  Options{Backoff: BackoffPolicy{Initial: time.Second, Max: 10 * time.Second}},
		clock: clock,
		rng:   deterministicRand(),
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan bool, 1)
	go func() { done <- c.sleepBackoff(ctx, 1) }()

	// Wait until the sleeper registers its waiter, then cancel.
	waitForWaiters(t, clock, 1)
	cancel()
	select {
	case ok := <-done:
		if ok {
			t.Fatal("sleepBackoff returned true after ctx cancel")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("sleepBackoff did not return after ctx cancel")
	}
}

func TestBackoffSleepFiresOnClock(t *testing.T) {
	clock := newFakeClock()
	c := &Client{
		opts:  Options{Backoff: BackoffPolicy{Initial: time.Second, Max: 10 * time.Second}},
		clock: clock,
		rng:   deterministicRand(),
	}
	done := make(chan bool, 1)
	go func() { done <- c.sleepBackoff(context.Background(), 1) }()
	waitForWaiters(t, clock, 1)
	clock.advance(2 * time.Second) // past any ceiling in [0, 1s]
	select {
	case ok := <-done:
		if !ok {
			t.Fatal("sleepBackoff returned false without cancel")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("sleepBackoff never fired on clock advance")
	}
}

func snapshot(mu *sync.Mutex, s *[]Status) []Status {
	mu.Lock()
	defer mu.Unlock()
	out := make([]Status, len(*s))
	copy(out, *s)
	return out
}

func containsStatus(states []Status, want Status) bool {
	for _, s := range states {
		if s == want {
			return true
		}
	}
	return false
}

func waitForWaiters(t *testing.T, c *fakeClock, n int) {
	t.Helper()
	for i := 0; i < 200; i++ {
		if c.pendingWaiters() >= n {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("clock never registered %d waiter(s)", n)
}
