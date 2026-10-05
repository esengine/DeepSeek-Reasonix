package browser

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"testing"
	"time"
)

// hostedBrowser is the smallest host a Session can drive: the commands Open
// sends, answered the way a browser would, with the load event it waits for.
type hostedBrowser struct {
	in      chan []byte
	mu      sync.Mutex
	methods []string
	closed  chan struct{}
	once    sync.Once
	// neverLoads withholds the load event, like a page whose script comes
	// from a host the network cannot reach.
	neverLoads bool
}

func newHostedBrowser() *hostedBrowser {
	return &hostedBrowser{in: make(chan []byte, 64), closed: make(chan struct{})}
}

func (h *hostedBrowser) ReadMessage() ([]byte, error) {
	select {
	case m := <-h.in:
		return m, nil
	case <-h.closed:
		return nil, io.EOF
	}
}

func (h *hostedBrowser) Close() error {
	h.once.Do(func() { close(h.closed) })
	return nil
}

func (h *hostedBrowser) send(v any) {
	raw, _ := json.Marshal(v)
	h.in <- raw
}

func (h *hostedBrowser) WriteMessage(raw []byte) error {
	var msg wireMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		return err
	}
	h.mu.Lock()
	h.methods = append(h.methods, msg.Method)
	h.mu.Unlock()
	result := map[string]any{}
	switch msg.Method {
	case "Target.createTarget":
		result["targetId"] = "view-1"
	case "Target.attachToTarget":
		result["sessionId"] = "S-view-1"
	case "Page.getFrameTree":
		result["frameTree"] = map[string]any{"frame": map[string]any{"id": "F1", "url": "about:blank"}}
	case "Page.navigate":
		result["loaderId"] = "L1"
		if h.neverLoads {
			break
		}
		defer h.send(map[string]any{"method": "Page.lifecycleEvent", "sessionId": msg.SessionID,
			"params": map[string]any{"frameId": "F1", "loaderId": "L1", "name": "load"}})
	case "Runtime.evaluate":
		result["result"] = map[string]any{"value": "Hosted"}
	}
	h.send(map[string]any{"id": msg.ID, "result": result})
	return nil
}

func TestSessionDrivesAHostedBrowserWithoutLaunchingOne(t *testing.T) {
	host := newHostedBrowser()
	pool := &Pool{}
	var dialed string
	pool.SetEndpoint(func(_ context.Context, profile string) (Endpoint, error) {
		dialed = profile
		return host, nil
	})
	s := NewSession(Config{Launch: LaunchSpec{Executable: "/nonexistent/chrome", ProfileDir: "/profiles/w1"}, Pool: pool})
	var changes sync.WaitGroup
	changes.Add(1)
	var once sync.Once
	s.OnTabsChanged(func() { once.Do(changes.Done) })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	info, err := s.Open(ctx, "https://example.com/", "", false)
	if err != nil {
		t.Fatalf("Open through the host: %v", err)
	}
	if info.Title != "Hosted" || info.Target != "view-1" || dialed != "/profiles/w1" {
		t.Fatalf("info = %+v, dialed %q", info, dialed)
	}
	changes.Wait()
	if tabs := s.Tabs(); len(tabs) != 1 || tabs[0].Target != "view-1" || !tabs[0].Active {
		t.Fatalf("tabs = %+v", tabs)
	}
	s.Close()
	host.mu.Lock()
	defer host.mu.Unlock()
	for _, want := range []string{"Browser.setDownloadBehavior", "Target.closeTarget", "Browser.close"} {
		found := false
		for _, m := range host.methods {
			found = found || m == want
		}
		if !found {
			t.Errorf("the host never received %s: %v", want, host.methods)
		}
	}
}

func TestNoHostFallsBackToLaunching(t *testing.T) {
	pool := &Pool{}
	pool.SetEndpoint(func(context.Context, string) (Endpoint, error) { return nil, ErrNoHost })
	s := NewSession(Config{Launch: LaunchSpec{Executable: "/nonexistent/chrome", ProfileDir: t.TempDir()}, Pool: pool})
	if _, err := s.Open(context.Background(), "https://example.com/", "", false); CodeOf(err) != CodeEngineMissing {
		t.Fatalf("with no host attached = %v, want the launch path's %s", err, CodeEngineMissing)
	}
	pool.SetEndpoint(func(context.Context, string) (Endpoint, error) { return nil, errors.New("refused") })
	s = NewSession(Config{Launch: LaunchSpec{Executable: "/nonexistent/chrome", ProfileDir: t.TempDir()}, Pool: pool})
	if _, err := s.Open(context.Background(), "https://example.com/", "", false); CodeOf(err) != CodeEngineFailed {
		t.Fatalf("a host that refuses = %v, want %s rather than a silent launch", err, CodeEngineFailed)
	}
}

// A person watching the page sees it as soon as it commits; waiting for a load
// that a missing subresource never lets fire left the panel blank for the
// whole navigation timeout. The agent's Open still waits for the load.
func TestVisitAnswersOnCommitWhileOpenWaitsForLoad(t *testing.T) {
	host := newHostedBrowser()
	host.neverLoads = true
	pool := &Pool{}
	pool.SetEndpoint(func(context.Context, string) (Endpoint, error) { return host, nil })
	s := NewSession(Config{Launch: LaunchSpec{Executable: "/nonexistent/chrome", ProfileDir: "/profiles/w1"}, Pool: pool})
	defer s.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	info, err := s.Visit(ctx, "https://intranet.example/", "", true)
	if err != nil {
		t.Fatalf("Visit of a page that never fires load: %v", err)
	}
	if info.Target != "view-1" {
		t.Fatalf("info = %+v", info)
	}

	short, cancelShort := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancelShort()
	if _, err := s.Open(short, "https://intranet.example/", "", false); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Open = %v, want it still waiting for load when its context ends", err)
	}
}

func TestVisitRefusesWhatOpenRefuses(t *testing.T) {
	pool := &Pool{}
	pool.SetEndpoint(func(context.Context, string) (Endpoint, error) { return newHostedBrowser(), nil })
	s := NewSession(Config{Launch: LaunchSpec{Executable: "/nonexistent/chrome", ProfileDir: "/profiles/w1"}, Pool: pool})
	defer s.Close()
	for _, raw := range []string{"file:///etc/passwd", "javascript:alert(1)", "example.com"} {
		_, visitErr := s.Visit(context.Background(), raw, "", true)
		_, openErr := s.Open(context.Background(), raw, "", true)
		if visitErr == nil || CodeOf(visitErr) != CodeOf(openErr) {
			t.Errorf("%s: Visit = %v, Open = %v; want the same refusal", raw, visitErr, openErr)
		}
	}
}
