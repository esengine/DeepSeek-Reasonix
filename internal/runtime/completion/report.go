package completion

import (
	"fmt"
	"slices"
	"strings"

	"reasonix/internal/runtime/taskcontract"
	"reasonix/internal/safety/evidence"
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
// the current tree. Inconclusive means every run of it so far hid its own
// verdict behind a later stage's exit status, which proves nothing either.
type Verification struct {
	Command      string
	Passed       bool
	Stale        bool
	Inconclusive bool
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
	GapInconclusiveVerification
	GapUnverifiedChange
	GapUnreviewedChange
)

func (k GapKind) String() string {
	switch k {
	case GapUnbackedClaim:
		return "unbacked_claim"
	case GapUnprovenCriterion:
		return "unproven_criterion"
	case GapMissingCheck:
		return "missing_check"
	case GapFailedVerification:
		return "failed_verification"
	case GapStaleVerification:
		return "stale_verification"
	case GapInconclusiveVerification:
		return "inconclusive_verification"
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
	// UnreadCheck is the last successful command the ledger ran and did not read
	// as a check. A blanket "nothing verified this" is false to a turn that just
	// ran one, and a gap that reads as false stops being read at all.
	UnreadCheck string
	// Claimed is what the turn said about itself; Risks and Unverified are its
	// declarations. All three are model-authored: they never clear a host-found
	// gap, and being honest about one never counts as a gap either.
	Claimed    Claim
	Risks      []string
	Unverified []string
}

// Build derives the report from a contract and the turn's receipts. Both may
// be nil: a nil contract means nothing declared acceptance criteria, which
// leaves the ledger alone to speak. inWorkspace, when non-nil, says which paths
// are the work product — a probe written to a scratch directory is a change to
// nothing the turn owes a check or a review for. Nil keeps every path.
func Build(c *taskcontract.Contract, ledger *evidence.Ledger, inWorkspace func(path string) bool) Report {
	receipts := ledger.Receipts()
	rep := Report{
		Mutations:     mutationsOf(receipts, inWorkspace),
		Criteria:      criteriaOf(c),
		Changes:       changesOf(ledger, receipts, inWorkspace),
		Verifications: verificationsOf(receipts),
		UnreadCheck:   unreadCheckOf(receipts),
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
func changesOf(ledger *evidence.Ledger, receipts []evidence.Receipt, inWorkspace func(string) bool) []Change {
	var out []Change
	at := map[string]int{}
	lastWrite := map[string]int{}
	authored := map[string]bool{}
	for i, r := range receipts {
		if !r.Success || !(r.Mutation || r.Write) {
			continue
		}
		for _, p := range r.Created {
			authored[p] = true
		}
		for _, p := range r.Paths {
			if p == "" || (inWorkspace != nil && !inWorkspace(p)) {
				continue
			}
			if _, seen := at[p]; !seen {
				at[p] = len(out)
				out = append(out, Change{Path: p})
			}
			lastWrite[p] = i
			// A path this turn created and has not since replaced is entirely
			// its own writing; a later edit means there is again a before to
			// compare the result against.
			if !slices.Contains(r.Created, p) {
				authored[p] = false
			}
		}
	}
	for i := range out {
		path := out[i].Path
		// Reading back a file the turn authored end to end reviews the model's
		// own text against itself. What answers for those is the check that ran
		// over them, which the report accounts for separately.
		out[i].Reviewed = authored[path] || ledger.HasHostReviewCoverageAfter(lastWrite[path], []string{path})
	}
	return out
}

// mutationsOf counts successful mutating receipts, path-named or not: a
// `sed -i` or `rm` that named nothing still changed the workspace, and must
// not escape the unverified-change gap by leaving no path behind.
func mutationsOf(receipts []evidence.Receipt, inWorkspace func(string) bool) int {
	count := 0
	for _, r := range receipts {
		if r.Success && (r.Mutation || r.Write) && keptAnyPath(r.Paths, inWorkspace) {
			count++
		}
	}
	return count
}

// keptAnyPath reports whether a receipt touched the work product. A receipt
// that named no path counts either way: `sed -i` and `rm` name nothing and
// must not escape the gap by staying silent about where they landed.
func keptAnyPath(paths []string, inWorkspace func(string) bool) bool {
	if inWorkspace == nil || len(paths) == 0 {
		return true
	}
	return slices.ContainsFunc(paths, inWorkspace)
}

// browserCheckTool is the browser call the host can classify as a check.
const browserCheckTool = "browser_act"

// verificationsOf keeps each delivery-verification command's latest run, in
// first-run order, and marks the ones that predate the newest mutation. Runs
// key on the verification they carry, not their shell wrapper, and the verdict
// is the host's classification, never the call's error: `go test ./... | head`
// exits 0 on a failing suite.
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
		// A page the host watched the steps pass on is a check with no command
		// line; the tool is its name, and the verdict is still the host's.
		if command == "" && r.ToolName == browserCheckTool {
			command = browserCheckTool
		}
		if command == "" || !evidence.ReceiptRunsVerification(r) {
			continue
		}
		key := evidence.VerificationIdentity(command)
		if _, seen := at[key]; !seen {
			at[key] = len(out)
			out = append(out, Verification{Command: key, Inconclusive: true})
		}
		v := &out[at[key]]
		switch r.Verification {
		case evidence.VerificationPassed:
			v.Passed, v.Inconclusive = true, false
		case evidence.VerificationFailed:
			v.Passed, v.Inconclusive = false, false
		case "":
			// No host classification at all — a command the turn declared, or a
			// receipt replayed from an older session. The call's own outcome is
			// the only evidence there is.
			v.Passed, v.Inconclusive = r.Success, false
		default:
			// An unreadable status neither proves nor refutes, so it must not
			// overwrite what an earlier readable run of the same check settled.
			continue
		}
		v.Stale = i < lastMutation
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
		if v.Passed && !v.Stale && !v.Inconclusive {
			proven = true
		}
	}
	for _, v := range rep.Verifications {
		switch {
		case v.Inconclusive:
			// Same reason as the stale case below: a check whose verdict the
			// shell hid matters only while nothing fresh has proven the tree.
			if !proven {
				gaps = append(gaps, Gap{GapInconclusiveVerification, v.Command})
			}
		case !v.Passed:
			// A failure before the latest change is usually what the change was
			// for — test-first projects produce one every time. Once something
			// fresh has proven the tree, reporting it back reports the bug.
			if !v.Stale || !proven {
				gaps = append(gaps, Gap{GapFailedVerification, v.Command})
			}
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
		// The kind's own phrase says the absence in the reader's language; the
		// detail is only the command the host declined, never a sentence.
		gaps = append(gaps, Gap{Kind: GapUnverifiedChange, Detail: unreadCheckDetail(rep.UnreadCheck)})
	}
	for _, ch := range rep.Changes {
		// Same rule the gaps above follow: once something fresh has proven the
		// tree, a read-back on top of it is a habit, not proof — and a gap that
		// fires on correct work stops being read.
		if ch.Reviewed || proven {
			continue
		}
		gaps = append(gaps, Gap{GapUnreviewedChange, ch.Path})
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
	declared := c != nil && (slices.ContainsFunc(c.Requirements, func(r taskcontract.Requirement) bool { return r.Required }) || len(c.Checks) > 0)
	switch {
	// Nothing declared, nothing changed, nothing claimed. Checks may well have
	// run, but a check reports on the tree, not on what this turn delivered,
	// so no verdict but "no answer" has ground to stand on.
	case !declared && rep.Mutations == 0 && rep.Claimed.Empty():
		return VerdictUnknown
	// Blocked before Complete: a turn that established it cannot do the task as
	// specified can still leave every declared check satisfied, and reading only
	// Complete turns its honest exit into the same word success gets.
	case c != nil && (c.Blocked() || !c.Complete()):
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
	for _, kind := range []GapKind{GapUnbackedClaim, GapUnprovenCriterion, GapMissingCheck, GapFailedVerification, GapStaleVerification, GapInconclusiveVerification, GapUnverifiedChange, GapUnreviewedChange} {
		for _, gap := range r.Gaps {
			if gap.Kind == kind && !seen[kind] {
				seen[kind] = true
				out = append(out, kind.String())
			}
		}
	}
	return out
}

// unreadCheckLimit bounds the command a gap quotes back; the head of it is
// enough to recognize, and a whole heredoc in a summary row is noise.
const unreadCheckLimit = 80

// unreadCheckOf returns the last successful command the host ran and classified
// as something other than a check. It is only ever a candidate: the classifier
// is what decides, and this reports what it declined.
func unreadCheckOf(receipts []evidence.Receipt) string {
	for _, r := range slices.Backward(receipts) {
		if r.Success && strings.TrimSpace(r.Command) != "" && r.Verification == evidence.VerificationNotVerification {
			return strings.TrimSpace(r.Command)
		}
	}
	return ""
}

func unreadCheckDetail(command string) string {
	if command == "" {
		return ""
	}
	if len(command) > unreadCheckLimit {
		command = command[:unreadCheckLimit] + "…"
	}
	return command
}
