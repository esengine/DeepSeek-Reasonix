package billing

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"
)

// How long one answer serves, and how long a stale one keeps standing in after
// the endpoint starts failing. A wallet moves on the order of minutes; a status
// line is read several times a second.
const (
	freshFor = 30 * time.Second
	standsIn = 5 * time.Minute
)

// Cache is one wallet endpoint, answered from memory. Without it every status
// read is a network round trip: the desktop polls status four times a second
// while a turn runs, and that poll is what the user waits on when opening a
// conversation.
type Cache struct {
	client *http.Client
	url    string
	key    string

	mu       sync.Mutex
	state    cacheState
	inflight chan struct{}
}

// cacheState is what one fetch left behind. Grouped rather than spread across
// the struct: the three move together and only ever mean anything together.
type cacheState struct {
	tried   time.Time // last attempt, successful or not — this is the throttle
	got     time.Time // last success
	balance *Balance
	err     error
}

// NewCache returns a cache for url; an empty url yields a cache that answers
// (nil, nil) without ever reaching the network.
func NewCache(client *http.Client, url, key string) *Cache {
	return &Cache{client: client, url: strings.TrimSpace(url), key: key}
}

// Store hands the same cache to everyone reading one wallet. Opening a
// conversation builds a whole runtime, so without this every switch starts a
// cold cache and pays the round trip again — and the accounts are what separate
// them, not the panes: two panes on one provider read one wallet.
type Store struct {
	mu sync.Mutex
	by map[string]*Cache
}

// Cache returns the shared cache for this endpoint and credential. A nil Store
// answers with a private cache, so a caller with no store still works.
func (s *Store) Cache(client *http.Client, url, key string) *Cache {
	if s == nil {
		return NewCache(client, url, key)
	}
	id := strings.TrimSpace(url) + "\x00" + key
	s.mu.Lock()
	defer s.mu.Unlock()
	if held, ok := s.by[id]; ok {
		return held
	}
	made := NewCache(client, url, key)
	if s.by == nil {
		s.by = map[string]*Cache{}
	}
	s.by[id] = made
	return made
}

// Reading is one wallet read as a reader has to see it: what the wallet said,
// when that was obtained, and which failure it was when it could not be.
// Configured separates "this provider has no wallet" from "the wallet could not
// be read" — opposite answers to why there is no number on screen.
type Reading struct {
	Configured bool
	Balance    *Balance
	At         time.Time
	Err        error
}

// Stale reports a value standing in past its freshness: still the last thing
// the wallet said, no longer what it says now.
func (r Reading) Stale() bool { return r.Balance != nil && time.Since(r.At) > freshFor }

// Read answers from memory while the last answer is fresh, and lets exactly one
// caller refresh it when it is not. A failure does not blank a readout that was
// good a moment ago — the endpoint being briefly unreachable is not the wallet
// being empty.
func (c *Cache) Read(ctx context.Context) Reading {
	if c == nil || c.url == "" {
		return Reading{}
	}
	for {
		c.mu.Lock()
		if time.Since(c.state.tried) < freshFor {
			answer := c.state.answer()
			c.mu.Unlock()
			return answer
		}
		if wait := c.inflight; wait != nil {
			c.mu.Unlock()
			select {
			case <-wait:
				continue
			case <-ctx.Done():
				return Reading{Configured: true, Err: ctx.Err()}
			}
		}
		done := make(chan struct{})
		c.inflight = done
		c.mu.Unlock()

		balance, err := FetchWithClient(ctx, c.client, c.url, c.key)

		c.mu.Lock()
		c.state.tried = time.Now()
		if err == nil {
			c.state.balance, c.state.got, c.state.err = balance, c.state.tried, nil
		} else {
			c.state.err = err
		}
		answer := c.state.answer()
		c.inflight = nil
		close(done)
		c.mu.Unlock()
		return answer
	}
}

// answer picks between the last good balance and the last error. Caller holds mu.
func (s *cacheState) answer() Reading {
	if s.balance != nil && time.Since(s.got) <= standsIn {
		return Reading{Configured: true, Balance: s.balance, At: s.got}
	}
	err := s.err
	// A caller that sees no balance is owed a reason it can act on, and only the
	// producer knows which one; leaving it nil moves that job to the reader.
	if err == nil {
		err = ErrUnreachable
	}
	return Reading{Configured: true, Err: err}
}
