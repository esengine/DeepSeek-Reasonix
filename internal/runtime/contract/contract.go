package contract

import (
	"encoding/json"
	"slices"
	"strings"

	"reasonix/internal/state/trustedstate"
)

// Verifier kinds.
const (
	VerifierCommand = "command"
	VerifierTest    = "test"
	// VerifierChange is satisfied by any change the host proved in the task.
	VerifierChange = "change"
)

// Verifier is what satisfies a criterion: a verification command recognised
// by its canonical identity, or a captured test criterion by its identity.
type Verifier struct {
	Kind     string `json:"kind"`
	Identity string `json:"identity"`
}

// Criterion is one accepted acceptance criterion.
type Criterion struct {
	ID       string   `json:"id"`
	Source   string   `json:"source"`
	Required bool     `json:"required"`
	Verifier Verifier `json:"verifier"`
}

// Contract is one accepted revision.
type Contract struct {
	ID         string      `json:"id"`
	Revision   int         `json:"revision"`
	Parent     string      `json:"parent,omitempty"`
	Criteria   []Criterion `json:"criteria"`
	AcceptedBy string      `json:"accepted_by"`
}

// Sources are the host-owned inputs a revision is derived from.
type Sources struct {
	// Checks are canonical identities of the check commands the task began
	// requiring (evidence.CheckContract baseline).
	Checks []string
	// Tests are captured test criterion identities.
	Tests []string
	// PlanChecks are the plan's verification commands for its required
	// criteria, by identity: criterion ids are positional within one plan and
	// mean nothing across two, while a command's identity does.
	PlanChecks []string
	// PlanChanges says the plan names files the work is expected to touch,
	// which makes a proven change one of the task's deliverables.
	PlanChanges bool
}

// Sources a criterion came from.
const (
	SourceProjectCheck = "project_check"
	SourceBaselineTest = "baseline_test"
	SourcePlan         = "plan"
)

// PlanDeliverable is the criterion a plan naming files to touch adds: the task
// must end with a change the host proved. It binds no path, because the plan's
// files are inferred and a replan moves them.
const PlanDeliverable = "change@plan"

// PolicyTemplate names the host policy that accepts a derived revision.
const PolicyTemplate = "host_policy:template/1"

// Derive turns sources into criteria in a canonical order, so the same sources
// always derive byte-identical criteria.
func Derive(s Sources) []Criterion {
	var out []Criterion
	add := func(source, kind string, ids []string) {
		for _, id := range ids {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			c := Criterion{ID: kind + "@" + id, Source: source, Required: true, Verifier: Verifier{Kind: kind, Identity: id}}
			if !slices.ContainsFunc(out, func(x Criterion) bool { return x.ID == c.ID }) {
				out = append(out, c)
			}
		}
	}
	add(SourceProjectCheck, VerifierCommand, s.Checks)
	add(SourceBaselineTest, VerifierTest, s.Tests)
	add(SourcePlan, VerifierCommand, s.PlanChecks)
	if s.PlanChanges {
		out = append(out, Criterion{ID: PlanDeliverable, Source: SourcePlan, Required: true, Verifier: Verifier{Kind: VerifierChange, Identity: "plan"}})
	}
	slices.SortFunc(out, func(a, b Criterion) int { return strings.Compare(a.ID, b.ID) })
	return out
}

// Decision is what host policy did with a derivation.
type Decision string

const (
	// Accepted: the first revision of a new contract.
	Accepted Decision = "accepted"
	// Tightened: a new revision that keeps every criterion and adds some.
	Tightened Decision = "tightened"
	// Unchanged: the derivation matches the current revision.
	Unchanged Decision = "unchanged"
	// RelaxationRefused: the derivation drops or alters a criterion, which only
	// the User may accept; the current revision stands.
	RelaxationRefused Decision = "relaxation_refused"
	// UserRelaxed: the User accepted a derivation that drops criteria.
	UserRelaxed Decision = "user_relaxed"
)

// Dropped lists the current revision's criteria a derivation no longer holds.
func Dropped(current *Contract, derived []Criterion) []Criterion {
	if current == nil {
		return nil
	}
	var out []Criterion
	for _, c := range current.Criteria {
		if !slices.ContainsFunc(derived, c.Equal) {
			out = append(out, c)
		}
	}
	return out
}

// AcceptRelaxation records a derivation the User accepted although it drops
// criteria. by names the act that carried the User's acceptance; host policy
// never calls this.
func AcceptRelaxation(current Contract, parentDigest string, derived []Criterion, by string) Contract {
	return Contract{ID: current.ID, Revision: current.Revision + 1, Parent: parentDigest, Criteria: derived, AcceptedBy: by}
}

// Accept applies host policy to derived criteria against the current revision
// (nil for a task with no contract yet). It returns the revision now in force
// and whether it is new. id names a first revision's contract.
func Accept(current *Contract, parentDigest string, derived []Criterion, id string) (Contract, Decision) {
	if current == nil {
		return Contract{ID: id, Revision: 1, Criteria: derived, AcceptedBy: PolicyTemplate}, Accepted
	}
	for _, c := range current.Criteria {
		if !slices.ContainsFunc(derived, func(d Criterion) bool { return d.Equal(c) }) {
			return *current, RelaxationRefused
		}
	}
	if len(derived) == len(current.Criteria) {
		return *current, Unchanged
	}
	return Contract{
		ID:         current.ID,
		Revision:   current.Revision + 1,
		Parent:     parentDigest,
		Criteria:   derived,
		AcceptedBy: PolicyTemplate,
	}, Tightened
}

// Encode is a revision's canonical bytes.
func (c Contract) Encode() []byte {
	b, _ := json.Marshal(c)
	return b
}

// Digest identifies a revision's exact content.
func (c Contract) Digest() string { return string(trustedstate.DigestOf(c.Encode())) }

// Equal reports whether two criteria demand the same thing of the same verifier.
func (c Criterion) Equal(o Criterion) bool {
	return c == o
}
