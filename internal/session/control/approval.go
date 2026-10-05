package control

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"reasonix/internal/base/i18n"
	"reasonix/internal/contract/event"
	"reasonix/internal/safety/permission"
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
	ids       promptIDs
	posture   approvalPosture
	// approvalTimeout bounds how long requestApproval/Ask block on a user
	// decision. Zero means wait indefinitely (correct for an interactive
	// terminal); bot/headless frontends set it so a walked-away user can't wedge
	// the session forever (#4626, #4402). Write-once at construction.
	approvalTimeout time.Duration
	// planAutoApprove auto-allows the ordinary writer fallback while a
	// just-approved plan executes. Explicit ask/deny rules and fresh decisions
	// remain authoritative, matching Auto rather than YOLO semantics.
	planAutoApprove bool

	// persists reports whether an answer can be written down as a rule at all:
	// without a place to write it, offering it would promise a file nobody has.
	persists bool

	// promptMu serializes outstanding prompts so at most one user decision is in
	// flight. Held across the blocking wait, so it must never be taken by the
	// resolve paths (Approve/AnswerQuestion). sink.Emit also runs under it (Ask,
	// requestApproval): Sink implementations must not block and must not call
	// back into Ask or the tool-approval chain, or they deadlock the prompt.
	promptMu sync.Mutex
	// promptEmitMu serializes prompt registration and emission with an SSE
	// attach handoff. It is separate from promptMu because promptMu remains
	// held while waiting for the user's answer.
	promptEmitMu sync.Mutex
}

func newApprovalManager(policy permission.Policy, mode string, timeout time.Duration, persists bool) approvalManager {
	return approvalManager{
		policy:          policy,
		persists:        persists,
		approvals:       map[string]pendingApproval{},
		asks:            map[string]pendingAsk{},
		granted:         map[string]bool{},
		ids:             newPromptIDs(),
		posture:         approvalPosture{mode: mode},
		approvalTimeout: timeout,
	}
}

// NewHeadlessPermissionGate builds the legacy bootstrap gate used before a
// frontend declares its approval posture. Interactive frontends replace it
// before running; callers that are actually headless must pass a non-empty mode
// through BuildHeadlessApprovalGate.
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
// getting it while the rest silently keep the mode-unaware default, which let
// a task sub-agent run a write an explicit ask
// rule was supposed to deny under auto.
func BuildHeadlessApprovalGate(policy permission.Policy, mode string) *freshHumanHeadlessGate {
	return buildHeadlessGate(policy, mode, nil)
}

func buildHeadlessGate(policy permission.Policy, mode string, folder *folderRefusal) *freshHumanHeadlessGate {
	// An empty mode is the boot-time placeholder used by interactive frontends
	// before they install their real gate. Keep that compatibility path distinct
	// from an explicit headless Ask posture, which has nobody to approve it.
	if strings.TrimSpace(mode) == "" {
		return NewHeadlessPermissionGate(policy)
	}
	switch normalizeToolApprovalMode(mode) {
	case ToolApprovalYolo:
		policy.Mode = permission.Allow
		return &freshHumanHeadlessGate{gate: permission.NewGate(policy, nil), dynamicBashBypass: true}
	case ToolApprovalAuto:
		policy.Mode = permission.Allow
		// Auto preserves explicit ask rules; a shape static analysis cannot read
		// is not one, and refusing it unattended only buys a round trip — the
		// model writes the same script to a file and runs it. policy is a copy.
		policy.AllowDynamicBash = true
		return &freshHumanHeadlessGate{gate: permission.NewGate(policy, denyPermissionApprover{})}
	case ToolApprovalDontAsk, ToolApprovalReadOnly:
		policy.Mode = permission.Deny
		policy.ReadOnly = normalizeToolApprovalMode(mode) == ToolApprovalReadOnly
		return &freshHumanHeadlessGate{gate: permission.NewGate(policy, denyPermissionApprover{})}
	default:
		policy.Mode = permission.Ask
		if folder != nil {
			f := *folder
			f.trusted = policy
			f.trusted.Mode, f.trusted.AllowDynamicBash = permission.Allow, true
			folder = &f
		}
		return &freshHumanHeadlessGate{gate: permission.NewGate(policy, denyPermissionApprover{folder: folder})}
	}
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
	return a.preApprovedForDecisionOptions(tool, subject, args, fresh, false)
}

