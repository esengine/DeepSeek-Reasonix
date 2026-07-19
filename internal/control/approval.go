package control

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/i18n"
	"reasonix/internal/permission"
	"reasonix/internal/tool"
)

// approvalManager owns the approval/ask prompt bookkeeping and the runtime
// approval posture, behind its own locks and off the controller's c.mu. It is a
// strict leaf: its methods only touch its own state and never call back into the
// Controller. The Controller keeps the I/O orchestration (emitting events,
// firing hooks, rebuilding the executor gate) that needs its other collaborators
// — approval, unlike the goal FSM, blocks on user input and has side effects, so
// only the bookkeeping is extracted, not the orchestration.
type approvalManager struct {
	// policy is the immutable base permission policy, captured at construction.
	// Used to decide whether a tool call would auto-approve under the writer
	// fallback (autoApprovalWouldAllowLocked); the Controller keeps its own copy
	// for building the executor gate.
	policy permission.Policy

	// mu guards the prompt maps and posture fields; every critical section under
	// it is short and non-blocking.
	mu                       sync.Mutex
	approvals                map[string]pendingApproval
	asks                     map[string]pendingAsk
	granted                  map[string]bool
	planModeReadOnlyCommands map[string]bool
	nextID                   int
	// toolApprovalMode is the runtime approval posture: "ask" prompts, "auto"
	// lets the policy auto-approve the writer fallback while preserving ask/deny
	// rules, and "yolo" skips ordinary tool prompts while deny rules and fresh
	// decisions remain enforced.
	toolApprovalMode string
	// approvalTimeout bounds how long requestApproval/Ask block on a user
	// decision. Zero means wait indefinitely (correct for an interactive
	// terminal); bot/headless frontends set it so a walked-away user can't wedge
	// the session forever (#4626, #4402). Write-once at construction.
	approvalTimeout time.Duration
	// planAutoApprove auto-allows the ordinary writer fallback while a
	// just-approved plan executes. Explicit ask/deny rules and fresh decisions
	// remain authoritative, matching Auto rather than YOLO semantics.
	planAutoApprove bool

	// promptMu serializes outstanding prompts so at most one user decision is in
	// flight. Held across the blocking wait, so it must never be taken by the
	// resolve paths (Approve/AnswerQuestion). sink.Emit also runs under it (Ask,
	// requestApproval): Sink implementations must not block and must not call
	// back into Ask or the tool-approval chain, or they deadlock the prompt.
	promptMu sync.Mutex
}

func newApprovalManager(policy permission.Policy, mode string, timeout time.Duration) approvalManager {
	return approvalManager{
		policy:                   policy,
		approvals:                map[string]pendingApproval{},
		asks:                     map[string]pendingAsk{},
		granted:                  map[string]bool{},
		planModeReadOnlyCommands: map[string]bool{},
		toolApprovalMode:         mode,
		approvalTimeout:          timeout,
	}
}

// NewHeadlessPermissionGate builds the non-interactive gate used by `reasonix run`
// and sub-agents. It preserves headless autonomy for ordinary Ask decisions, but
// refuses tools whose contract requires a fresh human approval.
func NewHeadlessPermissionGate(policy permission.Policy) *freshHumanHeadlessGate {
	return &freshHumanHeadlessGate{gate: permission.NewGate(policy, nil)}
}

