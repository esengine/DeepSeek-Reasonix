package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// ── A2 设计修订五：虚空 UI Wails 绑定 ──
// 这些方法暴露给前端通过 window.go.main.App.* 调用。
// 事件通过 Wails runtime.EventsEmit 推送到前端。

const phantomEventChannel = "phantom:update"

// PhantomPanelView 前端渲染用的视图结构
type PhantomPanelView struct {
	Entries     []*PhantomEntry `json:"entries"`
	ActiveCount int             `json:"activeCount"`
	TotalCount  int             `json:"totalCount"`
}

// startPhantomPanel 启动虚空 UI 事件推送
// 在 App 启动时调用，将 PhantomRegistry 的更新推送到前端
func (a *App) startPhantomPanel(ctx context.Context) {
	if a.phantomRegistry == nil {
		return
	}

	sub := a.phantomRegistry.Subscribe()
	go func() {
		for {
			select {
			case <-ctx.Done():
				a.phantomRegistry.Unsubscribe(sub)
				return
			case update, ok := <-sub:
				if !ok {
					return
				}
				// 通过 Wails 事件流推送到前端
				if a.ctx != nil {
					runtime.EventsEmit(a.ctx, phantomEventChannel, update)
				}
			}
		}
	}()
}

// GetPhantomEntries 获取所有虚空 UI 条目（前端调用）
// 返回按 Name 排序的条目列表，根据隔离级别过滤显示内容
func (a *App) GetPhantomEntries() PhantomPanelView {
	if a.phantomRegistry == nil {
		return PhantomPanelView{}
	}
	entries := a.phantomRegistry.GetEntries()
	activeCount := 0
	for _, e := range entries {
		if e.Status == PhantomActive {
			activeCount++
		}
	}
	return PhantomPanelView{
		Entries:     entries,
		ActiveCount: activeCount,
		TotalCount:  len(entries),
	}
}

// JumpToPhantomEntry 跳转到指定 Session 的标签页
func (a *App) JumpToPhantomEntry(sessionID string) error {
	if a.phantomRegistry == nil {
		return nil
	}
	entry := a.phantomRegistry.GetEntry(sessionID)
	if entry == nil {
		return nil
	}
	// 切换到目标标签页
	if entry.JumpTarget.TabID != "" {
		a.SetActiveTab(entry.JumpTarget.TabID)
	}
	return nil
}

// MarkPhantomCommRead 标记某个 Session 的交流为已读
func (a *App) MarkPhantomCommRead(sessionID string) {
	if a.phantomRegistry == nil {
		return
	}
	a.phantomRegistry.MarkCommRead(sessionID)
}

// SetPhantomIsolation 设置 Session 的隔离级别
func (a *App) SetPhantomIsolation(sessionID string, level string) error {
	if a.phantomRegistry == nil {
		return nil
	}
	var il IsolationLevel
	switch level {
	case "sandbox":
		il = IsolationSandbox
	case "zoned":
		il = IsolationZoned
	case "observed":
		il = IsolationObserved
	case "merged":
		il = IsolationMerged
	default:
		return nil
	}
	a.phantomRegistry.UpdateIsolation(sessionID, il)
	return nil
}

// registerTabInPhantom 将标签页注册到虚空 UI
// 在标签创建时调用
func (a *App) registerTabInPhantom(tab *WorkspaceTab) {
	if a.phantomRegistry == nil || tab == nil {
		return
	}
	name := tab.TopicTitle
	if name == "" {
		name = tab.WorkspaceRoot
		if name == "" {
			name = tab.ID
		}
	}
	a.phantomRegistry.Register(tab.ID, name, tab.WorkspaceRoot, tab.ID)
}

// unregisterTabFromPhantom 从虚空 UI 移除标签页
// 在标签关闭时调用
func (a *App) unregisterTabFromPhantom(tabID string) {
	if a.phantomRegistry == nil {
		return
	}
	a.phantomRegistry.Unregister(tabID)
}

// updateTabStatusInPhantom 更新标签页在虚空 UI 中的状态
// 在标签状态变更时调用（thinking/streaming/error 等）
func (a *App) updateTabStatusInPhantom(tabID string, status string, turnCount int) {
	if a.phantomRegistry == nil {
		return
	}
	var ps PhantomStatus
	switch status {
	case "thinking", "streaming":
		ps = PhantomActive
	case "waiting_confirmation":
		ps = PhantomWaiting
	case "error":
		ps = PhantomFailed
	case "background_job":
		ps = PhantomActive
	case "paused":
		ps = PhantomWaiting
	default:
		ps = PhantomIdle
	}
	a.phantomRegistry.UpdateStatus(tabID, ps, turnCount)
}

// updateTabConclusionInPhantom 更新标签页在虚空 UI 中的结论
// 在 turn 完成时调用，summary 是规则化提取的一行摘要
func (a *App) updateTabConclusionInPhantom(tabID, summary, status string, turnCount int) {
	if a.phantomRegistry == nil {
		return
	}
	a.phantomRegistry.UpdateConclusion(tabID, summary, status, turnCount)
}

// extractConclusionFromTurn 从 turn 结果中规则化提取结论摘要（不调用 LLM）
// 基于工具调用类型和结果推断一行摘要
func extractConclusionFromTurn(toolCalls []toolCallSummary, success bool) string {
	if !success {
		return "执行失败"
	}
	if len(toolCalls) == 0 {
		return "纯文本回复"
	}
	// 根据工具类型提取摘要
	for _, tc := range toolCalls {
		switch tc.Name {
		case "bash":
			if tc.Command != "" {
				return "执行命令: " + truncateStr(tc.Command, 40)
			}
		case "write_file", "edit_file", "str_replace":
			if tc.FilePath != "" {
				return "修改文件: " + tc.FilePath
			}
		case "read_file":
			if tc.FilePath != "" {
				return "读取文件: " + tc.FilePath
			}
		}
	}
	return "完成 " + intToStr(len(toolCalls)) + " 个工具调用"
}

// toolCallSummary 工具调用摘要
type toolCallSummary struct {
	Name     string `json:"name"`
	Command  string `json:"command,omitempty"`
	FilePath string `json:"filePath,omitempty"`
	Success  bool   `json:"success"`
}

// phantomTestState 用于测试的共享状态
type phantomTestState struct {
	mu     sync.Mutex
	events []PhantomUpdate
}

// MarshalJSON 确保 PhantomStatus 和 IsolationLevel 序列化为字符串
func (s PhantomStatus) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.String())
}

func (l IsolationLevel) MarshalJSON() ([]byte, error) {
	return json.Marshal(l.String())
}

func (c CommType) MarshalJSON() ([]byte, error) {
	return json.Marshal(c.String())
}

// 辅助函数
func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func intToStr(i int) string {
	if i == 0 {
		return "0"
	}
	neg := false
	if i < 0 {
		neg = true
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

// logPhantomEvent 记录虚空 UI 事件到日志（用于调试）
func logPhantomEvent(update PhantomUpdate) {
	slog.Debug("phantom update",
		"sessionID", update.SessionID,
		"type", update.Type,
	)
}
