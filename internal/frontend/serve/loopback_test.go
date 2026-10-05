package serve

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/session/control"
)

const loopbackToken = "loopback-test-credential-0123456789"

// gatedServer starts a gate in front of a handler that records whether anything
// reached it. Reaching the inner handler is the only proof that matters: a
// status code says what the caller saw, not what the control plane ran.
func gatedServer(t *testing.T) (base string, reached *atomic.Bool) {
	t.Helper()
	var seen atomic.Bool
	srv := httptest.NewUnstartedServer(nil)
	origin := "http://" + srv.Listener.Addr().String()
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		seen.Store(true)
		w.WriteHeader(http.StatusNoContent)
	})
	srv.Config.Handler = NewLoopbackGate(inner, LoopbackGateOptions{Token: loopbackToken, Origin: origin})
	srv.Start()
	t.Cleanup(srv.Close)
	return srv.URL, &seen
}

// probe sends one request and reports what the caller saw. mut is where a test
// spells the piece it is attacking with.
func probe(t *testing.T, method, url string, mut func(*http.Request)) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if mut != nil {
		mut(req)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var reason Reason
	_ = json.NewDecoder(resp.Body).Decode(&reason)
	return resp.StatusCode, reason.Code
}

func withCredential(r *http.Request) {
	r.AddCookie(&http.Cookie{Name: TokenCookie, Value: loopbackToken})
}

// The credential is the host's, minted per launch. Nothing else authenticates:
// not the address the request arrived on, and not a token spelled in the URL.
func TestLoopbackGateRefusesWithoutTheHostCredential(t *testing.T) {
	base, reached := gatedServer(t)

	cases := []struct {
		name string
		mut  func(*http.Request)
	}{
		{"no cookie at all", nil},
		{"a wrong credential of the same length", func(r *http.Request) {
			r.AddCookie(&http.Cookie{Name: TokenCookie, Value: strings.Repeat("x", len(loopbackToken))})
		}},
		{"an empty cookie", func(r *http.Request) {
			r.AddCookie(&http.Cookie{Name: TokenCookie, Value: ""})
		}},
		{"the credential in the query string", func(r *http.Request) {
			q := r.URL.Query()
			q.Set("token", loopbackToken)
			r.URL.RawQuery = q.Encode()
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			status, code := probe(t, http.MethodGet, base+"/status", c.mut)
			if status != http.StatusForbidden || code != codeLoopbackUnauthorized {
				t.Errorf("status = %d %q, want 403 %q", status, code, codeLoopbackUnauthorized)
			}
			if reached.Load() {
				t.Fatal("the request reached the control plane")
			}
		})
	}
}

// The two network invariants hold on their own. A leaked credential must not
// take the rebinding defense down with it, and arriving at the right address
// must not excuse an origin that belongs to someone else.
func TestLoopbackGateHoldsItsAddressAgainstAValidCredential(t *testing.T) {
	base, reached := gatedServer(t)
	origin := base

	cases := []struct {
		name string
		want string
		mut  func(*http.Request)
	}{
		{"a rebound name carrying the real credential", codeLoopbackHost, func(r *http.Request) {
			withCredential(r)
			r.Header.Set("Origin", origin)
			r.Host = "attacker.example"
		}},
		{"the right address under someone else's origin", codeLoopbackOrigin, func(r *http.Request) {
			withCredential(r)
			r.Header.Set("Origin", "https://attacker.example")
		}},
		{"a page on another loopback port", codeLoopbackOrigin, func(r *http.Request) {
			withCredential(r)
			r.Header.Set("Origin", "http://127.0.0.1:1")
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			status, code := probe(t, http.MethodPost, base+"/submit", c.mut)
			if status != http.StatusForbidden || code != c.want {
				t.Errorf("status = %d %q, want 403 %q", status, code, c.want)
			}
			if reached.Load() {
				t.Fatal("the request reached the control plane")
			}
		})
	}
}

// A read may arrive without an origin — a top-level navigation sends none — but
// anything that changes state has to name this listener. An originless mutation
// is refused rather than left to the credential alone.
func TestLoopbackGateRequiresAnOriginToChangeAnything(t *testing.T) {
	base, reached := gatedServer(t)

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions} {
		t.Run(method+" without an origin", func(t *testing.T) {
			reached.Store(false)
			status, code := probe(t, method, base+"/submit", withCredential)
			if status != http.StatusForbidden || code != codeLoopbackOrigin {
				t.Errorf("status = %d %q, want 403 %q", status, code, codeLoopbackOrigin)
			}
			if reached.Load() {
				t.Fatal("the request reached the control plane")
			}
		})
	}

	t.Run("a read without an origin is admitted", func(t *testing.T) {
		reached.Store(false)
		if status, code := probe(t, http.MethodGet, base+"/status", withCredential); status != http.StatusNoContent {
			t.Errorf("status = %d %q, want 204", status, code)
		}
		if !reached.Load() {
			t.Fatal("the request never reached the control plane")
		}
	})

	t.Run("a read under a foreign origin is not", func(t *testing.T) {
		reached.Store(false)
		status, code := probe(t, http.MethodGet, base+"/status", func(r *http.Request) {
			withCredential(r)
			r.Header.Set("Origin", "https://attacker.example")
		})
		if status != http.StatusForbidden || code != codeLoopbackOrigin {
			t.Errorf("status = %d %q, want 403 %q", status, code, codeLoopbackOrigin)
		}
		if reached.Load() {
			t.Fatal("the request reached the control plane")
		}
	})
}

