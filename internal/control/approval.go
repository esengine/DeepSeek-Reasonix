package control

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/permission"
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
	mu        sync.Mutex
	approvals map[string]pendingApproval
	asks      map[string]pendingAsk
	granted   map[string]bool
	nextID    int
	// toolApprovalMode is the runtime approval posture: "ask" prompts, "auto"
	// lets the policy auto-approve the writer fallback while preserving ask/deny
	// rules, and "yolo" skips every tool approval prompt except plan approval.
	toolApprovalMode string
	// approvalTimeout bounds how long requestApproval/Ask block on a user
	// decision. Zero means wait indefinitely (correct for an interactive
	// terminal); bot/headless frontends set it so a walked-away user can't wedge
	// the session forever (#4626, #4402). Write-once at construction.
	approvalTimeout time.Duration
	// planAutoApprove auto-allows writer tool calls without prompting while a
	// just-approved plan executes. Set by the turn loop, read by the bypass
	// check. Plan approval is the go-ahead, so the model shouldn't re-prompt for
	// every write of the work it just got cleared to do.
	planAutoApprove bool

	// promptMu serializes outstanding prompts so at most one user decision is in
	// flight. Held across the blocking wait, so it must never be taken by the
	// resolve paths (Approve/AnswerQuestion).
	promptMu sync.Mutex
}

func newApprovalManager(policy permission.Policy, mode string, timeout time.Duration) approvalManager {
	return approvalManager{
		policy:           policy,
		approvals:        map[string]pendingApproval{},
		asks:             map[string]pendingAsk{},
		granted:          map[string]bool{},
		toolApprovalMode: mode,
		approvalTimeout:  timeout,
	}
}

// preApproved reports whether a tool call can skip the prompt — either the
// posture bypasses it (YOLO / plan-execution window) or a session grant already
// covers the scope.
func (a *approvalManager) preApproved(tool, subject string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.bypassAllowsLocked(tool) || a.sessionGrantAllowsLocked(tool, subject)
}

// register allocates an approval ID, records the pending prompt, and returns the
// reply channel the resolve path will signal.
func (a *approvalManager) register(tool, subject, reason string) (string, chan approvalReply) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.nextID++
	id := strconv.Itoa(a.nextID)
	reply := make(chan approvalReply, 1)
	a.approvals[id] = pendingApproval{tool: tool, subject: subject, reason: reason, autoDrain: a.autoApprovalWouldAllowLocked(tool, subject), reply: reply}
	return id, reply
}

// grantSession records a session-scoped grant so future calls in the same scope
// short-circuit.
func (a *approvalManager) grantSession(tool, subject string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.granted[permission.SessionGrantRuleForScope(tool, subject)] = true
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
// the new posture should auto-allow, returning their reply channels for the
// caller to signal after unlocking.
func (a *approvalManager) setMode(mode string) []chan approvalReply {
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
		approvals = append(approvals, event.Approval{ID: id, Tool: p.tool, Subject: p.subject, Reason: p.reason})
	}
	asks := make([]event.Ask, 0, len(a.asks))
	for id, p := range a.asks {
		asks = append(asks, event.Ask{ID: id, Questions: p.questions})
	}
	return approvals, asks
}

// --- decision helpers (caller holds a.mu) ---

func (a *approvalManager) bypassAllowsLocked(tool string) bool {
	if requiresFreshApprovalTool(tool) {
		return false
	}
	return a.toolApprovalMode == ToolApprovalYolo || a.planAutoApprove
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
	if requiresFreshApprovalTool(tool) {
		return false
	}
	for rule := range a.granted {
		if permission.RuleMatchesString(rule, tool, subject) {
			return true
		}
	}
	return false
}

// drainLocked removes every pending approval the new posture should auto-allow
// and returns their reply channels; caller holds a.mu and sends {allow:true}
// after unlocking.
func (a *approvalManager) drainLocked(includeExplicitAsk bool) []chan approvalReply {
	pending := make([]chan approvalReply, 0, len(a.approvals))
	for id, approval := range a.approvals {
		if requiresFreshApprovalTool(approval.tool) {
			continue
		}
		if !includeExplicitAsk && !approval.autoDrain {
			continue
		}
		delete(a.approvals, id)
		pending = append(pending, approval.reply)
	}
	return pending
}

// --- pure approval helpers ---

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

func requiresFreshApprovalTool(tool string) bool {
	switch tool {
	case planApprovalTool, memoryRememberTool, memoryForgetTool:
		return true
	default:
		return false
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
