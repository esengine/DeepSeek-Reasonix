package serve

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"reasonix/internal/event"
)

// SSEWatermarkInterval is how often a transport restates the last numbered
// frame. It rides the SSE keepalive; the shell's bus has none and ticks on the
// same period, so how long a stalled view stays wrong does not depend on which
// transport is carrying it.
const SSEWatermarkInterval = sseKeepaliveInterval

// eventsReplay answers what a client missed. `complete` false means the log no
// longer reaches that far back, so each read model has to be re-read from the
// authority that answers it — the transcript for one, the run graph for
// another. Said plainly, because a hole looks like a quiet turn.
func (s *Server) eventsReplay(w http.ResponseWriter, r *http.Request) {
	frames, complete := s.bc.Replay(lastEventID(r))
	writeJSON(w, struct {
		Frames    []json.RawMessage `json:"frames"`
		Complete  bool              `json:"complete"`
		Watermark int64             `json:"watermark"`
	}{frames, complete, s.bc.Watermark()})
}

// lastEventID reads the resume point a client carries. The header is
// EventSource's own, sent unprompted; the query parameter is for clients that
// cannot set headers.
func lastEventID(r *http.Request) int64 {
	raw := r.Header.Get("Last-Event-ID")
	if raw == "" {
		raw = r.URL.Query().Get("lastEventId")
	}
	seq, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || seq < 0 {
		return 0
	}
	return seq
}

// Bytes, not frames: frames here span three orders of magnitude, so a count
// bounds nothing anyone cares about. 8MB holds a whole session of recoverable
// frames — a real 440-frame session weighs about 750KB — before a resume has to
// fall back to the transcript. Streaming deltas are bounded on a different axis
// entirely, the live window; see subscriber.push.
const (
	replayBudget = 8 << 20
	queueBudget  = 4 << 20
)

// Frame is one JSON frame on its way to a client, carrying the number it was
// assigned. Seq is zero for frames that are not resumable — the transports use
// it to decide what to write as an SSE id and what to remember as a resume
// point, and neither has to parse the payload back to find out.
type Frame struct {
	Seq  int64
	Data []byte
}

// replayFrame is one numbered frame kept for clients that have to resume.
type replayFrame struct {
	seq  int64
	data []byte
}

// replayLog is the bounded tail of the numbered stream. It is the difference
// between a dropped frame and a lost one: a client that notices a gap can ask
// for what it missed, and only a client that fell behind further than this
// window has to fall back to refetching the transcript.
type replayLog struct {
	frames []replayFrame
	bytes  int
}

func (l *replayLog) add(seq int64, data []byte) {
	l.frames = append(l.frames, replayFrame{seq: seq, data: data})
	l.bytes += len(data)
	for l.bytes > replayBudget && len(l.frames) > 1 {
		l.bytes -= len(l.frames[0].data)
		l.frames[0].data = nil
		l.frames = l.frames[1:]
	}
}

// since returns the frames numbered after `seq`, and whether the log could
// still account for all of them. Incomplete means the client's next frame was
// evicted before it came back, so no amount of replay makes it whole.
func (l *replayLog) since(seq int64) (frames []replayFrame, complete bool) {
	if len(l.frames) == 0 {
		return nil, false
	}
	if l.frames[0].seq > seq+1 {
		return l.frames, false
	}
	for i, f := range l.frames {
		if f.seq > seq {
			return l.frames[i:], true
		}
	}
	return nil, true
}

func (l *replayLog) reset() {
	l.frames = nil
	l.bytes = 0
}

// subscriber is one client's outbound queue, drained onto ch by a single pump
// goroutine — which is what lets Emit stay non-blocking however slowly the
// client reads. Over budget it sheds the oldest droppable frame first, since
// nobody has to ask for those again, and only then a numbered one, which the
// client will notice missing. Order among the survivors is never disturbed.
type subscriber struct {
	ch   chan Frame
	done chan struct{}
	wake chan struct{}

	mu     sync.Mutex
	queue  []queuedFrame
	bytes  int
	closed bool
}

// queuedFrame keeps the shedding decision next to the frame while it waits, so
// eviction never has to re-derive it from the payload.
type queuedFrame struct {
	f    Frame
	drop bool
}

func newSubscriber() *subscriber {
	s := &subscriber{ch: make(chan Frame, 64), done: make(chan struct{}), wake: make(chan struct{}, 1)}
	go s.pump()
	return s
}

