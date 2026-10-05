package openai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"reasonix/internal/contract/provider"
)

// sseServer answers one streaming request with the given SSE frames.
func sseServer(t *testing.T, frames ...string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		for _, f := range frames {
			_, _ = w.Write([]byte("data: " + f + "\n\n"))
			w.(http.Flusher).Flush()
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func streamErr(t *testing.T, srv *httptest.Server) (string, error) {
	t.Helper()
	p, err := New(provider.Config{Name: "relay", BaseURL: srv.URL, Model: "m", APIKey: "k"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ch, err := p.Stream(context.Background(), provider.Request{
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
	})
	if err != nil {
		return "", err
	}
	var text strings.Builder
	var last error
	for c := range ch {
		if c.Type == provider.ChunkText {
			text.WriteString(c.Text)
		}
		if c.Type == provider.ChunkError {
			last = c.Err
		}
	}
	return text.String(), last
}

const upstreamDown = `{"error":{"message":"Upstream service temporarily unavailable"}}`

// A gateway that answers 200 and then reports its own upstream failure inside
// the stream never reaches the status ladder — the headers succeeded. Before
// it has said anything, nothing is committed, so it is the same shape as a
// transport cut and belongs to the Agent's replay rather than being terminal.
func TestUpstreamErrorBeforeAnyOutputIsReplayable(t *testing.T) {
	text, err := streamErr(t, sseServer(t, upstreamDown))
	if text != "" {
		t.Fatalf("emitted %q before the error; the premise of this test is that nothing was said", text)
	}
	if !provider.IsStreamInterrupted(err) {
		t.Fatalf("err = %v (%T), want a stream interruption the Agent can replay", err, err)
	}
	if got := provider.StreamInterruptReason(err); got != provider.StreamInterruptUpstreamError {
		t.Fatalf("reason = %q, want %q", got, provider.StreamInterruptUpstreamError)
	}
	// The wording the gateway chose still reaches the user unchanged.
	if err.Error() != "relay: Upstream service temporarily unavailable" {
		t.Fatalf("message = %q, want the gateway's own sentence", err.Error())
	}
}

// After output has been emitted a replay would duplicate it, so the same
// failure stays terminal.
func TestUpstreamErrorAfterOutputStaysTerminal(t *testing.T) {
	text, err := streamErr(t, sseServer(t,
		`{"choices":[{"delta":{"content":"partial "}}]}`,
		upstreamDown,
	))
	if text != "partial " {
		t.Fatalf("text = %q, want the partial output kept", text)
	}
	if provider.IsStreamInterrupted(err) {
		t.Fatal("a failure after output was replayed; that duplicates what the user already saw")
	}
	if err == nil || err.Error() != "relay: Upstream service temporarily unavailable" {
		t.Fatalf("err = %v, want the gateway's sentence surfaced as terminal", err)
	}
}
