package serve

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"reasonix/internal/platform/browser"
)

// BrowserHost relays hosted-browser connections between the kernel and the
// window that draws them: the shell's main process holds one stream and draws
// a view per target, and every workspace's browser is one connection on it.
// A kernel with no window has no BrowserHost and launches its browsers.
type BrowserHost struct {
	mu     sync.Mutex
	stream *browserStream
	conns  map[string]*browserConn
	next   int
}

// NewBrowserHost returns a relay with no window attached yet.
func NewBrowserHost() *BrowserHost {
	return &BrowserHost{conns: map[string]*browserConn{}}
}

// browserFrame is one unit on the relay. Open starts a connection for a
// partition; Close ends one; Message carries a protocol message either way.
type browserFrame struct {
	Conn    string          `json:"conn"`
	Open    string          `json:"open,omitempty"`
	Close   bool            `json:"close,omitempty"`
	Message json.RawMessage `json:"message,omitempty"`
}

type browserStream struct {
	frames chan browserFrame
	done   chan struct{}
}

const browserFrameBuffer = 1024

// Dial opens a connection to the window's browser for one profile. The window
// sees a partition named from the profile, never its path.
func (h *BrowserHost) Dial(_ context.Context, profile string) (browser.Endpoint, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.stream == nil {
		return nil, browser.ErrNoHost
	}
	h.next++
	c := &browserConn{host: h, id: strconv.Itoa(h.next), stream: h.stream, inbound: make(chan []byte, browserFrameBuffer), done: make(chan struct{})}
	if !c.push(browserFrame{Conn: c.id, Open: browserPartition(profile)}) {
		return nil, browser.ErrNoHost
	}
	h.conns[c.id] = c
	return c, nil
}

func browserPartition(profile string) string {
	sum := sha256.Sum256([]byte(profile))
	return "reasonix-browser-" + hex.EncodeToString(sum[:8])
}

// attach makes s the window's stream, ending every connection the previous one
// carried: those views went with the window that drew them.
func (h *BrowserHost) attach(s *browserStream) {
	h.mu.Lock()
	old, conns := h.stream, h.conns
	h.stream, h.conns = s, map[string]*browserConn{}
	h.mu.Unlock()
	if old != nil {
		close(old.done)
	}
	for _, c := range conns {
		c.end()
	}
}

func (h *BrowserHost) detach(s *browserStream) {
	h.mu.Lock()
	if h.stream != s {
		h.mu.Unlock()
		return
	}
	conns := h.conns
	h.stream, h.conns = nil, map[string]*browserConn{}
	h.mu.Unlock()
	for _, c := range conns {
		c.end()
	}
}

func (h *BrowserHost) conn(id string) *browserConn {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.conns[id]
}

func (h *BrowserHost) forget(c *browserConn) {
	h.mu.Lock()
	if h.conns[c.id] == c {
		delete(h.conns, c.id)
	}
	h.mu.Unlock()
}

type browserConn struct {
	host    *BrowserHost
	id      string
	stream  *browserStream
	inbound chan []byte
	done    chan struct{}
	once    sync.Once
}

func (c *browserConn) push(f browserFrame) bool {
	select {
	case c.stream.frames <- f:
		return true
	case <-c.stream.done:
		return false
	case <-time.After(5 * time.Second):
		return false
	}
}

func (c *browserConn) ReadMessage() ([]byte, error) {
	select {
	case m := <-c.inbound:
		return m, nil
	case <-c.done:
		return nil, io.EOF
	}
}

func (c *browserConn) WriteMessage(m []byte) error {
	select {
	case <-c.done:
		return io.ErrClosedPipe
	default:
	}
	if !c.push(browserFrame{Conn: c.id, Message: m}) {
		return io.ErrClosedPipe
	}
	return nil
}

// Close ends the connection and tells the window to drop what it opened.
func (c *browserConn) Close() error {
	select {
	case <-c.done:
	default:
		c.push(browserFrame{Conn: c.id, Close: true})
	}
	c.end()
	c.host.forget(c)
	return nil
}

func (c *browserConn) end() { c.once.Do(func() { close(c.done) }) }

func (c *browserConn) deliver(m []byte) {
	select {
	case c.inbound <- m:
	case <-c.done:
	}
}

const codeBrowserHostFrames = "browser_host.bad_frames"

func (h *Hub) registerBrowserHostRoutes(mux *http.ServeMux) {
	if h.opts.BrowserHost == nil {
		return
	}
	mux.HandleFunc("GET /browser-host/stream", h.browserHostStream)
	mux.HandleFunc("POST /browser-host/frames", h.browserHostFrames)
}

// browserHostStream is the window's side: every frame the kernel sends, as SSE,
// for as long as the window holds it open.
func (h *Hub) browserHostStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		refuse(w, http.StatusInternalServerError, "stream.unsupported", "this transport cannot stream", nil)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	s := &browserStream{frames: make(chan browserFrame, browserFrameBuffer), done: make(chan struct{})}
	host := h.opts.BrowserHost
	host.attach(s)
	defer host.detach(s)
	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()
	keepalive := time.NewTicker(sseKeepaliveInterval)
	defer keepalive.Stop()
	for {
		select {
		case f := <-s.frames:
			data, err := json.Marshal(f)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		case <-keepalive.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		case <-s.done:
			return
		case <-r.Context().Done():
			return
		}
	}
}

// browserHostFrames is what the window answers: protocol replies and events,
// and the connections it ended on its own.
func (h *Hub) browserHostFrames(w http.ResponseWriter, r *http.Request) {
	var frames []browserFrame
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<20)).Decode(&frames); err != nil {
		refuse(w, http.StatusBadRequest, codeBrowserHostFrames, "frames must be a JSON array", nil)
		return
	}
	host := h.opts.BrowserHost
	for _, f := range frames {
		c := host.conn(f.Conn)
		if c == nil {
			continue
		}
		if f.Close {
			c.end()
			host.forget(c)
			continue
		}
		if len(f.Message) > 0 {
			c.deliver(f.Message)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}
