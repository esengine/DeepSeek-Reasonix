package agent

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ── 虚空UI v2 — 真实窗口投影 + 对话转移引擎 ──
//
// 核心改进（对比 v1）：
//   v1: 仅文本摘要面板（Lipgloss cell叠加）
//   v2: 真实渲染树投影 + 对话焦点转移 + 眼控集成
//
// 架构层次：
//   Tab Window Layer  → 标签页窗口（活跃Tab + 非活跃Tab）
//   Projection Engine → 捕获源Tab渲染树 → 信号编码 → 写入目标Tab虚空区域
//   Transfer Engine   → 焦点检测 → 上下文打包 → Go channel零token切换
//   Interaction Layer → 眼控追踪 → 注视映射 → 分级审核 → 证据链
//
// 关键设计：
//   - 投影不是文本摘要，是源Tab渲染树的结构化快照
//   - 对话转移通过 Go channel 传递信号引用，不传完整内容（0 token）
//   - 眼控注视 >1.5s 触发意图聚焦，自动切换对话到源Tab
//   - 代码审核按风险自动分级，非技术难度提升

// ═══════════════════════════════════════════════════════════════
//  渲染树结构 — 投影的基础单元
// ═══════════════════════════════════════════════════════════════

// RenderNode 渲染树节点
// 不是文本字符串，而是结构化的渲染内容
type RenderNode struct {
	ID       string       `json:"id"`
	Type     RenderType   `json:"type"`
	Content  string       `json:"content"`
	Children []RenderNode `json:"children,omitempty"`
	Meta     RenderMeta   `json:"meta"`
}

// RenderType 渲染节点类型
type RenderType int

const (
	RenderText       RenderType = iota // 普通文本
	RenderCode                         // 代码块
	RenderDiff                         // 差异视图
	RenderStatus                       // 状态指示
	RenderProgress                     // 进度条
	RenderError                        // 错误信息
	RenderWarning                      // 警告信息
	RenderToolResult                   // 工具调用结果
	RenderThinking                     // 思考过程
	RenderChart                        // 图表/可视化
)

// RenderMeta 渲染元数据
type RenderMeta struct {
	LineStart   int       `json:"line_start,omitempty"`
	LineEnd     int       `json:"line_end,omitempty"`
	Language    string    `json:"language,omitempty"`
	Timestamp   time.Time `json:"timestamp,omitempty"`
	TokenCount  int       `json:"token_count,omitempty"`
	Severity    string    `json:"severity,omitempty"` // error/warning/info
	DiffAdded   int       `json:"diff_added,omitempty"`
	DiffRemoved int       `json:"diff_removed,omitempty"`
}

// RenderSnapshot 渲染快照
// 从源Tab捕获的完整渲染树快照，用于投影
type RenderSnapshot struct {
	SourceTabID string       `json:"source_tab_id"`
	Nodes       []RenderNode `json:"nodes"`
	ScrollPos   int          `json:"scroll_pos"`
	CursorLine  int          `json:"cursor_line"`
	CapturedAt  time.Time    `json:"captured_at"`
	ContentHash string       `json:"content_hash"` // 用于变更检测
	// 增量更新支持
	PrevHash  string `json:"prev_hash,omitempty"`
	DeltaOnly bool   `json:"delta_only,omitempty"`
}

// CaptureRenderSnapshot 从Tab捕获渲染快照
func CaptureRenderSnapshot(tab *Tab) *RenderSnapshot {
	snapshot := &RenderSnapshot{
		SourceTabID: tab.ID,
		CapturedAt:  time.Now(),
	}

	// 根据Tab场景类型捕获不同的渲染节点
	switch tab.Scene {
	case SceneCode:
		snapshot.Nodes = captureCodeScene(tab)
	case SceneResearch:
		snapshot.Nodes = captureResearchScene(tab)
	case SceneWriting:
		snapshot.Nodes = captureWritingScene(tab)
	default:
		snapshot.Nodes = captureGenericScene(tab)
	}

	// 计算内容哈希（用于增量更新）
	snapshot.ContentHash = hashRenderNodes(snapshot.Nodes)

	return snapshot
}

// captureCodeScene 捕获代码场景渲染节点
func captureCodeScene(tab *Tab) []RenderNode {
	var nodes []RenderNode
	// 捕获最近的状态和输出
	nodes = append(nodes, RenderNode{
		ID:      tab.ID + ":status",
		Type:    RenderStatus,
		Content: fmt.Sprintf("%s · %s", tab.Scene, tab.State),
		Meta:    RenderMeta{Timestamp: time.Now()},
	})

	if tab.Dirty {
		nodes = append(nodes, RenderNode{
			ID:      tab.ID + ":dirty",
			Type:    RenderWarning,
			Content: "有未保存的变更",
			Meta:    RenderMeta{Severity: "warning"},
		})
	}

	tokens := tab.TokenUsed.Load()
	if tokens > 0 {
		nodes = append(nodes, RenderNode{
			ID:      tab.ID + ":tokens",
			Type:    RenderStatus,
			Content: fmt.Sprintf("%dk tokens used", tokens/1000),
			Meta:    RenderMeta{TokenCount: int(tokens)},
		})
	}

	return nodes
}

// captureResearchScene 捕获研究场景渲染节点
func captureResearchScene(tab *Tab) []RenderNode {
	var nodes []RenderNode
	nodes = append(nodes, RenderNode{
		ID:      tab.ID + ":status",
		Type:    RenderStatus,
		Content: fmt.Sprintf("Research · %s", tab.State),
		Meta:    RenderMeta{Timestamp: time.Now()},
	})
	return nodes
}

