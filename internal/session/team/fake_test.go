package team

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/state/teamroster"
)

type fakePane struct {
	mu        sync.Mutex
	got       []Delivery
	keys      map[string]bool
	cancelled int
}

func (p *fakePane) Deliver(d Delivery) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.keys == nil {
		p.keys = map[string]bool{}
	}
	if !p.keys[d.Key] {
		p.keys[d.Key] = true
		p.got = append(p.got, d)
	}
	return nil
}

func (p *fakePane) Cancel() { p.mu.Lock(); p.cancelled++; p.mu.Unlock() }

func (p *fakePane) deliveries() []Delivery {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]Delivery(nil), p.got...)
}

type fakePanes struct {
	mu    sync.Mutex
	open  map[SeatRef]*fakePane
	broke map[SeatRef]bool
}

func (f *fakePanes) Ensure(_ context.Context, seat SeatRef) (Pane, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.broke[seat] {
		return nil, errors.New("runtime would not open")
	}
	if f.open == nil {
		f.open = map[SeatRef]*fakePane{}
	}
	if f.open[seat] == nil {
		f.open[seat] = &fakePane{}
	}
	return f.open[seat], nil
}

func (f *fakePanes) Live(seat SeatRef) (Pane, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.open[seat]
	return p, ok
}

func (f *fakePanes) pane(seat SeatRef) *fakePane {
	f.mu.Lock()
	defer f.mu.Unlock()
	if p := f.open[seat]; p != nil {
		return p
	}
	return &fakePane{}
}

type clock struct{ t time.Time }

func (c *clock) now() time.Time      { return c.t }
func (c *clock) add(d time.Duration) { c.t = c.t.Add(d) }

type harness struct {
	t     *testing.T
	svc   *Service
	store teamroster.Store
	panes *fakePanes
	clock *clock
	ids   map[string]string
}

// newHarness builds a team of the named agents; the first is the main agent.
func newHarness(t *testing.T, names ...string) *harness {
	t.Helper()
	h := &harness{t: t, store: teamroster.Store{Dir: t.TempDir(), Credentials: &testSecretCredentials{values: map[string]string{}}}, panes: &fakePanes{}, clock: &clock{t: time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)}, ids: map[string]string{}}
	_, err := h.store.Update(context.Background(), func(r *teamroster.Roster) error {
		for i, n := range names {
			a := r.AddAgent(teamroster.Agent{Name: n, Main: i == 0, CreatedBy: teamroster.ByUser})
			h.ids[n] = a.ID
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	h.svc = New(Options{Store: h.store, Panes: h.panes, Now: h.clock.now})
	return h
}

func (h *harness) id(name string) string { return h.ids[name] }

func (h *harness) send(from, to, msg string, serving ...string) MessageReceipt {
	h.t.Helper()
	r, err := h.svc.MessageAgent(context.Background(), Caller{Agent: h.id(from), Serving: serving}, MessageRequest{To: to, Message: msg})
	if err != nil {
		h.t.Fatalf("%s → %s: %v", from, to, err)
	}
	return r
}

func (h *harness) settle(name string, st Settlement) {
	h.t.Helper()
	if err := h.svc.Settled(context.Background(), SeatRef{Agent: h.id(name)}, st); err != nil {
		h.t.Fatalf("settle %s: %v", name, err)
	}
}

func (h *harness) inbox(name string) []Delivery {
	return h.panes.pane(SeatRef{Agent: h.id(name)}).deliveries()
}

func (h *harness) exchange(id string) teamroster.Exchange {
	h.t.Helper()
	book, err := h.store.Exchanges()
	if err != nil {
		h.t.Fatal(err)
	}
	x, ok := find(book, id)
	if !ok {
		h.t.Fatalf("no exchange %s", id)
	}
	return x
}

func contains(t *testing.T, text string, parts ...string) {
	t.Helper()
	for _, p := range parts {
		if !strings.Contains(text, p) {
			t.Fatalf("text lacks %q:\n%s", p, text)
		}
	}
}
