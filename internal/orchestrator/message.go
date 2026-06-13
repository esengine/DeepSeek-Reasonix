package orchestrator

import "sync"

type AgentMessage struct {
	From    string
	To      string
	Content string
	Topic   string
}

type Inbox struct {
	mu       sync.Mutex
	messages []AgentMessage
}

func NewInbox() *Inbox {
	return &Inbox{}
}

func (ib *Inbox) Push(msg AgentMessage) {
	ib.mu.Lock()
	defer ib.mu.Unlock()
	ib.messages = append(ib.messages, msg)
}

func (ib *Inbox) Pop() (AgentMessage, bool) {
	ib.mu.Lock()
	defer ib.mu.Unlock()
	if len(ib.messages) == 0 {
		return AgentMessage{}, false
	}
	msg := ib.messages[0]
	ib.messages = ib.messages[1:]
	return msg, true
}

func (ib *Inbox) Peek() []AgentMessage {
	ib.mu.Lock()
	defer ib.mu.Unlock()
	out := make([]AgentMessage, len(ib.messages))
	copy(out, ib.messages)
	return out
}

func (ib *Inbox) Clear() {
	ib.mu.Lock()
	defer ib.mu.Unlock()
	ib.messages = nil
}
