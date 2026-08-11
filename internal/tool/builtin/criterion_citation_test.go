package builtin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/evidence"
)

func criterionCitationArgs(t *testing.T, criterionID string) json.RawMessage {
	t.Helper()
	args, err := json.Marshal(map[string]any{
		"step":   "fix it",
		"result": "the race is gone",
		"evidence": []map[string]any{
			{"kind": "manual", "summary": "walked the retry path", "criterion_id": criterionID},
		},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return args
}

// A citation the plan does not contain would resolve into nothing and only
// surface much later, as a completion refused for a reason the model never
// connected to this call. Reject it here, with the ids it may cite.
func TestCompleteStepRejectsAnUnknownCriterionCitation(t *testing.T) {
	ctx := evidence.WithAcceptanceCriteria(context.Background(), []string{"c1", "c2"})
	_, err := (completeStep{}).Execute(ctx, criterionCitationArgs(t, "c9"))
	if err == nil {
		t.Fatal("an unknown criterion_id must be rejected")
	}
	for _, want := range []string{"c9", "c1, c2"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should name %q", err, want)
		}
	}
}

func TestCompleteStepAcceptsAKnownCriterionCitation(t *testing.T) {
	ctx := evidence.WithAcceptanceCriteria(context.Background(), []string{"c1", "c2"})
	if _, err := (completeStep{}).Execute(ctx, criterionCitationArgs(t, "c2")); err != nil {
		t.Fatalf("a criterion the plan contains must be accepted: %v", err)
	}
}

// An unplanned turn carries no criteria, so citation checking must stand down
// rather than reject every proof.
func TestCompleteStepSkipsCitationCheckWithoutAPlan(t *testing.T) {
	if _, err := (completeStep{}).Execute(context.Background(), criterionCitationArgs(t, "c1")); err != nil {
		t.Fatalf("without an approved plan the citation must not be checked: %v", err)
	}
}
