package control

import (
	"context"
	"strings"

	"reasonix/internal/agent"
	"reasonix/internal/provider"
)

const ProtocolRecoveryAction = "protocol_recovery"
const RecoverContextCommand = "/recover-context"
const protocolRecoveryPrompt = "Continue the interrupted task from valid history. Preserve completed tool results. Do not repeat completed actions or actions with unknown outcomes; inspect unknown effects with permitted read-only tools first."

func ParseProtocolRecoveryCommand(input string) (id, guidance string, ok bool) {
	parts := strings.Fields(input)
	if len(parts) == 0 || parts[0] != RecoverContextCommand {
		return "", "", false
	}
	if len(parts) > 1 {
		id = parts[1]
	}
	if len(parts) > 2 {
		guidance = strings.Join(parts[2:], " ")
	}
	return id, guidance, true
}

func (c *Controller) protocolRecoveryContext(ctx context.Context, id string) (context.Context, error) {
	if c.executor == nil {
		return ctx, agent.ErrProtocolRecoveryUnavailable
	}
	action := c.executor.PendingProtocolRecovery()
	if action == nil || (id != "" && id != action.ID) {
		return ctx, agent.ErrProtocolRecoveryUnavailable
	}
	return agent.WithInputMessageOrigin(agent.WithProtocolRecovery(ctx, action.ID), provider.MessageOriginHost), nil
}

func (c *Controller) RunProtocolRecoveryWithAdmission(ctx context.Context, id, guidance string, admitted func()) error {
	return c.runSynchronousTurn(ctx, nil, func(runCtx context.Context) error {
		recoveryCtx, err := c.protocolRecoveryContext(runCtx, id)
		if err != nil {
			return err
		}
		if admitted != nil {
			admitted()
		}
		return c.runTurn(recoveryCtx, protocolRecoveryPrompt+recoveryGuidance(guidance))
	})
}

func recoveryGuidance(input string) string {
	if strings.TrimSpace(input) == "" {
		return ""
	}
	return "\n\nUser guidance: " + input
}

func (c *Controller) SubmitProtocolRecovery(id, guidance string) {
	// Bind a token before enqueueing; a later request cannot recover a different
	// incident just because it used the tokenless CLI shortcut.
	if id == "" && c.executor != nil {
		if pending := c.executor.PendingProtocolRecovery(); pending != nil {
			id = pending.ID
		}
	}
	c.runGuarded(func(ctx context.Context) error {
		if id == "" {
			return agent.ErrProtocolRecoveryUnavailable
		}
		recoveryCtx, err := c.protocolRecoveryContext(ctx, id)
		if err != nil {
			return err
		}
		return c.runTurn(recoveryCtx, protocolRecoveryPrompt+recoveryGuidance(guidance))
	})
}

// PendingProtocolRecovery exposes the same admission token on every transport.
func (c *Controller) PendingProtocolRecovery() *provider.ProtocolRecoveryAction {
	if c.executor == nil || c.Running() {
		return nil
	}
	return c.executor.PendingProtocolRecovery()
}
