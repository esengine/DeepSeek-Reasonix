package flywheel

import "strings"

// Judge scores a completed trajectory (docs/DATA_FLYWHEEL.md §2.4).
// Implementations are free — a heuristic default ships here; an LLM judge
// (Agent-as-a-Judge) can be plugged in by callers.
type Judge interface {
	// Score returns the quality label for a trajectory.
	Score(t *Trajectory) Label
}

// HeuristicJudge scores trajectories without an LLM:
//   - verification green + few failed tool calls → good/excellent
//   - verification green with stumbles → partial
//   - verification missing or red → failed
//
// It is the default until an LLM judge is wired (keep the heuristic cheap).
type HeuristicJudge struct{}

// Score implements Judge.
func (HeuristicJudge) Score(t *Trajectory) Label {
	failSteps := 0
	toolCalls := 0
	for _, s := range t.Steps {
		switch s.Kind {
		case "tool_call", "tool_result":
			toolCalls++
		}
		if !s.OK {
			failSteps++
		}
	}
	if t.Verify == nil || !t.Verify.OK {
		return Label{Score: 0.2, Name: "failed",
			Reason: "verification missing or failing (verify=" + verifyText(t) + ")"}
	}
	if failSteps > 0 && failSteps*3 >= toolCalls {
		return Label{Score: 0.55, Name: "partial",
			Reason: "verification green but many failed tool calls"}
	}
	if toolCalls <= 1 {
		return Label{Score: 0.8, Name: "good", Reason: "single-step, verification green"}
	}
	return Label{Score: 0.9, Name: "excellent", Reason: "verification green, no stumbles"}
}

func verifyText(t *Trajectory) string {
	if t.Verify == nil {
		return "none"
	}
	b := strings.Builder{}
	b.WriteString(t.Verify.Kind)
	if !t.Verify.OK {
		b.WriteString(":red")
	} else {
		b.WriteString(":green")
	}
	if t.Verify.Detail != "" {
		b.WriteString(" " + t.Verify.Detail)
	}
	return b.String()
}

// JudgeTrajectory labels and (if failed) persists the failure lesson.
// Returns the label so callers can log it.
func (s *Store) JudgeTrajectory(t *Trajectory, j Judge) (Label, error) {
	if j == nil {
		j = HeuristicJudge{}
	}
	l := j.Score(t)
	t.Judge = &l
	if err := s.SaveTrajectory(t); err != nil {
		return l, err
	}
	if l.Name == "failed" {
		if err := s.SaveFailure(t); err != nil {
			return l, err
		}
	}
	return l, nil
}
