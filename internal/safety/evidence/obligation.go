// What the host is owed, derived from the ledger rather than announced by the
// code that writes it. A caller that emitted its own events would keep two
// truths and drift between them, which is how a counter comes to be wired end
// to end and never incremented.
package evidence

import (
	"fmt"
	"slices"
	"strings"
)

// ObligationKind names what is owed. The kind is the fact; whether owing it may
// end a turn is a policy question the readiness gate answers, so no fatality is
// recorded here.
type ObligationKind string

const (
	// ObligationUnprovenMutation is a change whose extent was never established.
	// Only observation can settle it: a check that runs afterwards speaks for the
	// state the change left, not for what the change was.
	ObligationUnprovenMutation ObligationKind = "unproven_mutation"
	// ObligationStaleVerification is a change with no check passing after it.
	// Every earlier check answered for a workspace that no longer exists.
	ObligationStaleVerification ObligationKind = "stale_verification"
	// ObligationMissingProjectCheck is a check the project declares now that has
	// not run since the latest change.
	ObligationMissingProjectCheck ObligationKind = "missing_project_check"
	// ObligationBaselineCheck is a check the project required when the task
	// began. Rewriting the declaration creates a new requirement; it does not
	// retract the one the work was accepted under, or a turn could cancel its
	// own exam by editing the paper.
	ObligationBaselineCheck ObligationKind = "baseline_required_check"
	// ObligationBaselineTest is a criterion captured as bytes at task start that
	// the final state has no passing evidence for. The workspace's own test of
	// the same name is a different criterion — that is what capturing was for.
	ObligationBaselineTest ObligationKind = "baseline_test_criterion"
	// ObligationUnseenRender is a page or image written since a screenshot last
	// showed it. Only looking settles it: a check that ran says the file parses,
	// not that it looks like what was asked for.
	ObligationUnseenRender ObligationKind = "unseen_render"
)

// CheckContract is what the project required when the task began beside what it
// requires now. Both sides are the host's own reading of each command, so a
// respelling or a wrapper is not a rewrite and the file's raw text is never the
// identity.
type CheckContract struct {
	baseline      []string
	current       []string
	capturedTests int
	delivery      bool
	observeRoot   string
}

func (c CheckContract) WithObserveRoot(root string) CheckContract {
	c.observeRoot = root
	return c
}

func (c CheckContract) WithDelivery(delivery bool) CheckContract {
	c.delivery = delivery
	return c
}

// CaptureCheckContract canonicalises both declarations into criterion
// identities. Capture is the host's act: a contract derived from the workspace
// each time it is asked would be no provenance at all.
func CaptureCheckContract(baseline, current []string) CheckContract {
	return CheckContract{baseline: criterionIdentities(baseline), current: criterionIdentities(current)}
}

// WithCapturedTests preserves criteria even when their current bytes are unchanged.
func (c CheckContract) WithCapturedTests(count int) CheckContract {
	c.capturedTests = count
	return c
}

// Baseline returns the captured identities, for a host that has to persist them
// across a rebuild the task outlives.
func (c CheckContract) Baseline() []string { return slices.Clone(c.baseline) }

