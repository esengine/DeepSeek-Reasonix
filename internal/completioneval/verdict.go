package completioneval

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// MaxReasonBytes caps a parsed reason before it is handed back to the host.
const MaxReasonBytes = 500

// ErrInvalidOutput marks every verdict-parse failure (empty, non-JSON, bad
// enum, missing reason) so hosts can classify validator failures without
// matching error strings.
var ErrInvalidOutput = errors.New("invalid completion evaluator output")

// Outcome is the validator's structured verdict disposition.
type Outcome string

const (
	OutcomeComplete  Outcome = "complete"
	OutcomeContinue  Outcome = "continue"
	OutcomeNeedsUser Outcome = "needs_user"
	OutcomeBlocked   Outcome = "blocked"
	OutcomeUncertain Outcome = "uncertain"
)

// Valid reports whether o is one of the five defined outcomes.
func (o Outcome) Valid() bool {
	switch o {
	case OutcomeComplete, OutcomeContinue, OutcomeNeedsUser, OutcomeBlocked, OutcomeUncertain:
		return true
	}
	return false
}

// Verdict is the parsed validator response.
type Verdict struct {
	Outcome Outcome `json:"outcome"`
	Reason  string  `json:"reason"`
}

// parseVerdict extracts the JSON object from the model's response (tolerating
// fences or prose wrappers) and validates the outcome enum. Every outcome
// except complete requires a non-empty reason; the reason is never parsed for
// keywords — it is host-side diagnostics only.
func parseVerdict(text string) (Verdict, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return Verdict{}, fmt.Errorf("%w: empty response", ErrInvalidOutput)
	}
	if i := strings.Index(text, "{"); i >= 0 {
		if j := strings.LastIndex(text, "}"); j > i {
			text = text[i : j+1]
		}
	}
	var v Verdict
	if err := json.Unmarshal([]byte(text), &v); err != nil {
		return Verdict{}, fmt.Errorf("%w: %w", ErrInvalidOutput, err)
	}
	if !v.Outcome.Valid() {
		return Verdict{}, fmt.Errorf("%w: invalid outcome %q", ErrInvalidOutput, v.Outcome)
	}
	if v.Outcome != OutcomeComplete && strings.TrimSpace(v.Reason) == "" {
		return Verdict{}, fmt.Errorf("%w: reason omitted for outcome %q", ErrInvalidOutput, v.Outcome)
	}
	if strings.TrimSpace(v.Reason) != "" {
		v.Reason = clip(v.Reason, MaxReasonBytes)
	}
	return v, nil
}