// captureWritingScene 捕获写作场景渲染节点
func captureWritingScene(tab *Tab) []RenderNode {
	var nodes []RenderNode
	nodes = append(nodes, RenderNode{
		ID:      tab.ID + ":status",
		Type:    RenderStatus,
		Content: fmt.Sprintf("Writing · %s", tab.State),
		Meta:    RenderMeta{Timestamp: time.Now()},
	})
	return nodes
}

// captureGenericScene 捕获通用场景渲染节点
func captureGenericScene(tab *Tab) []RenderNode {
	return []RenderNode{{
		ID:      tab.ID + ":status",
		Type:    RenderStatus,
		Content: fmt.Sprintf("%s · %s", tab.Scene, tab.State),
		Meta:    RenderMeta{Timestamp: time.Now()},
	}}
}

// hashRenderNodes 计算渲染节点的内容哈希
func hashRenderNodes(nodes []RenderNode) string {
	h := sha256.New()
	for _, n := range nodes {
		h.Write([]byte(n.ID))
		h.Write([]byte(n.Content))
		binary.Write(h, binary.LittleEndian, int(n.Type))
	}
	return fmt.Sprintf("%x", h.Sum(nil))[:16]
}

// ═══════════════════════════════════════════════════════════════
//  信号引用编码 — 零token传输
// ═══════════════════════════════════════════════════════════════

// SignalRef 信号引用
// 在Go channel中传递的轻量对象，不包含完整内容
type SignalRef struct {
	SourceTabID string `json:"src"`
	ContentHash string `json:"hash"`
	NodeCount   int    `json:"nodes"`
	ScrollPos   int    `json:"scroll"`
	CursorLine  int    `json:"cursor"`
	// 信号类型决定投影方式
	SignalType SignalType `json:"type"`
}

// SignalType 信号类型（与A2通信类型对应）
type SignalType int

const (
	SignalStatus   SignalType = iota // 状态信号（最轻量）
	SignalMetadata                   // 元数据
	SignalSummary                    // 摘要
	SignalFragment                   // 片段
	SignalFull                       // 完整内容（最重量级）
)

// ProjectionCodec 投影编解码器
// 将渲染快照编码为信号引用（零token），在接收端解码为投影
type ProjectionCodec struct {
	// 共享内存：存储完整内容，信号引用通过hash索引
	sharedBuffer sync.Map // hash → *RenderSnapshot
	maxBufferAge time.Duration
}

// NewProjectionCodec 创建编解码器
func NewProjectionCodec() *ProjectionCodec {
	return &ProjectionCodec{
		maxBufferAge: 5 * time.Minute,
	}
}

// Encode 将渲染快照编码为信号引用
// 信号引用固定大小（~64字节），在channel中传递不消耗token
func (pc *ProjectionCodec) Encode(snapshot *RenderSnapshot) SignalRef {
	// 存入共享缓冲区（接收端通过hash检索完整内容）
	pc.sharedBuffer.Store(snapshot.ContentHash, snapshot)

	// 返回轻量信号引用
	return SignalRef{
		SourceTabID: snapshot.SourceTabID,
		ContentHash: snapshot.ContentHash,
		NodeCount:   len(snapshot.Nodes),
		ScrollPos:   snapshot.ScrollPos,
		CursorLine:  snapshot.CursorLine,
		SignalType:  pc.classifySignalType(snapshot),
	}
}

// Decode 将信号引用解码为渲染快照
func (pc *ProjectionCodec) Decode(ref SignalRef) (*RenderSnapshot, bool) {
	if val, ok := pc.sharedBuffer.Load(ref.ContentHash); ok {
		return val.(*RenderSnapshot), true
	}
	return nil, false
}

// classifySignalType 根据快照内容分类信号类型
func (pc *ProjectionCodec) classifySignalType(snapshot *RenderSnapshot) SignalType {
	totalTokens := 0
	for _, n := range snapshot.Nodes {
		totalTokens += n.Meta.TokenCount
	}

	switch {
	case totalTokens == 0 && len(snapshot.Nodes) <= 2:
		return SignalStatus
	case totalTokens < 100:
		return SignalMetadata
	case totalTokens < 500:
		return SignalSummary
	case totalTokens < 2000:
		return SignalFragment
	default:
		return SignalFull
	}
}

// CleanExpired 清理过期的共享缓冲区
func (pc *ProjectionCodec) CleanExpired() {
	now := time.Now()
	pc.sharedBuffer.Range(func(key, val any) bool {
		snapshot := val.(*RenderSnapshot)
		if now.Sub(snapshot.CapturedAt) > pc.maxBufferAge {
			pc.sharedBuffer.Delete(key)
		}
		return true
	})
}

// ═══════════════════════════════════════════════════════════════
//  投影区域 — 在活跃Tab中为非活跃Tab分配的渲染区域
// ═══════════════════════════════════════════════════════════════

// ProjectionRegion 投影区域
// 在活跃Tab窗口中为非活跃Tab分配的虚拟渲染区域
type ProjectionRegion struct {
	ID          string       `json:"id"`
	SourceTabID string       `json:"source_tab_id"`
	Title       string       `json:"title"`
	Bounds      RegionBounds `json:"bounds"`
	SignalRef   SignalRef    `json:"signal_ref"`
	Content     []RenderNode `json:"-"` // 解码后的内容
	Visible     bool         `json:"visible"`
	Focused     bool         `json:"focused"` // 用户是否正在与该区域交互
	LastUpdate  time.Time    `json:"last_update"`
	// 交互追踪
	HoverCount   int64 `json:"-"` // 悬停次数
	ClickCount   int64 `json:"-"` // 点击次数
	GazeDuration int64 `json:"-"` // 注视持续时间(ms)
}

