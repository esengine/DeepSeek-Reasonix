package delegation

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"reasonix/internal/runtime/agent"
)

func TestParentMessageBindsIdentityAndRejectsRoutingFields(t *testing.T) {
	var got []agent.ChildMessage
	tl := &parentMessageTool{sender: agent.ChildMessage{Ref: "sa_owned", Name: "review"}, receive: func(_ context.Context, m agent.ChildMessage) error {
		got = append(got, m)
		return nil
	}}
	for _, raw := range []string{`{"message":"hi","to":"other"}`, `{"message":"hi","ref":"sa_spoof"}`, `{"message":""}`} {
		if _, err := tl.Execute(context.Background(), json.RawMessage(raw)); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	out, err := tl.Execute(context.Background(), json.RawMessage(`{"message":"API shape changed; continuing tests"}`))
	if err != nil || !strings.Contains(out, "queued") || len(got) != 1 {
		t.Fatalf("reply = %q, %v; messages = %+v", out, err, got)
	}
	if got[0].Ref != "sa_owned" || got[0].Name != "review" {
		t.Fatalf("unbound sender: %+v", got[0])
	}
}

func TestParentMessageReportsQueueFailureAndBoundsPayload(t *testing.T) {
	want := errors.New("parent closed")
	tl := &parentMessageTool{receive: func(context.Context, agent.ChildMessage) error { return want }}
	if _, err := tl.Execute(context.Background(), json.RawMessage(`{"message":"hello"}`)); !errors.Is(err, want) {
		t.Fatalf("queue error = %v", err)
	}
	raw, _ := json.Marshal(map[string]string{"message": strings.Repeat("x", maxParentMessageBytes+1)})
	if _, err := tl.Execute(context.Background(), raw); err == nil || errors.Is(err, want) {
		t.Fatalf("oversized input reached receiver: %v", err)
	}
}

func TestParentMessageConcurrentAdmissionIsBounded(t *testing.T) {
	var accepted atomic.Int32
	tl := &parentMessageTool{receive: func(context.Context, agent.ChildMessage) error { accepted.Add(1); return nil }}
	var wg sync.WaitGroup
	for range 64 {
		wg.Go(func() { _, _ = tl.Execute(context.Background(), json.RawMessage(`{"message":"progress"}`)) })
	}
	wg.Wait()
	if accepted.Load() != maxParentMessages {
		t.Fatalf("accepted = %d", accepted.Load())
	}
}

func TestParentMessageIDsDoNotCollapseRepeatedProviderCallIDs(t *testing.T) {
	var messages []agent.ChildMessage
	tool := &parentMessageTool{sender: agent.ChildMessage{RunID: "run", Ref: "child"}, receive: func(_ context.Context, message agent.ChildMessage) error {
		messages = append(messages, message)
		return nil
	}}
	ctx := agent.WithCallContext(context.Background(), "reused-id", nil, nil, false)
	for _, body := range []string{`{"message":"first finding"}`, `{"message":"second finding"}`} {
		if _, err := tool.Execute(ctx, json.RawMessage(body)); err != nil {
			t.Fatal(err)
		}
	}
	if messages[0].CallID == messages[1].CallID {
		t.Fatal("distinct reports would share the durable inbox idempotency key")
	}
}