// BuildHeadlessApprovalGate constructs the non-interactive gate for a given
// approval mode, matching the contract ApplyHeadlessApprovalMode installs on a
// running controller's parent executor. boot uses this as the single
// construction point for every headless-only gate — the top-level executor,
// the `task`/`read_only_task` sub-agent, writer-capable skill sub-agents
// (run_skill/install_skill), and the planner runner — so all of them share the
// CLI-selected headless approval mode instead of only the parent executor
// getting it while the rest silently keep the mode-unaware default (ask
// resolves to allow), which let a task sub-agent run a write an explicit ask
// rule was supposed to deny under auto.
func BuildHeadlessApprovalGate(policy permission.Policy, mode string) *freshHumanHeadlessGate {
	switch normalizeToolApprovalMode(mode) {
	case ToolApprovalYolo:
		policy.Mode = permission.Allow
		gate := NewHeadlessPermissionGate(policy)
		gate.bypassMCPAuto = true
		return gate
	case ToolApprovalAuto:
		policy.Mode = permission.Allow
		return &freshHumanHeadlessGate{gate: permission.NewGate(policy, denyPermissionApprover{})}
	case ToolApprovalDontAsk:
		policy.Mode = permission.Deny
		return &freshHumanHeadlessGate{gate: permission.NewGate(policy, denyPermissionApprover{})}
	default:
		return NewHeadlessPermissionGate(policy)
	}
}

// SharedHeadlessGate is a mutable, concurrency-safe holder for the
// non-interactive gate that every headless-only sub-agent surface shares —
// `task`/`read_only_task`, writer-capable skill sub-agents, and the planner
// runner. Those surfaces capture their gate once at construction with no
// rebuild hook of their own, unlike the parent executor's gate (rebuilt in
// place via Agent.SetGate on every SetToolApprovalMode/
// ApplyHeadlessApprovalMode call). Every consumer holds this same pointer and
// reads through Check, so a runtime approval-mode switch (interactive
// Shift+Tab, or a headless --permission-mode passed at boot) only needs to
// call Update here to keep sub-agents on the same contract as the parent
// instead of silently pinning them to whatever mode was active when they were
// first constructed.
type SharedHeadlessGate struct {
	mu     sync.RWMutex
	policy permission.Policy
	gate   *freshHumanHeadlessGate
}

// NewSharedHeadlessGate builds a shared gate holder from the base policy and
// the initial approval mode (see BuildHeadlessApprovalGate for the mode
// contract).
func NewSharedHeadlessGate(policy permission.Policy, mode string) *SharedHeadlessGate {
	g := &SharedHeadlessGate{policy: policy}
	g.Update(mode)
	return g
}

// Update rebuilds the held gate for a new approval mode. Safe to call
// concurrently with Check (a turn may be mid-flight on another goroutine when
// the user switches modes).
func (g *SharedHeadlessGate) Update(mode string) {
	next := BuildHeadlessApprovalGate(g.policy, mode)
	g.mu.Lock()
	g.gate = next
	g.mu.Unlock()
}

func (g *SharedHeadlessGate) Check(ctx context.Context, toolName string, args json.RawMessage, readOnly bool) (bool, string, error) {
	g.mu.RLock()
	gate := g.gate
	g.mu.RUnlock()
	return gate.Check(ctx, toolName, args, readOnly)
}

func (g *SharedHeadlessGate) CheckFresh(ctx context.Context, toolName, subject string, args json.RawMessage, readOnly bool) (bool, string, error) {
	g.mu.RLock()
	gate := g.gate
	g.mu.RUnlock()
	return gate.CheckFresh(ctx, toolName, subject, args, readOnly)
}

func (g *SharedHeadlessGate) CheckMCP(ctx context.Context, toolName, subject string, args json.RawMessage, readOnly, destructive bool, mode, reviewer string) (bool, string, error) {
	g.mu.RLock()
	gate := g.gate
	g.mu.RUnlock()
	return gate.CheckMCP(ctx, toolName, subject, args, readOnly, destructive, mode, reviewer)
}

type freshHumanHeadlessGate struct {
	gate          *permission.Gate
	bypassMCPAuto bool
}

func (g *freshHumanHeadlessGate) Check(ctx context.Context, toolName string, args json.RawMessage, readOnly bool) (bool, string, error) {
	if RequiresFreshHumanApprovalTool(toolName) {
		return false, "this tool requires fresh human approval and cannot run in a non-interactive session. Use an interactive session or a user-initiated memory command.", nil
	}
	return g.gate.Check(ctx, toolName, args, readOnly)
}

