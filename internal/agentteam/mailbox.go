package agentteam

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"reasonix/internal/fileutil"
)

// Message 表示邮箱中的一条消息。
type Message struct {
	ID        string    `json:"id"`
	From      string    `json:"from"`
	To        string    `json:"to"`
	Subject   string    `json:"subject"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
	Read      bool      `json:"read"`
	Type      string    `json:"type"`
}

// Mailbox 表示团队的消息邮箱。
type Mailbox struct {
	dir      string
	mu       sync.RWMutex
	messages []*Message
}

// NewMailbox 创建一个新的邮箱。
func NewMailbox(dir string) *Mailbox {
	return &Mailbox{
		dir:      dir,
		messages: []*Message{},
	}
}

// LoadMailbox 从指定目录加载邮箱。
func LoadMailbox(dir string) (*Mailbox, error) {
	mb := NewMailbox(dir)
	msgsPath := filepath.Join(dir, "messages.json")
	data, err := os.ReadFile(msgsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return mb, nil
		}
		return nil, fmt.Errorf("read mailbox: %w", err)
	}
	var msgs []*Message
	if err := json.Unmarshal(data, &msgs); err != nil {
		return nil, fmt.Errorf("parse mailbox: %w", err)
	}
	mb.messages = msgs
	return mb, nil
}

func (mb *Mailbox) save() error {
	if strings.TrimSpace(mb.dir) == "" {
		return nil
	}
	if err := os.MkdirAll(mb.dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(mb.messages, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	path := filepath.Join(mb.dir, "messages.json")
	tmp, err := os.CreateTemp(mb.dir, ".mailbox-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return fileutil.ReplaceFile(tmpPath, path)
}

// Send 发送一条消息并返回消息 ID。
func (mb *Mailbox) Send(from, to, subject, content, msgType string) (string, error) {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	msg := &Message{
		ID:        fmt.Sprintf("msg_%d", time.Now().UnixNano()),
		From:      from,
		To:        to,
		Subject:   subject,
		Content:   content,
		CreatedAt: time.Now().UTC(),
		Read:      false,
		Type:      msgType,
	}

	mb.messages = append(mb.messages, msg)

	if err := mb.save(); err != nil {
		return "", err
	}

	return msg.ID, nil
}

// Inbox 获取指定成员的收件箱消息列表。
func (mb *Mailbox) Inbox(memberID string) []Message {
	mb.mu.RLock()
	defer mb.mu.RUnlock()

	var out []Message
	for _, msg := range mb.messages {
		if msg.To == memberID || msg.To == "all" {
			out = append(out, *msg)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out
}

// Unread 获取指定成员的未读消息列表。
func (mb *Mailbox) Unread(memberID string) []Message {
	mb.mu.RLock()
	defer mb.mu.RUnlock()

	var out []Message
	for _, msg := range mb.messages {
		if !msg.Read && (msg.To == memberID || msg.To == "all") {
			out = append(out, *msg)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out
}

// MarkRead 将指定消息标记为已读。
func (mb *Mailbox) MarkRead(msgID string) {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	for _, msg := range mb.messages {
		if msg.ID == msgID {
			msg.Read = true
		}
	}
	_ = mb.save()
}

// MarkAllRead 将指定成员的所有消息标记为已读。
func (mb *Mailbox) MarkAllRead(memberID string) {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	for _, msg := range mb.messages {
		if msg.To == memberID || msg.To == "all" {
			msg.Read = true
		}
	}
	_ = mb.save()
}

// Sent 获取指定成员发送的消息列表。
func (mb *Mailbox) Sent(from string) []Message {
	mb.mu.RLock()
	defer mb.mu.RUnlock()

	var out []Message
	for _, msg := range mb.messages {
		if msg.From == from {
			out = append(out, *msg)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out
}

// Get 根据消息 ID 获取消息的副本。
func (mb *Mailbox) Get(msgID string) (*Message, bool) {
	mb.mu.RLock()
	defer mb.mu.RUnlock()

	for _, msg := range mb.messages {
		if msg.ID == msgID {
			copy := *msg
			return &copy, true
		}
	}
	return nil, false
}

// UnreadCount 获取指定成员的未读消息数量。
func (mb *Mailbox) UnreadCount(memberID string) int {
	mb.mu.RLock()
	defer mb.mu.RUnlock()

	count := 0
	for _, msg := range mb.messages {
		if !msg.Read && (msg.To == memberID || msg.To == "all") {
			count++
		}
	}
	return count
}

// Broadcast 向所有成员广播一条消息。
func (mb *Mailbox) Broadcast(from, subject, content string) (string, error) {
	return mb.Send(from, "all", subject, content, "broadcast")
}
