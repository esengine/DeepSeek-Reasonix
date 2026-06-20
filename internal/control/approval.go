package control

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"reasonix/internal/event"
	"reasonix/internal/i18n"
	"reasonix/internal/memory"
	"reasonix/internal/permission"
)

// Approve answers a pending ApprovalRequest by ID: allow runs the call, session
// also remembers a grant for the rest of the session so the same approval scope
// is not re-prompted. Unknown/expired IDs are ignored.
func (c *Controller) Approve(id string, allow, session, persist bool) {
	c.mu.Lock()
	pending := c.approvals[id]
	delete(c.approvals, id)
	c.mu.Unlock()
	if pending.reply != nil {
		pending.reply <- approvalReply{allow: allow, session: session, persist: persist} // buffered, never blocks
	}
}

// EnableInteractiveApproval swaps the executor's gate for one that routes
// approval decisions to the frontend via ApprovalRequest events, and wires the
// controller in as the executor's Asker so the `ask` tool can question the user.
// Interactive frontends (chat, desktop) call this; the headless run keeps the
// silent gate and a nil asker from setup.
func (c *Controller) EnableInteractiveApproval() {
	if c.executor != nil {
		c.executor.SetGate(c.newInteractiveGate())
		c.executor.SetAsker(c)
	}
}

func normalizeToolApprovalMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case ToolApprovalAuto, "approve", "allow":
		return ToolApprovalAuto
	case ToolApprovalYolo, "full", "full-access", "bypass":
		return ToolApprovalYolo
	default:
		return ToolApprovalAsk
	}
}

func (c *Controller) newInteractiveGate() *permission.Gate {
	policy := c.policy
	c.mu.Lock()
	mode := normalizeToolApprovalMode(c.toolApprovalMode)
	c.mu.Unlock()
	switch mode {
	case ToolApprovalAuto, ToolApprovalYolo:
		policy.Mode = permission.Allow
	default:
		policy.Mode = permission.Ask
	}
	policy.Ask = append(policy.Ask,
		permission.Rule{Tool: memoryRememberTool},
		permission.Rule{Tool: memoryForgetTool},
	)
	gate := permission.NewGate(policy, gateApprover{c})
	gate.OnRemember = func(rule string) {
		if c.onRemember != nil {
			_ = c.onRemember(rule)
		}
	}
	return gate
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
	c.promptMu.Lock()
	defer c.promptMu.Unlock()

	c.mu.Lock()
	c.nextID++
	id := strconv.Itoa(c.nextID)
	reply := make(chan []event.AskAnswer, 1)
	c.asks[id] = pendingAsk{questions: questions, reply: reply}
	c.mu.Unlock()

	c.sink.Emit(event.Event{Kind: event.AskRequest, Ask: event.Ask{ID: id, Questions: questions}})

	waitCtx, cancelWait := c.approvalWaitContext(ctx)
	defer cancelWait()

	select {
	case ans := <-reply:
		return ans, nil
	case <-waitCtx.Done():
		c.mu.Lock()
		delete(c.asks, id)
		c.mu.Unlock()
		return nil, waitCtx.Err()
	}
}

// AnswerQuestion resolves a pending AskRequest by ID with the user's selections.
// Unknown/expired IDs are ignored.
func (c *Controller) AnswerQuestion(id string, answers []event.AskAnswer) {
	c.mu.Lock()
	pending, ok := c.asks[id]
	delete(c.asks, id)
	c.mu.Unlock()
	if ok {
		pending.reply <- answers // buffered, never blocks
	}
}