func (g *freshHumanHeadlessGate) CheckFresh(context.Context, string, string, json.RawMessage, bool) (bool, string, error) {
	return false, "this tool requires fresh human approval and cannot run in a non-interactive session.", nil
}

func (g *freshHumanHeadlessGate) CheckMCP(ctx context.Context, toolName, subject string, args json.RawMessage, readOnly, destructive bool, mode, reviewer string) (bool, string, error) {
	if g.bypassMCPAuto && !destructive && tool.NormalizeMCPApprovalMode(mode) == tool.MCPApprovalAuto {
		reviewer = ""
	}
	return g.gate.CheckMCP(ctx, toolName, subject, args, readOnly, destructive, mode, reviewer)
}

// preApproved reports whether a tool call can skip the prompt — either the
// posture bypasses it (YOLO / plan-execution window) or a session grant already
// covers the scope.
func (a *approvalManager) preApproved(tool, subject string, args json.RawMessage) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.bypassAllowsLocked(tool, subject, args) || a.sessionGrantAllowsLocked(tool, subject)
}

// preApprovedForDecision reports whether a prompt can be skipped for a decision
// class. Fresh user decisions may reuse an explicit session grant, but they are
// never answered by YOLO/full-access or the approved-plan execution window.
func (a *approvalManager) preApprovedForDecision(tool, subject string, args json.RawMessage, fresh bool) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if fresh {
		return a.sessionGrantAllowsLocked(tool, subject)
	}
	return a.bypassAllowsLocked(tool, subject, args) || a.sessionGrantAllowsLocked(tool, subject)
}

// register allocates an approval ID, records the pending prompt, and returns the
// reply channel the resolve path will signal.
func (a *approvalManager) register(tool, subject, reason string) (string, chan approvalReply) {
	return a.registerDecision(tool, subject, reason, false)
}

// registerDecision allocates an approval ID for either an ordinary tool
// permission or a fresh user decision. Fresh decisions are not auto-drained when
// the user switches to auto/yolo tool approval while the prompt is visible.
func (a *approvalManager) registerDecision(tool, subject, reason string, fresh bool) (string, chan approvalReply) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.nextID++
	id := strconv.Itoa(a.nextID)
	reply := make(chan approvalReply, 1)
	autoDrain := false
	if !fresh {
		autoDrain = a.autoApprovalWouldAllowLocked(tool, subject)
	}
	a.approvals[id] = pendingApproval{tool: tool, subject: subject, reason: reason, fresh: fresh, autoDrain: autoDrain, reply: reply}
	return id, reply
}

// grantSession records a session-scoped grant so future calls in the same scope
// short-circuit.
func (a *approvalManager) grantSession(tool, subject string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.granted[permission.SessionGrantRuleForScope(tool, subject)] = true
}

func (a *approvalManager) planModeReadOnlyCommandTrusted(prefix string) bool {
	prefix = normalizePlanModeReadOnlyCommandPrefix(prefix)
	if prefix == "" {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.planModeReadOnlyCommands[prefix]
}

func (a *approvalManager) grantPlanModeReadOnlyCommand(prefix string) {
	prefix = normalizePlanModeReadOnlyCommandPrefix(prefix)
	if prefix == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.planModeReadOnlyCommands[prefix] = true
}

// SessionAuthorizations is the same-session tool-grant and Plan-mode
// read-only command trust state a controller rebuild must carry forward; see
// Controller.SessionAuthorizations / RestoreSessionAuthorizations.
type SessionAuthorizations struct {
	Grants                   []string
	PlanModeReadOnlyCommands []string
}

func (a *approvalManager) snapshotSessionAuthorizations() SessionAuthorizations {
	a.mu.Lock()
	defer a.mu.Unlock()
	auth := SessionAuthorizations{
		Grants:                   make([]string, 0, len(a.granted)),
		PlanModeReadOnlyCommands: make([]string, 0, len(a.planModeReadOnlyCommands)),
	}
	for rule := range a.granted {
		auth.Grants = append(auth.Grants, rule)
	}
	for prefix := range a.planModeReadOnlyCommands {
		auth.PlanModeReadOnlyCommands = append(auth.PlanModeReadOnlyCommands, prefix)
	}
	return auth
}

func (a *approvalManager) restoreSessionAuthorizations(auth SessionAuthorizations) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, rule := range auth.Grants {
		a.granted[rule] = true
	}
	for _, prefix := range auth.PlanModeReadOnlyCommands {
		a.planModeReadOnlyCommands[prefix] = true
	}
}

