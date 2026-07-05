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

type Mailbox struct {
	dir      string
	mu       sync.RWMutex
	messages []*Message
}

func NewMailbox(dir string) *Mailbox {
	return &Mailbox{
		dir:      dir,
		messages: []*Message{},
	}
}

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

func (mb *Mailbox) Broadcast(from, subject, content string) (string, error) {
	return mb.Send(from, "all", subject, content, "broadcast")
}
