package control

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/permission"
	"reasonix/internal/planmode"
	"reasonix/internal/provider"
	"reasonix/internal/sandbox"
	"reasonix/internal/tool"
)

// PendingPrompt reports whether the current turn is blocked waiting for a user
// approval, plan approval, memory approval, or ask-tool answer.
func (c *Controller) PendingPrompt() bool {
	return c.approval.hasPending()
}

// Approve answers a pending ApprovalRequest by ID: allow runs the call, session
// also remembers a grant for the rest of the session so the same approval scope
// is not re-prompted. Unknown/expired IDs are ignored.
func (c *Controller) Approve(id string, allow, session, persist bool) {
	// Recovery cards are strict fresh decisions. Prefer ResolveRecovery so a
	// continue/deny from an old client that only knows Approve still maps onto
	// the recovery state machine (allow=continue, deny=revise without feedback).
	// Session/persist grants are intentionally ignored for recovery.
	//
	// Lookup must use the live waiter table (HasApproval), not Snapshot: pre-
	// normal-execution plan prompts park a waiter without an armed taskRuntime, so
	// they never appear in the persistence snapshot.
	c.mu.Lock()
	gate := c.recoveryGate
	c.mu.Unlock()
	if gate != nil && gate.HasApproval(id) {
		action := agent.RecoveryActionRevise
		if allow {
			action = agent.RecoveryActionContinue
		}
		_ = c.ResolveRecovery(id, action, "")
		return
	}
	pending := c.approval.resolve(id)
	if pending.reply == nil {
		return
	}
	outcome := "deny"
	if pending.tool == planApprovalTool {
		outcome = string(PlanDecisionRevisePlan)
		if allow {
			outcome = string(PlanDecisionStartExecution)
		}
	} else if allow {
		switch {
		case persist:
			outcome = "allow_persistent"
		case session:
			outcome = "allow_session"
		default:
			outcome = "allow_once"
		}
	}
	c.recordDecisionReceipt(pending, outcome)
	pending.reply <- approvalReply{allow: allow, session: session, persist: persist} // buffered, never blocks
}

// ResolvePlanDecision answers the Plan card without collapsing revise and exit
// into the generic approval boolean used by older clients.
func (c *Controller) ResolvePlanDecision(id string, action PlanDecisionAction) error {
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
	pending, ok := c.approval.resolveTool(id, planApprovalTool)
	if !ok || pending.reply == nil {
		return fmt.Errorf("plan approval %q is no longer pending", id)
	}
	pending.kind = "plan"
	c.recordDecisionReceipt(pending, string(action))
	// Move before answering, so the waiting turn sees the state the user chose.
	// Start stays with that turn: it holds the state it submitted, so only it
	// can tell an approval from a late one.
	switch action {
	case PlanDecisionRevisePlan:
		c.plan().Apply(planmode.Revise)
		c.sharePlanRuntime()
	case PlanDecisionExitPlan:
		c.plan().Apply(planmode.Exit)
		c.sharePlanRuntime()
	}
	pending.reply <- approvalReply{allow: action == PlanDecisionStartExecution}
	return nil
}

func (c *Controller) recordDecisionReceipt(pending pendingApproval, outcome string) {
	if c == nil || c.executor == nil || pending.reply == nil {
		return
	}
	kind := pending.kind
	if kind == "" {
		kind = "tool"
		if pending.tool == planApprovalTool {
			kind = "plan"
		}
	}
	receipt := &provider.DecisionReceipt{
		ID:      pending.id,
		Kind:    kind,
		Tool:    strings.TrimSpace(pending.tool),
		Subject: clipUTF8(strings.TrimSpace(pending.subject), 240),
		Outcome: strings.TrimSpace(outcome),
	}
	// Keep the receipt bounded and provider-excluded even when an older caller
	// omits optional approval metadata.
	c.executor.Session().AddDecisionReceipt(receipt)
	c.sink.Emit(event.Event{
		Kind:            event.Notice,
		Code:            event.NoticeCodeDecisionReceipt,
		Level:           event.LevelInfo,
		Text:            "Decision recorded: " + receipt.Outcome,
		DecisionReceipt: receipt,
	})
}