// ReplayPendingPrompts re-emits the ApprovalRequest / AskRequest event for every
// prompt currently blocking the run loop. A frontend that reconnected or reloaded
// after the original event has no way to rebuild its approval/ask modal otherwise,
// so the blocked gate goroutine stays stuck forever while the session shows a
// "waiting" status with no actionable prompt. promptMu serialises Ask and
// requestApproval, so in practice at most one prompt is outstanding; the loops
// stay general so a future concurrent prompt would still replay correctly.
func (c *Controller) ReplayPendingPrompts() {
	c.mu.Lock()
	approvals := make([]event.Approval, 0, len(c.approvals))
	for id, p := range c.approvals {
		approvals = append(approvals, event.Approval{ID: id, Tool: p.tool, Subject: p.subject})
	}
	asks := make([]event.Ask, 0, len(c.asks))
	for id, p := range c.asks {
		asks = append(asks, event.Ask{ID: id, Questions: p.questions})
	}
	c.mu.Unlock()
	for _, a := range approvals {
		c.sink.Emit(event.Event{Kind: event.ApprovalRequest, Approval: a})
	}
	for _, a := range asks {
		c.sink.Emit(event.Event{Kind: event.AskRequest, Ask: a})
	}
}

// SetToolApprovalMode changes the runtime approval posture for permission-gated
// tools. It does not answer business asks or plan approval.
func (c *Controller) SetToolApprovalMode(mode string) {
	mode = normalizeToolApprovalMode(mode)
	var pending []chan approvalReply

	c.mu.Lock()
	c.toolApprovalMode = mode
	c.autoApproveTools = mode == ToolApprovalYolo
	switch mode {
	case ToolApprovalAuto:
		pending = c.drainApprovalsLocked(false)
	case ToolApprovalYolo:
		pending = c.drainApprovalsLocked(true)
	}
	c.mu.Unlock()

	c.refreshInteractiveGate()
	for _, reply := range pending {
		reply <- approvalReply{allow: true}
	}
}

func (c *Controller) ToolApprovalMode() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return normalizeToolApprovalMode(c.toolApprovalMode)
}

// SetAutoApproveTools turns YOLO/full-access mode on or off for the session:
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

// SetBypass is the legacy name for SetAutoApproveTools. Keep it for existing
// desktop/serve bindings and CLI code that still uses the bypass wording.
func (c *Controller) SetBypass(on bool) {
	c.SetAutoApproveTools(on)
}

// SetMode applies plan (read-only) and tool auto-approval together so a turn
// submitted right after a composer mode switch can't observe a half-applied
// gate. Turning tool auto-approval on drains any pending tool approval.
func (c *Controller) SetMode(plan, autoApproveTools bool) {
	c.mu.Lock()
	c.planMode = plan
	c.mu.Unlock()

	if c.executor != nil {
		c.executor.SetPlanMode(plan)
	}
	if autoApproveTools {
		c.SetToolApprovalMode(ToolApprovalYolo)
	} else {
		c.SetToolApprovalMode(ToolApprovalAsk)
	}
}

// drainApprovalsLocked removes every pending approval gate and returns their
// reply channels; caller holds c.mu and sends {allow:true} after unlocking.
func (c *Controller) drainApprovalsLocked(includeExplicitAsk bool) []chan approvalReply {
	pending := make([]chan approvalReply, 0, len(c.approvals))
	for id, approval := range c.approvals {
		if requiresFreshApprovalTool(approval.tool) {
			continue
		}
		if !includeExplicitAsk && !approval.autoDrain {
			continue
		}
		delete(c.approvals, id)
		pending = append(pending, approval.reply)
	}
	return pending
}

// AutoApproveTools reports whether YOLO/full-access tool auto-approval is on,
// for status indicators and mode persistence.
func (c *Controller) AutoApproveTools() bool {
	return c.ToolApprovalMode() == ToolApprovalYolo
}

// Bypass is the legacy name for AutoApproveTools.
func (c *Controller) Bypass() bool {
	return c.AutoApproveTools()
}

// --- approval bridge (agent gate → events) ---

// gateApprover adapts the Controller to permission.Approver. It is distinct
// from the public Approve command (different signature, different direction).
type gateApprover struct{ c *Controller }

func (g gateApprover) Approve(ctx context.Context, tool, subject string, args json.RawMessage) (bool, bool, error) {
	subject = approvalDisplaySubject(tool, subject, args)
	// Auto-allow without prompting while executing a just-approved plan (the plan
	// was the approval), during an explicit continuation turn of that approved
	// plan, or while YOLO/full-access tool auto-approval is on. Deny rules already
	// bit before this point, so they still block.
	g.c.mu.Lock()
	auto := g.c.approvalBypassAllowsLocked(tool)
	g.c.mu.Unlock()
	if auto {
		return true, false, nil
	}
	return g.c.requestApproval(ctx, tool, subject, args)
}

