package bot

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	QueueModeSteer     = "steer"
	QueueModeFollowup  = "followup"
	QueueModeCollect   = "collect"
	QueueModeInterrupt = "interrupt"

	QueueDropSummarize = "summarize"
	QueueDropOld       = "old"
	QueueDropNew       = "new"

	DefaultQueueCap = 20
)

type QueueOptions struct {
	Mode string
	Cap  int
	Drop string
}

type QueueResult struct {
	Acquired bool
	Queued   bool
	Rejected bool
	Dropped  bool
	Pending  int
	Mode     string
}

type QueueSnapshot struct {
	Active   int
	Pending  int
	Dropped  int
	Sessions int
}

func NormalizeQueueMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case QueueModeSteer:
		return QueueModeSteer
	case QueueModeFollowup:
		return QueueModeFollowup
	case QueueModeCollect:
		return QueueModeCollect
	case QueueModeInterrupt:
		return QueueModeInterrupt
	default:
		return QueueModeSteer
	}
}

func NormalizeOptionalQueueMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case QueueModeSteer:
		return QueueModeSteer
	case QueueModeFollowup:
		return QueueModeFollowup
	case QueueModeCollect:
		return QueueModeCollect
	case QueueModeInterrupt:
		return QueueModeInterrupt
	default:
		return ""
	}
}

func NormalizeQueueDrop(drop string) string {
	switch strings.ToLower(strings.TrimSpace(drop)) {
	case QueueDropOld:
		return QueueDropOld
	case QueueDropNew:
		return QueueDropNew
	default:
		return QueueDropSummarize
	}
}

// BuildSessionKey 根据 Hermes 模式生成稳定的 session key：
//   - DM：按 chat 隔离（同一 DM 会话共享历史）
//   - 群聊：按 user 隔离（每人独立会话）
//   - thread：共享（thread 内所有人共享上下文）
func BuildSessionKey(src SessionSource) string {
	var scope string
	source := sessionSourceID(src)
	switch src.ChatType {
	case ChatDM:
		scope = fmt.Sprintf("%s:dm:%s", source, src.ChatID)
	case ChatGroup:
		scope = fmt.Sprintf("%s:group:%s:%s", source, src.ChatID, src.UserID)
	case ChatGuild:
		scope = fmt.Sprintf("%s:guild:%s:%s", source, src.ChatID, src.UserID)
	case ChatDirect:
		scope = fmt.Sprintf("%s:direct:%s", source, src.ChatID)
	case ChatThread:
		threadID := src.ThreadID
		if threadID == "" {
			threadID = src.ChatID
		}
		scope = fmt.Sprintf("%s:thread:%s", source, threadID)
	default:
		scope = fmt.Sprintf("%s:%s:%s:%s", source, src.ChatType, src.ChatID, src.UserID)
	}
	h := sha256.Sum256([]byte(scope))
	return hex.EncodeToString(h[:])[:16]
}

func sessionSourceID(src SessionSource) string {
	if src.ConnectionID != "" {
		return src.ConnectionID
	}
	if src.Domain != "" {
		return fmt.Sprintf("%s:%s", src.Platform, src.Domain)
	}
	return string(src.Platform)
}

// slashCommands 是绕过忙碌队列的命令集合。
var slashCommands = map[string]bool{
	"/stop":     true,
	"/new":      true,
	"/reset":    true,
	"/approve":  true,
	"/deny":     true,
	"/answer":   true,
	"/yolo":     true,
	"/mode":     true,
	"/queue":    true,
	"/projects": true,
	"/use":      true,
	"/sessions": true,
	"/attach":   true,
	"/search":   true,
	"/desktop":  true,
	"/status":   true,
	"/help":     true,
}

// IsSlashBypass 判断消息是否为绕过队列的斜杠命令。
func IsSlashBypass(text string) bool {
	if len(text) == 0 {
		return false
	}
	cmd := text
	for i, r := range text {
		if r == ' ' {
			cmd = text[:i]
			break
		}
	}
	return slashCommands[cmd]
}