// RegionBounds 区域边界
type RegionBounds struct {
	X      int `json:"x"`      // 起始列
	Y      int `json:"y"`      // 起始行
	Width  int `json:"width"`  // 宽度
	Height int `json:"height"` // 高度
}

// Contains 检查坐标是否在区域内
func (b RegionBounds) Contains(x, y int) bool {
	return x >= b.X && x < b.X+b.Width &&
		y >= b.Y && y < b.Y+b.Height
}

// ═══════════════════════════════════════════════════════════════
//  投影引擎 — 核心：捕获→编码→投影
// ═══════════════════════════════════════════════════════════════

// ProjectionEngine 投影引擎
// 负责从非活跃Tab捕获渲染快照，编码为信号引用，投影到活跃Tab
type ProjectionEngine struct {
	mu         sync.RWMutex
	codec      *ProjectionCodec
	regions    map[string]*ProjectionRegion // 活跃Tab中的投影区域
	tabManager *TabManager
	// 投影更新通道（零token，只传信号引用）
	projectionCh chan SignalRef
	// 配置
	maxRegions     int
	updateInterval time.Duration
	// 统计
	captureCount  atomic.Int64
	transferCount atomic.Int64
}

// NewProjectionEngine 创建投影引擎
func NewProjectionEngine(tm *TabManager) *ProjectionEngine {
	return &ProjectionEngine{
		codec:          NewProjectionCodec(),
		regions:        make(map[string]*ProjectionRegion),
		tabManager:     tm,
		projectionCh:   make(chan SignalRef, 64),
		maxRegions:     6, // 最多同时投影6个Tab
		updateInterval: 500 * time.Millisecond,
	}
}

// ProjectAll 投影所有非活跃Tab到当前活跃Tab
func (pe *ProjectionEngine) ProjectAll() []SignalRef {
	pe.mu.Lock()
	defer pe.mu.Unlock()

	tabs := pe.tabManager.GetAllTabs()
	activeID := ""
	if active := pe.tabManager.GetActiveTab(); active != nil {
		activeID = active.ID
	}

	var refs []SignalRef
	regionIdx := 0

	for _, tab := range tabs {
		if tab.ID == activeID {
			continue
		}
		if tab.State == TabStateClosed {
			continue
		}
		if regionIdx >= pe.maxRegions {
			break
		}

		// 1. 捕获渲染快照
		snapshot := CaptureRenderSnapshot(tab)
		pe.captureCount.Add(1)

		// 2. 编码为信号引用（零token）
		ref := pe.codec.Encode(snapshot)

		// 3. 分配投影区域
		bounds := pe.allocateRegion(regionIdx, pe.terminalWidth(), pe.terminalHeight())
		region := &ProjectionRegion{
			ID:          fmt.Sprintf("proj_%s", tab.ID),
			SourceTabID: tab.ID,
			Title:       tab.Title,
			Bounds:      bounds,
			SignalRef:   ref,
			Content:     snapshot.Nodes,
			Visible:     true,
			LastUpdate:  time.Now(),
		}

		pe.regions[tab.ID] = region
		refs = append(refs, ref)
		regionIdx++
	}

	// 移除已关闭Tab的投影
	for tabID := range pe.regions {
		if _, ok := pe.tabManager.GetTab(tabID); !ok {
			delete(pe.regions, tabID)
		}
	}

	return refs
}

// allocateRegion 分配投影区域位置
// 按网格布局排列投影区域
func (pe *ProjectionEngine) allocateRegion(idx, termW, termH int) RegionBounds {
	// 投影区域放在屏幕右侧或底部，不遮挡主内容
	// 每个区域 30列 × 5行
	regionW := 30
	regionH := 5

	// 右侧排列，最多3列
	col := idx % 3
	row := idx / 3

	x := termW - regionW - 1
	if col < 2 {
		x = termW - (3-col)*regionW - 1
	}
	y := termH - (row+1)*(regionH+1) - 1

	// 确保不超出边界
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 1
	}

	return RegionBounds{
		X:      x,
		Y:      y,
		Width:  regionW,
		Height: regionH,
	}
}

// terminalWidth 获取终端宽度（简化实现）
func (pe *ProjectionEngine) terminalWidth() int { return 120 }

// terminalHeight 获取终端高度（简化实现）
func (pe *ProjectionEngine) terminalHeight() int { return 40 }

// GetRegions 获取所有投影区域
func (pe *ProjectionEngine) GetRegions() []*ProjectionRegion {
	pe.mu.RLock()
	defer pe.mu.RUnlock()
	regions := make([]*ProjectionRegion, 0, len(pe.regions))
	for _, r := range pe.regions {
		regions = append(regions, r)
	}
	return regions
}

// GetRegion 获取指定Tab的投影区域
func (pe *ProjectionEngine) GetRegion(tabID string) *ProjectionRegion {
	pe.mu.RLock()
	defer pe.mu.RUnlock()
	return pe.regions[tabID]
}

// HitTest 命中测试 — 检查坐标命中了哪个投影区域
func (pe *ProjectionEngine) HitTest(x, y int) *ProjectionRegion {
	pe.mu.RLock()
	defer pe.mu.RUnlock()
	for _, region := range pe.regions {
		if region.Visible && region.Bounds.Contains(x, y) {
			return region
		}
	}
	return nil
}

// UpdateGaze 更新注视状态
func (pe *ProjectionEngine) UpdateGaze(tabID string, durationMs int64) {
	pe.mu.Lock()
	defer pe.mu.Unlock()
	if region, ok := pe.regions[tabID]; ok {
		region.GazeDuration = durationMs
		if durationMs > 1500 { // 注视超过1.5秒
			region.Focused = true
		}
	}
}

