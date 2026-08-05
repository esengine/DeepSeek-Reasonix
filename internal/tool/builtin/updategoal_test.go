package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"reasonix/internal/tool"
)

// recordingRecorder is a minimal GoalTurnRecorder for tool-level tests.
type recordingRecorder struct {
	reports []tool.GoalReport
	err     error
}

func (r *recordingRecorder) RecordGoalReport(report tool.GoalReport) (string, error) {
	if r.err != nil {
		return "", r.err
	}
	r.reports = append(r.reports, report)
	return "update_goal: " + report.Status + " recorded for this turn.", nil
}

func goalTool(t *testing.T) (updateGoal, *recordingRecorder, context.Context) {
	t.Helper()
	rec := &recordingRecorder{}
	ctx := tool.WithGoalTurnRecorder(context.Background(), rec)
	return updateGoal{}, rec, ctx
}

func TestUpdateGoalValidatesStatusAndReason(t *testing.T) {
	toolFn, rec, ctx := goalTool(t)

	cases := []struct {
		name    string
		args    string
		wantErr string
	}{
		{"continue without reason rejected", `{"status":"continue"}`, "reason is required"},
		{"blocked without reason rejected", `{"status":"blocked"}`, "reason is required"},
		{"complete without reason accepted", `{"status":"complete"}`, ""},
		{"unknown status rejected", `{"status":"sideways"}`, "status must be one of"},
		{"empty status rejected", `{}`, "status must be one of"},
		{"invalid json rejected", `{not json`, "invalid update_goal args"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := len(rec.reports)
			_, err := toolFn.Execute(ctx, json.RawMessage(tc.args))
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("Execute() error = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Execute() error = %v, want it to mention %q", err, tc.wantErr)
			}
			if len(rec.reports) != before {
				t.Fatalf("rejected call still recorded a report: %+v", rec.reports)
			}
		})
	}
}

func TestUpdateGoalFailsClosedOutsideActiveGoalTurn(t *testing.T) {
	toolFn := updateGoal{}
	// No recorder in the context — an ordinary chat turn.
	_, err := toolFn.Execute(context.Background(), json.RawMessage(`{"status":"continue","reason":"working"}`))
	if err == nil || !strings.Contains(err.Error(), "only available while an active goal turn") {
		t.Fatalf("Execute() error = %v, want structured out-of-context error", err)
	}
}

func TestUpdateGoalRecordsReport(t *testing.T) {
	toolFn, rec, ctx := goalTool(t)
	_, err := toolFn.Execute(ctx, json.RawMessage(`{"status":"continue","reason":"fixing the parser","next_action":"run tests"}`))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(rec.reports) != 1 {
		t.Fatalf("reports = %+v, want 1", rec.reports)
	}
	got := rec.reports[0]
	if got.Status != "continue" || got.Reason != "fixing the parser" || got.NextAction != "run tests" {
		t.Fatalf("report = %+v", got)
	}
}

func TestUpdateGoalRecorderErrorsPropagate(t *testing.T) {
	toolFn, rec, ctx := goalTool(t)
	rec.err = errors.New("conflicting reports this turn")
	_, err := toolFn.Execute(ctx, json.RawMessage(`{"status":"complete"}`))
	if err == nil || !strings.Contains(err.Error(), "conflicting reports") {
		t.Fatalf("Execute() error = %v, want recorder error propagated", err)
	}
}
