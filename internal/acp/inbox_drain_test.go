package acp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"reasonix/internal/control"
	"reasonix/internal/event"
)

type durableQueueFactory struct {
	dir      string
	behavior func(ctx context.Context, sink event.Sink, input string) error
}

func (f *durableQueueFactory) SessionDir() string { return f.dir }

func (f *durableQueueFactory) NewSession(_ context.Context, p SessionParams) (*control.Controller, error) {
	runner := &fakeRunner{sink: p.Sink, behavior: f.behavior}
	return control.New(control.Options{Runner: runner, Sink: p.Sink, SessionDir: f.dir}), nil
}

func TestSessionPromptDrainsDurableFollowupBeforeResponding(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	inputs := make(chan string, 2)
	factory := &durableQueueFactory{
		dir: t.TempDir(),
		behavior: func(ctx context.Context, _ event.Sink, input string) error {
			inputs <- input
			if input == "start" {
				close(started)
				select {
				case <-release:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			return nil
		},
	}
	client, stop := startServer(t, factory)
	defer stop()
	sessionID := openSession(t, client)
	promptCh := client.callAsync("session/prompt", SessionPromptParams{
		SessionID: sessionID,
		Prompt:    []ContentBlock{{Type: "text", Text: "start"}},
	})
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("first ACP prompt did not start")
	}
	enqueue := client.call(t, sessionInboxEnqueueMethod, SessionInboxEnqueueParams{
		SessionID: sessionID, Text: "queued followup", Intent: "followup", IdempotencyKey: "acp-msg-1",
	})
	if enqueue.Error != nil {
		t.Fatalf("durable enqueue failed: %+v", enqueue.Error)
	}
	close(release)
	for _, want := range []string{"start", "queued followup"} {
		select {
		case got := <-inputs:
			if got != want {
				t.Fatalf("ACP input = %q, want %q", got, want)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("missing ACP input %q", want)
		}
	}
	_, promptResp := drainPrompt(t, client, promptCh)
	if promptResp.Error != nil {
		t.Fatalf("session/prompt errored: %+v", promptResp.Error)
	}
	listed := client.call(t, sessionInboxListMethod, map[string]string{"sessionId": sessionID})
	if listed.Error != nil {
		t.Fatalf("inbox list errored: %+v", listed.Error)
	}
	var snapshot struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(listed.Result, &snapshot); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Items) != 0 {
		t.Fatalf("ACP left completed durable items queued: %s", listed.Result)
	}
}
