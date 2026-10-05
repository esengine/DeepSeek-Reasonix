package billing

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

const cacheWalletBody = `{"is_available":true,"balance_infos":[{"currency":"CNY","total_balance":"10.00","granted_balance":"0","topped_up_balance":"10.00"}]}`

func cacheStub(t *testing.T, hits *atomic.Int64, fail *atomic.Bool) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		if fail != nil && fail.Load() {
			http.Error(w, "down", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, cacheWalletBody)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// expire ages the cache past its freshness window without waiting for it. Both
// clocks move: real time ages the last attempt and the value it produced alike.
func expire(c *Cache) {
	c.mu.Lock()
	c.state.tried = c.state.tried.Add(-freshFor - time.Second)
	c.state.got = c.state.got.Add(-freshFor - time.Second)
	c.mu.Unlock()
}

func TestCacheAnswersFromMemoryWhileFresh(t *testing.T) {
	var hits atomic.Int64
	srv := cacheStub(t, &hits, nil)
	c := NewCache(nil, srv.URL, "")
	for range 10 {
		if r := c.Read(context.Background()); r.Balance == nil {
			t.Fatalf("Read = %+v", r)
		}
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("fetches = %d, want 1", got)
	}
}

func TestCacheRefetchesAfterFreshness(t *testing.T) {
	var hits atomic.Int64
	srv := cacheStub(t, &hits, nil)
	c := NewCache(nil, srv.URL, "")
	if r := c.Read(context.Background()); r.Err != nil {
		t.Fatal(r.Err)
	}
	expire(c)
	if r := c.Read(context.Background()); r.Err != nil {
		t.Fatal(r.Err)
	}
	if got := hits.Load(); got != 2 {
		t.Fatalf("fetches = %d, want 2", got)
	}
}

// A wallet that answered a moment ago beats a blank readout: the endpoint being
// briefly unreachable is not the account being empty.
func TestCacheStandsInForAFailingWallet(t *testing.T) {
	var hits atomic.Int64
	var fail atomic.Bool
	srv := cacheStub(t, &hits, &fail)
	c := NewCache(nil, srv.URL, "")
	if r := c.Read(context.Background()); r.Err != nil {
		t.Fatal(r.Err)
	}
	fail.Store(true)
	expire(c)
	stood := c.Read(context.Background())
	if stood.Err != nil || stood.Balance == nil {
		t.Fatalf("a failing wallet blanked the readout: %+v", stood)
	}
	// The value stands in, and says it is standing in rather than looking current.
	if !stood.Stale() {
		t.Fatal("a stood-in value reported itself as fresh")
	}

	// Past the stand-in window the error is the answer, not a stale balance.
	c.mu.Lock()
	c.state.got = c.state.got.Add(-standsIn - time.Second)
	c.mu.Unlock()
	expire(c)
	if r := c.Read(context.Background()); r.Err == nil || r.Balance != nil {
		t.Fatalf("stale balance outlived its window: %+v", r)
	}
}

// A first fetch that fails has nothing to stand in for, so it reports.
func TestCacheReportsFirstFailure(t *testing.T) {
	var hits atomic.Int64
	var fail atomic.Bool
	fail.Store(true)
	srv := cacheStub(t, &hits, &fail)
	c := NewCache(nil, srv.URL, "")
	first := c.Read(context.Background())
	if first.Err == nil || first.Balance != nil {
		t.Fatalf("Read = %+v; want an error", first)
	}
	if !first.Configured {
		t.Fatal("a wallet that failed read back as one that was never configured")
	}
	// Even a failure is throttled — a broken endpoint must not be retried on
	// every poll.
	if r := c.Read(context.Background()); r.Err == nil {
		t.Fatal("second Read lost the error")
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("fetches = %d, want 1", got)
	}
}

func TestCacheWithoutURLNeverFetches(t *testing.T) {
	c := NewCache(nil, "  ", "")
	r := c.Read(context.Background())
	if r.Balance != nil || r.Err != nil {
		t.Fatalf("Read = %+v; want nothing", r)
	}
	// Not configured and not readable are opposite answers to why there is no
	// number, and a surface renders them differently.
	if r.Configured {
		t.Fatal("a provider with no balance_url read back as configured")
	}
}
