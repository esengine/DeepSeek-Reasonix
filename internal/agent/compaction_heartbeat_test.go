package agent

import (
	"errors"
	"io"
	"net/http"
	"reflect"
	"testing"
	"testing/synctest"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/provider/openai"
)

func TestCompactionDeadlineSurvivesSSEHeartbeats(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		closed := make(chan struct{})
		requests, heartbeats := 0, 0
		client := &http.Client{Transport: accountingRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			requests++
			reader, writer := io.Pipe()
			go func() {
				defer close(closed)
				defer writer.Close()
				tick := time.NewTicker(10 * time.Second)
				defer tick.Stop()
				for {
					select {
					case <-req.Context().Done():
						return
					case <-tick.C:
						if _, err := io.WriteString(writer, ": heartbeat\n\n"); err != nil {
							return
						}
						heartbeats++
					}
				}
			}()
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: reader}, nil
		})}
		p, err := openai.New(provider.Config{Name: "fixture", BaseURL: "https://example.invalid/v1", Model: "fixture", APIKey: "fixture", HTTPClient: client})
		if err != nil {
			t.Fatal(err)
		}
		sess := foldableSessionOverForce(6)
		original := sess.Snapshot()
		a := New(p, nil, sess, Options{ContextWindow: 5000, CompactRatio: .5}, event.Discard)
		started := time.Now()
		err = a.CompactNow(t.Context(), "")
		elapsed := time.Since(started)
		// The synthetic transport can take another tick to finish its pipe
		// write. Assert the compaction deadline, not transport cleanup latency.
		<-closed
		if !errors.Is(err, errSummaryBudget) || elapsed != compactionBudget {
			t.Fatalf("elapsed=%s err=%v", elapsed, err)
		}
		if requests != 1 || heartbeats < 20 {
			t.Fatalf("requests=%d heartbeats=%d", requests, heartbeats)
		}
		if a.currentProjectionVersion() != 0 || !reflect.DeepEqual(original, sess.Snapshot()) {
			t.Fatal("heartbeat timeout changed the conversation")
		}
	})
}