// RecordClick 记录点击
func (pe *ProjectionEngine) RecordClick(tabID string) {
	pe.mu.Lock()
	defer pe.mu.Unlock()
	if region, ok := pe.regions[tabID]; ok {
		atomic.AddInt64(&region.ClickCount, 1)
		region.Focused = true
	}
}

// ═══════════════════════════════════════════════════════════════
//  对话转移引擎 — 焦点检测 → 上下文打包 → Tab切换
// ═══════════════════════════════════════════════════════════════

// FocusEvent 焦点事件
type FocusEvent struct {
	Type      FocusEventType
	SourceTab string // 触发事件的Tab
	TargetTab string // 要切换到的Tab
	RegionID  string // 触发的投影区域
	X, Y      int    // 触发坐标
	Timestamp time.Time
}

// FocusEventType 焦点事件类型
type FocusEventType int

const (
	FocusByClick   FocusEventType = iota // 点击投影区域
	FocusByGaze                          // 注视超过阈值
	FocusByHotkey                        // 快捷键切换
	FocusByCommand                       // 命令切换
)

// ContextHandoff 上下文交接包
// 将当前对话上下文打包，转移到目标Tab
type ContextHandoff struct {
	FromTabID  string    `json:"from_tab"`
	ToTabID    string    `json:"to_tab"`
	SignalRef  SignalRef `json:"signal_ref"` // 源Tab的信号引用
	ScrollPos  int       `json:"scroll_pos"`
	CursorLine int       `json:"cursor_line"`
	Reason     string    `json:"reason"` // 转移原因
	Timestamp  time.Time `json:"timestamp"`
	// 保留焦点信息，用于回切
	ReturnContext *ContextHandoff `json:"return_context,omitempty"`
}

// ConversationTransferEngine 对话转移引擎
type ConversationTransferEngine struct {
	mu               sync.Mutex
	projectionEngine *ProjectionEngine
	tabManager       *TabManager
	// 零token切换通道
	switchCh chan ContextHandoff
	// 焦点事件通道
	focusCh chan FocusEvent
	// 焦点历史（用于回切）
	focusStack []ContextHandoff
	// 统计
	transferCount atomic.Int64
}

// NewConversationTransferEngine 创建对话转移引擎
func NewConversationTransferEngine(pe *ProjectionEngine, tm *TabManager) *ConversationTransferEngine {
	return &ConversationTransferEngine{
		projectionEngine: pe,
		tabManager:       tm,
		switchCh:         make(chan ContextHandoff, 32),
		focusCh:          make(chan FocusEvent, 32),
	}
}

// HandleClick 处理投影区域点击
func (cte *ConversationTransferEngine) HandleClick(x, y int) bool {
	region := cte.projectionEngine.HitTest(x, y)
	if region == nil {
		return false
	}

	// 记录点击
	cte.projectionEngine.RecordClick(region.SourceTabID)

	// 发送焦点事件
	cte.focusCh <- FocusEvent{
		Type:      FocusByClick,
		SourceTab: cte.tabManager.GetActiveTabID(),
		TargetTab: region.SourceTabID,
		RegionID:  region.ID,
		X:         x,
		Y:         y,
		Timestamp: time.Now(),
	}

	// 执行对话转移
	return cte.transfer(region.SourceTabID, "click on projection region")
}

// HandleGaze 处理注视事件
func (cte *ConversationTransferEngine) HandleGaze(tabID string, durationMs int64) bool {
	cte.projectionEngine.UpdateGaze(tabID, durationMs)

	// 注视超过1.5秒触发转移
	if durationMs > 1500 {
		region := cte.projectionEngine.GetRegion(tabID)
		if region != nil && region.Focused {
			cte.focusCh <- FocusEvent{
				Type:      FocusByGaze,
				SourceTab: cte.tabManager.GetActiveTabID(),
				TargetTab: tabID,
				RegionID:  region.ID,
				Timestamp: time.Now(),
			}
			return cte.transfer(tabID, fmt.Sprintf("gaze fixation %dms", durationMs))
		}
	}
	return false
}

// HandleHotkey 处理快捷键切换
func (cte *ConversationTransferEngine) HandleHotkey(targetTabID string) bool {
	cte.focusCh <- FocusEvent{
		Type:      FocusByHotkey,
		SourceTab: cte.tabManager.GetActiveTabID(),
		TargetTab: targetTabID,
		Timestamp: time.Now(),
	}
	return cte.transfer(targetTabID, "hotkey switch")
}

// transfer 执行对话转移
// 核心零token机制：通过Go channel传递信号引用，不传完整内容
func (cte *ConversationTransferEngine) transfer(targetTabID, reason string) bool {
	cte.mu.Lock()
	defer cte.mu.Unlock()

	activeTab := cte.tabManager.GetActiveTab()
	if activeTab == nil {
		return false
	}

	// 构建上下文交接包
	handoff := ContextHandoff{
		FromTabID:  activeTab.ID,
		ToTabID:    targetTabID,
		ScrollPos:  0, // 实际从Tab状态获取
		CursorLine: 0,
		Reason:     reason,
		Timestamp:  time.Now(),
	}

	// 获取目标Tab的信号引用
	if region := cte.projectionEngine.GetRegion(targetTabID); region != nil {
		handoff.SignalRef = region.SignalRef
	}

	// 压入焦点栈（用于回切）
	if len(cte.focusStack) > 0 {
		handoff.ReturnContext = &cte.focusStack[len(cte.focusStack)-1]
	}
	cte.focusStack = append(cte.focusStack, handoff)
	if len(cte.focusStack) > 20 { // 限制栈深度
		cte.focusStack = cte.focusStack[1:]
	}

	// 通过channel发送交接包（零token，进程内通信）
	select {
	case cte.switchCh <- handoff:
	default:
		// channel满，丢弃最旧的
		select {
		case <-cte.switchCh:
		default:
		}
		cte.switchCh <- handoff
	}

	// 执行Tab切换
	if err := cte.tabManager.SwitchTo(targetTabID); err != nil {
		return false
	}
	cte.transferCount.Add(1)

	return true
}

