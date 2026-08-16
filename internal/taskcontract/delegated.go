package taskcontract

// DelegatedContract is the parent-owned slice a writer child may inherit.
type DelegatedContract struct {
	Requirements []Requirement
	Checks       []Check
	Obligations  []Obligation
}

// Delegate copies the selected parent criteria into an isolated child contract.
func Delegate(parent *Contract, ids []string) *DelegatedContract {
	if parent == nil {
		return nil
	}
	want := map[string]bool{}
	for _, id := range ids {
		if id != "" {
			want[id] = true
		}
	}
	out := &DelegatedContract{}
	for _, req := range parent.Requirements {
		if len(want) == 0 || want[req.ID] {
			out.Requirements = append(out.Requirements, req)
		}
	}
	out.Checks = append([]Check(nil), parent.Checks...)
	for _, o := range parent.Obligations {
		out.Obligations = append(out.Obligations, cloneObligation(o))
	}
	return out
}

// Apply overlays a delegated snapshot onto a child contract.
func (c *Contract) ApplyDelegated(d *DelegatedContract) {
	if c == nil || d == nil {
		return
	}
	for _, req := range d.Requirements {
		c.AddRequirement(req.ID, req.Text, req.Required)
	}
	for _, check := range d.Checks {
		c.AddCheck(check.Command)
	}
	for _, o := range d.Obligations {
		c.addObligation(o)
	}
}
