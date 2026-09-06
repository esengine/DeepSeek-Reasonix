package control

import (
	"errors"
	"fmt"
	"reasonix/internal/agent"
	"reasonix/internal/event"
	"sort"
	"sync"
)

type PendingPromptOwner struct {
	mu       sync.Mutex
	pending  map[string]PendingPrompt
	resolved map[string]PromptIdentity
}

type PendingPromptState string

const (
	PromptPending   PendingPromptState = "pending"
	PromptResolving PendingPromptState = "resolving"
)

type PendingPrompt struct {
	Identity PromptIdentity
	State    PendingPromptState
	Resolve  func(PromptAnswer) error
	Cancel   func() error
}

func (o *PendingPromptOwner) RegisterPrompt(prompt PendingPrompt) error {
	identity := prompt.Identity
	if identity.PromptID == "" {
		return ErrPromptNotPending
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.pending == nil {
		o.pending = make(map[string]PendingPrompt)
	}
	if o.resolved == nil {
		o.resolved = make(map[string]PromptIdentity)
	}
	if _, exists := o.pending[identity.PromptID]; exists {
		return fmt.Errorf("prompt %q already registered", identity.PromptID)
	}
	if prompt.State == "" {
		prompt.State = PromptPending
	}
	o.pending[identity.PromptID] = prompt
	return nil
}

func (o *PendingPromptOwner) Register(identity PromptIdentity) error {
	return o.RegisterPrompt(PendingPrompt{Identity: identity, State: PromptPending})
}
func (o *PendingPromptOwner) Identity(id string) (PromptIdentity, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	v, ok := o.pending[id]
	return v.Identity, ok
}
func (o *PendingPromptOwner) Prompt(id string) (PendingPrompt, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	p, ok := o.pending[id]
	return p, ok
}
func (o *PendingPromptOwner) Remove(id string) { o.mu.Lock(); delete(o.pending, id); o.mu.Unlock() }
func (o *PendingPromptOwner) RemoveKind(kind PromptKind) {
	o.mu.Lock()
	defer o.mu.Unlock()
	for id, prompt := range o.pending {
		if prompt.Identity.Kind == kind {
			delete(o.pending, id)
		}
	}
}
func (o *PendingPromptOwner) MarkResolved(identity PromptIdentity) {
	o.mu.Lock()
	defer o.mu.Unlock()
	delete(o.pending, identity.PromptID)
	if o.resolved == nil {
		o.resolved = make(map[string]PromptIdentity)
	}
	o.resolved[identity.PromptID] = identity
}
func (o *PendingPromptOwner) BeginResolve(identity PromptIdentity) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	p, ok := o.pending[identity.PromptID]
	if !ok {
		if _, resolved := o.resolved[identity.PromptID]; resolved {
			return ErrPromptAlreadyResolved
		}
		return ErrPromptNotPending
	}
	if p.Identity != identity {
		return ErrPromptStaleTurn
	}
	if p.State == PromptResolving {
		return ErrPromptAlreadyResolved
	}
	p.State = PromptResolving
	o.pending[identity.PromptID] = p
	return nil
}
func (o *PendingPromptOwner) Restore(identity PromptIdentity) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if p, ok := o.pending[identity.PromptID]; ok && p.Identity == identity {
		p.State = PromptPending
		o.pending[identity.PromptID] = p
	}
}
func (o *PendingPromptOwner) Resolve(identity PromptIdentity, answer PromptAnswer) error {
	if err := o.BeginResolve(identity); err != nil {
		return err
	}
	prompt, ok := o.Prompt(identity.PromptID)
	if !ok || prompt.Resolve == nil {
		o.Restore(identity)
		return ErrPromptNotPending
	}
	err := prompt.Resolve(answer)
	if err != nil {
		o.Restore(identity)
		return err
	}
	if _, pending := o.Identity(identity.PromptID); pending {
		o.Restore(identity)
		return ErrPromptNotPending
	}
	o.MarkResolved(identity)
	return nil
}
func (o *PendingPromptOwner) WasResolved(id string) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	_, ok := o.resolved[id]
	return ok
}
func (o *PendingPromptOwner) Clear() {
	o.mu.Lock()
	o.pending = make(map[string]PendingPrompt)
	o.resolved = make(map[string]PromptIdentity)
	o.mu.Unlock()
}
func (o *PendingPromptOwner) CancelAll() {
	o.mu.Lock()
	cancels := make([]func() error, 0, len(o.pending))
	for _, prompt := range o.pending {
		if prompt.Cancel != nil {
			cancels = append(cancels, prompt.Cancel)
		}
	}
	o.pending = make(map[string]PendingPrompt)
	o.mu.Unlock()
	for _, cancel := range cancels {
		_ = cancel()
	}
}
func (o *PendingPromptOwner) Identities() []PromptIdentity {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make([]PromptIdentity, 0, len(o.pending))
	for _, prompt := range o.pending {
		out = append(out, prompt.Identity)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PromptID < out[j].PromptID })
	return out
}

