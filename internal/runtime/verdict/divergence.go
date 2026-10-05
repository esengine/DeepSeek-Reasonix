package verdict

import (
	"slices"

	"reasonix/internal/runtime/completion"
	"reasonix/internal/runtime/taskcontract"
)

// Divergence says how the host-evidence outcome differs from the completion
// report's verdict for the same turn, and why. The classes are what decides
// which old path an accepted contract has to remove.
type Divergence struct {
	Old     string   `json:"old"`
	New     Outcome  `json:"new"`
	Class   string   `json:"class"`
	Reasons []string `json:"reasons,omitempty"`
}

// Divergence classes.
const (
	ClassAgree       = "agree"
	ClassNewStricter = "new_stricter"
	ClassNewLooser   = "new_looser"
	// ClassOldSilent: the report gave no verdict — nothing declared, changed
	// or claimed — while the outcome still owes, fails or blocks.
	ClassOldSilent = "old_silent"
)

// Reasons beyond the obligation causes.
const (
	// ReasonClaimOnly: the report counted a criterion satisfied that no host
	// evidence satisfies — a todo marked completed, or a citation the model
	// bound to it.
	ReasonClaimOnly = "claim_only"
	// ReasonOldGaps: the report stopped short on gaps the outcome does not owe.
	ReasonOldGaps = "old_gaps"
	// ReasonOldCriteria: the report counted a criterion unsatisfied.
	ReasonOldCriteria = "old_criteria"
	// ReasonBlocked: the host checked the model's conclusion that the task
	// cannot be done as specified.
	ReasonBlocked = "blocked"
)

// Classify compares res with the report's verdict. The criteria's recorded
// statuses are read here and only here: they are what the report believed,
// which is exactly what the comparison is about.
func Classify(old completion.Verdict, c *taskcontract.Contract, res Result) Divergence {
	d := Divergence{Old: old.String(), New: res.Outcome}
	oldCompleted := old == completion.VerdictDone
	newCompleted := res.Outcome == Completed
	switch {
	case old == completion.VerdictUnknown && (newCompleted || res.Outcome == NoOutcome):
		d.Class = ClassAgree
	case old == completion.VerdictUnknown:
		d.Class = ClassOldSilent
		d.Reasons = stricterReasons(c, res)
	case oldCompleted == newCompleted:
		d.Class = ClassAgree
	case oldCompleted:
		d.Class = ClassNewStricter
		d.Reasons = stricterReasons(c, res)
	default:
		d.Class = ClassNewLooser
		d.Reasons = looserReasons(old, c)
	}
	return d
}

func stricterReasons(c *taskcontract.Contract, res Result) []string {
	var reasons []string
	add := func(r string) {
		if r != "" && !slices.Contains(reasons, r) {
			reasons = append(reasons, r)
		}
	}
	if res.Outcome == Blocked {
		add(ReasonBlocked)
	}
	claimed := map[string]bool{}
	if c != nil {
		for _, req := range c.Requirements {
			if req.Status == taskcontract.Satisfied {
				claimed["criterion@"+req.ID] = true
			}
		}
	}
	for _, o := range res.Obligations {
		if !o.Required || o.Verdict == Satisfied {
			continue
		}
		if claimed[o.ID] {
			add(ReasonClaimOnly)
		}
		if o.Cause == CauseHostObligation || o.Cause == CauseContractOwed {
			add(o.Source)
			continue
		}
		add(o.Cause)
	}
	slices.Sort(reasons)
	return reasons
}

func looserReasons(old completion.Verdict, c *taskcontract.Contract) []string {
	var reasons []string
	if old == completion.VerdictPartial {
		reasons = append(reasons, ReasonOldGaps)
	}
	if c != nil && slices.ContainsFunc(c.Requirements, func(r taskcontract.Requirement) bool {
		return r.Required && r.Status != taskcontract.Satisfied
	}) {
		reasons = append(reasons, ReasonOldCriteria)
	}
	return reasons
}
