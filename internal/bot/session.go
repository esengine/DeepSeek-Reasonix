package bot

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// BuildSessionKey 根据 Hermes 模式生成稳定的 session key：
//   - DM：按 chat 隔离（同一 DM 会话共享历史）
//   - 群聊：按 user 隔离（每人独立会话）
//   - thread：共享（thread 内所有人共享上下文）
func BuildSessionKey(src SessionSource) string {
	var scope string
	switch src.ChatType {
	case ChatDM:
		scope = fmt.Sprintf("%s:dm:%s", src.Platform, src.ChatID)
	case ChatGroup:
		scope = fmt.Sprintf("%s:group:%s:%s", src.Platform, src.ChatID, src.UserID)
	case ChatGuild:
		scope = fmt.Sprintf("%s:guild:%s:%s", src.Platform, src.ChatID, src.UserID)
	case ChatDirect:
		scope = fmt.Sprintf("%s:direct:%s", src.Platform, src.ChatID)
	case ChatThread:
		threadID := src.ThreadID
		if threadID == "" {
			threadID = src.ChatID
		}
		scope = fmt.Sprintf("%s:thread:%s", src.Platform, threadID)
	default:
		scope = fmt.Sprintf("%s:%s:%s:%s", src.Platform, src.ChatType, src.ChatID, src.UserID)
	}
	h := sha256.Sum256([]byte(scope))
	return hex.EncodeToString(h[:])[:16]
}

// slashCommands 是绕过忙碌队列的命令集合。
var slashCommands = map[string]bool{
	"/stop":    true,
	"/new":     true,
	"/reset":   true,
	"/approve": true,
	"/deny":    true,
	"/answer":  true,
	"/status":  true,
	"/help":    true,
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
}

// SessionManager 管理 session 级别的并发控制：同一 session 同时只跑一个任务。
type SessionManager struct {
	mu       sync.Mutex
	active   map[string]bool          // session key -> 是否正在运行
	pending  map[string][]pendingTurn // session key -> 等待队列
	debounce time.Duration
}

// NewSessionManager 创建一个新的 session 管理器。debounce 是消息合并窗口。
func NewSessionManager(debounce time.Duration) *SessionManager {
	if debounce <= 0 {
		debounce = 1500 * time.Millisecond
	}
	return &SessionManager{
		active:   make(map[string]bool),
		pending:  make(map[string][]pendingTurn),
		debounce: debounce,
	}
}

// TryAcquire 尝试获取 session 锁。如果 session 正忙且消息非绕过命令，返回 false。
// 返回 (acquired, merged) — merged 为 true 表示消息已合并到等待队列。
func (sm *SessionManager) TryAcquire(key string, msg InboundMessage) (acquired bool, merged bool) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.active[key] {
		// 绕过命令立即返回 true（让调用方直接处理）
		if IsSlashBypass(msg.Text) {
			return true, false
		}
		// 合并到等待队列（debounce 合并：同时段内同 session 多条消息取最新）
		queue := sm.pending[key]
		if len(queue) > 0 {
			last := &queue[len(queue)-1]
			if msg.Text != "" && time.Since(last.timestamp) < sm.debounce {
				// 合并：替换最后一条的 text（连续输入合并为一次 turn）
				if last.msg.Text != "" {
					last.msg.Text = last.msg.Text + "\n" + msg.Text
				} else {
					last.msg.Text = msg.Text
				}
				last.timestamp = time.Now()
				return false, true
			}
		}
		queue = append(queue, pendingTurn{msg: msg, timestamp: time.Now()})
		sm.pending[key] = queue
		return false, true
	}

	sm.active[key] = true
	return true, false
}

// Release 释放 session 锁，返回等待队列中的下一条消息（合并后）。
func (sm *SessionManager) Release(key string) *InboundMessage {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	queue := sm.pending[key]
	if len(queue) == 0 {
		delete(sm.active, key)
		delete(sm.pending, key)
		return nil
	}

	// 取出等待队列，合并其中所有消息
	var merged *InboundMessage
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
	// active 保持 true，因为调用方会立即用 merged 消息开始新 turn
	return merged
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

// ForceRelease 强制释放 session（用于 session 关闭或错误恢复）。
func (sm *SessionManager) ForceRelease(key string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	delete(sm.active, key)
	delete(sm.pending, key)
}
