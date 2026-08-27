package control

import (
	"fmt"
	"strings"

	"reasonix/internal/event"
	"reasonix/internal/sessioninbox"
)

// ResolvePlanDecision answers the Plan card without collapsing revise and exit.
func (c *Controller) ResolvePlanDecision(id string, action PlanDecisionAction) error {
	return c.ResolvePlanDecisionWithFeedback(id, action, "")
}

// ResolvePlanDecisionWithFeedback durably stages revision guidance before the
// approval is removed. A persistence failure leaves the card pending for retry.
func (c *Controller) ResolvePlanDecisionWithFeedback(id string, action PlanDecisionAction, feedback string) error {
	if c == nil {
		return fmt.Errorf("controller is nil")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("empty plan approval id")
	}
	switch action {
	case PlanDecisionStartExecution, PlanDecisionRevisePlan, PlanDecisionExitPlan:
	default:
		return fmt.Errorf("unknown plan decision %q", action)
	}
	feedback = strings.TrimSpace(feedback)
	var staged sessioninbox.InboxReceipt
	rollback := func() {
		if staged.ItemID != "" && !staged.Idempotent {
			_ = c.DeleteInboxItem(staged.ItemID)
		}
	}
	pending, ok, err := c.approval.resolveToolAfter(id, planApprovalTool, func(p pendingApproval) error {
		if action == PlanDecisionRevisePlan && feedback != "" {
			var enqueueErr error
			staged, enqueueErr = c.EnqueueInbox(InboxRequest{Intent: sessioninbox.IntentFollowup, Display: feedback, Raw: feedback, Submit: feedback, Source: "plan_revision", Idempotency: "plan-revision:" + id})
			if enqueueErr != nil {
				return fmt.Errorf("queue plan revision: %w", enqueueErr)
			}
		}
		if emitErr := c.emitTurnEventChecked(event.Event{Kind: event.PromptAnswered, ItemID: id, Status: event.TurnInProgress}); emitErr != nil {
			rollback()
			return emitErr
		}
		return nil
	})
	if err != nil {
		return err
	}
	if !ok || pending.reply == nil {
		rollback()
		return nil
	}
	pending.kind = "plan"
	c.recordDecisionReceipt(pending, string(action))
	pending.reply <- approvalReply{allow: action == PlanDecisionStartExecution}
	return nil
}
