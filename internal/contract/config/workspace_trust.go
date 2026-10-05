package config

import "fmt"

// WorkspaceTrust is the person's decision about one workspace folder. It is
// kept under their Reasonix home beside the folder's other grants, so nothing
// a checkout carries can record it.
type WorkspaceTrust string

const (
	WorkspaceTrustUndecided WorkspaceTrust = ""
	WorkspaceTrusted        WorkspaceTrust = "trusted"
	WorkspaceTrustDeclined  WorkspaceTrust = "declined"
)

// Trust returns what was decided about root. Anything but the two recorded
// answers reads as undecided.
func (s *ProjectGrantStore) Trust(root string) (WorkspaceTrust, error) {
	grant, err := s.Grant(root)
	if err != nil {
		return WorkspaceTrustUndecided, err
	}
	switch grant.Trust {
	case WorkspaceTrusted, WorkspaceTrustDeclined:
		return grant.Trust, nil
	}
	return WorkspaceTrustUndecided, nil
}

// SetTrust records a decision about root; undecided forgets it.
func (s *ProjectGrantStore) SetTrust(root string, trust WorkspaceTrust) error {
	switch trust {
	case WorkspaceTrustUndecided, WorkspaceTrusted, WorkspaceTrustDeclined:
	default:
		return fmt.Errorf("workspace trust %q: must be trusted or declined", trust)
	}
	return s.Update(root, func(g ProjectGrant) (ProjectGrant, error) {
		g.Trust = trust
		return g, nil
	})
}