// EnableInteractiveApproval swaps the executor's gate for one that routes
// approval decisions to the frontend via ApprovalRequest events, and wires the
// controller in as the executor's Asker so the `ask` tool can question the user.
// Interactive frontends (chat, desktop) call this; the headless run keeps the
// silent gate and a nil asker from setup.
func (c *Controller) EnableInteractiveApproval() {
	escapeApprover := sandboxEscapeApprover{c}
	configApprover := managedConfigWriteApprover{c}
	if c.executor != nil {
		c.executor.SetGate(c.newInteractiveGate())
		c.executor.SetSandboxEscapeApprover(escapeApprover)
		c.executor.SetConfigWriteApprover(configApprover)
		c.executor.SetAsker(c)
	}
	if setter, ok := c.runner.(interface {
		SetSandboxEscapeApprover(sandbox.EscapeApprover)
	}); ok {
		setter.SetSandboxEscapeApprover(escapeApprover)
	}
	if setter, ok := c.runner.(interface {
		SetConfigWriteApprover(tool.ConfigWriteApprover)
	}); ok {
		setter.SetConfigWriteApprover(configApprover)
	}
	if setter, ok := c.runner.(interface {
		SetPlannerPlanApprover(agent.PlannerPlanApprover)
	}); ok {
		setter.SetPlannerPlanApprover(plannerPlanApprover{c: c})
	}
	// The planner holds the real ask tool, so it reaches the same approval
	// surface the executor does instead of a parallel prose-question path.
	if setter, ok := c.runner.(interface{ SetAsker(agent.Asker) }); ok {
		setter.SetAsker(c)
	}
}

func (c *Controller) newInteractiveGate() *permission.Gate {
	policy := c.policy
	mode := c.approval.mode()
	switch mode {
	case ToolApprovalAuto, ToolApprovalYolo:
		policy.Mode = permission.Allow
	case ToolApprovalDontAsk:
		policy.Mode = permission.Deny
	default:
		policy.Mode = permission.Ask
	}
	// A session allowlist (e.g. --allowed-tools) must never satisfy a tool that
	// requires fresh human approval on every call — memory remember/forget, plan
	// approval, sandbox escape, managed config write. SessionAllow is checked
	// before Ask in Policy.Decide, so leaving those entries in would let
	// `--allowed-tools remember` write memory with no prompt. Strip them so the
	// forced Ask rules below stay authoritative.
	policy.SessionAllow = rulesWithoutFreshHumanApproval(policy.SessionAllow)
	policy.Ask = append(policy.Ask,
		permission.Rule{Tool: memoryRememberTool},
		permission.Rule{Tool: memoryForgetTool},
	)
	var approver permission.Approver = gateApprover{c}
	if mode == ToolApprovalDontAsk {
		approver = denyPermissionApprover{}
	}
	gate := permission.NewGate(policy, approver)
	gate.OnRemember = func(rule string) {
		if c.onRemember != nil {
			_ = c.onRemember(rule)
		}
	}
	return gate
}

func (c *Controller) newHeadlessGate(mode string) *freshHumanHeadlessGate {
	gate := BuildHeadlessApprovalGate(c.policy, mode)
	gate.allowLowRiskFreshAction = func(toolName string, args json.RawMessage) bool {
		return toolName == memoryRememberTool && c.allowLowRiskRemember(args)
	}
	return gate
}

