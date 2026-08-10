package evidence

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// CompletionStatus is a sub-agent's claim about its own run. The host may lower
// it to what receipts support; nothing may raise it.
type CompletionStatus string

const (
	CompletionComplete CompletionStatus = "complete"
	CompletionPartial  CompletionStatus = "partial"
	CompletionBlocked  CompletionStatus = "blocked"
	CompletionFailed   CompletionStatus = "failed"
)

// CriterionStatus is the child's claim about one acceptance criterion.
type CriterionStatus string

const (
	CriterionSatisfied   CriterionStatus = "satisfied"
	CriterionUnsatisfied CriterionStatus = "unsatisfied"
)

// Completion evidence kinds mirror complete_step so a sub-agent and the root
// agent prove work in the same vocabulary.
const (
	CompletionEvidenceVerification = "verification"
	CompletionEvidenceReview       = "review"
	CompletionEvidenceDiff         = "diff"
	CompletionEvidenceFiles        = "files"
	CompletionEvidenceManual       = "manual"
)

// CompletionEvidence is one proof attached to an acceptance criterion.
type CompletionEvidence struct {
	Kind    string   `json:"kind"`
	Summary string   `json:"summary"`
	Command string   `json:"command,omitempty"`
	Paths   []string `json:"paths,omitempty"`
}

// AcceptanceCriterion is one checkable condition the sub-task had to meet.
type AcceptanceCriterion struct {
	ID       string               `json:"id"`
	Status   CriterionStatus      `json:"status"`
	Evidence []CompletionEvidence `json:"evidence,omitempty"`
}

// CompletionReport is the structured payload submitted via complete_subtask.
type CompletionReport struct {
	Status     CompletionStatus      `json:"status"`
	Summary    string                `json:"summary"`
	Criteria   []AcceptanceCriterion `json:"acceptance_criteria,omitempty"`
	Unresolved []string              `json:"unresolved,omitempty"`
}

// ParseCompletionReport validates and normalizes a complete_subtask argument
// object. It checks shape only: whether the claims are true is the host's job.
func ParseCompletionReport(raw json.RawMessage) (CompletionReport, error) {
	var r CompletionReport
	if err := json.Unmarshal(raw, &r); err != nil {
		return CompletionReport{}, fmt.Errorf("invalid complete_subtask JSON: %w", err)
	}
	r.Status = CompletionStatus(strings.ToLower(strings.TrimSpace(string(r.Status))))
	switch r.Status {
	case CompletionComplete, CompletionPartial, CompletionBlocked, CompletionFailed:
	default:
		return CompletionReport{}, fmt.Errorf("complete_subtask.status must be complete, partial, blocked, or failed")
	}
	r.Summary = strings.TrimSpace(r.Summary)
	if r.Summary == "" {
		return CompletionReport{}, fmt.Errorf("complete_subtask.summary is required")
	}
	for i := range r.Criteria {
		c := &r.Criteria[i]
		c.ID = strings.TrimSpace(c.ID)
		if c.ID == "" {
			return CompletionReport{}, fmt.Errorf("acceptance_criteria[%d].id is required", i)
		}
		c.Status = CriterionStatus(strings.ToLower(strings.TrimSpace(string(c.Status))))
		switch c.Status {
		case CriterionSatisfied, CriterionUnsatisfied:
		default:
			return CompletionReport{}, fmt.Errorf("acceptance_criteria[%d].status must be satisfied or unsatisfied", i)
		}
		for j := range c.Evidence {
			e := &c.Evidence[j]
			e.Kind = strings.ToLower(strings.TrimSpace(e.Kind))
			switch e.Kind {
			case CompletionEvidenceVerification:
				if strings.TrimSpace(e.Command) == "" {
					return CompletionReport{}, fmt.Errorf("acceptance_criteria[%d].evidence[%d]: verification requires command", i, j)
				}
			case CompletionEvidenceDiff, CompletionEvidenceFiles:
				if len(e.Paths) == 0 {
					return CompletionReport{}, fmt.Errorf("acceptance_criteria[%d].evidence[%d]: %s requires paths", i, j, e.Kind)
				}
			case CompletionEvidenceReview, CompletionEvidenceManual:
			default:
				return CompletionReport{}, fmt.Errorf("acceptance_criteria[%d].evidence[%d].kind is not a known evidence kind", i, j)
			}
		}
	}
	return r, nil
}

// LatestCompletionReport returns the most recent successful complete_subtask
// payload recorded this run.
func (l *Ledger) LatestCompletionReport() (CompletionReport, bool) {
	if l == nil {
		return CompletionReport{}, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, r := range slices.Backward(l.receipts) {
		if r.ToolName != "complete_subtask" || !r.Success {
			continue
		}
		report, err := ParseCompletionReport(r.Args)
		if err != nil {
			continue
		}
		return report, true
	}
	return CompletionReport{}, false
}

// AdjudicateCompletion lowers a report to what the host's own receipts support.
// A criterion claimed satisfied whose evidence no receipt backs is downgraded,
// and a report holding any downgraded criterion cannot stay "complete". The
// returned reasons name every downgrade so the parent sees why.
func (l *Ledger) AdjudicateCompletion(r CompletionReport) (CompletionReport, []string) {
	if l == nil {
		return r, nil
	}
	var reasons []string
	downgraded := false
	for i := range r.Criteria {
		c := &r.Criteria[i]
		if c.Status != CriterionSatisfied {
			continue
		}
		if backed, why := l.criterionIsBacked(*c); !backed {
			c.Status = CriterionUnsatisfied
			downgraded = true
			reasons = append(reasons, c.ID+": "+why)
		}
	}
	if downgraded && r.Status == CompletionComplete {
		r.Status = CompletionPartial
	}
	return r, reasons
}

// criterionIsBacked reports whether at least one attached proof is one the host
// itself observed. A manual claim is never host-backed on its own.
func (l *Ledger) criterionIsBacked(c AcceptanceCriterion) (bool, string) {
	if len(c.Evidence) == 0 {
		return false, "claimed satisfied with no evidence"
	}
	for _, e := range c.Evidence {
		switch e.Kind {
		case CompletionEvidenceVerification:
			if l.HasSuccessfulCommand(e.Command) {
				return true, ""
			}
		case CompletionEvidenceDiff:
			if l.HasSuccessfulWrite(e.Paths) {
				return true, ""
			}
		case CompletionEvidenceFiles:
			if l.HasSuccessfulReadOrWrite(e.Paths) {
				return true, ""
			}
		case CompletionEvidenceReview:
			if l.HasCompletedReview() {
				return true, ""
			}
		}
	}
	return false, "no host receipt backs the cited evidence"
}