// PromptKind identifies the interactive surface that owns a pending decision.
type PromptKind string

const (
	PromptAsk      PromptKind = "ask"
	PromptApproval PromptKind = "approval"
	PromptPlan     PromptKind = "plan"
	PromptRecovery PromptKind = "recovery"
	PromptMCP      PromptKind = "mcp"
)

// PromptIdentity is the immutable identity captured when a decision card is
// emitted. Turn and runtime fences prevent a delayed UI action crossing a
// controller replacement.
type PromptIdentity struct {
	PromptID     string
	TurnID       string
	RuntimeEpoch string
	Kind         PromptKind
}

func (c *Controller) promptIdentitySnapshot() (string, string) {
	c.promptResolveMu.Lock()
	defer c.promptResolveMu.Unlock()
	turnID, _, _, _ := c.turnEventRuntimeStatus()
	return turnID, c.promptRuntimeEpoch
}

func (c *Controller) registerOwnedPrompt(id string, kind PromptKind) {
	turn, epoch := c.promptIdentitySnapshot()
	identity := PromptIdentity{PromptID: id, TurnID: turn, RuntimeEpoch: epoch, Kind: kind}
	resolve := func(answer PromptAnswer) error {
		switch kind {
		case PromptAsk:
			return c.answerQuestionCheckedLocked(id, answer.Questions)
		case PromptApproval:
			return c.resolveApprovalLocked(id, answer.Allow, scopeFromApprove(answer.Allow, answer.Session, answer.Persist))
		case PromptPlan:
			return c.resolvePlanDecisionWithFeedbackLocked(id, PlanDecisionAction(answer.Action), answer.Feedback)
		case PromptRecovery:
			return c.resolveRecoveryLocked(id, agent.RecoveryAction(answer.Action), answer.Feedback)
		case PromptMCP:
			return c.answerMCPInteractionCheckedLocked(id, answer.Action, answer.Content)
		default:
			return ErrPromptNotPending
		}
	}
	cancel := func() error {
		switch kind {
		case PromptAsk:
			c.approval.cancelAsk(id)
		case PromptApproval, PromptPlan, PromptRecovery:
			c.approval.cancel(id)
		case PromptMCP:
			c.approval.cancelMCPInteraction(id)
		}
		return nil
	}
	_ = c.promptOwner.RegisterPrompt(PendingPrompt{Identity: identity, State: PromptPending, Resolve: resolve, Cancel: cancel})
}

func (c *Controller) PendingPromptIdentities() []PromptIdentity { return c.promptOwner.Identities() }

func (c *Controller) cancelOwnedPrompt(id string) {
	c.promptResolveMu.Lock()
	defer c.promptResolveMu.Unlock()
	c.cancelOwnedPromptLocked(id)
}

func (c *Controller) cancelOwnedPromptLocked(id string) {
	if prompt, ok := c.promptOwner.Prompt(id); ok && prompt.Cancel != nil {
		_ = prompt.Cancel()
	} else {
		c.approval.cancel(id)
		c.approval.cancelAsk(id)
		c.approval.cancelMCPInteraction(id)
	}
	c.promptOwner.Remove(id)
}

var (
	ErrPromptStaleTurn       = errors.New("prompt belongs to a stale turn")
	ErrPromptStaleRuntime    = errors.New("prompt belongs to a stale runtime")
	ErrPromptAlreadyResolved = errors.New("prompt is already resolved")
	ErrPromptNotPending      = errors.New("prompt is not pending")
)

// PromptAnswer is the transport-neutral union used by exact prompt resolve.
type PromptAnswer struct {
	Questions []event.AskAnswer
	Allow     bool
	Session   bool
	Persist   bool
	Action    string
	Feedback  string
	Content   map[string]any
}

// ResolvePromptExact is the single controller-owned decision boundary. The
// specialized resolvers retain their validation and durable receipts; this
// mutex makes their check-and-wake path single-writer for one controller.
func (c *Controller) ResolvePromptExact(identity PromptIdentity, answer PromptAnswer) error {
	if c == nil {
		return ErrPromptNotPending
	}
	if identity.PromptID == "" || identity.TurnID == "" {
		return ErrPromptNotPending
	}
	c.promptResolveMu.Lock()
	defer c.promptResolveMu.Unlock()
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed {
		return ErrPromptNotPending
	}
	if identity.RuntimeEpoch != "" && identity.RuntimeEpoch != c.promptRuntimeEpoch {
		return ErrPromptStaleRuntime
	}
	turnID, _, _, _ := c.turnEventRuntimeStatus()
	if turnID != identity.TurnID {
		return ErrPromptStaleTurn
	}
	owned, ok := c.promptOwner.Identity(identity.PromptID)
	if !ok {
		if c.promptOwner.WasResolved(identity.PromptID) {
			return ErrPromptAlreadyResolved
		}
		return ErrPromptNotPending
	}
	if owned.TurnID != identity.TurnID || owned.Kind != identity.Kind {
		return ErrPromptStaleTurn
	}
	return c.promptOwner.Resolve(identity, answer)
}
