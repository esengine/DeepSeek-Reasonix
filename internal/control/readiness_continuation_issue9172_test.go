package control

import (
	"context"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

func TestIssue9172StandardTaskSurvivesPromisesAndMultipleFiles(t *testing.T) {
	c, prov := readinessContinuationController(t, [][]provider.Chunk{
		textTurn("先读 login.php。"),
		textTurn("立即继续执行。"),
		{toolCallChunk("read-login", "read_file", `{"path":"login.php"}`), {Type: provider.ChunkDone}},
		textTurn("我从读 login.php 开始，下一步修改 login.php。"),
		{toolCallChunk("todo-open", "todo_write", `{"todos":[{"content":"修改 login.php","status":"in_progress"},{"content":"修改 functions.php","status":"pending"}]}`), {Type: provider.ChunkDone}},
		textTurn("待办已建立，下一步修改 login.php。"),
		{toolCallChunk("write-login", "write_file", `{"path":"login.php","content":"updated login"}`), {Type: provider.ChunkDone}},
		textTurn("login.php 已修改，下一步修改 functions.php。"),
		{toolCallChunk("write-functions", "write_file", `{"path":"functions.php","content":"updated functions"}`), {Type: provider.ChunkDone}},
		{toolCallChunk("todo-done", "todo_write", `{"todos":[{"content":"修改 login.php","status":"completed"},{"content":"修改 functions.php","status":"completed"}]}`), {Type: provider.ChunkDone}},
		textTurn("已完成 login.php 和 functions.php 的修改。"),
	}, event.Discard)
	if err := c.SetQualityFloor(QualityFloorStandard); err != nil {
		t.Fatalf("SetQualityFloor: %v", err)
	}

	err := newTurnOrchestrator(c).runGoalLoopWithRawDisplay(
		context.Background(),
		"修改 login.php 和 functions.php 的按键位置",
		"修改 login.php 和 functions.php 的按键位置",
		"",
	)
	if err != nil {
		t.Fatalf("Issue #9172 scenario required another user continuation: %v", err)
	}
	if got := readinessSyntheticTurns(c); got != 5 {
		t.Fatalf("synthetic readiness turns = %d, want 5", got)
	}
	if prov.call != 11 {
		t.Fatalf("provider calls = %d, want 11 scripted calls", prov.call)
	}
	writes := map[string]bool{}
	for _, message := range c.executor.Session().Snapshot() {
		if message.Role == provider.RoleTool && message.Content == "write_file done" {
			writes[message.ToolCallID] = true
		}
	}
	if !writes["write-login"] || !writes["write-functions"] {
		t.Fatalf("successful write results = %v, want both PHP mutations", writes)
	}
	if c.executor.PrepareFinalReadinessRecovery() {
		t.Fatal("completed multi-file task left a recovery card pending")
	}
}

func TestStandardDeferredActionAfterMutationContinuesWithoutTodo(t *testing.T) {
	c, prov := readinessContinuationController(t, [][]provider.Chunk{
		{toolCallChunk("write-one", "write_file", `{"path":"main.go","content":"package main"}`), {Type: provider.ChunkDone}},
		textTurn("main.go 已完成第一处修改，下一步继续修改 main.go。"),
		{toolCallChunk("write-two", "write_file", `{"path":"main.go","content":"package main\n\nfunc main() {}"}`), {Type: provider.ChunkDone}},
		textTurn("已完成 main.go 的全部修改。"),
	}, event.Discard)
	if err := c.SetQualityFloor(QualityFloorStandard); err != nil {
		t.Fatalf("SetQualityFloor: %v", err)
	}

	if err := newTurnOrchestrator(c).runGoalLoopWithRawDisplay(context.Background(), "修改 main.go", "修改 main.go", ""); err != nil {
		t.Fatalf("deferred action required a user continuation: %v", err)
	}
	if got := readinessSyntheticTurns(c); got != 1 {
		t.Fatalf("synthetic readiness turns = %d, want 1", got)
	}
	if prov.call != 4 {
		t.Fatalf("provider calls = %d, want 4", prov.call)
	}
}
