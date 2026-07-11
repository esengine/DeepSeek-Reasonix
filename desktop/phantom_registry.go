package main

import (
	"context"
	"log/slog"
	"sort"
	"sync"
	"time"
)

// ── A2 设计修订五：虚空 UI（Phantom Panel） ──
// PhantomRegistry 是零 token 更新中心。
// 所有 Session 状态变更通过 Go channel 异步更新，不经过 LLM。
// 前端通过 Wails 事件流接收更新。

// PhantomStatus 虚空 UI 条目状态
type PhantomStatus int

const (
	PhantomActive   PhantomStatus = iota // ● 活跃（正在执行 turn）
	PhantomIdle                          // ○ 空闲
	PhantomFailed                        // ✗ 失败
	PhantomWaiting                       // ⏳ 等待
	PhantomArchived                      // 📦 已归档
)

func (s PhantomStatus) String() string {
	switch s {
	case PhantomActive:
		return "active"
	case PhantomIdle:
		return "idle"
	case PhantomFailed:
		return "failed"
	case PhantomWaiting:
		return "waiting"
	case PhantomArchived:
		return "archived"
	default:
		return "unknown"
	}
}

func (s PhantomStatus) Icon() string {
	switch s {
	case PhantomActive:
		return "●"
	case PhantomIdle:
		return "○"
	case PhantomFailed:
		return "✗"
	case PhantomWaiting:
		return "⏳"
	case PhantomArchived:
		return "📦"
	default:
		return "?"
	}
}

// IsolationLevel 隔离级别（影响虚空 UI 显示内容）
type IsolationLevel int

const (
	IsolationSandbox  IsolationLevel = iota // 沙盒隔离（只显示状态）
	IsolationZoned                          // 区域隔离（显示元数据）
	IsolationObserved                       // 观察隔离（显示摘要）
	IsolationMerged                         // 合并（显示完整结论）
)

func (l IsolationLevel) String() string {
	switch l {
	case IsolationSandbox:
		return "sandbox"
	case IsolationZoned:
		return "zoned"
	case IsolationObserved:
		return "observed"
	case IsolationMerged:
		return "merged"
	default:
		return "unknown"
	}
}

// CommType 交流类型（代价递增）
type CommType int

const (
	CommSignal      CommType = iota // 信号（零代价）
	CommMetadata                    // 元数据（极低）
	CommSummary                     // 摘要（低）
	CommFragment                    // 片段（中）
	CommFull                        // 完整（高）
	CommNotify                      // 通知（零）
	CommQuery                       // 查询（低）
)

func (c CommType) String() string {
	switch c {
	case CommSignal:
		return "signal"
	case CommMetadata:
		return "metadata"
	case CommSummary:
		return "summary"
	case CommFragment:
		return "fragment"
	case CommFull:
		return "full"
	case CommNotify:
		return "notify"
	case CommQuery:
		return "query"
	default:
		return "unknown"
	}
}

// PhantomConclusion 结论摘要（从 Session 的上下文投影中提取）
type PhantomConclusion struct {
	Summary    string    `json:"summary"`    // 一行摘要
	Status     string    `json:"status"`     // "已就绪" / "等待中" / "编译错误" 等
	Confidence float64   `json:"confidence"` // 置信度
	Timestamp  time.Time `json:"timestamp"`
	SourceTurn int       `json:"sourceTurn"` // 来自第几个 turn
}

// CommBadge 交流提示徽章
type CommBadge struct {
	PendingCount int       `json:"pendingCount"` // 待处理交流数
	TotalCount   int       `json:"totalCount"`   // 累计交流数
	SentCount    int       `json:"sentCount"`    // 发出的交流数
	RecvCount    int       `json:"recvCount"`    // 收到的交流数
	LastCommType CommType  `json:"lastCommType"`
	LastCommTime time.Time `json:"lastCommTime"`
	Unread       bool      `json:"unread"` // 是否有未读交流
}

// JumpTarget 跳转目标
type JumpTarget struct {
	TabID    string `json:"tabId"`    // 目标标签页 ID
	TopicID  string `json:"topicId"`  // 目标话题 ID
	ScrollPos int    `json:"scrollPos"` // 跳转后滚动位置
}

// PhantomEntry 虚空 UI 的一条条目
type PhantomEntry struct {
	SessionID      string            `json:"sessionId"`
	Name           string            `json:"name"`           // 显示名称（按此字段排序）
	WorkspaceRoot  string            `json:"workspaceRoot"`  // 工作区根目录
	Status         PhantomStatus     `json:"status"`         // 活跃/空闲/失败/等待
	Conclusion     *PhantomConclusion `json:"conclusion"`    // 最近结论摘要
	CommBadge      CommBadge         `json:"commBadge"`      // 交流提示徽章
	IsolationLevel IsolationLevel    `json:"isolationLevel"` // 当前隔离级别
	LastUpdate     time.Time         `json:"lastUpdate"`     // 最后更新时间
	JumpTarget     JumpTarget        `json:"jumpTarget"`     // 点击跳转目标
	TurnCount      int               `json:"turnCount"`      // 当前 turn 数
}

