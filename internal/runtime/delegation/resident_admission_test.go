package delegation

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/writeclaim"
	"reasonix/internal/state/mutation"
)

func TestResidentRefusalPrecedesEnvironmentAndBackgroundCreation(t *testing.T) {
	root := t.TempDir()
	c, err := mutation.New(root, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	s := writeclaim.NewSubagentScheduler(1, 1)
	for {
		release, err := s.ReserveResident()
		if err != nil {
			var limit *writeclaim.ResidentLimitError
			if !errors.As(err, &limit) {
				t.Fatal(err)
			}
			break
		}
		defer release()
	}
	task := NewTaskToolWithOptions(TaskToolOptions{ParentRegistry: tool.NewRegistry(), SysPrompt: "sys"}).
		WithTranscripts(NewSubagentStore(t.TempDir()), root, "model", "").WithScheduler(s).WithMutationCoordinator(c)
	task.WithEnvironmentFactory("shared", func(context.Context, ProfileExecSpec, *EnvironmentSource) (*ExecutionEnvironment, error) {
		t.Fatal("resident refusal created an environment")
		return nil, nil
	})
	_, err = task.Execute(testTaskContext(), json.RawMessage(`{"prompt":"work","isolation":"worktree","run_in_background":true}`))
	var limit *writeclaim.ResidentLimitError
	if !errors.As(err, &limit) {
		t.Fatalf("resident refusal did not precede provider/job preparation: %v", err)
	}
}