// What the host's own page sends: this listener's origin and this launch's
// credential. Everything above is only meaningful if this still passes.
func TestLoopbackGateAdmitsTheHostsOwnPage(t *testing.T) {
	base, reached := gatedServer(t)
	status, code := probe(t, http.MethodPost, base+"/submit", func(r *http.Request) {
		withCredential(r)
		r.Header.Set("Origin", base)
	})
	if status != http.StatusNoContent {
		t.Fatalf("status = %d %q, want 204", status, code)
	}
	if !reached.Load() {
		t.Fatal("the request never reached the control plane")
	}
}

// A gate that cannot state what it guards guards nothing, so it refuses
// everything instead of passing traffic through under a policy it does not have.
func TestLoopbackGateFailsClosedWhenMisconfigured(t *testing.T) {
	cases := []struct {
		name   string
		token  string
		origin string
	}{
		{"no credential", "", "http://127.0.0.1:8080"},
		{"no origin", loopbackToken, ""},
		{"a name a resolver answers for", loopbackToken, "http://localhost:8080"},
		{"an address off the loopback interface", loopbackToken, "http://192.168.1.5:8080"},
		{"an origin with no port", loopbackToken, "http://127.0.0.1"},
		{"a scheme this listener cannot speak", loopbackToken, "https://127.0.0.1:8080"},
		{"an origin carrying a path", loopbackToken, "http://127.0.0.1:8080/api"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var seen atomic.Bool
			inner := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { seen.Store(true) })
			srv := httptest.NewServer(NewLoopbackGate(inner, LoopbackGateOptions{Token: c.token, Origin: c.origin}))
			defer srv.Close()

			status, code := probe(t, http.MethodGet, srv.URL+"/status", func(r *http.Request) {
				withCredential(r)
				r.Header.Set("Origin", srv.URL)
			})
			if status != http.StatusInternalServerError || code != codeLoopbackMisconfigured {
				t.Errorf("status = %d %q, want 500 %q", status, code, codeLoopbackMisconfigured)
			}
			if seen.Load() {
				t.Fatal("a misconfigured gate passed a request through")
			}
		})
	}
}

// The gate must not cost the stream its resumability: a client that drops mid
// turn comes back with Last-Event-ID and is sent what it missed, once. Driven
// through a real listener and a real kernel, because the property being fixed
// is end-to-end — a middleware that only forwarded the header would prove
// nothing about whether the frames still arrive.
func TestLoopbackGateKeepsTheStreamResumable(t *testing.T) {
	ln, err := ListenLoopback()
	if err != nil {
		t.Fatal(err)
	}
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Runner: fakeRunner{got: make(chan string, 1)}, Sink: bc})
	gate := NewLoopbackGate(New(ctrl, bc, config.ServeConfig{}).Handler(), LoopbackGateOptions{
		Token:  loopbackToken,
		Origin: LoopbackOrigin(ln),
	})
	srv := &http.Server{Handler: gate}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	base := LoopbackOrigin(ln)

	first := dialStream(t, base, "")
	bc.Emit(event.Event{Kind: event.ToolResult, Tool: event.Tool{ID: "t1", Name: "bash"}})
	if id, data := first.frame(t); id != "1" || !strings.Contains(data, `"t1"`) {
		t.Fatalf("first frame = id %q %s, want id 1 carrying t1", id, data)
	}
	first.close()

	// Emitted while nothing is listening: the replay log is what has to carry
	// them across the reconnect.
	bc.Emit(event.Event{Kind: event.ToolResult, Tool: event.Tool{ID: "t2", Name: "bash"}})
	bc.Emit(event.Event{Kind: event.ToolResult, Tool: event.Tool{ID: "t3", Name: "bash"}})

	resumed := dialStream(t, base, "1")
	defer resumed.close()
	for _, want := range []struct{ id, tool string }{{"2", "t2"}, {"3", "t3"}} {
		id, data := resumed.frame(t)
		if id != want.id || !strings.Contains(data, `"`+want.tool+`"`) {
			t.Fatalf("resumed frame = id %q %s, want id %s carrying %s", id, data, want.id, want.tool)
		}
	}
}

// A stream the gate let through, read the way EventSource reads one.
type stream struct {
	body   *bufio.Scanner
	cancel context.CancelFunc
	closer func() error
}

func dialStream(t *testing.T, base, lastEventID string) *stream {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/events", nil)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	req.AddCookie(&http.Cookie{Name: TokenCookie, Value: loopbackToken})
	if lastEventID != "" {
		req.Header.Set("Last-Event-ID", lastEventID)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		cancel()
		t.Fatalf("/events status = %d, want 200", resp.StatusCode)
	}
	return &stream{body: bufio.NewScanner(resp.Body), cancel: cancel, closer: resp.Body.Close}
}

// frame returns the id and payload of the next data frame, skipping the comment
// lines the transport uses to keep the socket warm.
func (s *stream) frame(t *testing.T) (string, string) {
	t.Helper()
	id := ""
	for s.body.Scan() {
		line := s.body.Text()
		switch {
		case strings.HasPrefix(line, "id: "):
			id = strings.TrimPrefix(line, "id: ")
		case strings.HasPrefix(line, "data: "):
			return id, strings.TrimPrefix(line, "data: ")
		}
	}
	t.Fatalf("the stream ended before a frame arrived: %v", s.body.Err())
	return "", ""
}

func (s *stream) close() {
	s.cancel()
	_ = s.closer()
}