// PhantomUpdate 更新事件（零 token，纯程序化）
type PhantomUpdate struct {
	SessionID string      `json:"sessionId"`
	Type      string      `json:"type"` // status|conclusion|comm|isolation|added|removed
	Entry     *PhantomEntry `json:"entry,omitempty"`
}

// PhantomRegistry 管理所有虚空 UI 条目
// 更新通过 Go channel 异步完成，不消耗 token
type PhantomRegistry struct {
	mu          sync.RWMutex
	entries     map[string]*PhantomEntry // SessionID → Entry
	updates     chan PhantomUpdate        // 更新通道（带缓冲）
	subscribers []chan PhantomUpdate      // UI 订阅者
	subMu       sync.RWMutex
	ctx         context.Context
	cancel      context.CancelFunc
}

// NewPhantomRegistry 创建虚空 UI 注册中心
func NewPhantomRegistry() *PhantomRegistry {
	ctx, cancel := context.WithCancel(context.Background())
	r := &PhantomRegistry{
		entries: make(map[string]*PhantomEntry),
		updates: make(chan PhantomUpdate, 256), // 带缓冲，避免阻塞 Session
		ctx:     ctx,
		cancel:  cancel,
	}
	go r.processUpdates()
	return r
}

// Stop 停止注册中心的后台 goroutine
func (r *PhantomRegistry) Stop() {
	r.cancel()
}

// Register 注册一个新 Session 到虚空 UI
func (r *PhantomRegistry) Register(sessionID, name, workspaceRoot string, tabID string) {
	entry := &PhantomEntry{
		SessionID:      sessionID,
		Name:           name,
		WorkspaceRoot:  workspaceRoot,
		Status:         PhantomIdle,
		IsolationLevel: IsolationObserved,
		LastUpdate:     time.Now(),
		JumpTarget:     JumpTarget{TabID: tabID},
	}
	r.mu.Lock()
	r.entries[sessionID] = entry
	r.mu.Unlock()

	r.enqueue(PhantomUpdate{
		SessionID: sessionID,
		Type:      "added",
		Entry:     entry,
	})
}

// Unregister 从虚空 UI 移除一个 Session
func (r *PhantomRegistry) Unregister(sessionID string) {
	r.mu.Lock()
	delete(r.entries, sessionID)
	r.mu.Unlock()

	r.enqueue(PhantomUpdate{
		SessionID: sessionID,
		Type:      "removed",
	})
}

// UpdateStatus 更新 Session 状态（零 token）
func (r *PhantomRegistry) UpdateStatus(sessionID string, status PhantomStatus, turnCount int) {
	r.mu.Lock()
	if entry, ok := r.entries[sessionID]; ok {
		entry.Status = status
		entry.TurnCount = turnCount
		entry.LastUpdate = time.Now()
		r.mu.Unlock()
		r.enqueue(PhantomUpdate{
			SessionID: sessionID,
			Type:      "status",
			Entry:     entry,
		})
	} else {
		r.mu.Unlock()
	}
}

// UpdateConclusion 更新 Session 结论（零 token）
// summary 是规则化提取的一行摘要，不调用 LLM
func (r *PhantomRegistry) UpdateConclusion(sessionID, summary, status string, turnCount int) {
	r.mu.Lock()
	if entry, ok := r.entries[sessionID]; ok {
		entry.Conclusion = &PhantomConclusion{
			Summary:    summary,
			Status:     status,
			Confidence: 1.0,
			Timestamp:  time.Now(),
			SourceTurn: turnCount,
		}
		entry.TurnCount = turnCount
		entry.LastUpdate = time.Now()
		r.mu.Unlock()
		r.enqueue(PhantomUpdate{
			SessionID: sessionID,
			Type:      "conclusion",
			Entry:     entry,
		})
	} else {
		r.mu.Unlock()
	}
}

// IncrementComm 交流发生时更新计数（零 token）
func (r *PhantomRegistry) IncrementComm(sessionID string, commType CommType, sent bool) {
	r.mu.Lock()
	if entry, ok := r.entries[sessionID]; ok {
		entry.CommBadge.TotalCount++
		if sent {
			entry.CommBadge.SentCount++
		} else {
			entry.CommBadge.RecvCount++
			entry.CommBadge.PendingCount++
			entry.CommBadge.Unread = true
		}
		entry.CommBadge.LastCommType = commType
		entry.CommBadge.LastCommTime = time.Now()
		entry.LastUpdate = time.Now()
		r.mu.Unlock()
		r.enqueue(PhantomUpdate{
			SessionID: sessionID,
			Type:      "comm",
			Entry:     entry,
		})
	} else {
		r.mu.Unlock()
	}
}

