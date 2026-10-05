package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/contract/eventwire"
)

// scriptedEvents serves one scripted /events connection per entry, then holds
// the last one open, and answers /events/replay from replay.
type scriptedEvents struct {
	mu          sync.Mutex
	connections [][]eventwire.Event
	replay      map[string][]eventwire.Event
	incomplete  bool
	lastIDs     []string
}

func (s *scriptedEvents) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/events":
		s.mu.Lock()
		s.lastIDs = append(s.lastIDs, r.Header.Get("Last-Event-ID"))
		var frames []eventwire.Event
		hold := len(s.connections) <= 1
		if len(s.connections) > 0 {
			frames = s.connections[0]
			s.connections = s.connections[1:]
		}
		s.mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, ": connected\n\n")
		for _, ev := range frames {
			raw, _ := json.Marshal(ev)
			if ev.Seq > 0 {
				_, _ = fmt.Fprintf(w, "id: %d\n", ev.Seq)
			}
			_, _ = fmt.Fprintf(w, "data: %s\n\n", raw)
		}
		w.(http.Flusher).Flush()
		if hold {
			<-r.Context().Done()
		}
	case "/events/replay":
		frames := s.replay[r.URL.Query().Get("lastEventId")]
		raws := make([]json.RawMessage, 0, len(frames))
		for _, ev := range frames {
			raw, _ := json.Marshal(ev)
			raws = append(raws, raw)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"frames": raws, "complete": !s.incomplete})
	}
}

func seq(n int64, kind string) eventwire.Event { return eventwire.Event{Kind: kind, Seq: n} }

// collect reads n updates, rendering each as its seq, "text", or "GAP".
func collect(t *testing.T, s *scriptedEvents, n int) []string {
	t.Helper()
	srv := httptest.NewServer(s)
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	updates := (&Client{HTTP: srv.Client(), Base: srv.URL}).Subscribe(ctx)
	var got []string
	for len(got) < n {
		select {
		case u := <-updates:
			switch {
			case u.Gap:
				got = append(got, "GAP")
			case u.Event.Seq > 0:
				got = append(got, fmt.Sprint(u.Event.Seq))
			default:
				got = append(got, u.Event.Kind)
			}
		case <-ctx.Done():
			t.Fatalf("got %v before timing out", got)
		}
	}
	return got
}

func TestStreamDeliversNumberedFramesOnceAndInOrder(t *testing.T) {
	cases := []struct {
		name string
		s    *scriptedEvents
		want string
	}{
		{"a jump is filled from the replay before the frame that revealed it",
			&scriptedEvents{connections: [][]eventwire.Event{{seq(1, "a"), seq(2, "a"), seq(5, "a")}},
				replay: map[string][]eventwire.Event{"2": {seq(3, "a"), seq(4, "a"), seq(5, "a")}}},
			"1 2 3 4 5"},
		{"what the replay no longer holds is a gap",
			&scriptedEvents{connections: [][]eventwire.Event{{seq(1, "a"), seq(4, "a")}}, incomplete: true,
				replay: map[string][]eventwire.Event{"1": {seq(3, "a")}}},
			"1 3 GAP 4"},
		{"a repeated frame is delivered once",
			&scriptedEvents{connections: [][]eventwire.Event{{seq(1, "a"), seq(2, "a"), seq(2, "a"), seq(3, "a")}}},
			"1 2 3"},
		{"unnumbered deltas pass straight through",
			&scriptedEvents{connections: [][]eventwire.Event{{seq(1, "a"), {Kind: "text", Text: "x"}, seq(2, "a")}}},
			"1 text 2"},
		{"the first watermark is the baseline, a later one ahead of it is a loss",
			&scriptedEvents{connections: [][]eventwire.Event{{seq(10, "stream_watermark"), seq(11, "a"), seq(13, "stream_watermark")}},
				replay: map[string][]eventwire.Event{"11": {seq(12, "a"), seq(13, "a")}}},
			"11 12 13"},
		{"numbering that goes backwards is a restarted stream",
			&scriptedEvents{connections: [][]eventwire.Event{{seq(5, "a"), seq(1, "a"), seq(2, "a")}}},
			"5 GAP 1 2"},
		{"a gap frame is reported",
			&scriptedEvents{connections: [][]eventwire.Event{{seq(1, "a"), seq(7, "stream_gap"), seq(8, "a")}}},
			"1 GAP 8"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			want := strings.Fields(c.want)
			if got := collect(t, c.s, len(want)); strings.Join(got, " ") != c.want {
				t.Fatalf("got %v, want %v", got, want)
			}
		})
	}
}

// A dropped connection resumes from the last frame it delivered, so the server
// replays from there instead of starting the client over.
func TestStreamResumesAfterAReconnect(t *testing.T) {
	s := &scriptedEvents{connections: [][]eventwire.Event{{seq(1, "a"), seq(2, "a")}, {seq(3, "a")}}}
	if got := collect(t, s, 3); strings.Join(got, " ") != "1 2 3" {
		t.Fatalf("got %v", got)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.lastIDs) < 2 || s.lastIDs[0] != "" || s.lastIDs[1] != "2" {
		t.Fatalf("Last-Event-ID per connection = %q, want [\"\" \"2\"]", s.lastIDs)
	}
}

// lateAttach registers an /events subscriber only after a delay, and emit
// reaches only subscribers already registered — the server's own contract.
type lateAttach struct {
	mu   sync.Mutex
	subs []chan eventwire.Event
}

func (l *lateAttach) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	time.Sleep(200 * time.Millisecond)
	ch := make(chan eventwire.Event, 4)
	l.mu.Lock()
	l.subs = append(l.subs, ch)
	l.mu.Unlock()
	w.Header().Set("Content-Type", "text/event-stream")
	_, _ = fmt.Fprint(w, ": connected\n\n")
	w.(http.Flusher).Flush()
	for {
		select {
		case ev := <-ch:
			raw, _ := json.Marshal(ev)
			_, _ = fmt.Fprintf(w, "id: %d\ndata: %s\n\n", ev.Seq, raw)
			w.(http.Flusher).Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func (l *lateAttach) emit(ev eventwire.Event) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, ch := range l.subs {
		ch <- ev
	}
}

// A frame emitted by an action taken right after Subscribe returns reaches
// the stream, however long the first connection took to attach.
func TestSubscribeReturnsOnceAttached(t *testing.T) {
	l := &lateAttach{}
	srv := httptest.NewServer(l)
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	updates := (&Client{HTTP: srv.Client(), Base: srv.URL}).Subscribe(ctx)
	l.emit(eventwire.Event{Kind: "notice", Seq: 1})
	select {
	case u := <-updates:
		if u.Event.Kind != "notice" {
			t.Fatalf("got %+v, want the notice", u)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a frame emitted after Subscribe returned never arrived")
	}
}