// cancel drops a pending approval (timeout/abort path).
func (a *approvalManager) cancel(id string) {
	a.mu.Lock()
	delete(a.approvals, id)
	a.mu.Unlock()
}

// resolve removes and returns the pending approval for id (Approve path).
func (a *approvalManager) resolve(id string) pendingApproval {
	a.mu.Lock()
	defer a.mu.Unlock()
	p := a.approvals[id]
	delete(a.approvals, id)
	return p
}

// registerAsk allocates an ask ID, records the pending question batch, and
// returns the reply channel.
func (a *approvalManager) registerAsk(questions []event.AskQuestion) (string, chan []event.AskAnswer) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.nextID++
	id := strconv.Itoa(a.nextID)
	reply := make(chan []event.AskAnswer, 1)
	a.asks[id] = pendingAsk{questions: questions, reply: reply}
	return id, reply
}

// cancelAsk drops a pending ask (timeout/abort path).
func (a *approvalManager) cancelAsk(id string) {
	a.mu.Lock()
	delete(a.asks, id)
	a.mu.Unlock()
}

// resolveAsk removes and returns the pending ask for id (AnswerQuestion path).
func (a *approvalManager) resolveAsk(id string) (pendingAsk, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	p, ok := a.asks[id]
	delete(a.asks, id)
	return p, ok
}

// clearAll drops every in-flight prompt without signaling — the cancel path,
// where blocked waiters unblock via their cancelled context instead.
func (a *approvalManager) clearAll() {
	a.mu.Lock()
	defer a.mu.Unlock()
	clear(a.approvals)
	clear(a.asks)
}

// hasPending reports whether any prompt is awaiting a user decision.
func (a *approvalManager) hasPending() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.approvals) > 0 || len(a.asks) > 0
}

// mode returns the normalized runtime approval posture.
func (a *approvalManager) mode() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return normalizeToolApprovalMode(a.toolApprovalMode)
}

// setMode applies a (pre-normalized) posture and drains any pending approvals
// the new posture should auto-allow, returning them for the caller to signal
// {allow:true} after unlocking.
func (a *approvalManager) setMode(mode string) []drainedApproval {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.toolApprovalMode = mode
	switch mode {
	case ToolApprovalAuto:
		return a.drainLocked(false)
	case ToolApprovalYolo:
		return a.drainLocked(true)
	}
	return nil
}

// setPlanAutoApprove toggles the just-approved-plan execution window.
func (a *approvalManager) setPlanAutoApprove(on bool) {
	a.mu.Lock()
	a.planAutoApprove = on
	a.mu.Unlock()
}

// waitContext bounds the blocking wait by approvalTimeout when set.
func (a *approvalManager) waitContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if a.approvalTimeout <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, a.approvalTimeout)
}

// snapshotPrompts copies the in-flight prompts for re-emission to a reconnected
// frontend (ReplayPendingPrompts).
func (a *approvalManager) snapshotPrompts() ([]event.Approval, []event.Ask) {
	a.mu.Lock()
	defer a.mu.Unlock()
	approvals := make([]event.Approval, 0, len(a.approvals))
	for id, p := range a.approvals {
		approvals = append(approvals, event.Approval{ID: id, Tool: p.tool, Subject: p.subject, Reason: p.reason, Fresh: p.fresh})
	}
	asks := make([]event.Ask, 0, len(a.asks))
	for id, p := range a.asks {
		asks = append(asks, event.Ask{ID: id, Questions: p.questions})
	}
	return approvals, asks
}