// MarkCommRead 标记交流为已读
func (r *PhantomRegistry) MarkCommRead(sessionID string) {
	r.mu.Lock()
	if entry, ok := r.entries[sessionID]; ok {
		entry.CommBadge.Unread = false
		entry.CommBadge.PendingCount = 0
		entry.LastUpdate = time.Now()
		r.mu.Unlock()
		r.enqueue(PhantomUpdate{
			SessionID: sessionID,
			Type:      "comm",
			Entry:     entry,
		})
	} else {
		r.mu.Unlock()
	}
}

// UpdateIsolation 更新隔离级别
func (r *PhantomRegistry) UpdateIsolation(sessionID string, level IsolationLevel) {
	r.mu.Lock()
	if entry, ok := r.entries[sessionID]; ok {
		entry.IsolationLevel = level
		entry.LastUpdate = time.Now()
		r.mu.Unlock()
		r.enqueue(PhantomUpdate{
			SessionID: sessionID,
			Type:      "isolation",
			Entry:     entry,
		})
	} else {
		r.mu.Unlock()
	}
}

// GetEntries 返回所有条目，按 Name 排序
func (r *PhantomRegistry) GetEntries() []*PhantomEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	entries := make([]*PhantomEntry, 0, len(r.entries))
	for _, e := range r.entries {
		// 根据隔离级别过滤显示内容
		visible := r.filterByIsolation(e)
		entries = append(entries, visible)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name < entries[j].Name
	})
	return entries
}

// GetEntry 返回单个条目
func (r *PhantomRegistry) GetEntry(sessionID string) *PhantomEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if entry, ok := r.entries[sessionID]; ok {
		return r.filterByIsolation(entry)
	}
	return nil
}

// Subscribe 订阅虚空 UI 更新（供前端事件流使用）
func (r *PhantomRegistry) Subscribe() <-chan PhantomUpdate {
	ch := make(chan PhantomUpdate, 64)
	r.subMu.Lock()
	r.subscribers = append(r.subscribers, ch)
	r.subMu.Unlock()
	return ch
}

// Unsubscribe 取消订阅
func (r *PhantomRegistry) Unsubscribe(ch <-chan PhantomUpdate) {
	r.subMu.Lock()
	defer r.subMu.Unlock()
	for i, sub := range r.subscribers {
		if sub == ch {
			r.subscribers = append(r.subscribers[:i], r.subscribers[i+1:]...)
			close(sub)
			return
		}
	}
}

// filterByIsolation 根据隔离级别过滤显示内容
func (r *PhantomRegistry) filterByIsolation(entry *PhantomEntry) *PhantomEntry {
	// 深拷贝
	visible := *entry
	switch entry.IsolationLevel {
	case IsolationSandbox:
		// 只显示状态，隐藏结论
		visible.Conclusion = nil
		visible.CommBadge.TotalCount = 0
		visible.CommBadge.PendingCount = 0
	case IsolationZoned:
		// 显示元数据标签，隐藏结论详情
		if visible.Conclusion != nil {
			visible.Conclusion.Summary = "" // 隐藏摘要内容
		}
	case IsolationObserved:
		// 显示摘要预览（截断到 50 字符）
		if visible.Conclusion != nil && len(visible.Conclusion.Summary) > 50 {
			visible.Conclusion.Summary = visible.Conclusion.Summary[:50] + "..."
		}
	case IsolationMerged:
		// 显示完整结论
	}
	return &visible
}

// enqueue 将更新放入通道（非阻塞，队列满时丢弃并记录日志）
func (r *PhantomRegistry) enqueue(update PhantomUpdate) {
	select {
	case r.updates <- update:
	default:
		slog.Warn("phantom registry update channel full, dropping update",
			"sessionID", update.SessionID, "type", update.Type)
	}
}

// processUpdates 后台 goroutine 处理更新（不阻塞任何 Session）
func (r *PhantomRegistry) processUpdates() {
	for {
		select {
		case <-r.ctx.Done():
			return
		case update := <-r.updates:
			r.notifySubscribers(update)
		}
	}
}

// notifySubscribers 通知所有订阅者
func (r *PhantomRegistry) notifySubscribers(update PhantomUpdate) {
	r.subMu.RLock()
	defer r.subMu.RUnlock()
	for _, ch := range r.subscribers {
		select {
		case ch <- update:
		default:
			// 订阅者通道满，跳过（前端可能不活跃）
		}
	}
}