func approvalDisplaySubject(tool, subject string, args json.RawMessage) string {
	switch tool {
	case memoryRememberTool:
		return rememberApprovalSubject(subject, args)
	case memoryForgetTool:
		return forgetApprovalSubject(subject, args)
	case "move_file":
		return moveApprovalSubject(subject, args)
	default:
		return subject
	}
}

func moveApprovalSubject(fallback string, args json.RawMessage) string {
	if len(args) == 0 {
		return fallback
	}
	var in struct {
		SourcePath      string `json:"source_path"`
		DestinationPath string `json:"destination_path"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return fallback
	}
	if in.SourcePath == "" || in.DestinationPath == "" {
		return fallback
	}
	return in.SourcePath + " -> " + in.DestinationPath
}

func rememberApprovalSubject(fallback string, args json.RawMessage) string {
	if len(args) == 0 {
		return fallback
	}
	var in struct {
		Name        string `json:"name"`
		Title       string `json:"title"`
		Description string `json:"description"`
		Type        string `json:"type"`
		Body        string `json:"body"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return fallback
	}
	name := approvalCompactText(firstNonEmpty(in.Name, in.Title))
	desc := approvalTruncate(approvalCompactText(in.Description), 180)
	body := approvalTruncate(approvalCompactText(in.Body), 240)
	typ := string(memory.NormalizeType(in.Type))

	var b strings.Builder
	b.WriteString("Save/update memory")
	if name != "" {
		fmt.Fprintf(&b, " %q", name)
	}
	if typ != "" {
		fmt.Fprintf(&b, " [%s]", typ)
	}
	if desc != "" {
		b.WriteString(": ")
		b.WriteString(desc)
	}
	if body != "" {
		if desc == "" {
			b.WriteString(": ")
		} else {
			b.WriteString(" | ")
		}
		b.WriteString("body: ")
		b.WriteString(body)
	}
	if b.Len() == len("Save/update memory") && fallback != "" {
		return fallback
	}
	return b.String()
}

func forgetApprovalSubject(fallback string, args json.RawMessage) string {
	if len(args) == 0 {
		return fallback
	}
	var in struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return fallback
	}
	name := approvalCompactText(in.Name)
	if name == "" {
		return fallback
	}
	return fmt.Sprintf("Archive memory %q", name)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func approvalCompactText(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func approvalTruncate(s string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}

// approvalWaitContext returns the context the approval/ask wait blocks on. When
// approvalTimeout is zero it just forwards ctx (interactive: wait forever). When
// positive it layers a timeout so a headless/bot session can't hang on a prompt
// nobody will answer (#4626, #4402); the caller treats expiry as a denial.
func (c *Controller) approvalWaitContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if c.approvalTimeout <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, c.approvalTimeout)
}