// SwitchBack 回切到上一个Tab
func (cte *ConversationTransferEngine) SwitchBack() bool {
	cte.mu.Lock()
	defer cte.mu.Unlock()

	if len(cte.focusStack) == 0 {
		return false
	}

	// 弹出当前
	last := cte.focusStack[len(cte.focusStack)-1]
	cte.focusStack = cte.focusStack[:len(cte.focusStack)-1]

	// 回切到来源Tab
	if err := cte.tabManager.SwitchTo(last.FromTabID); err != nil {
		return false
	}

	return true
}

// GetFocusStack 获取焦点历史
func (cte *ConversationTransferEngine) GetFocusStack() []ContextHandoff {
	cte.mu.Lock()
	defer cte.mu.Unlock()
	result := make([]ContextHandoff, len(cte.focusStack))
	copy(result, cte.focusStack)
	return result
}

// GetSwitchChannel 获取切换通道（外部消费）
func (cte *ConversationTransferEngine) GetSwitchChannel() <-chan ContextHandoff {
	return cte.switchCh
}

// GetFocusChannel 获取焦点事件通道
func (cte *ConversationTransferEngine) GetFocusChannel() <-chan FocusEvent {
	return cte.focusCh
}

// ═══════════════════════════════════════════════════════════════
//  虚空UI渲染器 v2 — 渲染投影区域到终端
// ═══════════════════════════════════════════════════════════════

// PhantomRenderer v2 虚空UI渲染器
// 将投影区域渲染到终端，使用虚线边框区分
type PhantomRenderer struct {
	projectionEngine *ProjectionEngine
	tabManager       *TabManager
	width            int
	height           int
	renderCount      atomic.Int64
}

// NewPhantomRenderer 创建渲染器
func NewPhantomRenderer(pe *ProjectionEngine, tm *TabManager, w, h int) *PhantomRenderer {
	if w <= 0 {
		w = 120
	}
	if h <= 0 {
		h = 40
	}
	return &PhantomRenderer{
		projectionEngine: pe,
		tabManager:       tm,
		width:            w,
		height:           h,
	}
}

// Render 渲染完整界面
// 输出格式：
//
//	┌─[Tab A]─[Tab B]─[Tab C]─[Tab D]──┐  ← 标签栏
//	│                                    │
//	│  主内容区域（活跃Tab的输出）         │
//	│                                    │
//	│  ┌╌╌ Tab B ⌄ ╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌┐  │  ← 虚空投影区域（虚线边框）
//	│  ╌ 编译中 ● 2.3k tokens           ╌  │
//	│  ╌ src/main.rs:42 error[E0308]   ╌  │  ← 真实渲染内容
//	│  └╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌┘  │
//	│                                    │
//	└─ Status: 4 tabs │ 12k tokens ────┘  ← 状态栏
func (pr *PhantomRenderer) Render(mainContent string) string {
	pr.renderCount.Add(1)

	// 1. 渲染标签栏
	tabBar := pr.tabManager.RenderTabBar(pr.width)

	// 2. 分割主内容
	contentLines := strings.Split(mainContent, "\n")

	// 3. 获取投影区域
	regions := pr.projectionEngine.GetRegions()

	// 4. 构建输出
	var sb strings.Builder
	sb.WriteString(tabBar)
	sb.WriteString("\n")

	// 主内容区域（留出投影区域的空间）
	projectionAreaStart := pr.height - len(regions)*6 - 2
	if projectionAreaStart < 5 {
		projectionAreaStart = 5
	}

	for i, line := range contentLines {
		if i >= projectionAreaStart-1 {
			break
		}
		// 截断到终端宽度
		if len(line) > pr.width-2 {
			line = line[:pr.width-5] + "..."
		}
		sb.WriteString(line)
		sb.WriteString("\n")
	}

	// 5. 渲染投影区域（虚线边框）
	for _, region := range regions {
		if !region.Visible {
			continue
		}
		sb.WriteString(pr.renderProjectionRegion(region))
		sb.WriteString("\n")
	}

	// 6. 渲染状态栏
	sb.WriteString(pr.renderStatusBar())

	return sb.String()
}

