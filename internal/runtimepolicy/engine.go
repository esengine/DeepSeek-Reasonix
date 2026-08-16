package runtimepolicy

import (
	"strings"
	"sync"

	"reasonix/internal/evidence"
	"reasonix/internal/taskcontract"
)

// Engine owns the fact contract behind a dedicated mutex. Permission, tool
// execution, and user prompts must happen outside that lock.
type Engine struct {
	mu          sync.Mutex
	contract    *taskcontract.Contract
	guards      []Guard
	constraints Constraints
	seq         int
}

// NewEngine starts an empty contract with the fixed monotonic guard set.
func NewEngine(constraints Constraints, extra ...Guard) *Engine {
	guards := []Guard{
		PlanGuard{},
		ConstraintGuard{Constraints: constraints},
		MutationDependencyGuard{},
		ContractPreconditionGuard{},
		OpaqueWriterGuard{},
	}
	guards = append(guards, extra...)
	return &Engine{contract: taskcontract.New(""), guards: guards, constraints: constraints}
}

// Constraints returns a copy of the frozen user/host limits.
func (e *Engine) Constraints() Constraints {
	if e == nil {
		return Constraints{}
	}
	return e.constraints
}

// Rebuild replaces the contract from a pure fact replay.
func (e *Engine) Rebuild(facts taskcontract.RebuildFacts) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.contract = taskcontract.Rebuild(facts)
	e.seq = len(facts.Receipts)
}

// Snapshot copies the contract for lock-free inspection.
func (e *Engine) Snapshot() *taskcontract.Contract {
	if e == nil {
		return taskcontract.New("")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return cloneContract(e.contract)
}

// BeforeTool merges guard decisions under the contract lock and returns an
// immutable snapshot. Callers must release before asking the user or running
// a tool.
func (e *Engine) BeforeTool(ctx CallContext) GuardDecision {
	if e == nil {
		return GuardDecision{Action: GuardAbstain}
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.contract != nil {
		ctx.HasTodo = ctx.HasTodo || hasTodo(e.contract)
		ctx.HasCriteria = ctx.HasCriteria || hasCriteria(e.contract)
		ctx.TestsForbidden = ctx.TestsForbidden || e.constraints.ForbidTests
		ctx.PriorWriteTargets, ctx.PriorProductionWrite = observedWorkspaceWrites(e.contract)
	}
	var decisions []GuardDecision
	for _, g := range e.guards {
		decisions = append(decisions, g.BeforeTool(ctx))
	}
	merged := MergeDecisions(decisions...)
	if merged.Action == GuardAbstain {
		// Abstain is not authorization; availability stays with the owner.
		return merged
	}
	return merged
}

// CommitReceipt absorbs a frozen receipt under the contract lock.
func (e *Engine) CommitReceipt(ctx ResultContext) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if ctx.Seq == 0 {
		e.seq++
		ctx.Seq = e.seq
	} else if ctx.Seq > e.seq {
		e.seq = ctx.Seq
	}
	if e.contract == nil {
		e.contract = taskcontract.New("")
	}
	e.contract.AbsorbReceipt(
		ctx.Seq,
		ctx.Receipt,
		ctx.WorkspaceRoot,
		ctx.TestsForbidden || e.constraints.ForbidTests,
		e.constraints.RequireFullVerification,
	)
	for _, g := range e.guards {
		_ = g.AfterTool(ctx)
	}
}

// SyncReceipts absorbs ledger receipts that the engine has not yet seen.
func (e *Engine) SyncReceipts(receipts []evidence.Receipt, workspaceRoot string, testsForbidden bool) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.contract == nil {
		e.contract = taskcontract.New("")
	}
	if testsForbidden || e.constraints.ForbidTests {
		testsForbidden = true
	}
	for i, rec := range receipts {
		seq := i + 1
		if seq <= e.seq {
			continue
		}
		e.seq = seq
		e.contract.AbsorbReceipt(seq, rec, workspaceRoot, testsForbidden, e.constraints.RequireFullVerification)
	}
}

// BeforeStop evaluates the contract and guards without waiting on I/O.
func (e *Engine) BeforeStop(ctx StopContext) StopDecision {
	if e == nil {
		return StopDecision{Disposition: taskcontract.StopReady}
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	dec := StopDecision{Disposition: taskcontract.StopReady}
	if e.contract != nil {
		dec.Disposition = e.contract.Stop(ctx.Opts)
		dec.Advisory = e.contract.AdvisoryGaps()
	}
	for _, g := range e.guards {
		got := g.BeforeStop(ctx)
		if got.Disposition > dec.Disposition {
			dec.Disposition = got.Disposition
			dec.Message = got.Message
		}
	}
	return dec
}

// NoteRecoveryAttempt records that a recoverable/strict gap was retried.
func (e *Engine) NoteRecoveryAttempt() {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.contract != nil {
		e.contract.NoteRecoveryAttempt()
	}
}

func hasTodo(c *taskcontract.Contract) bool {
	if c == nil {
		return false
	}
	for _, o := range c.Obligations {
		if o.Kind == taskcontract.ObligationTodo && len(o.SatisfiedBy) > 0 {
			return true
		}
	}
	for _, req := range c.Requirements {
		if req.ID != "" {
			return true
		}
	}
	return false
}

func hasCriteria(c *taskcontract.Contract) bool {
	if c == nil {
		return false
	}
	for _, req := range c.Requirements {
		if req.Required {
			return true
		}
	}
	return false
}

func observedWorkspaceWrites(c *taskcontract.Contract) ([]evidence.TargetKey, bool) {
	if c == nil {
		return nil, false
	}
	seen := make(map[evidence.TargetKey]bool)
	var targets []evidence.TargetKey
	production := false
	for _, obligation := range c.Obligations {
		local, prod := localWriteOrigin(obligation.Origin)
		if !local {
			continue
		}
		for _, target := range obligation.Targets {
			key := string(target)
			if (!strings.HasPrefix(key, "file:") && !strings.HasPrefix(key, "dir:")) || seen[target] {
				continue
			}
			seen[target] = true
			targets = append(targets, target)
		}
		production = production || prod
	}
	return targets, production
}

func localWriteOrigin(origin taskcontract.ReasonCode) (local, production bool) {
	switch origin {
	case taskcontract.ReasonDocsEdit:
		return true, false
	case taskcontract.ReasonProductionEdit, taskcontract.ReasonMultiFile,
		taskcontract.ReasonSchemaPath, taskcontract.ReasonAuthPath,
		taskcontract.ReasonDestructive, taskcontract.ReasonOpaqueWriter:
		return true, true
	default:
		return false, false
	}
}

func cloneContract(c *taskcontract.Contract) *taskcontract.Contract {
	if c == nil {
		return taskcontract.New("")
	}
	out := *c
	out.Requirements = append([]taskcontract.Requirement(nil), c.Requirements...)
	out.Checks = append([]taskcontract.Check(nil), c.Checks...)
	out.Obligations = append([]taskcontract.Obligation(nil), c.Obligations...)
	for i := range out.Obligations {
		out.Obligations[i] = cloneStored(out.Obligations[i])
	}
	return &out
}

func cloneStored(o taskcontract.Obligation) taskcontract.Obligation {
	if len(o.Targets) > 0 {
		o.Targets = append(o.Targets[:0:0], o.Targets...)
	}
	if len(o.SatisfiedBy) > 0 {
		o.SatisfiedBy = append(o.SatisfiedBy[:0:0], o.SatisfiedBy...)
	}
	return o
}
