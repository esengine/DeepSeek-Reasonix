package control

import (
	"context"
	"errors"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func manualReadinessController(t *testing.T, turns [][]provider.Chunk) (*Controller, *scriptedTurns) {
	t.Helper()
	reg := tool.NewRegistry()
	reg.Add(minimalFakeTool{name: "write_file"})
	reg.Add(minimalFakeTool{name: "read_file", readOnly: true})
	if todoWrite, ok := tool.LookupBuiltin("todo_write"); ok {
		reg.Add(todoWrite)
	}
	prov := &scriptedTurns{turns: turns}
	executor := agent.New(prov, reg, agent.NewSession("stable-system-prefix"), agent.Options{}, event.Discard)
	c := New(Options{Runner: executor, Executor: executor, Sink: event.Discard})
	t.Cleanup(c.Close)
	return c, prov
}

func syntheticUserTurnCount(messages []provider.Message) int {
	count := 0
	for _, message := range messages {
		if message.Role == provider.RoleUser && IsSyntheticUserMessage(message.Content) {
			count++
		}
	}
	return count
}

func TestStandardStopsAfterOneVisibleTurn(t *testing.T) {
	c, prov := manualReadinessController(t, [][]provider.Chunk{
		textTurn("已完成学习说明。下一步可以由你决定。"),
		textTurn("不应被隐藏续跑消费"),
	})
	if err := c.SetQualityFloor(QualityFloorStandard); err != nil {
		t.Fatalf("SetQualityFloor: %v", err)
	}

	if err := newTurnOrchestrator(c).runGoalLoopWithRawDisplay(context.Background(), "学习并解释当前内容", "学习并解释当前内容", ""); err != nil {
		t.Fatalf("standard turn returned %v", err)
	}
	if prov.call != 1 {
		t.Fatalf("provider calls = %d, want one visible turn", prov.call)
	}
	if got := syntheticUserTurnCount(c.executor.Session().Snapshot()); got != 0 {
		t.Fatalf("synthetic user turns = %d, want zero", got)
	}
}

func TestStandardStartReplyContinuesCurrentTodoInsideVisibleTurn(t *testing.T) {
	c, prov := manualReadinessController(t, [][]provider.Chunk{
		{toolCallChunk("todo-1", "todo_write", `{"todos":[{"content":"重写第 4 节","status":"in_progress"}]}`), {Type: provider.ChunkDone}},
		textTurn("让我先列出待办，接下来会重写。"),
		{
			toolCallChunk("write-1", "write_file", `{"path":"PRD.md"}`),
			toolCallChunk("todo-2", "todo_write", `{"todos":[{"content":"重写第 4 节","status":"completed"}]}`),
			{Type: provider.ChunkDone},
		},
		textTurn("第 4 节已经完成重写。"),
	})
	if err := c.SetQualityFloor(QualityFloorStandard); err != nil {
		t.Fatalf("SetQualityFloor: %v", err)
	}
	c.executor.Session().Add(provider.Message{Role: provider.RoleAssistant, Content: "让我写完整新第 4 节。"})

	if err := newTurnOrchestrator(c).runGoalLoopWithRawDisplay(context.Background(), "开始", "开始", "开始"); err != nil {
		t.Fatalf("standard start reply returned %v", err)
	}
	if prov.call != 2 {
		t.Fatalf("provider calls = %d, want a clean final with no ordinary todo continuation", prov.call)
	}
	if got := syntheticUserTurnCount(c.executor.Session().Snapshot()); got != 0 {
		t.Fatalf("synthetic user turns = %d, want none on the ordinary Agent path", got)
	}
}

func TestStandardStartWithoutConversationDoesNotArmTodoContinuation(t *testing.T) {
	c, prov := manualReadinessController(t, [][]provider.Chunk{
		{toolCallChunk("todo-1", "todo_write", `{"todos":[{"content":"重写第 4 节","status":"in_progress"}]}`), {Type: provider.ChunkDone}},
		textTurn("等待下一步。"),
		textTurn("不应被隐藏续跑消费"),
	})

	if err := newTurnOrchestrator(c).runGoalLoopWithRawDisplay(context.Background(), "开始", "开始", "开始"); err != nil {
		t.Fatalf("standard start reply returned %v", err)
	}
	if prov.call != 2 {
		t.Fatalf("provider calls = %d, want no continuation without prior context", prov.call)
	}
}

func TestStandardTodoContinuationYieldsToPendingUserWork(t *testing.T) {
	c, prov := manualReadinessController(t, [][]provider.Chunk{
		{toolCallChunk("todo-1", "todo_write", `{"todos":[{"content":"重写第 4 节","status":"in_progress"}]}`), {Type: provider.ChunkDone}},
		textTurn("等待下一步。"),
		textTurn("不应被隐藏续跑消费"),
	})
	c.executor.Session().Add(provider.Message{Role: provider.RoleAssistant, Content: "让我写完整新第 4 节。"})
	c.mu.Lock()
	c.canceling = true
	c.mu.Unlock()

	if err := newTurnOrchestrator(c).runGoalLoopWithRawDisplay(context.Background(), "开始", "开始", "开始"); err != nil {
		t.Fatalf("standard start reply returned %v", err)
	}
	if prov.call != 2 {
		t.Fatalf("provider calls = %d, want pending user work to preempt continuation", prov.call)
	}
}

func TestDeliveryStopsAtReadinessAndWaitsForExplicitRecovery(t *testing.T) {
	c, prov := manualReadinessController(t, [][]provider.Chunk{
		{toolCallChunk("write", "write_file", `{"path":"main.go"}`), {Type: provider.ChunkDone}},
		textTurn("已完成修改，但没有执行验证。"),
		textTurn("显式恢复才允许消费这一轮"),
	})
	if err := c.SetQualityFloor(QualityFloorDelivery); err != nil {
		t.Fatalf("SetQualityFloor: %v", err)
	}

	err := newTurnOrchestrator(c).runGoalLoopWithRawDisplay(context.Background(), "修改 main.go", "修改 main.go", "")
	var readinessErr *agent.FinalReadinessError
	if !errors.As(err, &readinessErr) {
		t.Fatalf("run error = %v, want FinalReadinessError", err)
	}
	if readinessErr.Attempts != 1 {
		t.Fatalf("readiness attempts = %d, want one completed visible turn", readinessErr.Attempts)
	}
	if prov.call != 2 {
		t.Fatalf("provider calls = %d, want one tool round plus one final answer", prov.call)
	}
	if got := syntheticUserTurnCount(c.executor.Session().Snapshot()); got != 0 {
		t.Fatalf("synthetic user turns = %d, want zero before explicit recovery", got)
	}
	if !c.executor.PrepareFinalReadinessRecovery() {
		t.Fatal("readiness failure did not preserve explicit recovery state")
	}
}