// requestApproval emits an ApprovalRequest and blocks until Approve(ID, …)
// answers or ctx is cancelled. A prior session grant for the same approval scope
// short-circuits. promptMu serialises outstanding prompts.
func (c *Controller) requestApproval(ctx context.Context, tool, subject string, args json.RawMessage) (bool, bool, error) {
	c.mu.Lock()
	// YOLO/full access and the just-approved-plan execution window auto-allow
	// approval-gated tools without prompting. Plan approval is a user decision,
	// not a tool permission, so it deliberately stays interactive.
	if c.approvalBypassAllowsLocked(tool) || c.sessionGrantAllowsLocked(tool, subject) {
		c.mu.Unlock()
		return true, false, nil
	}
	c.mu.Unlock()

	c.promptMu.Lock()
	defer c.promptMu.Unlock()

	// Re-check the grant: a session grant may have landed while we queued behind
	// another prompt for the same subject.
	c.mu.Lock()
	if c.approvalBypassAllowsLocked(tool) || c.sessionGrantAllowsLocked(tool, subject) {
		c.mu.Unlock()
		return true, false, nil
	}
	c.nextID++
	id := strconv.Itoa(c.nextID)
	reply := make(chan approvalReply, 1)
	c.approvals[id] = pendingApproval{tool: tool, subject: subject, autoDrain: c.autoApprovalWouldAllowLocked(tool, subject), reply: reply}
	c.mu.Unlock()

	c.sink.Emit(event.Event{Kind: event.ApprovalRequest, Approval: event.Approval{ID: id, Tool: tool, Subject: subject}})
	if hookSubject, hookArgs, ok := permissionRequestHookPayload(tool, subject, args); ok {
		go c.hooks.PermissionRequest(ctx, tool, hookSubject, hookArgs)
	}
	// The agent now needs the user's attention; a Notification hook can ping an
	// external channel (desktop notice, phone) while the run blocks on the reply.
	go c.hooks.Notification(ctx, approvalNotificationText(tool, subject))

	waitCtx, cancelWait := c.approvalWaitContext(ctx)
	defer cancelWait()

	select {
	case r := <-reply:
		// Plan approvals are one-shot — never persist a session grant for them, or
		// every future plan would auto-approve.
		if r.allow && r.session && !requiresFreshApprovalTool(tool) {
			rule := permission.SessionGrantRuleForScope(tool, subject)
			c.mu.Lock()
			c.granted[rule] = true
			c.mu.Unlock()
		}
		if r.allow && r.persist && !requiresFreshApprovalTool(tool) && c.onRemember != nil {
			c.emitRememberResult(c.onRemember(permission.RememberRuleForScope(tool, subject)))
		}
		return r.allow, false, nil
	case <-waitCtx.Done():
		c.mu.Lock()
		delete(c.approvals, id)
		c.mu.Unlock()
		return false, false, waitCtx.Err()
	}
}

func approvalNotificationText(tool, subject string) string {
	if requiresFreshApprovalTool(tool) {
		return "approval needed: " + tool
	}
	if subject == "" {
		return "approval needed: " + tool
	}
	return "approval needed: " + tool + " " + subject
}

func permissionRequestHookPayload(tool, subject string, args json.RawMessage) (string, json.RawMessage, bool) {
	switch tool {
	case planApprovalTool:
		return "", nil, false
	case memoryRememberTool, memoryForgetTool:
		return "", nil, true
	default:
		return subject, args, true
	}
}

func (c *Controller) approvalBypassAllowsLocked(tool string) bool {
	if requiresFreshApprovalTool(tool) {
		return false
	}
	return c.toolApprovalMode == ToolApprovalYolo ||
		c.approvedPlanAutoApproveTools
}

func (c *Controller) autoApprovalWouldAllowLocked(tool, subject string) bool {
	if requiresFreshApprovalTool(tool) {
		return false
	}
	policy := c.policy
	policy.Mode = permission.Allow
	return policy.DecideSubject(tool, false, subject) == permission.Allow
}

func (c *Controller) sessionGrantAllowsLocked(tool, subject string) bool {
	if requiresFreshApprovalTool(tool) {
		return false
	}
	for rule := range c.granted {
		if permission.RuleMatchesString(rule, tool, subject) {
			return true
		}
	}
	return false
}

func requiresFreshApprovalTool(tool string) bool {
	switch tool {
	case planApprovalTool, memoryRememberTool, memoryForgetTool:
		return true
	default:
		return false
	}
}

func (c *Controller) emitRememberResult(r RememberResult) {
	if r.Err != nil {
		c.sink.Emit(event.Event{
			Kind:  event.Notice,
			Level: event.LevelWarn,
			Text:  fmt.Sprintf(i18n.M.PermissionSaveFailedFmt, r.Rule, r.Err),
		})
		return
	}
	switch {
	case r.Saved:
		c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: fmt.Sprintf(i18n.M.PermissionSavedFmt, r.Path, r.Rule)})
	case strings.TrimSpace(r.CoveredBy) != "":
		c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: fmt.Sprintf(i18n.M.PermissionAlreadyAllowedFmt, r.Path, r.CoveredBy)})
	}
}
