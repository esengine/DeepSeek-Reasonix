package openai

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"reasonix/internal/contract/provider"
)

// A relay can send the whole answer and its finish_reason, then keep the
// connection open on heartbeats without ever sending [DONE]. Every heartbeat
// resets the idle watchdog, so the turn would wait for the relay to hang up —
// minutes after the answer was complete.
func relayThatNeverSaysDone(heartbeats bool, usageAfter time.Duration) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"done\"},\"finish_reason\":null}]}\n\n")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
		flush(w)
		if usageAfter > 0 {
			time.Sleep(usageAfter)
			_, _ = io.WriteString(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":2}}\n\n")
			flush(w)
		}
		tick := time.NewTicker(10 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-tick.C:
				if heartbeats {
					_, _ = io.WriteString(w, ": keep-alive\n\n")
					flush(w)
				}
			}
		}
	}
}

func drainStream(t *testing.T, p provider.Provider) (text string, usage *provider.Usage, done bool, failure error, took time.Duration) {
	t.Helper()
	start := time.Now()
	ch, err := p.Stream(context.Background(), provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var sb strings.Builder
	for chunk := range ch {
		switch chunk.Type {
		case provider.ChunkText:
			sb.WriteString(chunk.Text)
		case provider.ChunkUsage:
			usage = chunk.Usage
		case provider.ChunkDone:
			done = true
		case provider.ChunkError:
			failure = chunk.Err
		}
	}
	return sb.String(), usage, done, failure, time.Since(start)
}

func newGraceClient(t *testing.T, url string) provider.Provider {
	t.Helper()
	p, err := New(provider.Config{Name: "relay", BaseURL: url, Model: "m", APIKey: "k"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	p.(*client).postFinishGrace = 150 * time.Millisecond
	return p
}

func TestAStreamThatNeverSaysDoneEndsOnceItsFinishReasonHasHadItsGrace(t *testing.T) {
	for name, heartbeats := range map[string]bool{"silent": false, "heartbeats": true} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(relayThatNeverSaysDone(heartbeats, 0))
			defer srv.Close()
			text, _, done, failure, took := drainStream(t, newGraceClient(t, srv.URL))
			if failure != nil {
				t.Fatalf("a complete answer was reported as a failure: %v", failure)
			}
			if text != "done" || !done {
				t.Fatalf("text=%q done=%v, want the answer and a clean end", text, done)
			}
			if took > 3*time.Second {
				t.Fatalf("took %v, want the stream cut shortly after the grace window", took)
			}
		})
	}
}

func TestAUsageRecordInsideTheGraceWindowIsStillRead(t *testing.T) {
	srv := httptest.NewServer(relayThatNeverSaysDone(true, 40*time.Millisecond))
	defer srv.Close()
	_, usage, done, failure, _ := drainStream(t, newGraceClient(t, srv.URL))
	if failure != nil || !done {
		t.Fatalf("done=%v failure=%v", done, failure)
	}
	if usage == nil || usage.PromptTokens != 10 {
		t.Fatalf("usage = %+v, want the record that arrived inside the window", usage)
	}
}
