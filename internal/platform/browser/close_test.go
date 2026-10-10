package browser

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

type rejectingClose struct {
	*hostedBrowser
	reject    bool
	refused   bool
	beforeAck func()
	gone      bool
	hang      bool
}

func (p *rejectingClose) WriteMessage(raw []byte) error {
	var msg wireMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		return err
	}
	if msg.Method == "Target.getTargets" {
		targets := []map[string]string{}
		if !p.gone {
			targets = append(targets, map[string]string{"targetId": "view-1"})
		}
		p.send(map[string]any{"id": msg.ID, "result": map[string]any{"targetInfos": targets}})
		return nil
	}
	if msg.Method == "Target.closeTarget" {
		if p.hang {
			return nil
		}
		if p.beforeAck != nil {
			p.beforeAck()
		}
		if p.reject {
			p.send(map[string]any{"id": msg.ID, "error": map[string]any{"code": -32000, "message": "busy"}})
		} else {
			p.send(map[string]any{"id": msg.ID, "result": map[string]any{"success": !p.refused}})
		}
		return nil
	}
	return p.hostedBrowser.WriteMessage(raw)
}

func TestCloseTabRetainsARejectedPageForRetry(t *testing.T) {
	for _, mode := range []string{"error", "refused"} {
		t.Run(mode, func(t *testing.T) {
			page := &rejectingClose{hostedBrowser: newHostedBrowser(), reject: mode == "error", refused: mode == "refused"}
			pool := &Pool{}
			pool.SetEndpoint(func(_ context.Context, _ string) (Endpoint, error) { return page, nil })
			s := NewSession(Config{Launch: LaunchSpec{ProfileDir: "/profiles/w1"}, Pool: pool})
			t.Cleanup(s.Close)
			tab, err := s.Visit(t.Context(), "https://example.com", "", true)
			if err != nil {
				t.Fatal(err)
			}
			if err := s.CloseTab(t.Context(), tab.ID); err == nil {
				t.Fatal("rejected close reported success")
			}
			if len(s.Tabs()) != 1 {
				t.Fatal("failed page was removed")
			}
			page.reject, page.refused = false, false
			if err := s.CloseTab(t.Context(), tab.ID); err != nil {
				t.Fatal(err)
			}
			if len(s.Tabs()) != 0 {
				t.Fatal("successful retry retained page")
			}
		})
	}
}

func TestCloseTabHandlesDestructionBeforeAcknowledgement(t *testing.T) {
	page := &rejectingClose{hostedBrowser: newHostedBrowser()}
	pool := &Pool{}
	pool.SetEndpoint(func(context.Context, string) (Endpoint, error) { return page, nil })
	s := NewSession(Config{Launch: LaunchSpec{ProfileDir: "/profiles/w1"}, Pool: pool})
	t.Cleanup(s.Close)
	tab, err := s.Visit(t.Context(), "https://example.com", "", true)
	if err != nil {
		t.Fatal(err)
	}
	page.beforeAck = func() {
		params, _ := json.Marshal(map[string]string{"targetId": tab.Target})
		s.onBrowserEvent(event{Method: "Target.targetDestroyed", Params: params})
	}
	if err := s.CloseTab(t.Context(), tab.ID); err != nil {
		t.Fatal(err)
	}
	if len(s.Tabs()) != 0 {
		t.Fatal("closed page retained")
	}
	s.mu.Lock()
	lost := s.lost[tab.ID]
	s.mu.Unlock()
	if lost {
		t.Fatal("acknowledged close recorded as an external loss")
	}
}

func TestCloseTabDiscardsOnlyKnownGonePages(t *testing.T) {
	for _, mode := range []string{"destroyed", "active destroyed", "unannounced", "engine"} {
		t.Run(mode, func(t *testing.T) {
			page := &rejectingClose{hostedBrowser: newHostedBrowser()}
			pool := &Pool{}
			pool.SetEndpoint(func(context.Context, string) (Endpoint, error) { return page, nil })
			s := NewSession(Config{Launch: LaunchSpec{ProfileDir: "/profiles/w1"}, Pool: pool})
			t.Cleanup(s.Close)
			tab, err := s.Visit(t.Context(), "https://example.com", "", true)
			if err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "destroyed", "active destroyed":
				params, _ := json.Marshal(map[string]string{"targetId": tab.Target})
				s.onBrowserEvent(event{Method: "Target.targetDestroyed", Params: params})
			case "unannounced":
				page.gone, page.reject = true, true
			case "engine":
				_ = page.Close()
				<-s.eng.conn.closed()
			}
			id := tab.ID
			if mode == "active destroyed" {
				id = ""
			}
			if err := s.CloseTab(t.Context(), id); err != nil {
				t.Fatal(err)
			}
			if len(s.Tabs()) != 0 {
				t.Fatal("gone page retained")
			}
			s.mu.Lock()
			lost := s.lost[tab.ID]
			s.mu.Unlock()
			if lost {
				t.Fatal("gone page still recorded as lost")
			}
		})
	}
}

func TestCloseTabBoundsAnUnresponsiveEngine(t *testing.T) {
	page := &rejectingClose{hostedBrowser: newHostedBrowser(), hang: true}
	pool := &Pool{}
	pool.SetEndpoint(func(context.Context, string) (Endpoint, error) { return page, nil })
	s := NewSession(Config{Launch: LaunchSpec{ProfileDir: "/profiles/w1"}, Pool: pool})
	t.Cleanup(s.Close)
	tab, err := s.Visit(t.Context(), "https://example.com", "", true)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), closeTimeout+2*time.Second)
	defer cancel()
	start := time.Now()
	err = s.CloseTab(ctx, tab.ID)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("close = %v", err)
	}
	if elapsed := time.Since(start); elapsed > closeTimeout+time.Second {
		t.Fatalf("close took %s", elapsed)
	}
	if len(s.Tabs()) != 1 {
		t.Fatal("timed-out close removed a live page")
	}
	page.hang = false
}

func TestCloseTabHonoursEarlierDeadlineAndRejectsUnknownIDs(t *testing.T) {
	page := &rejectingClose{hostedBrowser: newHostedBrowser(), hang: true}
	pool := &Pool{}
	pool.SetEndpoint(func(context.Context, string) (Endpoint, error) { return page, nil })
	s := NewSession(Config{Launch: LaunchSpec{ProfileDir: "/profiles/w1"}, Pool: pool})
	t.Cleanup(s.Close)
	tab, err := s.Visit(t.Context(), "https://example.com", "", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CloseTab(t.Context(), "foreign-tab"); CodeOf(err) != CodeNoTab {
		t.Fatalf("foreign close = %v", err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	start := time.Now()
	err = s.CloseTab(ctx, tab.ID)
	page.hang = false
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > time.Second {
		t.Fatalf("early deadline = %v after %s", err, time.Since(start))
	}
	if len(s.Tabs()) != 1 {
		t.Fatal("cancelled close removed a live page")
	}
}