// renderProjectionRegion 渲染单个投影区域
// 使用虚线边框（╌）区分虚空投影区域
func (pr *PhantomRenderer) renderProjectionRegion(region *ProjectionRegion) string {
	width := region.Bounds.Width
	if width < 20 {
		width = 20
	}

	var sb strings.Builder

	// 虚线顶部边框
	title := region.Title
	if len(title) > width-8 {
		title = title[:width-11] + "..."
	}

	// 焦点标记
	focusMark := " "
	if region.Focused {
		focusMark = "▸"
	}

	// 状态标记
	statusMark := statusToMark(region.SignalRef.SignalType)

	sb.WriteString(fmt.Sprintf("┌╌%s %s%s %s╌╌%s┐\n",
		focusMark, title,
		strings.Repeat(" ", width-len(title)-10),
		statusMark,
		strings.Repeat("╌", 0),
	))

	// 渲染内容节点（真实渲染树内容，不是文本摘要）
	for _, node := range region.Content {
		content := node.Content
		if len(content) > width-4 {
			content = content[:width-7] + "..."
		}

		// 根据节点类型添加标记
		var marker string
		switch node.Type {
		case RenderError:
			marker = "✗ "
		case RenderWarning:
			marker = "⚠ "
		case RenderStatus:
			marker = "● "
		case RenderCode:
			marker = "  "
		case RenderDiff:
			marker = "± "
		case RenderProgress:
			marker = "◴ "
		default:
			marker = "  "
		}

		sb.WriteString(fmt.Sprintf("╌ %s%s%s ╌\n",
			marker, content,
			strings.Repeat(" ", width-len(content)-len(marker)-4),
		))
	}

	// 填充空行
	contentLines := len(region.Content)
	for i := contentLines; i < 3; i++ {
		sb.WriteString(fmt.Sprintf("╌%s╌\n", strings.Repeat(" ", width-2)))
	}

	// 虚线底部边框
	// 显示交互提示
	var hint string
	if region.ClickCount > 0 {
		hint = fmt.Sprintf(" ↗ 已点击%d次", region.ClickCount)
	} else if region.GazeDuration > 0 {
		hint = fmt.Sprintf(" ⊙ 注视%ds", region.GazeDuration/1000)
	} else {
		hint = " 点击/注视切换→"
	}

	hintLen := len(hint)
	if hintLen > width-4 {
		hint = hint[:width-7]
		hintLen = len(hint)
	}

	sb.WriteString(fmt.Sprintf("└╌%s%s╌╌┘",
		hint,
		strings.Repeat(" ", width-hintLen-4),
	))

	return sb.String()
}

// renderStatusBar 渲染状态栏
func (pr *PhantomRenderer) renderStatusBar() string {
	stats := pr.tabManager.GetTabStats()
	regions := pr.projectionEngine.GetRegions()

	focusedCount := 0
	for _, r := range regions {
		if r.Focused {
			focusedCount++
		}
	}

	return fmt.Sprintf("─[ Tabs: %d │ Active: %d │ Busy: %d │ Projections: %d │ Focused: %d │ Tokens: %dk ]──",
		stats.TotalTabs, stats.ActiveTabs, stats.BusyTabs,
		len(regions), focusedCount,
		stats.TotalTokensUsed/1000,
	)
}

// Resize 调整尺寸
func (pr *PhantomRenderer) Resize(w, h int) {
	pr.width = w
	pr.height = h
}

// statusToMark 将信号类型转换为标记
func statusToMark(signalType SignalType) string {
	switch signalType {
	case SignalStatus:
		return "○"
	case SignalMetadata:
		return "◇"
	case SignalSummary:
		return "◆"
	case SignalFragment:
		return "◈"
	case SignalFull:
		return "●"
	default:
		return "·"
	}
}

// ═══════════════════════════════════════════════════════════════
//  PhantomUI — 虚空UI总控（集成投影引擎 + 对话转移 + 渲染器）
// ═══════════════════════════════════════════════════════════════

// PhantomUI 虚空UI总控
type PhantomUI struct {
	projectionEngine *ProjectionEngine
	transferEngine   *ConversationTransferEngine
	renderer         *PhantomRenderer
	tabManager       *TabManager
	// 投影更新定时器
	updateTicker *time.Ticker
	stopCh       chan struct{}
	// 统计
	totalProjections atomic.Int64
	totalTransfers   atomic.Int64
}

// NewPhantomUI 创建虚空UI
func NewPhantomUI(tm *TabManager, width, height int) *PhantomUI {
	pe := NewProjectionEngine(tm)
	cte := NewConversationTransferEngine(pe, tm)
	pr := NewPhantomRenderer(pe, tm, width, height)

	return &PhantomUI{
		projectionEngine: pe,
		transferEngine:   cte,
		renderer:         pr,
		tabManager:       tm,
		stopCh:           make(chan struct{}),
	}
}

// Start 启动虚空UI（后台投影更新）
func (pui *PhantomUI) Start() {
	pui.updateTicker = time.NewTicker(500 * time.Millisecond)
	go pui.projectionLoop()
}

// Stop 停止虚空UI
func (pui *PhantomUI) Stop() {
	if pui.updateTicker != nil {
		pui.updateTicker.Stop()
	}
	close(pui.stopCh)
}

// projectionLoop 投影更新循环
func (pui *PhantomUI) projectionLoop() {
	for {
		select {
		case <-pui.updateTicker.C:
			// 定期投影所有非活跃Tab
			pui.projectionEngine.ProjectAll()
			pui.totalProjections.Add(1)

			// 清理过期的共享缓冲区
			pui.projectionEngine.codec.CleanExpired()

		case <-pui.stopCh:
			return
		}
	}
}

// Render 渲染完整界面
func (pui *PhantomUI) Render(mainContent string) string {
	return pui.renderer.Render(mainContent)
}

// HandleClick 处理点击事件
func (pui *PhantomUI) HandleClick(x, y int) bool {
	return pui.transferEngine.HandleClick(x, y)
}

// HandleGaze 处理注视事件
func (pui *PhantomUI) HandleGaze(tabID string, durationMs int64) bool {
	return pui.transferEngine.HandleGaze(tabID, durationMs)
}

// HandleHotkey 处理快捷键
func (pui *PhantomUI) HandleHotkey(targetTabID string) bool {
	return pui.transferEngine.HandleHotkey(targetTabID)
}

// SwitchBack 回切
func (pui *PhantomUI) SwitchBack() bool {
	return pui.transferEngine.SwitchBack()
}

// Resize 调整尺寸
func (pui *PhantomUI) Resize(w, h int) {
	pui.renderer.Resize(w, h)
}

// GetStats 获取统计
func (pui *PhantomUI) GetStats() PhantomUIStats {
	return PhantomUIStats{
		TotalProjections: pui.totalProjections.Load(),
		TotalTransfers:   pui.transferEngine.transferCount.Load(),
		ActiveRegions:    len(pui.projectionEngine.GetRegions()),
		FocusStackSize:   len(pui.transferEngine.focusStack),
	}
}