// ApplyHeadlessApprovalMode configures the executor gate for a non-interactive
// (`reasonix run`) session from an explicit --permission-mode. Unlike
// EnableInteractiveApproval it installs no blocking approver, asker, or
// fresh-approval prompt: there is no key loop to answer them, and the default
// infinite approval timeout would wedge the run forever on an Ask rule, the
// `ask` tool, or a sandbox/config approval. Modes map straight onto a headless
// gate, and each preserves the interactive contract as closely as a run with no
// one to prompt allows:
//
//   - auto: auto-approve the writer fallback (Mode=Allow) but PRESERVE explicit
//     ask rules. Interactive auto prompts on those (it never auto-approves them);
//     headless can't prompt, so a would-ask decision fails closed (deny) rather
//     than running silently. Only bypass may run such a command unattended.
//   - yolo/bypassPermissions: skip ordinary approval-gated decisions (nil
//     approver); deny rules and fresh decisions still fail closed.
//   - dontAsk: deny anything that would ask, and deny the writer fallback too.
//
// Deny rules and fresh-human tools (memory, plan, sandbox, config) stay enforced
// by the gate for every mode. The only exception is a controller-assessed,
// create-only project/reference memory; every other memory write remains denied.
func (c *Controller) ApplyHeadlessApprovalMode(mode string) {
	mode = normalizeToolApprovalMode(mode)
	c.approval.setMode(mode)
	if c.subagentGate != nil {
		c.subagentGate.Update(mode)
	}
	if c.executor != nil {
		c.executor.SetGate(c.newHeadlessGate(mode))
	}
}

func (c *Controller) refreshInteractiveGate() {
	if c.executor != nil {
		c.executor.SetGate(c.newInteractiveGate())
	}
}

// Ask implements agent.Asker: it emits an AskRequest and blocks until
// AnswerQuestion(ID, …) answers or ctx is cancelled. promptMu serialises it
// against tool-approval prompts so at most one user prompt is outstanding.
// Unlike tool-approval gates, Ask is NOT bypassed in YOLO mode — the `ask`
// tool exists to get a genuine user decision, and YOLO only auto-approves
// tool calls; it must not answer the user's questions for them.
func (c *Controller) Ask(ctx context.Context, questions []event.AskQuestion) ([]event.AskAnswer, error) {
	// Registering after the lock left a queued question invisible everywhere:
	// no event, absent from the snapshot, unreachable by ReplayPendingPrompts.
	// The record comes first: registration is what makes it answerable.
	id := c.approval.nextAskID()
	if err := c.openBarrier(id, string(DecisionAsk), barrierSummary(questions)); err != nil {
		return nil, err
	}
	reply := c.approval.registerAsk(id, questions)

	if !c.lockPromptFor(ctx, "question") {
		c.approval.cancelAsk(id)
		c.closeBarrier(id, barrierCancelled)
		return nil, ctx.Err()
	}
	defer c.approval.promptMu.Unlock()

	c.approval.promptEmitMu.Lock()
	c.approval.markAskEmitted(id)
	c.sink.Emit(event.Event{Kind: event.AskRequest, Ask: event.Ask{ID: id, Questions: questions}})
	c.approval.promptEmitMu.Unlock()

	waitCtx, cancelWait := c.approval.waitContext(ctx)
	defer cancelWait()

	select {
	case ans := <-reply:
		c.closeBarrier(id, barrierResolved)
		return ans, nil
	case <-waitCtx.Done():
		c.approval.cancelAsk(id)
		c.closeBarrier(id, barrierCancelled)
		return nil, waitCtx.Err()
	}
}

// AnswerQuestion resolves a pending AskRequest by ID with the user's selections.
// Unknown/expired IDs are ignored.
func (c *Controller) AnswerQuestion(id string, answers []event.AskAnswer) {
	if pending, ok := c.approval.resolveAsk(id); ok {
		// An answer batch with no selections is the explicit "skip and continue
		// chat" path. End the current turn instead of feeding a prose dismissal
		// back to the model and trusting it not to ask again (#6869).
		if !askAnswersHaveSelection(answers) {
			c.mu.Lock()
			activeTurn := c.gate.cancel != nil
			c.mu.Unlock()
			if activeTurn {
				c.Cancel()
				return
			}
		}
		c.recordAskDecisionReceipt(id, pending, answers)
		pending.reply <- answers // buffered, never blocks
	}
}

