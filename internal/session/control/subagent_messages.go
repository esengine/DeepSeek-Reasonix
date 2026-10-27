package control

import (
	"context"
	"fmt"

	"reasonix/internal/runtime/agent"
	"reasonix/internal/runtime/delegation"
)

func (c *Controller) withDelegationContext(ctx context.Context, parent string) context.Context {
	ctx = agent.WithParentSession(ctx, parent)
	ctx = delegation.WithExecutionSessionPath(ctx, c.SessionPath())
	return agent.WithChildMessageReceiver(ctx, func(ctx context.Context, message agent.ChildMessage) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if c.parentSessionID() != parent {
			return fmt.Errorf("the delegating conversation is no longer active")
		}
		if c.SessionPath() == "" {
			if c.executor != nil && c.executor.SteerHostNotice(agent.ChildMessageNotice(message)) {
				return nil
			}
			return fmt.Errorf("the delegating conversation has no inbox or active turn")
		}
		_, err := c.EnqueueHostContinuation(InboxRequest{
			expectedSession: parent,
			Display:         "Sub-agent " + message.Name + ": " + message.Text,
			Submit:          agent.ChildMessageNotice(message), Source: "host:subagent-message",
			Idempotency: "subagent-message:" + message.RunID + ":" + message.Ref + ":" + message.CallID,
		})
		return err
	})
}
