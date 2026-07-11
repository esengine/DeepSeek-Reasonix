package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ── 多窗口标签栏管理器 ──
// 基于 Cursor 3 Glass 的 worktree 隔离模型和 VS Code 标签栏设计。
// 每个 Tab 对应一个独立的 Agent Session，拥有独立的工作树和上下文。
// Tab 之间通过共享黑板同步状态，标签栏 UI 使用 Lipgloss v2 图层合成器渲染。
//
// 核心设计原则（来自 Claude Code prompt caching 架构）：
// 1. 每个 Tab 的 system prompt + tools 定义是缓存稳定的，不中途修改
// 2. Tab 间传递摘要而非原始输出（减少 token 消耗）
// 3. Tab 切换不销毁上下文（保持缓存命中）
// 4. 新 Tab 继承父 Tab 的缓存前缀（cache-safe forking）

// TabState 标签状态
type TabState int

const (
	TabStateLoading TabState = iota // 正在加载
	TabStateActive                  // 活跃（正在执行）
	TabStateIdle                    // 空闲（等待输入）
	TabStateBusy                    // 忙碌（LLM 调用中）
	TabStateError                   // 错误
	TabStateClosed                  // 已关闭
)

// String 返回标签状态字符串
func (s TabState) String() string {
	switch s {
	case TabStateLoading:
		return "loading"
	case TabStateActive:
		return "active"
	case TabStateIdle:
		return "idle"
	case TabStateBusy:
		return "busy"
	case TabStateError:
		return "error"
	case TabStateClosed:
		return "closed"
	default:
		return "unknown"
	}
}

// Tab 表示一个标签页，对应一个 Agent Session
type Tab struct {
	ID          string       // 唯一标识
	Title       string       // 标签标题
	SessionID   string       // 关联的 Session ID
	Workspace   string       // 工作目录（worktree 路径）
	ParentID    string       // 父 Tab ID（用于派生关系）
	Scene       Scene        // 场景类型（驱动策略选择）
	State       TabState     // 当前状态
	CreatedAt   time.Time    // 创建时间
	LastActive  time.Time    // 最后活跃时间
	TokenBudget int64        // Token 预算（独立计量）
	TokenUsed   atomic.Int64 // 已用 Token（原子计数）
	// 缓存前缀哈希（来自 Claude Code 的 prefix hash 监控）
	PrefixHash string // system prompt + tools 的 SHA256 前缀
	// 标签颜色（用于 UI 渲染）
	Color string
	// 是否有未保存的变更
	Dirty bool
	// 焦点状态
	Focused bool
}

// TabManager 标签栏管理器
type TabManager struct {
	mu          sync.RWMutex
	tabs        map[string]*Tab
	order       []string // Tab 显示顺序
	activeTabID string   // 当前焦点 Tab
	maxTabs     int      // 最大 Tab 数量
	// 全局共享黑板（Tab 间状态同步）
	blackboard *SharedBlackboard
	// 事件通知
	onTabChanged func(tab *Tab)
}

// SharedBlackboard 共享黑板（Tab 间状态同步）
// 基于 A2 的 EventLog 设计，Tab 之间通过黑板感知彼此状态
type SharedBlackboard struct {
	mu      sync.Mutex
	entries []BlackboardEntry
	maxSize int
}

// BlackboardEntry 黑板条目
type BlackboardEntry struct {
	SourceTabID string
	Type        string // "file_changed", "task_completed", "error", "status"
	Message     string
	Timestamp   time.Time
}

// NewSharedBlackboard 创建共享黑板
func NewSharedBlackboard(maxSize int) *SharedBlackboard {
	if maxSize <= 0 {
		maxSize = 1000
	}
	return &SharedBlackboard{maxSize: maxSize}
}

// Write 写入黑板条目
func (b *SharedBlackboard) Write(entry BlackboardEntry) {
	b.mu.Lock()
	defer b.mu.Unlock()
	entry.Timestamp = time.Now()
	b.entries = append(b.entries, entry)
	if len(b.entries) > b.maxSize {
		b.entries = b.entries[len(b.entries)-b.maxSize:]
	}
}

// ReadSince 读取指定时间后的所有条目
func (b *SharedBlackboard) ReadSince(t time.Time) []BlackboardEntry {
	b.mu.Lock()
	defer b.mu.Unlock()
	var result []BlackboardEntry
	for _, e := range b.entries {
		if e.Timestamp.After(t) {
			result = append(result, e)
		}
	}
	return result
}

