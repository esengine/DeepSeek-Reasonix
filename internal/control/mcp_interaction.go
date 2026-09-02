package control

import (
	"context"
	"fmt"
	"strconv"

	"reasonix/internal/event"
	"reasonix/internal/mcpinteraction"
	"reasonix/internal/provider"
)

// pendingMCPInteraction is one server-initiated elicitation awaiting the user's
// accept/decline/cancel.
type pendingMCPInteraction struct {
	request event.MCPInteraction
	reply   chan mcpinteraction.Result
	queued  bool
}

// mcpInteractionState groups the elicitation bookkeeping so the mutex-guarded
// approvalManager ratchets by one field, not one per map.
type mcpInteractionState struct {
	pending     map[string]pendingMCPInteraction
	resolutions map[string]*promptResolution
}

func (a *approvalManager) registerMCPInteraction(req mcpinteraction.Request) (string, chan mcpinteraction.Result) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.mcpInteractions.pending == nil {
		a.mcpInteractions.pending = map[string]pendingMCPInteraction{}
		a.mcpInteractions.resolutions = map[string]*promptResolution{}
	}
	a.nextID++
	id := strconv.Itoa(a.nextID)
	reply := make(chan mcpinteraction.Result, 1)
	a.mcpInteractions.pending[id] = pendingMCPInteraction{
		request: event.MCPInteraction{
			ID: id, Server: req.Server, Mode: req.Mode, Message: req.Message,
			RequestedSchema: append([]byte(nil), req.RequestedSchema...),
			URL:             req.URL, ElicitationID: req.ElicitationID,
		},
		reply:  reply,
		queued: true,
	}
	return id, reply
}

func (a *approvalManager) markMCPInteractionEmitted(id string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if p, ok := a.mcpInteractions.pending[id]; ok {
		p.queued = false
		a.mcpInteractions.pending[id] = p
	}
}

func (a *approvalManager) cancelMCPInteraction(id string) {
	a.mu.Lock()
	delete(a.mcpInteractions.pending, id)
	if attempt := a.mcpInteractions.resolutions[id]; attempt != nil {
		a.finishMCPInteractionResolutionLocked(id, attempt, context.Canceled)
	}
	a.mu.Unlock()
}

func (a *approvalManager) finishMCPInteractionResolutionLocked(id string, attempt *promptResolution, err error) {
	if attempt == nil || a.mcpInteractions.resolutions[id] != attempt {
		return
	}
	delete(a.mcpInteractions.resolutions, id)
	attempt.err = err
	close(attempt.done)
}

// resolveMCPInteractionAfter mirrors resolveAskAfter: the durable
// PromptAnswered transition is persisted before the waiter is released.
func (a *approvalManager) resolveMCPInteractionAfter(id string, persist func(pendingMCPInteraction) error) (pendingMCPInteraction, bool, error) {
	a.mu.Lock()
	p, ok := a.mcpInteractions.pending[id]
	if !ok {
		a.mu.Unlock()
		return pendingMCPInteraction{}, false, nil
	}
	if inFlight := a.mcpInteractions.resolutions[id]; inFlight != nil {
		a.mu.Unlock()
		return pendingMCPInteraction{}, false, inFlight.wait()
	}
	attempt := newPromptResolution()
	a.mcpInteractions.resolutions[id] = attempt
	a.mu.Unlock()
	if persist != nil {
		if err := persist(p); err != nil {
			a.mu.Lock()
			a.finishMCPInteractionResolutionLocked(id, attempt, err)
			a.mu.Unlock()
			return pendingMCPInteraction{}, false, err
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	current, ok := a.mcpInteractions.pending[id]
	if !ok || a.mcpInteractions.resolutions[id] != attempt || current.reply != p.reply {
		if a.mcpInteractions.resolutions[id] == attempt {
			a.finishMCPInteractionResolutionLocked(id, attempt, context.Canceled)
		}
		return pendingMCPInteraction{}, false, attempt.err
	}
	delete(a.mcpInteractions.pending, id)
	a.finishMCPInteractionResolutionLocked(id, attempt, nil)
	return p, true, nil
}

// snapshotMCPInteractions copies emitted-but-unanswered elicitations for
// frontend replay; queued ones have never been shown and stay hidden.
func (a *approvalManager) snapshotMCPInteractions() []event.MCPInteraction {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]event.MCPInteraction, 0, len(a.mcpInteractions.pending))
	for id, p := range a.mcpInteractions.pending {
		if p.queued {
			continue
		}
		interaction := p.request
		interaction.ID = id
		out = append(out, interaction)
	}
	return out
}