func normalizePlanModeReadOnlyCommandPrefix(prefix string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(prefix)), " ")
}

// --- decision helpers (caller holds a.mu) ---

func (a *approvalManager) bypassAllowsLocked(tool, subject string, args json.RawMessage) bool {
	if requiresFreshApprovalTool(tool) {
		return false
	}
	if a.toolApprovalMode == ToolApprovalYolo {
		return true
	}
	if !a.planAutoApprove {
		return false
	}
	policy := a.policy
	policy.Mode = permission.Allow
	if len(args) > 0 {
		return policy.Decide(tool, false, args) == permission.Allow
	}
	return policy.DecideSubject(tool, false, subject) == permission.Allow
}

func (a *approvalManager) autoApprovalWouldAllowLocked(tool, subject string) bool {
	if requiresFreshApprovalTool(tool) {
		return false
	}
	policy := a.policy
	policy.Mode = permission.Allow
	return policy.DecideSubject(tool, false, subject) == permission.Allow
}

func (a *approvalManager) sessionGrantAllowsLocked(tool, subject string) bool {
	if requiresFreshApprovalTool(tool) && !allowsFreshSessionGrantTool(tool) {
		return false
	}
	for rule := range a.granted {
		if permission.RuleMatchesString(rule, tool, subject) {
			return true
		}
	}
	return false
}

// drainedApproval is a pending approval removed by a posture switch, keeping
// its prompt id so frontends can dismiss exactly the prompts the new posture
// resolved (fresh/plan/memory prompts stay pending and must stay visible).
type drainedApproval struct {
	id    string
	reply chan approvalReply
}

// drainLocked removes every pending approval the new posture should auto-allow
// and returns them; caller holds a.mu and sends {allow:true} after unlocking.
func (a *approvalManager) drainLocked(includeExplicitAsk bool) []drainedApproval {
	pending := make([]drainedApproval, 0, len(a.approvals))
	for id, approval := range a.approvals {
		if approval.fresh || requiresFreshApprovalTool(approval.tool) {
			continue
		}
		if !includeExplicitAsk && !approval.autoDrain {
			continue
		}
		delete(a.approvals, id)
		pending = append(pending, drainedApproval{id: id, reply: approval.reply})
	}
	return pending
}

// --- pure approval helpers ---

func normalizeToolApprovalMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case ToolApprovalAuto, "approve", "allow":
		return ToolApprovalAuto
	case "dontask", "dont-ask", "deny":
		return ToolApprovalDontAsk
	case ToolApprovalYolo, "full", "full-access", "bypass":
		return ToolApprovalYolo
	default:
		return ToolApprovalAsk
	}
}

// RequiresFreshHumanApprovalTool reports whether a tool must be answered by a
// human decision, not by YOLO/auto approval, Guardian, or a non-interactive nil
// approver. A small subset may still opt into explicit session grants.
func RequiresFreshHumanApprovalTool(tool string) bool {
	switch tool {
	case planApprovalTool, memoryRememberTool, memoryForgetTool, SandboxEscapeApprovalTool, ManagedConfigWriteApprovalTool:
		return true
	default:
		return false
	}
}

func requiresFreshApprovalTool(tool string) bool {
	return RequiresFreshHumanApprovalTool(tool)
}

func allowsFreshSessionGrantTool(tool string) bool {
	switch tool {
	case SandboxEscapeApprovalTool, ManagedConfigWriteApprovalTool:
		return true
	default:
		return false
	}
}

func approvalNotificationText(tool, subject string) string {
	if requiresFreshApprovalTool(tool) {
		return fmt.Sprintf(i18n.M.ApprovalNeededFmt, tool)
	}
	if subject == "" {
		return fmt.Sprintf(i18n.M.ApprovalNeededFmt, tool)
	}
	return fmt.Sprintf(i18n.M.ApprovalNeededWithSubjectFmt, tool, subject)
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