func (c *Controller) recordAskDecisionReceipt(id string, pending pendingAsk, answers []event.AskAnswer) {
	if c == nil || c.executor == nil {
		return
	}
	selected := make(map[string][]string, len(answers))
	for _, answer := range answers {
		selected[answer.QuestionID] = append([]string(nil), answer.Selected...)
	}
	parts := make([]string, 0, len(pending.questions))
	for _, question := range pending.questions {
		answer := strings.TrimSpace(strings.Join(selected[question.ID], ", "))
		if answer == "" {
			answer = "—"
		}
		prompt := strings.TrimSpace(question.Prompt)
		if prompt == "" {
			prompt = strings.TrimSpace(question.Header)
		}
		if prompt == "" {
			prompt = question.ID
		}
		parts = append(parts, prompt+": "+answer)
	}
	receipt := &provider.DecisionReceipt{
		ID:      id,
		Kind:    "ask",
		Subject: clipUTF8(strings.Join(parts, " · "), 240),
		Outcome: "answered",
	}
	c.executor.Session().AddDecisionReceipt(receipt)
	c.sink.Emit(event.Event{
		Kind:            event.Notice,
		Code:            event.NoticeCodeDecisionReceipt,
		Level:           event.LevelInfo,
		Text:            "Decision recorded: answered",
		DecisionReceipt: receipt,
	})
}

// ReplayPendingPrompts re-emits the ApprovalRequest / AskRequest event for every
// prompt currently blocking the run loop. A frontend that reconnected or reloaded
// after the original event has no way to rebuild its approval/ask modal otherwise,
// so the blocked gate goroutine stays stuck forever while the session shows a
// "waiting" status with no actionable prompt. promptMu serialises Ask and
// requestApproval, so in practice at most one prompt is outstanding; the loops
// stay general so a future concurrent prompt would still replay correctly.
func (c *Controller) ReplayPendingPrompts() {
	c.approval.promptEmitMu.Lock()
	noApprovals := c.replayPendingPromptsTo(c.sink)
	c.approval.promptEmitMu.Unlock()
	if noApprovals {
		// Retained compatibility hook; live Auto Guard cards are ordinary approvals.
		c.ReplayUnresolvedRecoveries()
	}
}

// ReplayPendingPromptsWith performs an SSE connection handoff while prompt
// registration and emission are paused. The factory must subscribe the new
// client and return a sink that targets it; this closes the attach race where
// the original prompt could otherwise land between Subscribe and replay.
func (c *Controller) ReplayPendingPromptsWith(sinkFactory func() event.Sink) {
	if sinkFactory == nil {
		return
	}
	c.approval.promptEmitMu.Lock()
	defer c.approval.promptEmitMu.Unlock()
	c.replayPendingPromptsTo(sinkFactory())
}

func (c *Controller) replayPendingPromptsTo(sink event.Sink) bool {
	approvals, asks := c.approval.snapshotPrompts()
	c.emitPendingPrompts(sink, approvals, asks)
	return len(approvals) == 0
}

func (c *Controller) emitPendingPrompts(sink event.Sink, approvals []event.Approval, asks []event.Ask) {
	if sink == nil {
		return
	}
	for _, a := range approvals {
		sink.Emit(c.approvalRequestEvent(a))
	}
	for _, a := range asks {
		sink.Emit(event.Event{Kind: event.AskRequest, Ask: a})
	}
}

