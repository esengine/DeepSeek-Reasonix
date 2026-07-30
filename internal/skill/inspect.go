package skill

import (
	"io"
	"sort"
	"strings"
)

// CandidateStatus is the diagnostic disposition of one skill candidate.
type CandidateStatus string

const (
	CandidateWinner   CandidateStatus = "winner"
	CandidateShadowed CandidateStatus = "shadowed"
	CandidateDisabled CandidateStatus = "disabled"
)

// Candidate is one discovered skill entry, including shadowed and disabled ones
// that Store.List omits. Used by capability diagnostics so rules stay aligned
// with discovery without changing List/Read behavior.
type Candidate struct {
	Name        string
	Description string
	Scope       Scope
	Path        string
	Status      CandidateStatus
	WinnerPath  string // set when Status is shadowed
	RunAs       RunAs
}

// Inspection is a read-only snapshot of skill discovery for diagnostics.
type Inspection struct {
	Roots      []Root
	Candidates []Candidate
}

// Inspect walks the same roots as List but keeps shadowed and disabled
// candidates. Missing convention roots stay StatusMissing without issues.
// Stderr parse warnings are suppressed so diagnostics stay quiet.
func (s *Store) Inspect() Inspection {
	if s == nil || s.disableDiscovery {
		return Inspection{}
	}
	origStderr := s.stderr
	s.stderr = io.Discard
	defer func() { s.stderr = origStderr }()

	roots := s.Roots()
	var candidates []Candidate
	winnerByName := map[string]Candidate{}

	// Scan roots highest-priority first (same as List).
	for _, r := range s.roots() {
		if r.Status != StatusOK {
			continue
		}
		for _, sk := range s.discoverRoot(r) {
			candidates = append(candidates, classifyCandidate(sk, s.disabledName(sk.Name), winnerByName)...)
			if !s.disabledName(sk.Name) {
				if _, ok := winnerByName[sk.Name]; !ok {
					winnerByName[sk.Name] = Candidate{
						Name: sk.Name, Description: sk.Description, Scope: sk.Scope,
						Path: sk.Path, Status: CandidateWinner, RunAs: sk.RunAs,
					}
				}
			}
		}
	}

	if !s.disableBuiltins {
		for _, sk := range builtinSkills() {
			candidates = append(candidates, classifyCandidate(sk, s.disabledName(sk.Name), winnerByName)...)
			if !s.disabledName(sk.Name) {
				if _, ok := winnerByName[sk.Name]; !ok {
					winnerByName[sk.Name] = Candidate{
						Name: sk.Name, Description: sk.Description, Scope: sk.Scope,
						Path: sk.Path, Status: CandidateWinner, RunAs: sk.RunAs,
					}
				}
			}
		}
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Name != candidates[j].Name {
			return candidates[i].Name < candidates[j].Name
		}
		order := map[CandidateStatus]int{
			CandidateWinner: 0, CandidateDisabled: 1, CandidateShadowed: 2,
		}
		return order[candidates[i].Status] < order[candidates[j].Status]
	})

	return Inspection{Roots: roots, Candidates: candidates}
}

func classifyCandidate(sk Skill, disabled bool, winners map[string]Candidate) []Candidate {
	if disabled {
		return []Candidate{{
			Name: sk.Name, Description: sk.Description, Scope: sk.Scope,
			Path: sk.Path, Status: CandidateDisabled, RunAs: sk.RunAs,
		}}
	}
	if win, ok := winners[sk.Name]; ok {
		return []Candidate{{
			Name: sk.Name, Description: sk.Description, Scope: sk.Scope,
			Path: sk.Path, Status: CandidateShadowed, WinnerPath: win.Path, RunAs: sk.RunAs,
		}}
	}
	return []Candidate{{
		Name: sk.Name, Description: sk.Description, Scope: sk.Scope,
		Path: sk.Path, Status: CandidateWinner, RunAs: sk.RunAs,
	}}
}

// MissingDescription reports whether a winner skill lacks a usable description.
func MissingDescription(desc string) bool {
	return strings.TrimSpace(desc) == ""
}