// pendingTurn 是等待执行的一轮对话。
type pendingTurn struct {
	msg       InboundMessage
	timestamp time.Time
	mode      string
}

// SessionManager 管理 session 级别的并发控制：同一 session 同时只跑一个任务。
type SessionManager struct {
	mu            sync.Mutex
	active        map[string]bool          // session key -> 是否正在运行
	pending       map[string][]pendingTurn // session key -> 等待队列
	debounce      time.Duration
	modeOverrides map[string]string
	dropped       map[string][]string
}

// NewSessionManager 创建一个新的 session 管理器。debounce 是消息合并窗口。
func NewSessionManager(debounce time.Duration) *SessionManager {
	if debounce <= 0 {
		debounce = 1500 * time.Millisecond
	}
	return &SessionManager{
		active:        make(map[string]bool),
		pending:       make(map[string][]pendingTurn),
		debounce:      debounce,
		modeOverrides: make(map[string]string),
		dropped:       make(map[string][]string),
	}
}

// TryAcquire 尝试获取 session 锁。如果 session 正忙且消息非绕过命令，返回 false。
// 返回 (acquired, merged) — merged 为 true 表示消息已合并到等待队列。
func (sm *SessionManager) TryAcquire(key string, msg InboundMessage) (acquired bool, merged bool) {
	result := sm.TryAcquireWithQueue(key, msg, QueueOptions{Mode: QueueModeCollect, Cap: DefaultQueueCap, Drop: QueueDropSummarize})
	return result.Acquired, result.Queued
}

func (sm *SessionManager) TryAcquireWithQueue(key string, msg InboundMessage, opts QueueOptions) QueueResult {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	mode := NormalizeQueueMode(opts.Mode)
	if mode == QueueModeSteer || mode == QueueModeInterrupt {
		mode = QueueModeFollowup
	}
	cap := opts.Cap
	if cap <= 0 {
		cap = DefaultQueueCap
	}
	drop := NormalizeQueueDrop(opts.Drop)

	if sm.active[key] {
		// 绕过命令立即返回 true（让调用方直接处理）
		if IsSlashBypass(msg.Text) {
			return QueueResult{Acquired: true, Mode: mode}
		}
		queue := sm.pending[key]
		if len(queue) >= cap {
			switch drop {
			case QueueDropNew:
				return QueueResult{Rejected: true, Pending: len(queue), Mode: mode}
			case QueueDropOld, QueueDropSummarize:
				removed := queue[0]
				queue = queue[1:]
				if drop == QueueDropSummarize {
					sm.dropped[key] = append(sm.dropped[key], queueSummary(removed.msg.Text))
				}
			}
		}
		if mode == QueueModeCollect && len(queue) > 0 {
			last := &queue[len(queue)-1]
			if msg.Text != "" && time.Since(last.timestamp) < sm.debounce {
				if last.msg.Text != "" {
					last.msg.Text = last.msg.Text + "\n" + msg.Text
				} else {
					last.msg.Text = msg.Text
				}
				last.timestamp = time.Now()
				last.mode = mode
				sm.pending[key] = queue
				return QueueResult{Queued: true, Dropped: len(sm.dropped[key]) > 0, Pending: len(queue), Mode: mode}
			}
		}
		queue = append(queue, pendingTurn{msg: msg, timestamp: time.Now(), mode: mode})
		sm.pending[key] = queue
		return QueueResult{Queued: true, Dropped: len(sm.dropped[key]) > 0, Pending: len(queue), Mode: mode}
	}

	sm.active[key] = true
	return QueueResult{Acquired: true, Mode: mode}
}

func (sm *SessionManager) ReplacePending(key string, msg InboundMessage) QueueResult {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if !sm.active[key] {
		sm.active[key] = true
		return QueueResult{Acquired: true, Mode: QueueModeInterrupt}
	}
	sm.pending[key] = []pendingTurn{{msg: msg, timestamp: time.Now(), mode: QueueModeFollowup}}
	delete(sm.dropped, key)
	return QueueResult{Queued: true, Pending: 1, Mode: QueueModeInterrupt}
}

