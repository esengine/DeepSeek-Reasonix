package agent

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"reasonix/internal/diff"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// fakePreviewTool 是一个实现 tool.Previewer 的非只读工具，用于测试
// executeBatch 是否把 Preview 返回的 change 字段正确传递到 ToolDispatch。
// 它不触碰磁盘，让测试聚焦于传递逻辑而非文件系统副作用。
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

// TestExecuteBatchRenameDispatch 锁定：当 Previewer 工具返回 Kind=Rename 的
// change 时，executeBatch emit 的 ToolDispatch 必须携带 kind/src_path/dst_path
// 字段，这样前端才能渲染 "src → dst" 卡片。rename 的 Diff/Added/Removed 全空，
// 若不传递 Kind，前端 fileDiffFromWire 会把它当作无 diff 丢弃。
func TestExecuteBatchRenameDispatch(t *testing.T) {
	src, dst := "/workspace/a.txt", "/workspace/b.txt"
	prov := &mockProvider{name: "p", chunks: []provider.Chunk{
		{Type: provider.ChunkToolCallStart, ToolCall: &provider.ToolCall{ID: "c1", Name: "move_file"}},
		{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{
			ID:        "c1",
			Name:      "move_file",
			Arguments: `{"source_path":"` + src + `","destination_path":"` + dst + `"}`,
		}},
		{Type: provider.ChunkDone},
	}}
	reg := tool.NewRegistry()
	reg.Add(fakePreviewTool{
		name:   "move_file",
		change: diff.BuildRename(src, dst),
	})

	var got []event.Event
	sink := event.FuncSink(func(e event.Event) { got = append(got, e) })
	a := New(prov, reg, NewSession(""), Options{MaxSteps: 1}, sink)
	_ = a.Run(context.Background(), "go") // MaxSteps=1 后停止；只关心 dispatch 事件

	for _, e := range got {
		if e.Kind != event.ToolDispatch || e.Tool.Partial {
			continue
		}
		fd := e.Tool.FileDiff
		if fd.Kind != string(diff.Rename) {
			t.Errorf("FileDiff.Kind = %q, want %q", fd.Kind, diff.Rename)
		}
		if fd.SrcPath != src {
			t.Errorf("FileDiff.SrcPath = %q, want %q", fd.SrcPath, src)
		}
		if fd.DstPath != dst {
			t.Errorf("FileDiff.DstPath = %q, want %q", fd.DstPath, dst)
		}
		return // 只需验证第一个 full dispatch
	}
	t.Fatal("没有捕获到完整的 ToolDispatch 事件")
}

func TestRenameMetadataSurvivesSessionReload(t *testing.T) {
	src, dst := "/workspace/old.txt", "/workspace/new.txt"
	prov := &mockProvider{name: "p", chunks: []provider.Chunk{
		{Type: provider.ChunkToolCallStart, ToolCall: &provider.ToolCall{ID: "c1", Name: "move_file"}},
		{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{
			ID:        "c1",
			Name:      "move_file",
			Arguments: `{"source_path":"` + src + `","destination_path":"` + dst + `"}`,
		}},
		{Type: provider.ChunkDone},
	}}
	reg := tool.NewRegistry()
	reg.Add(fakePreviewTool{name: "move_file", change: diff.BuildRename(src, dst)})
	sess := NewSession("")
	a := New(prov, reg, sess, Options{MaxSteps: 1}, event.FuncSink(func(event.Event) {}))
	_ = a.Run(context.Background(), "go")

	path := filepath.Join(t.TempDir(), "rename.jsonl")
	if err := sess.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	for _, msg := range loaded.Snapshot() {
		if msg.Role != provider.RoleAssistant || len(msg.ToolCalls) == 0 {
			continue
		}
		call := msg.ToolCalls[0]
		if call.Kind != string(diff.Rename) || call.SrcPath != src || call.DstPath != dst {
			t.Fatalf("reloaded rename metadata = kind:%q src:%q dst:%q", call.Kind, call.SrcPath, call.DstPath)
		}
		return
	}
	t.Fatal("reloaded session has no assistant tool call")
}