func criterionIdentities(commands []string) []string {
	var out []string
	for _, command := range commands {
		id := VerificationIdentity(strings.TrimSpace(command))
		if id != "" && !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}

// Obligation is one thing the host is owed. ID is stable for as long as the
// obligation stands, so the same debt reported twice is recognised as one — and
// it encodes whatever the cause and the discharge are derived from, so a debt
// cannot keep its identity while what would settle it changes underneath. A
// kind that ever breaks that owes the diff an "updated" it does not have.
type Obligation struct {
	ID    string         `json:"id"`
	Kind  ObligationKind `json:"kind"`
	Cause string         `json:"cause,omitempty"`
	// Discharge says what would settle it, in the terms that actually settle it.
	Discharge string `json:"discharge,omitempty"`
}

// Subject is what the obligation is keyed on: the check identity, or the
// receipt index for the debts a single action left. The ID encodes it, and
// reading it back here keeps the format's one reader in the package that
// writes it.
func (o Obligation) Subject() string {
	_, subject, _ := strings.Cut(o.ID, "@")
	return subject
}

// ObligationDelta is the net change one action made to the ledger's debts, not
// a history of what happened inside it: a debt that fell and rose again under
// one identity is reported as neither. An action can settle one and create
// another in the same breath — scope discharges the unproven mutation and
// stales every check before it — so both halves travel together.
type ObligationDelta struct {
	Added      []Obligation `json:"added,omitempty"`
	Discharged []Obligation `json:"discharged,omitempty"`
}

// Empty reports whether this action left the debts as it found them.
func (d ObligationDelta) Empty() bool { return len(d.Added) == 0 && len(d.Discharged) == 0 }

// Obligations derives everything the ledger currently owes. declaredChecks are
// the project's own commands, which define there what covering a change means.
// This is the only place obligations come from: diffing two of these around an
// action is what an ObligationDelta is.
func (l *Ledger) Obligations(contract CheckContract) []Obligation {
	if l == nil {
		return nil
	}
	var out []Obligation
	if at, ok := l.LatestUnprovenMutationIndex(); ok {
		out = append(out, Obligation{
			ID:        fmt.Sprintf("unproven_mutation@%d", at),
			Kind:      ObligationUnprovenMutation,
			Cause:     l.commandAt(at),
			Discharge: "establish what the change touched by making it through a tool that reports its paths",
		})
	}
	at, changed := l.LatestSuccessfulMutationIndex()
	if !changed {
		return out
	}
	if !l.ProseOnlyWithoutChecks(contract) {
		out = append(out, staleVerificationOf(l, at)...)
	}
	return append(out, l.checkObligations(contract, at)...)
}

// ProseOnlyWithoutChecks waives the generic check when every change this task
// made is a prose file and the project names no check of its own. Scope must be
// established for every mutation; a watched subset cannot exempt effects the
// host never observed.
func (l *Ledger) ProseOnlyWithoutChecks(contract CheckContract) bool {
	if contract.delivery || contract.DeclaresChecks() {
		return false
	}
	beyond, scoped, changed := l.mutationsBeyondProse(contract.observeRoot)
	return changed && scoped && len(beyond) == 0
}

// DeclaresChecks reports that the project or the task's start named checks of
// its own, which define verification there.
func (c CheckContract) DeclaresChecks() bool {
	return len(c.baseline) != 0 || len(c.current) != 0 || c.capturedTests != 0
}

// MutationPathsBeyondProse names the changed paths that keep the generic check
// owed, in first-written order, and whether every mutation had its extent
// established.
func (l *Ledger) MutationPathsBeyondProse(contract CheckContract) (paths []string, scoped bool) {
	beyond, scoped, _ := l.mutationsBeyondProse(contract.observeRoot)
	return beyond, scoped
}

func (l *Ledger) mutationsBeyondProse(root string) (beyond []string, scoped, changed bool) {
	if l == nil {
		return nil, false, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	scoped = true
	for _, r := range l.receipts {
		if !r.Mutation {
			continue
		}
		// A failure proves nothing was left unwritten: tool.after can fail a
		// finished write and a move can stop half-done, so a failed named-path
		// call keeps its targets; any other failed mutation is unscoped.
		if !r.Success {
			if !r.Write {
				scoped = false
				continue
			}
			for _, path := range r.MutationPaths {
				if !proseMutationPath(root, path) && !slices.Contains(beyond, path) {
					beyond = append(beyond, path)
				}
			}
			continue
		}
		changed = true
		// Named-path writers establish scope by contract; other tools need a
		// complete observation rather than a watched subset.
		if r.MutationEvidence != MutationProven || len(r.Paths) == 0 || (!r.Write && !r.PathsComplete) {
			scoped = false
			continue
		}
		for _, path := range r.MutationPaths {
			if !proseMutationPath(root, path) && !slices.Contains(beyond, path) {
				beyond = append(beyond, path)
			}
		}
	}
	return beyond, scoped, changed
}

// checkObligations owes every criterion either declaration named, baseline
// first. A baseline criterion the current declaration dropped is still owed —
// the rewrite is reported as what it is, an observation, never as its own
// discharge.
func (l *Ledger) checkObligations(contract CheckContract, at int) []Obligation {
	var out []Obligation
	for _, id := range contract.baseline {
		if l.HasSuccessfulCommandAfter(id, at) {
			continue
		}
		cause := fmt.Sprintf("the task began requiring %q", id)
		if !slices.Contains(contract.current, id) {
			cause += ", and the declaration that required it was rewritten"
		}
		out = append(out, Obligation{
			ID:        "baseline_required_check@" + id,
			Kind:      ObligationBaselineCheck,
			Cause:     cause,
			Discharge: "run " + id,
		})
	}
	for _, id := range contract.current {
		if slices.Contains(contract.baseline, id) || l.HasSuccessfulCommandAfter(id, at) {
			continue
		}
		out = append(out, Obligation{
			ID:        "missing_project_check@" + id,
			Kind:      ObligationMissingProjectCheck,
			Cause:     fmt.Sprintf("the project declares %q and it has not run since the change", id),
			Discharge: "run " + id,
		})
	}
	return out
}

func staleVerificationOf(l *Ledger, at int) []Obligation {
	if l.HasSuccessfulVerificationCommandAfter(at) && !l.HasFailedVerificationAfter(at) {
		return nil
	}
	return []Obligation{{
		ID:        fmt.Sprintf("stale_verification@%d", at),
		Kind:      ObligationStaleVerification,
		Cause:     l.commandAt(at),
		Discharge: "run a check after the change, and leave none of them standing failed",
	}}
}

// commandAt names the receipt a debt came from, so the model is told which of
// its own actions it is answering for.
func (l *Ledger) commandAt(at int) string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if at < 0 || at >= len(l.receipts) {
		return ""
	}
	r := l.receipts[at]
	if strings.TrimSpace(r.Command) != "" {
		return r.Command
	}
	return r.ToolName
}

// DiffObligations reports what changed between two derivations. It is the only
// way an ObligationDelta is built: nothing announces a debt, the ledger is asked
// what it owes and the answers are compared.
func DiffObligations(before, after []Obligation) ObligationDelta {
	var delta ObligationDelta
	for _, o := range after {
		if !slices.ContainsFunc(before, func(b Obligation) bool { return b.ID == o.ID }) {
			delta.Added = append(delta.Added, o)
		}
	}
	for _, o := range before {
		if !slices.ContainsFunc(after, func(a Obligation) bool { return a.ID == o.ID }) {
			delta.Discharged = append(delta.Discharged, o)
		}
	}
	return delta
}