// drop is passed rather than read off f.Seq: a connection-local replay carries
// no number — nobody else can be sent it — and still must not be shed, since it
// is usually the prompt the client is waiting on.
func (s *subscriber) push(f Frame, drop bool) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	// A queue a whole channel deep means this client is out of step: a delta
	// behind that backlog arrives describing text already on screen, and costs
	// everything numbered behind it. The live window is the channel itself.
	if drop && len(s.queue) >= cap(s.ch) {
		s.mu.Unlock()
		return
	}
	s.queue = append(s.queue, queuedFrame{f: f, drop: drop})
	s.bytes += len(f.Data)
	for s.bytes > queueBudget && len(s.queue) > 1 {
		s.evictLocked()
	}
	s.mu.Unlock()
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// evictLocked removes the oldest frame the client can most afford to lose,
// preferring a droppable one wherever it sits and falling back to the oldest
// frame of any kind. The newest is never the candidate: it carries the state
// that would let the client stop waiting.
func (s *subscriber) evictLocked() {
	at := -1
	for i := range s.queue[:len(s.queue)-1] {
		if s.queue[i].drop {
			at = i
			break
		}
	}
	if at < 0 {
		at = 0
	}
	s.bytes -= len(s.queue[at].f.Data)
	s.queue = append(s.queue[:at], s.queue[at+1:]...)
}

// pump owns ch: the only goroutine that sends on it, and the only one that
// closes it — so a client disconnecting mid-send cannot race a send on a closed
// channel.
func (s *subscriber) pump() {
	defer close(s.ch)
	for {
		s.mu.Lock()
		if len(s.queue) == 0 {
			s.mu.Unlock()
			select {
			case <-s.wake:
				continue
			case <-s.done:
				return
			}
		}
		q := s.queue[0]
		s.queue[0] = queuedFrame{}
		s.queue = s.queue[1:]
		s.bytes -= len(q.f.Data)
		s.mu.Unlock()
		select {
		case s.ch <- q.f:
		case <-s.done:
			return
		}
	}
}

func (s *subscriber) close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.mu.Unlock()
	close(s.done)
}

// sseKeepaliveInterval is how often /events emits a `: ping`. Reverse proxies
// (nginx, ALB, Cloudflare) close idle upstreams after 30–60s, which a quiet
// turn hits easily; the comment keeps the socket warm and the EventSource
// client drops it, so it costs the consumer nothing.
const sseKeepaliveInterval = 15 * time.Second

// events streams the controller's event flow as SSE until the client
// disconnects. Each event is one `data:` frame of the JSON wire form, and every
// frame a client cannot afford to miss also carries an `id:` it can resume from.
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		refuse(w, http.StatusInternalServerError, "stream.unsupported", "this transport cannot stream", nil)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	var ch <-chan Frame
	var unsubscribe func()
	// EventSource carries its last id on a reconnect unasked, so a dropped
	// connection costs only what the replay log can no longer reach.
	after := lastEventID(r)
	// Subscribe and replay as one handoff. Prompt producers are serialized with
	// this operation, so no original event can land between the two steps.
	s.ctl().ReplayPendingPromptsWith(func() event.Sink {
		ch, unsubscribe = s.bc.SubscribeFrom(after)
		return event.FuncSink(func(e event.Event) {
			s.bc.EmitTo(ch, e)
		})
	})
	defer unsubscribe()

	fmt.Fprint(w, ": connected\n\n") // open the stream immediately
	flusher.Flush()

	keepalive := time.NewTicker(sseKeepaliveInterval)
	defer keepalive.Stop()

	for {
		select {
		case f, ok := <-ch:
			if !ok {
				return
			}
			// Unnumbered frames get no id, so a reconnect never asks to have a
			// stream of deltas replayed at it.
			if f.Seq > 0 {
				fmt.Fprintf(w, "id: %d\n", f.Seq)
			}
			fmt.Fprintf(w, "data: %s\n\n", f.Data)
			flusher.Flush()
		case <-keepalive.C:
			fmt.Fprint(w, ": ping\n\n")
			// A real frame, not a comment: a comment never reaches onmessage.
			// Losing the last frame of a turn is what nothing else reveals, and
			// this is how a client finds out within one interval.
			fmt.Fprintf(w, "data: {\"kind\":\"stream_watermark\",\"seq\":%d}\n\n", s.bc.Watermark())
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}
