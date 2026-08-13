package completion

import (
	"fmt"
	"strings"

	"reasonix/internal/evidence"
	"reasonix/internal/taskcontract"
)

// Verdict is the report's headline. Partial is terminal: the work is proven
// against its criteria, and the gaps it still carries are declared rather
// than hidden.
type Verdict uint8

const (
	VerdictUnknown Verdict = iota
	VerdictIncomplete
	VerdictPartial
	VerdictDone
)

func (v Verdict) String() string {
	switch v {
	case VerdictIncomplete:
		return "incomplete"
	case VerdictPartial:
		return "partial"
	case VerdictDone:
		return "done"
	default:
		return "unknown"
	}
}

// Criterion is one acceptance criterion and the proof the ledger attached.
type Criterion struct {
	ID       string
	Text     string
	Required bool
	Status   taskcontract.Status
	Proofs   int // successful evidence refs attached to it
}

// Change is one path the turn mutated. Reviewed reports whether the changed
// result was inspected after the last write to it.
type Change struct {
	Path     string
	Reviewed bool
}

// Verification is a delivery-verification command's latest outcome. Stale
// means it last ran before the newest mutation, so it proves nothing about
// the current tree.
type Verification struct {
	Command string
	Passed  bool
	Stale   bool
}

// GapKind classifies one thing the report refuses to present as verified.
type GapKind uint8

const (
	// GapUnbackedClaim is first because it is the worst: the turn asserted a
	// verification the ledger does not support.
	GapUnbackedClaim GapKind = iota
	GapUnprovenCriterion
	GapMissingCheck
	GapFailedVerification
	GapStaleVerification
	GapUnverifiedChange
	GapUnreviewedChange
	GapDeclaredUnverified
)

func (k GapKind) String() string {
	switch k {
	case GapUnbackedClaim:
		return "unbacked_claim"
	case GapDeclaredUnverified:
		return "declared_unverified"
	case GapUnprovenCriterion:
		return "unproven_criterion"
	case GapMissingCheck:
		return "missing_check"
	case GapFailedVerification:
		return "failed_verification"
	case GapStaleVerification:
		return "stale_verification"
	case GapUnverifiedChange:
		return "unverified_change"
	case GapUnreviewedChange:
		return "unreviewed_change"
	default:
		return "unknown"
	}
}

// Gap is one unproven thing, in the report's own words.
type Gap struct {
	Kind   GapKind
	Detail string
}

// Report is the host's completion record for one turn.
type Report struct {
	Verdict Verdict
	Risk    taskcontract.Risk
	// Mutations counts every successful mutating receipt, including ones that
	// named no path; Changes lists only the paths.
	Mutations     int
	Criteria      []Criterion
	Changes       []Change
	Verifications []Verification
	Gaps          []Gap
	// Claimed is what the turn said about itself; Risks is its declared risk
	// list. Both are model-authored and never clear a host-found gap.
	Claimed Claim
	Risks   []string
}

// Build derives the report from a contract and the turn's receipts. Both may
// be nil: a nil contract means nothing declared acceptance criteria, which
// leaves the ledger alone to speak.
func Build(c *taskcontract.Contract, ledger *evidence.Ledger) Report {
	receipts := ledger.Receipts()
	rep := Report{
		Mutations:     mutationsOf(receipts),
		Criteria:      criteriaOf(c),
		Changes:       changesOf(ledger, receipts),
		Verifications: verificationsOf(receipts),
	}
	if c != nil {
		rep.Risk = c.Risk
	}
	rep.Gaps = gapsOf(rep, c)
	rep = reconcile(rep, claimOf(receipts), receipts)
	rep.Verdict = verdictOf(rep, c)
	return rep
}

func criteriaOf(c *taskcontract.Contract) []Criterion {
	if c == nil {
		return nil
	}
	out := make([]Criterion, 0, len(c.Requirements))
	for _, req := range c.Requirements {
		proofs := 0
		for _, ref := range req.Evidence {
			if ref.Success {
				proofs++
			}
		}
		out = append(out, Criterion{
			ID:       req.ID,
			Text:     req.Text,
			Required: req.Required,
			Status:   req.Status,
			Proofs:   proofs,
		})
	}
	return out
}

// changesOf lists mutated paths in first-write order and asks the ledger
// whether each one was inspected after its own latest write, so a review that
// covered one file never vouches for another.
func changesOf(ledger *evidence.Ledger, receipts []evidence.Receipt) []Change {
	var out []Change
	at := map[string]int{}
	lastWrite := map[string]int{}
	for i, r := range receipts {
		if !r.Success || !(r.Mutation || r.Write) {
			continue
		}
		for _, p := range r.Paths {
			if p == "" {
				continue
			}
			if _, seen := at[p]; !seen {
				at[p] = len(out)
				out = append(out, Change{Path: p})
			}
			lastWrite[p] = i
		}
	}
	for i := range out {
		out[i].Reviewed = ledger.HasHostReviewCoverageAfter(lastWrite[out[i].Path], []string{out[i].Path})
	}
	return out
}

