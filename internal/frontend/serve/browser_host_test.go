package serve

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"reasonix/internal/platform/browser"
)

func browserHostServer(t *testing.T) (*BrowserHost, *httptest.Server) {
	t.Helper()
	bh := NewBrowserHost()
	h := &Hub{opts: HubOptions{BrowserHost: bh}}
	mux := http.NewServeMux()
	h.registerBrowserHostRoutes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return bh, srv
}

// windowStream is the shell's side of the relay: it reads frames as they come.
func windowStream(t *testing.T, ctx context.Context, url string) <-chan browserFrame {
	t.Helper()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url+"/browser-host/stream", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	frames := make(chan browserFrame, 16)
	go func() {
		defer resp.Body.Close()
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() {
			if data, ok := strings.CutPrefix(sc.Text(), "data: "); ok {
				var f browserFrame
				if json.Unmarshal([]byte(data), &f) == nil {
					frames <- f
				}
			}
		}
		close(frames)
	}()
	return frames
}

func nextFrame(t *testing.T, frames <-chan browserFrame) browserFrame {
	t.Helper()
	select {
	case f, ok := <-frames:
		if !ok {
			t.Fatal("the stream ended")
		}
		return f
	case <-time.After(5 * time.Second):
		t.Fatal("no frame arrived")
	}
	return browserFrame{}
}

func postFrames(t *testing.T, url string, frames []browserFrame) {
	t.Helper()
	body, _ := json.Marshal(frames)
	resp, err := http.Post(url+"/browser-host/frames", "application/json", bytes.NewReader(body))
	if err != nil || resp.StatusCode != http.StatusNoContent {
		t.Fatalf("post frames: %v %v", err, resp)
	}
	resp.Body.Close()
}

func waitAttached(t *testing.T, bh *BrowserHost) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		bh.mu.Lock()
		attached := bh.stream != nil
		bh.mu.Unlock()
		if attached {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the window's stream never attached")
}

func TestBrowserHostRelaysAConnectionToTheWindow(t *testing.T) {
	bh, srv := browserHostServer(t)
	if _, err := bh.Dial(context.Background(), "/p"); !errors.Is(err, browser.ErrNoHost) {
		t.Fatalf("dial with no window = %v, want ErrNoHost", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	frames := windowStream(t, ctx, srv.URL)
	waitAttached(t, bh)

	ep, err := bh.Dial(ctx, "/profiles/workspace")
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	open := nextFrame(t, frames)
	if open.Open == "" || strings.Contains(open.Open, "workspace") {
		t.Fatalf("open frame = %+v, want a partition name that does not spell the profile", open)
	}
	if err := ep.WriteMessage([]byte(`{"id":1,"method":"Browser.getVersion"}`)); err != nil {
		t.Fatal(err)
	}
	if got := nextFrame(t, frames); got.Conn != open.Conn || string(got.Message) != `{"id":1,"method":"Browser.getVersion"}` {
		t.Fatalf("message frame = %+v", got)
	}
	postFrames(t, srv.URL, []browserFrame{{Conn: open.Conn, Message: json.RawMessage(`{"id":1,"result":{}}`)}})
	if reply, err := ep.ReadMessage(); err != nil || string(reply) != `{"id":1,"result":{}}` {
		t.Fatalf("reply = %s, %v", reply, err)
	}
	postFrames(t, srv.URL, []browserFrame{{Conn: open.Conn, Close: true}})
	if _, err := ep.ReadMessage(); !errors.Is(err, io.EOF) {
		t.Fatalf("after the window closed the connection, read = %v", err)
	}

	second, err := bh.Dial(ctx, "/profiles/workspace")
	if err != nil {
		t.Fatal(err)
	}
	nextFrame(t, frames)
	cancel()
	if _, err := second.ReadMessage(); !errors.Is(err, io.EOF) {
		t.Fatalf("after the window went away, read = %v", err)
	}
}