// NewTabManager 创建标签栏管理器
func NewTabManager(maxTabs int) *TabManager {
	if maxTabs <= 0 {
		maxTabs = 10 // 默认最多 10 个标签
	}
	return &TabManager{
		tabs:       make(map[string]*Tab),
		maxTabs:    maxTabs,
		blackboard: NewSharedBlackboard(1000),
	}
}

// CreateTab 创建新标签页
func (m *TabManager) CreateTab(ctx context.Context, opts TabCreateOptions) (*Tab, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.tabs) >= m.maxTabs {
		return nil, fmt.Errorf("max tabs limit reached (%d)", m.maxTabs)
	}

	tab := &Tab{
		ID:         generateTabID(),
		Title:      opts.Title,
		Workspace:  opts.Workspace,
		ParentID:   opts.ParentID,
		Scene:      opts.Scene,
		State:      TabStateLoading,
		CreatedAt:  time.Now(),
		LastActive: time.Now(),
		Color:      opts.Color,
	}

	if tab.Title == "" {
		tab.Title = truncatePath(opts.Workspace, 20)
	}
	if tab.Color == "" {
		tab.Color = defaultTabColor(len(m.tabs))
	}

	m.tabs[tab.ID] = tab
	m.order = append(m.order, tab.ID)

	// 新 Tab 自动获得焦点
	m.activeTabID = tab.ID
	tab.Focused = true

	// 通知黑板
	m.blackboard.Write(BlackboardEntry{
		SourceTabID: tab.ID,
		Type:        "tab_created",
		Message:     fmt.Sprintf("Tab '%s' created for workspace %s", tab.Title, tab.Workspace),
	})

	if m.onTabChanged != nil {
		m.onTabChanged(tab)
	}
	return tab, nil
}

// TabCreateOptions 创建标签的选项
type TabCreateOptions struct {
	Title     string
	Workspace string
	ParentID  string
	Scene     Scene
	Color     string
}

// CloseTab 关闭标签页
func (m *TabManager) CloseTab(tabID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	tab, ok := m.tabs[tabID]
	if !ok {
		return fmt.Errorf("tab %s not found", tabID)
	}
	if tab.State == TabStateBusy {
		return fmt.Errorf("tab %s is busy, cannot close", tabID)
	}

	tab.State = TabStateClosed
	delete(m.tabs, tabID)

	// 从顺序中移除
	for i, id := range m.order {
		if id == tabID {
			m.order = append(m.order[:i], m.order[i+1:]...)
			break
		}
	}

	// 如果关闭的是焦点 Tab，切换到最后一个
	if m.activeTabID == tabID {
		if len(m.order) > 0 {
			m.activeTabID = m.order[len(m.order)-1]
			if newTab, ok := m.tabs[m.activeTabID]; ok {
				newTab.Focused = true
			}
		} else {
			m.activeTabID = ""
		}
	}

	m.blackboard.Write(BlackboardEntry{
		SourceTabID: tabID,
		Type:        "tab_closed",
		Message:     fmt.Sprintf("Tab '%s' closed", tab.Title),
	})

	return nil
}

// SwitchTo 切换到指定标签页
func (m *TabManager) SwitchTo(tabID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	tab, ok := m.tabs[tabID]
	if !ok {
		return fmt.Errorf("tab %s not found", tabID)
	}

	// 取消旧焦点
	if old, ok := m.tabs[m.activeTabID]; ok {
		old.Focused = false
	}

	m.activeTabID = tabID
	tab.Focused = true
	tab.LastActive = time.Now()

	if m.onTabChanged != nil {
		m.onTabChanged(tab)
	}
	return nil
}

// GetActiveTab 返回当前焦点标签
func (m *TabManager) GetActiveTab() *Tab {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.tabs[m.activeTabID]
}

// GetTab 返回指定标签
func (m *TabManager) GetTab(tabID string) (*Tab, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	tab, ok := m.tabs[tabID]
	return tab, ok
}

// GetAllTabs 返回所有标签（按顺序）
func (m *TabManager) GetAllTabs() []*Tab {
	m.mu.RLock()
	defer m.mu.RUnlock()
	tabs := make([]*Tab, 0, len(m.order))
	for _, id := range m.order {
		if tab, ok := m.tabs[id]; ok {
			tabs = append(tabs, tab)
		}
	}
	return tabs
}

// UpdateTabState 更新标签状态
func (m *TabManager) UpdateTabState(tabID string, state TabState) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if tab, ok := m.tabs[tabID]; ok {
		tab.State = state
		if state == TabStateActive || state == TabStateBusy {
			tab.LastActive = time.Now()
		}
	}
}

