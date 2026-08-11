package bot

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"
	"sync"
	"time"
)

type interactionRecord struct {
	Token        string
	ConnectionID string
	ChatType     ChatType
	ChatID       string
	MessageID    string
	MessageIDs   map[string]bool
	RequestID    string
	Action       string
	Allowed      map[string]bool
	ExpiresAt    time.Time
	Used         bool
}

// InteractionStore is a server-side authority for QQ keyboard callbacks. The
// button only carries an opaque token; action and authorization never come
// from the client-visible label or callback text.
type InteractionStore struct {
	mu    sync.Mutex
	items map[string]interactionRecord
	ttl   time.Duration
}

func NewInteractionStore(ttl time.Duration) *InteractionStore {
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	return &InteractionStore{items: make(map[string]interactionRecord), ttl: ttl}
}

func (s *InteractionStore) Issue(connectionID string, chatType ChatType, chatID, messageID, requestID, action string, allowed []string) string {
	if s == nil {
		return ""
	}
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return ""
	}
	token := "rx:" + base64.RawURLEncoding.EncodeToString(buf)
	allow := make(map[string]bool, len(allowed))
	for _, id := range allowed {
		if id = strings.TrimSpace(id); id != "" {
			allow[id] = true
		}
	}
	s.mu.Lock()
	s.pruneLocked(time.Now())
	messageID = strings.TrimSpace(messageID)
	messageIDs := make(map[string]bool)
	if messageID != "" {
		messageIDs[messageID] = true
	}
	s.items[token] = interactionRecord{Token: token, ConnectionID: strings.TrimSpace(connectionID), ChatType: chatType, ChatID: strings.TrimSpace(chatID), MessageID: messageID, MessageIDs: messageIDs, RequestID: strings.TrimSpace(requestID), Action: action, Allowed: allow, ExpiresAt: time.Now().Add(s.ttl)}
	s.mu.Unlock()
	return token
}

// BindMessage attaches the provider-generated message ID after a keyboard is
// sent. A single outbound send may produce multiple chunks, so all returned
// IDs remain valid for the one-shot callback.
func (s *InteractionStore) BindMessage(token string, messageIDs ...string) error {
	if s == nil {
		return fmt.Errorf("interaction store is unavailable")
	}
	token = strings.TrimSpace(token)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(time.Now())
	record, ok := s.items[token]
	if !ok || record.Used {
		return fmt.Errorf("interaction expired or already used")
	}
	if record.MessageIDs == nil {
		record.MessageIDs = make(map[string]bool)
	}
	for _, messageID := range messageIDs {
		if messageID = strings.TrimSpace(messageID); messageID != "" {
			record.MessageIDs[messageID] = true
			if record.MessageID == "" {
				record.MessageID = messageID
			}
		}
	}
	if len(record.MessageIDs) == 0 {
		return fmt.Errorf("interaction message binding is empty")
	}
	s.items[token] = record
	return nil
}

func (s *InteractionStore) Revoke(token string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	delete(s.items, strings.TrimSpace(token))
	s.mu.Unlock()
}

func (s *InteractionStore) Consume(token string, incoming Interaction) (string, error) {
	if s == nil {
		return "", fmt.Errorf("interaction store is unavailable")
	}
	token = strings.TrimSpace(token)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(time.Now())
	record, ok := s.items[token]
	if !ok {
		return "", fmt.Errorf("interaction expired or unknown")
	}
	if record.Used {
		return "", fmt.Errorf("interaction already used")
	}
	if record.ConnectionID != strings.TrimSpace(incoming.ConnectionID) || record.ChatID != strings.TrimSpace(incoming.ChatID) {
		return "", fmt.Errorf("interaction conversation mismatch")
	}
	incomingMessageID := strings.TrimSpace(incoming.MessageID)
	if len(record.MessageIDs) == 0 || incomingMessageID == "" || !record.MessageIDs[incomingMessageID] {
		return "", fmt.Errorf("interaction message mismatch")
	}
	if !record.Allowed[strings.TrimSpace(incoming.UserID)] {
		return "", fmt.Errorf("interaction operator is not authorized")
	}
	record.Used = true
	s.items[token] = record
	return record.Action, nil
}

func (s *InteractionStore) pruneLocked(now time.Time) {
	for token, item := range s.items {
		if item.Used || now.After(item.ExpiresAt) {
			delete(s.items, token)
		}
	}
}