func (a *approvalManager) preApprovedForDecisionOptions(tool, subject string, args json.RawMessage, fresh, requireHuman bool) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if fresh {
		return a.sessionGrantAllowsLocked(tool, subject)
	}
	if requireHuman {
		return a.posture.mode == ToolApprovalYolo || a.sessionGrantAllowsLocked(tool, subject)
	}
	return a.bypassAllowsLocked(tool, subject, args) || a.sessionGrantAllowsLocked(tool, subject)
}

func (a *approvalManager) preApprovedForRequiredHuman(tool, subject string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.posture.mode == ToolApprovalYolo || a.sessionGrantAllowsLocked(tool, subject)
}

// register allocates an approval ID, records the pending prompt, and returns the
// reply channel the resolve path will signal.
func (a *approvalManager) register(tool, subject, reason string) (string, chan approvalReply) {
	return a.registerWithInput(tool, subject, reason, nil)
}

func (a *approvalManager) registerWithInput(tool, subject, reason string, rawInput json.RawMessage) (string, chan approvalReply) {
	return a.registerDecisionWithInput(tool, subject, reason, rawInput, false, false)
}

// registerDecision allocates an approval ID for either an ordinary tool
// permission or a fresh user decision. Fresh decisions are not auto-drained when
// the user switches to auto/yolo tool approval while the prompt is visible.
func (a *approvalManager) registerDecision(tool, subject, reason string, fresh, requireHuman bool) (string, chan approvalReply) {
	return a.registerDecisionWithInput(tool, subject, reason, nil, fresh, requireHuman)
}

func (a *approvalManager) registerDecisionWithInput(tool, subject, reason string, rawInput json.RawMessage, fresh, requireHuman bool) (string, chan approvalReply) {
	return a.registerDecisionKindWithInput(tool, subject, reason, rawInput, fresh, requireHuman, "", nil)
}

// registerDecisionKind is registerDecision with optional Kind/Recovery payload
// so Auto Guard cards survive ReplayPendingPrompts.
func (a *approvalManager) registerDecisionKind(tool, subject, reason string, fresh, requireHuman bool, kind string, rec *event.RecoveryApproval) (string, chan approvalReply) {
	return a.registerDecisionKindWithInput(tool, subject, reason, nil, fresh, requireHuman, kind, rec)
}

func (a *approvalManager) registerDecisionKindWithInput(tool, subject, reason string, rawInput json.RawMessage, fresh, requireHuman bool, kind string, rec *event.RecoveryApproval) (string, chan approvalReply) {
	a.mu.Lock()
	defer a.mu.Unlock()
	id := a.ids.issue()
	reply := make(chan approvalReply, 1)
	autoDrain := false
	if !fresh && !requireHuman {
		autoDrain = a.autoApprovalWouldAllowLocked(tool, subject)
	}
	a.approvals[id] = pendingApproval{
		id:   id,
		tool: tool, subject: subject, reason: reason, rawInput: append(json.RawMessage(nil), rawInput...), fresh: fresh, requireHuman: requireHuman,
		autoDrain: autoDrain, kind: kind, recovery: rec, reply: reply,
	}
	return id, reply
}

// grantSession records a session-scoped grant so future calls in the same scope
// short-circuit.
func (a *approvalManager) grantSession(tool, subject string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.granted[permission.SessionGrantRuleForScope(tool, subject)] = true
}

// SessionAuthorizations is the same-session tool-grant state a controller
// rebuild must carry forward; see Controller.SessionAuthorizations /
// RestoreSessionAuthorizations.
type SessionAuthorizations struct {
	Grants []string
}

func (a *approvalManager) snapshotSessionAuthorizations() SessionAuthorizations {
	a.mu.Lock()
	defer a.mu.Unlock()
	auth := SessionAuthorizations{Grants: make([]string, 0, len(a.granted))}
	for rule := range a.granted {
		auth.Grants = append(auth.Grants, rule)
	}
	return auth
}

func (a *approvalManager) restoreSessionAuthorizations(auth SessionAuthorizations) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, rule := range auth.Grants {
		a.granted[rule] = true
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

// resolveTool removes id only when it belongs to the expected specialized
// decision surface. A mismatched bridge call must not consume another approval
// type that happens to share the same short numeric id.
func (a *approvalManager) resolveTool(id, tool string) (pendingApproval, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	p, ok := a.approvals[id]
	if !ok || p.tool != tool {
		return pendingApproval{}, false
	}
	delete(a.approvals, id)
	return p, true
}

// nextAskID issues the identity before the ask exists anywhere.
func (a *approvalManager) nextAskID() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.ids.issue()
}

// promptIDs issues the identity an answer is correlated by. A rebuilt controller
// keeps the pane's event stream and the session's adjudication journal, and a
// frontend drops a request whose id it already answered, so ids must not repeat
// across generations: the prefix is drawn once per manager.
type promptIDs struct {
	prefix string
	next   int
}

