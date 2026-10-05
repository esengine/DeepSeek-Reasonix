package agent

import (
	"context"
	"fmt"
	"reasonix/internal/state/sessionstore"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent/testutil"
)

func TestGoalRunHasNoDefaultModelRoundCeiling(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "read_file", readOnly: true})
	turns := make([]testutil.Turn, 0, 102)
	for i := range 101 {
		turns = append(turns, testutil.Turn{ToolCalls: []provider.ToolCall{{
			ID: fmt.Sprintf("r%d", i), Name: "read_file", Arguments: fmt.Sprintf(`{"path":"file-%d"}`, i),
		}}})
	}
	turns = append(turns, testutil.Turn{Text: "Done."})
	prov := testutil.NewMock("m", turns...)
	a := New(prov, reg, sessionstore.NewSession(""), Options{}, event.Discard)
	ctx := WithDeliveryExecutionScope(context.Background(), DeliveryExecutionScope{ID: "goal-1", TaskText: "work"})
	if err := a.Run(ctx, "work"); err != nil {
		t.Fatalf("Goal run stopped at a default round boundary: %v", err)
	}
	if prov.CallCount() != 102 {
		t.Fatalf("provider calls = %d, want 101 tool rounds plus final", prov.CallCount())
	}
}
