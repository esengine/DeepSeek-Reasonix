package delegation

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/tool"
	"reasonix/internal/platform/gitcmd"
	"reasonix/internal/runtime/isolation"
)

func schemaOffersIsolation(t *testing.T, tt *TaskTool) bool {
	t.Helper()
	var s struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(tt.Schema(), &s); err != nil {
		t.Fatalf("task schema is not JSON: %v", err)
	}
	return s.Properties["isolation"] != nil && s.Properties["prompt"] != nil
}

// The schema names isolation only in a session that offers it, and says the
// same bytes every time it is asked.
func TestTaskSchemaOffersIsolationOnlyWhenSet(t *testing.T) {
	tt := NewTaskToolWithOptions(TaskToolOptions{})
	if schemaOffersIsolation(t, tt) {
		t.Fatal("isolation offered without a runner")
	}
	tt.SetIsolation(isolation.NewStore(testenv.TempDir(t), testenv.TempDir(t), gitcmd.Repo{}), testenv.TempDir(t), func(context.Context, isolation.Run) (isolation.Outcome, error) {
		return isolation.Outcome{}, nil
	})
	if !schemaOffersIsolation(t, tt) {
		t.Fatal("isolation not offered once set")
	}
	if first, again := tt.Schema(), tt.Schema(); string(first) != string(again) {
		t.Fatal("the schema is not byte-stable")
	}
}

func refusalCode(err error) string {
	var r tool.Refusal
	if errors.As(err, &r) {
		return r.Code
	}
	return ""
}

// An isolated task refuses what it cannot honour instead of dropping it, and a
// session without isolation says so by code.
func TestIsolatedTaskRefusals(t *testing.T) {
	tt := NewTaskToolWithOptions(TaskToolOptions{})
	ctx := context.Background()
	_, err := tt.Execute(ctx, json.RawMessage(`{"prompt":"x","isolation":"worktree"}`))
	if refusalCode(err) != isolation.CodeUnavailable {
		t.Fatalf("no isolation: err = %v", err)
	}
	ran := false
	tt.SetIsolation(isolation.NewStore(testenv.TempDir(t), testenv.TempDir(t), gitcmd.Repo{}), testenv.TempDir(t), func(context.Context, isolation.Run) (isolation.Outcome, error) {
		ran = true
		return isolation.Outcome{}, nil
	})
	_, err = tt.Execute(ctx, json.RawMessage(`{"prompt":"x","isolation":"worktree","profile":"p","run_in_background":true}`))
	if refusalCode(err) != codeIsolationUnsupported || !strings.Contains(err.Error(), "profile, run_in_background") || ran {
		t.Fatalf("unsupported args: err = %v, ran = %v", err, ran)
	}
	_, err = tt.Execute(ctx, json.RawMessage(`{"prompt":"x","isolation":"copy"}`))
	if refusalCode(err) != codeIsolationUnsupported {
		t.Fatalf("unknown mode: err = %v", err)
	}
}