func newPromptIDs() promptIDs {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return promptIDs{prefix: hex.EncodeToString(b[:])}
}

func (p *promptIDs) issue() string {
	if p.prefix == "" {
		*p = newPromptIDs()
	}
	p.next++
	return p.prefix + "-" + strconv.Itoa(p.next)
}

// pendingAsk is an in-flight ask question batch. questions is retained so the
// AskRequest can be re-emitted to a frontend that reconnected after the original
// event (see ReplayPendingPrompts).
type pendingAsk struct {
	questions []event.AskQuestion
	reply     chan []event.AskAnswer
	queued    bool // registered but not yet shown; replay must skip it
	origin    *event.AskOrigin
}

// registerAsk records the pending question batch under an identity already
// issued and returns the reply channel. It is the moment the ask becomes
// observable — queued, so a question waiting behind another prompt is visible
// rather than living only inside a blocked goroutine.
func (a *approvalManager) registerAsk(id string, questions []event.AskQuestion, origin *event.AskOrigin) chan []event.AskAnswer {
	a.mu.Lock()
	defer a.mu.Unlock()
	reply := make(chan []event.AskAnswer, 1)
	a.asks[id] = pendingAsk{questions: questions, reply: reply, queued: true, origin: origin}
	return reply
}

// liveBarrierIDs names the prompts this process waits on.
func (a *approvalManager) liveBarrierIDs() map[string]bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make(map[string]bool, len(a.asks)+len(a.approvals))
	for id := range a.asks {
		out[id] = true
	}
	for id := range a.approvals {
		out[id] = true
	}
	return out
}

// markAskEmitted clears the queued flag once the ask has reached a frontend,
// which is what makes it eligible for replay.
func (a *approvalManager) markAskEmitted(id string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if p, ok := a.asks[id]; ok {
		p.queued = false
		a.asks[id] = p
	}
}

// queuedAsks reports asks registered but not yet shown.
func (a *approvalManager) queuedAsks() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	n := 0
	for _, p := range a.asks {
		if p.queued {
			n++
		}
	}
	return n
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

// clearKind drops pending approvals of one specialized kind. Session recovery
// state uses this during rotations so a card from the previous session cannot
// be answered against the newly active one.
func (a *approvalManager) clearKind(kind string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for id, pending := range a.approvals {
		if pending.kind == kind {
			delete(a.approvals, id)
		}
	}
}

// hasPending reports whether any prompt is awaiting a user decision.
func (a *approvalManager) hasPending() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.approvals) > 0 || len(a.asks) > 0
}

// approvalPosture is the runtime approval posture — "ask" prompts, "auto" lets
// the policy auto-approve the writer fallback while preserving ask/deny rules,
// "yolo" skips ordinary prompts while deny rules and fresh decisions hold — and
// whether it is the session's default rather than a posture somebody named.
type approvalPosture struct {
	mode      string
	defaulted bool
}

// setDefaultMode applies mode as the default nobody named, draining what it
// auto-allows. With onlyIfDefaulted it is a check-and-set: a posture somebody
// named since the caller looked is theirs, and nothing changes.
func (a *approvalManager) setDefaultMode(mode string, onlyIfDefaulted bool) ([]drainedApproval, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if onlyIfDefaulted && !a.posture.defaulted {
		return nil, false
	}
	a.posture = approvalPosture{mode: mode, defaulted: true}
	return a.drainForLocked(mode), true
}

func (a *approvalManager) defaulted() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.posture.defaulted
}

// mode returns the normalized runtime approval posture.
func (a *approvalManager) mode() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return normalizeToolApprovalMode(a.posture.mode)
}

// setMode applies a (pre-normalized) posture and drains any pending approvals
// the new posture should auto-allow, returning them for the caller to signal
// {allow:true} after unlocking.
func (a *approvalManager) setMode(mode string) []drainedApproval {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.posture = approvalPosture{mode: mode}
	return a.drainForLocked(mode)
}

func (a *approvalManager) drainForLocked(mode string) []drainedApproval {
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
		session, persist := approvalGrantsForRequest(p.tool, p.fresh, p.requireHuman)
		approvals = append(approvals, event.Approval{
			ID: id, Tool: p.tool, Subject: p.subject, Reason: p.reason, ReasonCode: ExplicitApprovalCode(p.tool, p.subject),
			RawInput: append(json.RawMessage(nil), p.rawInput...), Fresh: p.fresh,
			AllowsSession: session, AllowsPersist: persist && a.persists,
			Kind: p.kind, Recovery: p.recovery,
		})
	}
	asks := make([]event.Ask, 0, len(a.asks))
	for id, p := range a.asks {
		// A queued ask has never been shown; replaying it would put a question
		// on screen ahead of the prompt it is waiting behind.
		if p.queued {
			continue
		}
		asks = append(asks, event.Ask{ID: id, Questions: p.questions, Origin: p.origin})
	}
	return approvals, asks
}