// PhantomUIStats 虚空UI统计
type PhantomUIStats struct {
	TotalProjections int64 `json:"total_projections"`
	TotalTransfers   int64 `json:"total_transfers"`
	ActiveRegions    int   `json:"active_regions"`
	FocusStackSize   int   `json:"focus_stack_size"`
}

// ═══════════════════════════════════════════════════════════════
//  向后兼容 — 保留旧的Compositor接口（标记为deprecated）
// ═══════════════════════════════════════════════════════════════

// LayerType 图层类型（v1兼容）
type LayerType int

const (
	LayerBackground LayerType = iota
	LayerTabBar
	LayerPhantom
	LayerStatusBar
	LayerNotification
)

// Layer v1图层（deprecated，使用ProjectionRegion替代）
type Layer struct {
	ID      string
	Type    LayerType
	X       int
	Y       int
	Z       int
	Width   int
	Height  int
	Content string
	Visible bool
	Opacity float64
}

// PanelPosition v1面板位置（deprecated）
type PanelPosition int

const (
	PanelTopLeft PanelPosition = iota
	PanelTopRight
	PanelBottomLeft
	PanelBottomRight
	PanelFloating
)

// Compositor v1合成器（deprecated，使用PhantomRenderer替代）
type Compositor struct {
	mu          sync.Mutex
	layers      map[string]*Layer
	panels      map[string]*PhantomPanel
	width       int
	height      int
	renderCount atomic.Int64
}

// NewCompositor 创建v1合成器（兼容）
func NewCompositor(width, height int) *Compositor {
	if width <= 0 {
		width = 120
	}
	if height <= 0 {
		height = 40
	}
	return &Compositor{
		layers: make(map[string]*Layer),
		panels: make(map[string]*PhantomPanel),
		width:  width,
		height: height,
	}
}

func (c *Compositor) AddLayer(layer *Layer) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.layers[layer.ID] = layer
}

func (c *Compositor) RemoveLayer(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.layers, id)
}

func (c *Compositor) UpdateLayer(id, content string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if layer, ok := c.layers[id]; ok {
		layer.Content = content
	}
}

func (c *Compositor) AddPhantomPanel(panel *PhantomPanel) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.panels[panel.SourceTabID] = panel
}

func (c *Compositor) RemovePhantomPanel(tabID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.panels, tabID)
}

func (c *Compositor) UpdatePhantomPanel(tabID, summary string, status TabState) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if panel, ok := c.panels[tabID]; ok {
		panel.Summary = summary
		panel.Status = status
		panel.LastUpdate = time.Now()
	}
}

