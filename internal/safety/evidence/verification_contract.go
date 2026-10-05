// A Goal is accepted under the declaration the process that started it read.
// That acceptance is state, not a re-reading: the declaration on disk can
// change while the Goal it was frozen for is still running, and which one then
// governs is a decision, not a lookup.
package evidence

import "slices"

// VerificationContract is what one Goal is held to. Checks are canonical
// criterion identities, so a respelling is not a different contract. Epoch is
// the contract's generation; nothing increments it, because superseding a
// contract is a decision no code here is allowed to make.
type VerificationContract struct {
	Epoch  uint64   `json:"epoch"`
	Checks []string `json:"checks,omitempty"`
}

// FreezeVerificationContract canonicalises a declaration into the contract a
// Goal begins under. A declaration naming nothing freezes an empty contract,
// which is a contract — distinct from a checkpoint that never carried one.
func FreezeVerificationContract(commands []string) VerificationContract {
	return VerificationContract{Epoch: 1, Checks: criterionIdentities(commands)}
}

// DriftsFrom reports whether a declaration names a different set of criteria
// than this contract was frozen under. It compares identities as sets and says
// nothing about which of the two should govern.
func (c VerificationContract) DriftsFrom(commands []string) bool {
	current := criterionIdentities(commands)
	if len(current) != len(c.Checks) {
		return true
	}
	for _, id := range current {
		if !slices.Contains(c.Checks, id) {
			return true
		}
	}
	return false
}