// decision helpers (caller holds a.mu)

func (a *approvalManager) bypassAllowsLocked(tool, subject string, args json.RawMessage) bool {
	if requiresFreshApprovalTool(tool) {
		return false
	}
	if a.posture.mode == ToolApprovalYolo {
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
		if permission.SessionGrantMatches(rule, tool, subject) {
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
		if approval.requireHuman && !includeExplicitAsk {
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

// pure approval helpers

func normalizeToolApprovalMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case ToolApprovalAuto, "approve", "allow":
		return ToolApprovalAuto
	case "dontask", "dont-ask", "deny":
		return ToolApprovalDontAsk
	case "readonly", "read-only":
		return ToolApprovalReadOnly
	case ToolApprovalYolo, "full", "full-access", "bypass":
		return ToolApprovalYolo
	default:
		return ToolApprovalAsk
	}
}

// ParseToolApprovalMode canonicalises a posture name and reports whether it
// names one. A frontend validating a request must call this instead of keeping
// its own list: the HTTP face carried a three-name copy and answered 400 to
// dontAsk, a posture the kernel has always had.
func ParseToolApprovalMode(mode string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case ToolApprovalAsk, ToolApprovalAuto, "approve", "allow",
		"dontask", "dont-ask", "deny", "readonly", "read-only",
		ToolApprovalYolo, "full", "full-access", "bypass":
		return normalizeToolApprovalMode(mode), true
	}
	return "", false
}

// RequiresFreshHumanApprovalTool reports whether a tool's unsafe variants must
// be answered by a human decision, not by YOLO/auto approval, Guardian, or a
// non-interactive nil approver. A controller that owns the scoped memory store
// may still classify a bounded new project memory as create-only and allow that
// narrow operation in interactive or headless mode.
func RequiresFreshHumanApprovalTool(tool string) bool {
	switch tool {
	case planApprovalTool, memoryRememberTool, memoryForgetTool, SandboxEscapeApprovalTool, ManagedConfigWriteApprovalTool, NetworkEgressApprovalTool:
		return true
	default:
		return false
	}
}

func requiresFreshApprovalTool(tool string) bool {
	return RequiresFreshHumanApprovalTool(tool)
}

// ApprovalGrants says which answers beyond "once" the host will honour for this
// call: whether a session grant covers the calls after it, and whether the
// answer may be written down as a rule. A window that offers an answer the host
// drops promises what it cannot keep — the label said "do not ask again" while
// the grant died with the session.
func ApprovalGrants(tool string, fresh bool) (session, persist bool) {
	if fresh || RequiresFreshHumanApprovalTool(tool) {
		return allowsFreshSessionGrantTool(tool), false
	}
	return true, true
}

// approvalGrantsForRequest narrows the generic tool grant contract for calls
// whose shape itself requires a person. An exact same-session grant is useful
// for these calls, but a persisted broad rule is not: required-human checks do
// not consult broad rules on the next launch, so offering "always" would make
// a promise the runtime deliberately cannot keep.
func approvalGrantsForRequest(tool string, fresh, requireHuman bool) (session, persist bool) {
	session, persist = ApprovalGrants(tool, fresh)
	if requireHuman {
		persist = false
	}
	return session, persist
}

func allowsFreshSessionGrantTool(tool string) bool {
	switch tool {
	case SandboxEscapeApprovalTool, ManagedConfigWriteApprovalTool, NetworkEgressApprovalTool:
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

// sessionGrants is what this session was allowed on a prompt, sorted so a
// reader sees the same order twice.
func (a *approvalManager) sessionGrants() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]string, 0, len(a.granted))
	for rule := range a.granted {
		out = append(out, rule)
	}
	sort.Strings(out)
	return out
}

// revokeSessionGrant takes back one of them, or all of them when rule is empty.
// It answers how many it took back, because a rule a rebuild already dropped is
// not an error and is not a revocation either.
func (a *approvalManager) revokeSessionGrant(rule string) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	if rule == "" {
		n := len(a.granted)
		clear(a.granted)
		return n
	}
	if !a.granted[rule] {
		return 0
	}
	delete(a.granted, rule)
	return 1
}
