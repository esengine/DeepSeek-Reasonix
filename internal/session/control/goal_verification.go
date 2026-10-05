// What a Goal is accepted under, and how that acceptance travels. The
// declaration on disk can change while the Goal frozen under it is still
// running; which one governs is a decision, so nothing here makes it.
package control

import (
	"slices"

	"reasonix/internal/contract/event"
	"reasonix/internal/safety/evidence"
)

// observeVerificationContractDrift compares the contract a resumed Goal was
// accepted under with the declaration this process loaded. Shadow only: it
// blocks nothing, owes nothing and supersedes nothing.
func (c *Controller) observeVerificationContractDrift() {
	if c.executor == nil {
		return
	}
	state := c.goals.deliveryState()
	// A checkpoint written before contracts existed records no acceptance, and
	// filling one in from the current declaration would invent the history this
	// observation exists to read.
	if state.Verification == nil {
		return
	}
	current := c.executor.DeclaredProjectChecks()
	event.RecordVerificationContractDrift(c.sink, event.VerificationContractDrift{
		ScopeID: state.ScopeID,
		Epoch:   state.Verification.Epoch,
		Frozen:  slices.Clone(state.Verification.Checks),
		Current: evidence.FreezeVerificationContract(current).Checks,
		Drift:   state.Verification.DriftsFrom(current),
	})
}

// frozenVerificationContract is what a new Goal is accepted under: the
// project's declaration as this process loaded it, canonicalised.
func (c *Controller) frozenVerificationContract() evidence.VerificationContract {
	if c.executor == nil {
		return evidence.VerificationContract{}
	}
	return evidence.FreezeVerificationContract(c.executor.DeclaredProjectChecks())
}

func (g *goalMachine) setDeliveryCheckpoint(checkpoint evidence.DeliveryCheckpoint, todos []evidence.TodoItem) (string, []byte, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.scopeID == "" || checkpoint.ScopeID != g.scopeID {
		return "", nil, false
	}
	// The executor reports delivery progress, not what this Goal was accepted
	// under: its checkpoint is rebuilt from the scope id alone at the start of a
	// scope, so letting it write here would clear the contract the Goal froze.
	if checkpoint.Verification == nil {
		checkpoint.Verification = g.deliveryCheckpoint.Verification
	}
	g.deliveryCheckpoint = checkpoint
	return g.buildStateLocked(todos)
}

func (g *goalMachine) deliveryState() evidence.DeliveryCheckpoint {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.deliveryCheckpoint
}