// Interact implements mcpinteraction.Broker: it surfaces one server-initiated
// elicitation to the frontend and blocks for the user's decision, serialized
// against every other user prompt by promptMu. Form values and URL targets
// ride the resolve call only — nothing is logged from here.
func (c *Controller) Interact(ctx context.Context, req mcpinteraction.Request) (mcpinteraction.Result, error) {
	id, reply := c.approval.registerMCPInteraction(req)

	if !c.lockPromptFor(ctx, "mcp interaction") {
		c.approval.cancelMCPInteraction(id)
		return mcpinteraction.Result{Action: mcpinteraction.ActionCancel}, ctx.Err()
	}
	defer c.approval.promptMu.Unlock()

	c.approval.promptEmitMu.Lock()
	payload := event.MCPInteraction{
		ID: id, Server: req.Server, Mode: req.Mode, Message: req.Message,
		RequestedSchema: append([]byte(nil), req.RequestedSchema...),
		URL:             req.URL, ElicitationID: req.ElicitationID,
	}
	if err := event.EmitChecked(c.sink, event.Event{Kind: event.MCPInteractionRequest, ItemID: id, MCPInteraction: payload}); err != nil {
		c.approval.promptEmitMu.Unlock()
		c.approval.cancelMCPInteraction(id)
		return mcpinteraction.Result{Action: mcpinteraction.ActionCancel}, fmt.Errorf("persist elicitation request: %w", err)
	}
	c.approval.markMCPInteractionEmitted(id)
	c.approval.promptEmitMu.Unlock()

	waitCtx, cancelWait := c.approval.waitContext(ctx)
	defer cancelWait()

	select {
	case res := <-reply:
		return res, nil
	case <-waitCtx.Done():
		c.approval.cancelMCPInteraction(id)
		return mcpinteraction.Result{Action: mcpinteraction.ActionCancel}, waitCtx.Err()
	}
}

// AnswerMCPInteraction resolves a pending elicitation with the user's action
// (accept/decline/cancel) and, for accept, the submitted form values.
func (c *Controller) AnswerMCPInteraction(id, action string, content map[string]any) {
	_ = c.AnswerMCPInteractionChecked(id, action, content)
}

// AnswerMCPInteractionChecked persists the prompt transition before releasing
// the blocked MCP call, so a crashed frontend cannot lose an answered decision.
func (c *Controller) AnswerMCPInteractionChecked(id, action string, content map[string]any) error {
	switch action {
	case mcpinteraction.ActionAccept, mcpinteraction.ActionDecline, mcpinteraction.ActionCancel:
	default:
		return fmt.Errorf("invalid elicitation action %q", action)
	}
	if action != mcpinteraction.ActionAccept {
		content = nil
	}
	pending, ok, err := c.approval.resolveMCPInteractionAfter(id, func(p pendingMCPInteraction) error {
		return c.emitTurnEventChecked(event.Event{Kind: event.PromptAnswered, ItemID: id, Status: event.TurnInProgress})
	})
	if err != nil {
		return err
	}
	if ok {
		c.recordMCPInteractionReceipt(id, pending, action)
		pending.reply <- mcpinteraction.Result{Action: action, Content: content}
	}
	return nil
}

func (c *Controller) recordMCPInteractionReceipt(id string, pending pendingMCPInteraction, action string) {
	if c == nil || c.executor == nil {
		return
	}
	receipt := &provider.DecisionReceipt{
		ID:      id,
		Kind:    "mcp_elicitation",
		Subject: clipUTF8(pending.request.Server+" elicitation ("+pending.request.Mode+")", 240),
		Outcome: action,
	}
	c.executor.Session().AddDecisionReceipt(receipt)
}