// SetToolApprovalMode changes the runtime approval posture for permission-gated
// tools. It does not answer business asks or plan approval. Sub-agents (task,
// writer-capable skill sub-agents, the planner) have no UI to prompt through,
// so this also pushes the mode to the shared headless gate they read from —
// without it, a mode switch (Shift+Tab) would only rebuild the parent
// executor's gate and leave sub-agents pinned to whatever mode was active
// when the session booted.
func (c *Controller) SetToolApprovalMode(mode string) {
	c.ApplyToolApprovalMode(mode)
}

// ApplyToolApprovalMode is SetToolApprovalMode reporting which pending
// approval prompt ids the new posture auto-allowed. Prompts NOT in the
// returned set are still pending here — fresh user decisions (plan, memory,
// sandbox escape) never drain, and auto keeps approvals an allow policy would
// not cover — so a frontend must keep showing them instead of assuming the
// posture switch resolved everything (#6432).
func (c *Controller) ApplyToolApprovalMode(mode string) []string {
	mode = normalizeToolApprovalMode(mode)
	// Capture mode-change recovery dismissals before approval drain so a
	// same-value hydrate/reconcile never rotates Episode state, while a real
	// Auto↔Yolo/Ask switch clears temporary failure/reviewer locks and waiters
	// without auto-approving the original mutation.
	var recoveryDismissed []string
	c.mu.Lock()
	gate := c.recoveryGate
	c.mu.Unlock()
	if gate != nil {
		if ctrl, ok := any(gate).(agent.RecoveryEpisodeControl); ok {
			// Do not hold controller/approval locks while rotating the gate.
			recoveryDismissed = ctrl.OnModeChange(mode)
		}
	}
	pending := c.approval.setMode(mode)
	if c.subagentGate != nil {
		c.subagentGate.Update(mode)
	}
	c.refreshInteractiveGate()
	// Clear recovery cards dismissed by the mode switch outside the gate lock.
	for _, id := range recoveryDismissed {
		p := c.approval.resolve(id)
		if p.reply != nil {
			// Do not approve the pending mutation; signal cancel/deny so legacy
			// paths drop the card.
			select {
			case p.reply <- approvalReply{allow: false}:
			default:
			}
		}
	}
	drained := make([]string, 0, len(pending))
	for _, p := range pending {
		p.reply <- approvalReply{allow: true}
		drained = append(drained, p.id)
	}
	return drained
}

func (c *Controller) ToolApprovalMode() string {
	return c.approval.mode()
}

// SetAutoApproveTools turns YOLO tool auto-approval on or off for the session:
// while on, every tool approval request is auto-allowed (writers and bash run
// without asking). Ask requests and plan approval still reach the user. Deny
// rules still block. Runtime-only — never written to config.
func (c *Controller) SetAutoApproveTools(on bool) {
	if on {
		c.SetToolApprovalMode(ToolApprovalYolo)
		return
	}
	c.SetToolApprovalMode(ToolApprovalAsk)
}

// AutoApproveTools reports whether YOLO tool auto-approval is on,
// for status indicators and mode persistence.
func (c *Controller) AutoApproveTools() bool {
	return c.ToolApprovalMode() == ToolApprovalYolo
}

// requestApproval asks, then applies what the answer authorises — a session
// grant, a remembered rule — before reporting whether the tool may run.
func (c *Controller) requestApproval(ctx context.Context, req approvalRequest) (bool, bool, error) {
	r, err := c.requestApprovalDecision(ctx, req)
	if err != nil {
		return false, false, err
	}
	// Plan approvals are one-shot — never persist a session grant for them, or
	// every future plan would auto-approve.
	if r.allow && r.session && !requiresFreshApprovalTool(req.tool) {
		c.approval.grantSession(req.tool, req.subject)
	}
	if r.allow && r.persist && !requiresFreshApprovalTool(req.tool) && c.onRemember != nil {
		c.emitRememberResult(c.onRemember(permission.RememberRuleForScope(req.tool, req.subject)))
	}
	return r.allow, false, nil
}

