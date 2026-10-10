package delegation

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"reasonix/internal/contract/tool"
)

func TestTaskToolPropagatesTargetAccessCheck(t *testing.T) {
	denied := tool.Refusal{Code: "workspace.read_forbidden", Message: "Cannot read target file: permission denied."}
	check := func(context.Context, tool.Tool, json.RawMessage) error { return denied }
	task := NewTaskToolWithOptions(TaskToolOptions{CheckTargetAccess: check})
	opts := task.subagentOptions(context.Background(), 0, nil, 0, 1, "", nil)
	if opts.CheckTargetAccess == nil {
		t.Fatal("child lost the mandatory target access policy")
	}
	if err := opts.CheckTargetAccess(context.Background(), nil, nil); !errors.Is(err, denied) {
		t.Fatalf("child has a different policy: %v", err)
	}
}
