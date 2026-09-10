package taskcontract

// DelegatedContract is the parent-owned slice a writer child may inherit.
type DelegatedContract struct {
	Requirements []Requirement
	Checks       []Check
	Obligations  []Obligation
}