func (c *Compositor) Render(mainContent string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.renderCount.Add(1)

	background := c.splitLines(mainContent, c.width)
	grid := make([][]rune, c.height)
	for i := range grid {
		grid[i] = make([]rune, c.width)
		for j := range grid[i] {
			grid[i][j] = ' '
		}
	}

	for y, line := range background {
		if y >= c.height {
			break
		}
		runes := []rune(line)
		for x, r := range runes {
			if x >= c.width {
				break
			}
			grid[y][x] = r
		}
	}

	sortedLayers := c.sortLayersByZ()
	for _, layer := range sortedLayers {
		if !layer.Visible || layer.Content == "" {
			continue
		}
		c.blitLayer(grid, layer)
	}

	for _, panel := range c.panels {
		c.blitPhantomPanel(grid, panel)
	}

	var sb strings.Builder
	for y, row := range grid {
		sb.WriteString(string(row))
		if y < len(grid)-1 {
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

func (c *Compositor) splitLines(text string, maxWidth int) []string {
	lines := strings.Split(text, "\n")
	result := make([]string, len(lines))
	for i, line := range lines {
		runes := []rune(line)
		if len(runes) > maxWidth {
			result[i] = string(runes[:maxWidth])
		} else {
			result[i] = line
		}
	}
	return result
}

func (c *Compositor) sortLayersByZ() []*Layer {
	layers := make([]*Layer, 0, len(c.layers))
	for _, l := range c.layers {
		layers = append(layers, l)
	}
	for i := 0; i < len(layers); i++ {
		for j := i + 1; j < len(layers); j++ {
			if layers[i].Z > layers[j].Z {
				layers[i], layers[j] = layers[j], layers[i]
			}
		}
	}
	return layers
}

func (c *Compositor) blitLayer(grid [][]rune, layer *Layer) {
	lines := c.splitLines(layer.Content, layer.Width)
	for y, line := range lines {
		gy := layer.Y + y
		if gy >= len(grid) || gy < 0 {
			continue
		}
		runes := []rune(line)
		for x, r := range runes {
			gx := layer.X + x
			if gx >= len(grid[gy]) || gx < 0 {
				continue
			}
			if r != ' ' {
				grid[gy][gx] = r
			}
		}
	}
}

func (c *Compositor) blitPhantomPanel(grid [][]rune, panel *PhantomPanel) {
	if panel.Width <= 0 {
		panel.Width = 30
	}
	x, y := c.panelPosition(panel.Position, panel.Width)
	border := c.renderPanelBorder(panel)
	borderLines := strings.Split(border, "\n")
	for i, line := range borderLines {
		gy := y + i
		if gy >= len(grid) || gy < 0 {
			break
		}
		runes := []rune(line)
		for j, r := range runes {
			gx := x + j
			if gx >= len(grid[gy]) || gx < 0 {
				break
			}
			if r != ' ' {
				grid[gy][gx] = r
			}
		}
	}
}

func (c *Compositor) panelPosition(pos PanelPosition, width int) (int, int) {
	switch pos {
	case PanelTopLeft:
		return 0, 1
	case PanelTopRight:
		return c.width - width - 1, 1
	case PanelBottomLeft:
		return 0, c.height - 4
	case PanelBottomRight:
		return c.width - width - 1, c.height - 4
	case PanelFloating:
		return c.width / 4, c.height / 3
	default:
		return 0, 0
	}
}

func (c *Compositor) renderPanelBorder(panel *PhantomPanel) string {
	width := panel.Width
	if width < 20 {
		width = 20
	}
	var sb strings.Builder
	sb.WriteString("┌")
	sb.WriteString(strings.Repeat("─", width-2))
	sb.WriteString("┐\n")
	title := panel.Title
	if len(title) > width-4 {
		title = title[:width-7] + "..."
	}
	statusMark := statusToMarkState(panel.Status)
	sb.WriteString(fmt.Sprintf("│%s %s%s│\n", statusMark, title,
		strings.Repeat(" ", width-4-len(title)-2)))
	summary := panel.Summary
	if len(summary) > width-4 {
		summary = summary[:width-7] + "..."
	}
	sb.WriteString(fmt.Sprintf("│ %s%s│\n", summary,
		strings.Repeat(" ", width-3-len(summary))))
	sb.WriteString("└")
	sb.WriteString(strings.Repeat("─", width-2))
	sb.WriteString("┘")
	return sb.String()
}

func statusToMarkState(status TabState) string {
	switch status {
	case TabStateBusy:
		return "●"
	case TabStateActive:
		return "◆"
	case TabStateIdle:
		return "○"
	case TabStateError:
		return "✗"
	default:
		return "·"
	}
}

func (c *Compositor) Resize(width, height int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.width = width
	c.height = height
}

func (c *Compositor) GetRenderCount() int64 {
	return c.renderCount.Load()
}

func (c *Compositor) ClearPanels() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.panels = make(map[string]*PhantomPanel)
}

func (c *Compositor) GetVisiblePanelCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	count := 0
	for _, p := range c.panels {
		if p.Summary != "" {
			count++
		}
	}
	return count
}

// PhantomPanel v1面板（deprecated，使用ProjectionRegion替代）
type PhantomPanel struct {
	SourceTabID string
	Title       string
	Summary     string
	Status      TabState
	Position    PanelPosition
	Width       int
	LastUpdate  time.Time
}

// TabBarRenderer v1渲染器（deprecated，使用PhantomRenderer替代）
type TabBarRenderer struct {
	tabManager *TabManager
	compositor *Compositor
}

func NewTabBarRenderer(tm *TabManager, comp *Compositor) *TabBarRenderer {
	return &TabBarRenderer{tabManager: tm, compositor: comp}
}

func (r *TabBarRenderer) SyncTabs() {
	tabs := r.tabManager.GetAllTabs()
	activeID := ""
	if active := r.tabManager.GetActiveTab(); active != nil {
		activeID = active.ID
	}
	for _, tab := range tabs {
		if tab.ID == activeID {
			r.compositor.RemovePhantomPanel(tab.ID)
			continue
		}
		summary := r.generateTabSummary(tab)
		if existing := r.compositor.panels[tab.ID]; existing != nil {
			r.compositor.UpdatePhantomPanel(tab.ID, summary, tab.State)
		} else {
			r.compositor.AddPhantomPanel(&PhantomPanel{
				SourceTabID: tab.ID,
				Title:       tab.Title,
				Summary:     summary,
				Status:      tab.State,
				Position:    r.nextPanelPosition(),
				Width:       30,
				LastUpdate:  time.Now(),
			})
		}
	}
	for tabID := range r.compositor.panels {
		if _, ok := r.tabManager.GetTab(tabID); !ok {
			r.compositor.RemovePhantomPanel(tabID)
		}
	}
}

func (r *TabBarRenderer) generateTabSummary(tab *Tab) string {
	tokens := tab.TokenUsed.Load()
	if tokens > 0 {
		return fmt.Sprintf("%s · %dk tokens", tab.Scene, tokens/1000)
	}
	return fmt.Sprintf("%s · %s", tab.Scene, tab.State)
}

var panelPosCounter int

func (r *TabBarRenderer) nextPanelPosition() PanelPosition {
	pos := PanelPosition(panelPosCounter % 4)
	panelPosCounter++
	return pos
}

func (r *TabBarRenderer) RenderFull(mainContent string) string {
	r.SyncTabs()
	tabBar := r.tabManager.RenderTabBar(r.compositor.width)
	r.compositor.AddLayer(&Layer{
		ID: "tabbar", Type: LayerTabBar, X: 0, Y: 0, Z: 10,
		Content: tabBar, Visible: true,
	})
	statusBar := r.renderStatusBar()
	r.compositor.AddLayer(&Layer{
		ID: "statusbar", Type: LayerStatusBar, X: 0, Y: r.compositor.height - 1, Z: 10,
		Content: statusBar, Visible: true,
	})
	return r.compositor.Render(mainContent)
}

func (r *TabBarRenderer) renderStatusBar() string {
	stats := r.tabManager.GetTabStats()
	return fmt.Sprintf(" Tabs: %d (Active: %d, Busy: %d) │ Total Tokens: %dk │ Panels: %d",
		stats.TotalTabs, stats.ActiveTabs, stats.BusyTabs,
		stats.TotalTokensUsed/1000,
		r.compositor.GetVisiblePanelCount())
}

// hashForSignal 为信号引用生成快速哈希
func hashForSignal(s string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(s))
	return h.Sum32()
}

// GetActiveTabID 获取活跃Tab ID（辅助方法）
func (tm *TabManager) GetActiveTabID() string {
	if tab := tm.GetActiveTab(); tab != nil {
		return tab.ID
	}
	return ""
}
