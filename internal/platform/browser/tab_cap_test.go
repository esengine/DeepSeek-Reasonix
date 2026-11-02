package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
)

// manyTargets hands out a fresh target for each createTarget, which the plain
// hostedBrowser does not: it names every page view-1.
type manyTargets struct {
	*hostedBrowser
	mu   sync.Mutex
	next int
	last string
}

func (m *manyTargets) WriteMessage(raw []byte) error {
	var msg wireMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		return err
	}
	switch msg.Method {
	case "Target.createTarget":
		m.mu.Lock()
		m.next++
		m.last = fmt.Sprintf("view-%d", m.next)
		id := m.last
		m.mu.Unlock()
		m.send(map[string]any{"id": msg.ID, "result": map[string]any{"targetId": id}})
		return nil
	case "Target.attachToTarget":
		m.mu.Lock()
		id := m.last
		m.mu.Unlock()
		m.send(map[string]any{"id": msg.ID, "result": map[string]any{"sessionId": "S-" + id}})
		return nil
	case "Target.closeTarget":
		m.send(map[string]any{"id": msg.ID, "result": map[string]any{"success": true}})
		return nil
	}
	return m.hostedBrowser.WriteMessage(raw)
}

func newManyTabsSession(t *testing.T) *Session {
	t.Helper()
	page := &manyTargets{hostedBrowser: newHostedBrowser()}
	pool := &Pool{}
	pool.SetEndpoint(func(_ context.Context, _ string) (Endpoint, error) { return page, nil })
	s := NewSession(Config{Launch: LaunchSpec{ProfileDir: "/profiles/w1"}, Pool: pool})
	t.Cleanup(s.Close)
	return s
}

// A model that opens a page per result must hit a limit the host enforces, and
// closing a page must make room again; a page the person opens is never refused,
// though it counts toward the total.
func TestTheAgentCannotOpenMoreThanTheLimitOfPages(t *testing.T) {
	s := newManyTabsSession(t)
	for i := range maxTabs {
		if _, err := s.Open(t.Context(), "https://example.com", "", true); err != nil {
			t.Fatalf("page %d: %v", i+1, err)
		}
	}
	_, err := s.Open(t.Context(), "https://example.com", "", true)
	if CodeOf(err) != CodeTooManyTabs {
		t.Fatalf("page %d = %v, want %s", maxTabs+1, err, CodeTooManyTabs)
	}
	if n := s.tabCount(); n != maxTabs {
		t.Fatalf("%d pages open after a refusal, want %d", n, maxTabs)
	}
	if _, err := s.Visit(t.Context(), "https://example.org", "", true); err != nil {
		t.Fatalf("a page the person opens was refused: %v", err)
	}
	if err := s.CloseTab(t.Context(), ""); err != nil {
		t.Fatal(err)
	}
	if err := s.CloseTab(t.Context(), ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Open(t.Context(), "https://example.com", "", true); err != nil {
		t.Fatalf("closing pages did not make room: %v", err)
	}
}

// Opens that overlap must not both pass at the limit: the count includes the
// pages still being created.
func TestOverlappingOpensCannotPassTheLimitTogether(t *testing.T) {
	s := newManyTabsSession(t)
	for range maxTabs - 1 {
		if _, err := s.Open(t.Context(), "https://example.com", "", true); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for range 8 {
		wg.Go(func() {
			_, err := s.Open(t.Context(), "https://example.com", "", true)
			results <- err
		})
	}
	wg.Wait()
	close(results)
	opened := 0
	for err := range results {
		if err == nil {
			opened++
		}
	}
	if opened != 1 {
		t.Fatalf("%d of 8 overlapping opens passed with room for one", opened)
	}
	if n := s.tabCount(); n != maxTabs {
		t.Fatalf("%d pages open, want %d", n, maxTabs)
	}
}
