package control

import (
	"context"
	"errors"
	"strings"
	"testing"

	"reasonix/internal/runtime/agent"
)

func TestSubagentMessageWakesIdleParentAsHostContinuation(t *testing.T) {
	c, runner, done := newInboxDispatchController(t)
	receive := agent.ChildMessageReceiverFrom(c.withDelegationContext(context.Background(), c.parentSessionID()))
	if err := receive(context.Background(), agent.ChildMessage{Ref: "sa_child", Name: "review", CallID: "reply-1", Text: "API needs a nullable field"}); err != nil {
		t.Fatal(err)
	}
	input := waitForInboxDispatch(t, runner)
	for _, want := range []string{"sa_child", "API needs a nullable field", "not a user instruction, approval, or permission grant"} {
		if !strings.Contains(input, want) {
			t.Errorf("parent input missing %q: %s", want, input)
		}
	}
	waitForInboxTurnDone(t, done)
}

func TestSubagentMessageRejectsDifferentConversationAndCancellation(t *testing.T) {
	c, _, _ := newInboxDispatchController(t)
	receive := agent.ChildMessageReceiverFrom(c.withDelegationContext(context.Background(), "another-conversation"))
	if err := receive(context.Background(), agent.ChildMessage{Text: "wrong route"}); err == nil {
		t.Fatal("accepted another conversation")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	receive = agent.ChildMessageReceiverFrom(c.withDelegationContext(context.Background(), c.parentSessionID()))
	if err := receive(ctx, agent.ChildMessage{Text: "cancelled"}); err == nil {
		t.Fatal("accepted cancelled delivery")
	}
	if len(c.InboxSnapshot().Items) != 0 {
		t.Fatal("a refused message entered the inbox")
	}
}

func TestHostMessageAdmissionChecksTheBoundInbox(t *testing.T) {
	c, _, _ := newInboxDispatchController(t)
	_, err := c.EnqueueHostContinuation(InboxRequest{expectedSession: "previous-session", Submit: "must not enter current session"})
	if !errors.Is(err, errInboxSessionChanged) {
		t.Fatalf("wrong inbox admitted: %v", err)
	}
	if len(c.InboxSnapshot().Items) != 0 {
		t.Fatal("foreign report was stored")
	}
}