// AddTokenUsage 增加标签的 Token 使用量
func (m *TabManager) AddTokenUsage(tabID string, tokens int64) {
	m.mu.RLock()
	tab, ok := m.tabs[tabID]
	m.mu.RUnlock()
	if ok {
		tab.TokenUsed.Add(tokens)
	}
}

// SetPrefixHash 设置标签的缓存前缀哈希
func (m *TabManager) SetPrefixHash(tabID, hash string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if tab, ok := m.tabs[tabID]; ok {
		tab.PrefixHash = hash
	}
}

// GetBlackboard 返回共享黑板
func (m *TabManager) GetBlackboard() *SharedBlackboard {
	return m.blackboard
}

// SetOnTabChanged 设置标签变更回调
func (m *TabManager) SetOnTabChanged(fn func(tab *Tab)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onTabChanged = fn
}

// RenderTabBar 渲染标签栏（使用 Lipgloss v2 图层合成器风格）
// 返回终端友好的标签栏字符串
func (m *TabManager) RenderTabBar(width int) string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.order) == 0 {
		return ""
	}

	var sb strings.Builder
	availableWidth := width
	if availableWidth <= 0 {
		availableWidth = 120
	}

	// 计算每个标签的最大宽度
	tabCount := len(m.order)
	maxTabWidth := availableWidth / tabCount
	if maxTabWidth < 10 {
		maxTabWidth = 10 // 最小宽度
	}
	if maxTabWidth > 30 {
		maxTabWidth = 30 // 最大宽度
	}

	for _, id := range m.order {
		tab, ok := m.tabs[id]
		if !ok {
			continue
		}
		// 标签状态指示器
		var indicator string
		switch tab.State {
		case TabStateBusy:
			indicator = "●"
		case TabStateActive:
			indicator = "◆"
		case TabStateIdle:
			indicator = "○"
		case TabStateError:
			indicator = "✗"
		default:
			indicator = "·"
		}

		// 标签标题
		title := tab.Title
		if len(title) > maxTabWidth-4 {
			title = title[:maxTabWidth-7] + "..."
		}

		// 焦点标记
		var prefix, suffix string
		if tab.Focused {
			prefix = "["
			suffix = "]"
		} else {
			prefix = " "
			suffix = " "
		}

		// 脏标记
		dirtyMark := ""
		if tab.Dirty {
			dirtyMark = "*"
		}

		sb.WriteString(fmt.Sprintf("%s%s %s%s%s", prefix, indicator, title, dirtyMark, suffix))
		sb.WriteString("│")
	}

	result := sb.String()
	// 移除最后的分隔符
	if strings.HasSuffix(result, "│") {
		result = result[:len(result)-1]
	}
	return result
}

// FindTabByWorkspace 按工作目录查找标签
func (m *TabManager) FindTabByWorkspace(workspace string) (*Tab, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, tab := range m.tabs {
		if tab.Workspace == workspace {
			return tab, true
		}
	}
	return nil, false
}

// GetTabsByScene 按场景类型筛选标签
func (m *TabManager) GetTabsByScene(scene Scene) []*Tab {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []*Tab
	for _, tab := range m.tabs {
		if tab.Scene == scene {
			result = append(result, tab)
		}
	}
	return result
}

// GetTabStats 返回标签栏统计信息
func (m *TabManager) GetTabStats() TabBarStats {
	m.mu.RLock()
	defer m.mu.RUnlock()
	stats := TabBarStats{
		TotalTabs: len(m.tabs),
	}
	for _, tab := range m.tabs {
		switch tab.State {
		case TabStateBusy:
			stats.BusyTabs++
		case TabStateIdle:
			stats.IdleTabs++
		case TabStateError:
			stats.ErrorTabs++
		case TabStateActive:
			stats.ActiveTabs++
		}
		stats.TotalTokensUsed += tab.TokenUsed.Load()
	}
	return stats
}

// TabBarStats 标签栏统计
type TabBarStats struct {
	TotalTabs       int
	ActiveTabs      int
	BusyTabs        int
	IdleTabs        int
	ErrorTabs       int
	TotalTokensUsed int64
}

// ── 辅助函数 ──

var tabCounter atomic.Int64

func generateTabID() string {
	n := tabCounter.Add(1)
	return fmt.Sprintf("tab-%d", n)
}

func truncatePath(path string, maxLen int) string {
	if len(path) <= maxLen {
		return path
	}
	// 保留最后 maxLen 个字符（通常包含项目名）
	return "..." + path[len(path)-maxLen+3:]
}

func defaultTabColor(index int) string {
	colors := []string{"blue", "green", "yellow", "magenta", "cyan", "red"}
	return colors[index%len(colors)]
}