// requestApprovalDecision emits an ApprovalRequest and blocks until
// Approve(ID, …) answers or ctx is cancelled. A prior session grant (or bypass
// posture) for the same scope short-circuits; the approvalManager's promptMu
// serialises outstanding prompts. It authorises nothing on its own — that is
// requestApproval's half.
func (c *Controller) requestApprovalDecision(ctx context.Context, req approvalRequest) (approvalReply, error) {
	// YOLO/full access and the just-approved-plan execution window auto-allow
	// approval-gated tools without prompting. Plan approval is a user decision,
	// not a tool permission, so it deliberately stays interactive.
	if c.approval.preApprovedForDecisionOptions(req.tool, req.subject, req.args, req.fresh, req.requireHuman) {
		return approvalReply{allow: true}, nil
	}

	c.approval.promptMu.Lock()
	defer c.approval.promptMu.Unlock()

	// Re-check: a session grant may have landed while we queued behind another
	// prompt for the same subject.
	if c.approval.preApprovedForDecisionOptions(req.tool, req.subject, req.args, req.fresh, req.requireHuman) {
		return approvalReply{allow: true}, nil
	}

	// Claude's PermissionRequest contract answers the dialog on the plugin's
	// behalf (auto-allow/auto-deny) instead of merely observing it, so a
	// decision here must preempt the prompt rather than just notify — this
	// runs synchronously and before the dialog is shown. Native Reasonix
	// PermissionRequest hooks stay advisory-only (see claudePermissionBlocking).
	//
	// A hook's auto-allow must never stand in for a human-required decision:
	// sandbox escapes, Reasonix config writes, memory remember/forget, and
	// plan approval (RequiresFreshHumanApprovalTool) are deliberately excluded
	// from YOLO/auto-approval and Guardian too, so a broadly-matched plugin
	// hook returning "allow" can't silently rubber-stamp them. A deny still
	// applies universally — refusing is always safe to honor automatically.
	if hookSubject, hookArgs, ok := permissionRequestHookPayload(req.tool, req.subject, req.args); ok {
		if decision, _ := c.hooks.PermissionRequest(ctx, req.tool, hookSubject, hookArgs); decision != nil {
			switch {
			case !*decision:
				return approvalReply{}, nil
			case !req.fresh && !req.requireHuman && !requiresFreshApprovalTool(req.tool):
				return approvalReply{allow: true}, nil
			}
			// An "allow" opinion on a fresh-human-required decision is
			// ignored; fall through to the normal interactive prompt.
		}
	}

	c.approval.promptEmitMu.Lock()
	var id string
	var reply chan approvalReply
	if req.fresh || req.requireHuman || req.tool == planApprovalTool {
		kind := ""
		if req.tool == planApprovalTool {
			kind = "plan"
		}
		id, reply = c.approval.registerDecisionKindWithInput(req.tool, req.subject, req.reason, req.args, req.fresh, req.requireHuman, kind, nil)
	} else {
		id, reply = c.approval.registerWithInput(req.tool, req.subject, req.reason, req.args)
	}

	c.sink.Emit(c.approvalRequestEvent(event.Approval{ID: id, Tool: req.tool, Subject: req.subject, Reason: req.reason, RawInput: append(json.RawMessage(nil), req.args...), Fresh: req.fresh}))
	c.approval.promptEmitMu.Unlock()
	// The agent now needs the user's attention; a Notification hook can ping an
	// external channel (desktop notice, phone) while the run blocks on the reply.
	go c.hooks.Notification(ctx, approvalNotificationText(req.tool, req.subject), "permission_prompt")

	waitCtx, cancelWait := c.approval.waitContext(ctx)
	defer cancelWait()

	select {
	case r := <-reply:
		return r, nil
	case <-waitCtx.Done():
		c.approval.cancel(id)
		return approvalReply{}, waitCtx.Err()
	}
}

func (c *Controller) approvalRequestEvent(approval event.Approval) event.Event {
	return event.Event{Kind: event.ApprovalRequest, Approval: approval}
}
