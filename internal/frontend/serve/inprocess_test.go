package serve

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"reasonix/internal/session/control"
)

func clientPostJSON(t *testing.T, c *http.Client, url, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	return resp
}

// frames reads SSE data frames off an /events stream until ctx ends.
func frames(ctx context.Context, t *testing.T, c *http.Client, url string) <-chan map[string]any {
	t.Helper()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("content type = %q", ct)
	}
	out := make(chan map[string]any, 64)
	go func() {
		defer close(out)
		defer resp.Body.Close()
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() {
			data, ok := strings.CutPrefix(sc.Text(), "data: ")
			if !ok {
				continue
			}
			var ev map[string]any
			if json.Unmarshal([]byte(data), &ev) == nil {
				out <- ev
			}
		}
	}()
	return out
}

// A frontend in the kernel's process reaches the same routes with no listener,
// and the event stream arrives as it is written rather than when it ends.
func TestInProcessClientStreamsEventsAndRunsTheUsersShell(t *testing.T) {
	h, _, _ := adoptedPane(t, control.ToolApprovalAsk)
	c := h.InProcessClient()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	events := frames(ctx, t, c, "http://reasonix.local/rt/r1/events")

	resp := clientPostJSON(t, c, "http://reasonix.local/rt/r1/submit", `{"input":"!echo in-process-shell"}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("in-process shell submit = %d, want 202", resp.StatusCode)
	}
	for ev := range events {
		if ev["kind"] != "tool_result" {
			continue
		}
		tool, _ := ev["tool"].(map[string]any)
		if out, _ := tool["output"].(string); !strings.Contains(out, "in-process-shell") {
			t.Fatalf("shell result output = %q", out)
		}
		return
	}
	t.Fatal("the event stream ended before the shell's result arrived")
}

// The same request off a socket is refused: only the in-process transport can
// mark a request, and nothing a network client sends reaches that mark.
func TestShellOverTheNetworkStaysRefused(t *testing.T) {
	h, _, _ := adoptedPane(t, control.ToolApprovalAsk)
	srv := httptest.NewServer(operatorHandler(h))
	defer srv.Close()
	resp := clientPostJSON(t, srv.Client(), srv.URL+"/rt/r1/submit", `{"input":"!echo over-the-network"}`)
	defer resp.Body.Close()
	var reason struct {
		Code string `json:"code"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&reason)
	if resp.StatusCode != http.StatusForbidden || reason.Code != "shell.unavailable_over_http" {
		t.Fatalf("network shell submit = %d %q, want 403 shell.unavailable_over_http", resp.StatusCode, reason.Code)
	}
}

// A client that stops reading ends the handler instead of leaving it blocked
// on a pipe nobody drains.
func TestInProcessStreamEndsWithItsCaller(t *testing.T) {
	h, _, _ := adoptedPane(t, control.ToolApprovalAsk)
	ctx, cancel := context.WithCancel(context.Background())
	events := frames(ctx, t, h.InProcessClient(), "http://reasonix.local/rt/r1/events")
	cancel()
	select {
	case _, open := <-events:
		for open {
			_, open = <-events
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the stream outlived its caller")
	}
}