// mutationsOf counts successful mutating receipts, path-named or not: a
// `sed -i` or `rm` that named nothing still changed the workspace, and must
// not escape the unverified-change gap by leaving no path behind.
func mutationsOf(receipts []evidence.Receipt) int {
	count := 0
	for _, r := range receipts {
		if r.Success && (r.Mutation || r.Write) {
			count++
		}
	}
	return count
}

// verificationsOf keeps each delivery-verification command's latest run, in
// first-run order, and marks the ones that predate the newest mutation.
func verificationsOf(receipts []evidence.Receipt) []Verification {
	lastMutation := -1
	for i, r := range receipts {
		if r.Success && (r.Mutation || r.Write) {
			lastMutation = i
		}
	}
	var out []Verification
	at := map[string]int{}
	for i, r := range receipts {
		command := strings.TrimSpace(r.Command)
		if command == "" || !evidence.IsDeliveryVerificationCommand(command) {
			continue
		}
		if _, seen := at[command]; !seen {
			at[command] = len(out)
			out = append(out, Verification{Command: command})
		}
		out[at[command]].Passed = r.Success
		out[at[command]].Stale = i < lastMutation
	}
	return out
}

func gapsOf(rep Report, c *taskcontract.Contract) []Gap {
	var gaps []Gap
	for _, cr := range rep.Criteria {
		if cr.Required && cr.Status != taskcontract.Satisfied {
			gaps = append(gaps, Gap{GapUnprovenCriterion, fmt.Sprintf("%s: %s", cr.ID, cr.Text)})
		}
	}
	missingCheck := false
	if c != nil {
		for _, check := range c.Checks {
			if check.Status == taskcontract.Satisfied {
				continue
			}
			missingCheck = true
			gaps = append(gaps, Gap{GapMissingCheck, checkLabel(check)})
		}
	}
	proven := false
	for _, v := range rep.Verifications {
		if v.Passed && !v.Stale {
			proven = true
		}
	}
	for _, v := range rep.Verifications {
		switch {
		case !v.Passed:
			gaps = append(gaps, Gap{GapFailedVerification, v.Command})
		case v.Stale && !proven:
			// Superseded commands matter only while nothing fresh has proven
			// the tree; listing them after a green run is the pedantry that
			// teaches people to skip receipts.
			gaps = append(gaps, Gap{GapStaleVerification, v.Command})
		}
	}
	// Only report the blanket gap when no declared check already said it: a
	// contract with checks states the same absence in more specific words.
	if rep.Mutations > 0 && !proven && !missingCheck {
		gaps = append(gaps, Gap{GapUnverifiedChange, "no verification passed after the latest change"})
	}
	for _, ch := range rep.Changes {
		if !ch.Reviewed {
			gaps = append(gaps, Gap{GapUnreviewedChange, ch.Path})
		}
	}
	return gaps
}

func checkLabel(check taskcontract.Check) string {
	switch {
	case check.Command != "":
		return check.Command
	case check.Kind == taskcontract.CheckMutation:
		return "the required change"
	default:
		return "any verification"
	}
}

func verdictOf(rep Report, c *taskcontract.Contract) Verdict {
	declared := c != nil && (len(c.Requirements) > 0 || len(c.Checks) > 0)
	switch {
	case !declared && rep.Mutations == 0 && len(rep.Verifications) == 0 && rep.Claimed.Empty():
		return VerdictUnknown
	case c != nil && !c.Complete():
		return VerdictIncomplete
	case len(rep.Gaps) > 0:
		return VerdictPartial
	default:
		return VerdictDone
	}
}

// Summary is the one-line report headline for logs and host notes.
func (r Report) Summary() string {
	satisfied := 0
	required := 0
	for _, cr := range r.Criteria {
		if !cr.Required {
			continue
		}
		required++
		if cr.Status == taskcontract.Satisfied {
			satisfied++
		}
	}
	return fmt.Sprintf("%s · criteria %d/%d · changes %d · verifications %d · gaps %d",
		r.Verdict, satisfied, required, len(r.Changes), len(r.Verifications), len(r.Gaps))
}

// GapKinds lists the distinct gap kinds present, in declaration order, so a
// content-free audit can carry what kind of proof is missing.
func (r Report) GapKinds() []string {
	seen := map[GapKind]bool{}
	var out []string
	for _, kind := range []GapKind{GapUnbackedClaim, GapUnprovenCriterion, GapMissingCheck, GapFailedVerification, GapStaleVerification, GapUnverifiedChange, GapUnreviewedChange, GapDeclaredUnverified} {
		for _, gap := range r.Gaps {
			if gap.Kind == kind && !seen[kind] {
				seen[kind] = true
				out = append(out, kind.String())
			}
		}
	}
	return out
}
