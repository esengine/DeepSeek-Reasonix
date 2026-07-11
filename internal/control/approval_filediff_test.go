package control

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"reasonix/internal/diff"
	"reasonix/internal/event"
	"reasonix/internal/tool"
)

// fakePreviewTool 实现 tool.Tool + tool.Previewer，用于测试 ApprovalRequest
// 是否携带 Preview 返回的 fileDiff。它不触碰磁盘，让测试聚焦于传递逻辑。
type fakePreviewTool struct {
	name   string
	change diff.Change
}

func (f fakePreviewTool) Name() string            { return f.name }
func (f fakePreviewTool) Description() string     { return "" }
func (f fakePreviewTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (f fakePreviewTool) ReadOnly() bool          { return false }
func (f fakePreviewTool) Execute(context.Context, json.RawMessage) (string, error) {
	return f.name + " done", nil
}
func (f fakePreviewTool) Preview(json.RawMessage) (diff.Change, error) {
	return f.change, nil
}

// TestApprovalRequestCarriesRenameFileDiff 锁定：当审批的工具实现 Previewer 且
// 返回 Kind=Rename 的 change 时，ApprovalRequest 事件必须携带 kind/src_path/
// dst_path 字段，让前端审批弹窗能渲染 "src → dst" 路径变更预览。
func TestApprovalRequestCarriesRenameFileDiff(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakePreviewTool{
		name:   "move_file",
		change: diff.BuildRename("/workspace/a.txt", "/workspace/b.txt"),
	})

	approvals := make(chan event.Approval, 8)
	c := New(Options{
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.ApprovalRequest {
				approvals <- e.Approval
			}
		}),
		Registry: reg,
	})

	var captured event.Approval
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		captured = <-approvals
		c.Approve(captured.ID, true, false, false)
	}()

	args := json.RawMessage(`{"source_path":"/workspace/a.txt","destination_path":"/workspace/b.txt"}`)
	allow, _, err := gateApprover{c}.Approve(context.Background(), "move_file", "a.txt -> b.txt", args)
	if err != nil || !allow {
		t.Fatalf("Approve: allow=%v err=%v", allow, err)
	}
	wg.Wait()

	if captured.Kind != string(diff.Rename) {
		t.Errorf("Approval.Kind = %q, want %q", captured.Kind, diff.Rename)
	}
	if captured.SrcPath != "/workspace/a.txt" {
		t.Errorf("Approval.SrcPath = %q, want /workspace/a.txt", captured.SrcPath)
	}
	if captured.DstPath != "/workspace/b.txt" {
		t.Errorf("Approval.DstPath = %q, want /workspace/b.txt", captured.DstPath)
	}
}

// TestApprovalRequestNoFileDiffForNonPreviewer 确认非 Previewer 工具的审批
// 不携带 fileDiff（保持现状，omitempty 字段为零值）。
func TestApprovalRequestNoFileDiffForNonPreviewer(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeNonPreviewerTool{name: "bash"})

	approvals := make(chan event.Approval, 8)
	c := New(Options{
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.ApprovalRequest {
				approvals <- e.Approval
			}
		}),
		Registry: reg,
	})

	var captured event.Approval
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		captured = <-approvals
		c.Approve(captured.ID, true, false, false)
	}()

	allow, _, err := gateApprover{c}.Approve(context.Background(), "bash", "go test", nil)
	if err != nil || !allow {
		t.Fatalf("Approve: allow=%v err=%v", allow, err)
	}
	wg.Wait()

	if captured.Kind != "" {
		t.Errorf("Approval.Kind = %q, want empty for non-Previewer", captured.Kind)
	}
}

// fakeNonPreviewerTool 是不实现 Previewer 的工具，用于对照组。
type fakeNonPreviewerTool struct {
	name string
}

func (f fakeNonPreviewerTool) Name() string            { return f.name }
func (f fakeNonPreviewerTool) Description() string     { return "" }
func (f fakeNonPreviewerTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (f fakeNonPreviewerTool) ReadOnly() bool          { return false }
func (f fakeNonPreviewerTool) Execute(context.Context, json.RawMessage) (string, error) {
	return f.name + " done", nil
}