// Release 释放 session 锁，返回等待队列中的下一条消息（合并后）。
func (sm *SessionManager) Release(key string) *InboundMessage {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	queue := sm.pending[key]
	if len(queue) == 0 {
		delete(sm.active, key)
		delete(sm.pending, key)
		delete(sm.dropped, key)
		return nil
	}

	mode := NormalizeQueueMode(queue[0].mode)
	var merged *InboundMessage
	if mode == QueueModeFollowup {
		m := queue[0].msg
		merged = &m
		if len(queue) == 1 {
			delete(sm.pending, key)
		} else {
			sm.pending[key] = queue[1:]
		}
		merged.Text = sm.consumeDroppedPrefixLocked(key, merged.Text)
		return merged
	}

	// collect 模式取出等待队列，并合并其中所有消息。
	for i := range queue {
		if merged == nil {
			m := queue[i].msg
			merged = &m
		} else {
			if queue[i].msg.Text != "" {
				merged.Text = merged.Text + "\n" + queue[i].msg.Text
			}
		}
	}
	delete(sm.pending, key)
	merged.Text = sm.consumeDroppedPrefixLocked(key, merged.Text)
	// active 保持 true，因为调用方会立即用 merged 消息开始新 turn
	return merged
}

func (sm *SessionManager) consumeDroppedPrefixLocked(key, text string) string {
	dropped := sm.dropped[key]
	if len(dropped) == 0 {
		return text
	}
	delete(sm.dropped, key)
	var b strings.Builder
	fmt.Fprintf(&b, "[Queue note: %d older pending message(s) were dropped because this bot session reached its queue cap.", len(dropped))
	if len(dropped) > 0 {
		b.WriteString(" Dropped summaries:")
		limit := len(dropped)
		if limit > 3 {
			limit = 3
		}
		for i := 0; i < limit; i++ {
			fmt.Fprintf(&b, "\n- %s", dropped[i])
		}
		if len(dropped) > limit {
			fmt.Fprintf(&b, "\n- ... and %d more", len(dropped)-limit)
		}
	}
	b.WriteString("]\n\n")
	b.WriteString(text)
	return b.String()
}

func queueSummary(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return "(empty message)"
	}
	runes := []rune(text)
	if len(runes) <= 180 {
		return text
	}
	return string(runes[:180]) + "..."
}

// IsActive 返回 session 是否有正在运行的任务。
func (sm *SessionManager) IsActive(key string) bool {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	return sm.active[key]
}

// ActiveCount 返回当前活跃 session 数。
func (sm *SessionManager) ActiveCount() int {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	return len(sm.active)
}

func (sm *SessionManager) PendingCount(key string) int {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	return len(sm.pending[key])
}

func (sm *SessionManager) Snapshot() QueueSnapshot {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	var pending int
	var dropped int
	for _, queue := range sm.pending {
		pending += len(queue)
	}
	for _, summaries := range sm.dropped {
		dropped += len(summaries)
	}
	return QueueSnapshot{
		Active:   len(sm.active),
		Pending:  pending,
		Dropped:  dropped,
		Sessions: len(sm.active) + len(sm.pending),
	}
}

func (sm *SessionManager) QueueMode(key, fallback string) string {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if mode := sm.modeOverrides[key]; mode != "" {
		return mode
	}
	return NormalizeQueueMode(fallback)
}

func (sm *SessionManager) SetQueueMode(key, mode string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if normalized := NormalizeOptionalQueueMode(mode); normalized != "" {
		sm.modeOverrides[key] = normalized
	}
}

func (sm *SessionManager) ClearQueueMode(key string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	delete(sm.modeOverrides, key)
}

// ForceRelease 强制释放 session（用于 session 关闭或错误恢复）。
func (sm *SessionManager) ForceRelease(key string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	delete(sm.active, key)
	delete(sm.pending, key)
	delete(sm.dropped, key)
}
