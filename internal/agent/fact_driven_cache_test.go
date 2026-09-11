package agent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func TestFactDrivenModesShareExecutorSchemaBytes(t *testing.T) {
	prov := &mockProvider{name: "p", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "ok"},
		{Type: provider.ChunkDone},
	}}
	reg := tool.NewRegistry()
	reg.Add(fakeReadFileTool{})
	reg.Add(fakeWriterTool{})
	a := New(prov, reg, NewSession("STABLE-SYS"), Options{}, event.Discard)

	capture := func(ctx context.Context, input string) (system, schemas string) {
		t.Helper()
		prov.chunks = []provider.Chunk{{Type: provider.ChunkText, Text: "ok"}, {Type: provider.ChunkDone}}
		err := a.Run(ctx, input)
		if err != nil {
			var readiness *FinalReadinessError
			if !errors.As(err, &readiness) {
				t.Fatalf("Run(%q): %v", input, err)
			}
		}
		if len(prov.lastReq.Messages) == 0 || prov.lastReq.Messages[0].Role != provider.RoleSystem {
			t.Fatalf("missing system prefix for %q", input)
		}
		return prov.lastReq.Messages[0].Content, serializeToolSchemas(t, prov.lastReq.Tools)
	}

	ordinarySys, ordinaryTools := capture(context.Background(), "解释 OAuth token")

	a.SetPlanMode(true)
	planSys, planTools := capture(context.Background(), "先规划这个认证迁移，不要执行")
	a.SetPlanMode(false)

	goalCtx := WithDeliveryExecutionScope(context.Background(), DeliveryExecutionScope{
		ID: "goal-cache", TaskText: "ship the parser",
	})
	goalSys, goalTools := capture(goalCtx, "ship the parser")

	a.task.ledger.Record(evidence.Receipt{
		ToolName: "edit_file", Success: true, Write: true, Mutation: true,
		Args: json.RawMessage(`{"path":"internal/agent/agent.go"}`), Paths: []string{"internal/agent/agent.go"},
	})
	afterSys, afterTools := capture(goalCtx, "continue")

	if ordinarySys != planSys || ordinarySys != goalSys || ordinarySys != afterSys {
		t.Fatalf("executor system prefix drifted:\nordinary=%q\nplan=%q\ngoal=%q\nafter=%q",
			ordinarySys, planSys, goalSys, afterSys)
	}
	if ordinaryTools != planTools || ordinaryTools != goalTools || ordinaryTools != afterTools {
		t.Fatalf("tool schemas drifted across ordinary/plan/goal/obligation")
	}
}
