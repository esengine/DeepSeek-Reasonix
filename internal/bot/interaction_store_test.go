package bot

import (
	"testing"
	"time"
)

func TestInteractionStoreAuthorizesAndConsumesOnce(t *testing.T) {
	s := NewInteractionStore(time.Minute)
	token := s.Issue("qq-1", ChatDM, "user-1", "msg-1", "approval-1", "/approve approval-1", []string{"owner"})
	if token == "" || token[:3] != "rx:" {
		t.Fatalf("token=%q", token)
	}
	got, err := s.Consume(token, Interaction{ConnectionID: "qq-1", ChatType: ChatDM, ChatID: "user-1", UserID: "owner", MessageID: "msg-1"})
	if err != nil || got != "/approve approval-1" {
		t.Fatalf("consume=%q err=%v", got, err)
	}
	if _, err := s.Consume(token, Interaction{ConnectionID: "qq-1", ChatType: ChatDM, ChatID: "user-1", UserID: "owner", MessageID: "msg-1"}); err == nil {
		t.Fatal("reused interaction was accepted")
	}
}

func TestInteractionStoreRejectsConversationAndOperatorMismatch(t *testing.T) {
	s := NewInteractionStore(time.Minute)
	token := s.Issue("qq-1", ChatGroup, "group-1", "msg-1", "a", "allow", []string{"owner"})
	if _, err := s.Consume(token, Interaction{ConnectionID: "qq-1", ChatType: ChatGroup, ChatID: "group-2", UserID: "owner", MessageID: "msg-1"}); err == nil {
		t.Fatal("wrong conversation accepted")
	}
	if _, err := s.Consume(token, Interaction{ConnectionID: "qq-1", ChatType: ChatGroup, ChatID: "group-1", UserID: "member", MessageID: "msg-1"}); err == nil {
		t.Fatal("wrong operator accepted")
	}
}

func TestInteractionStoreRejectsMissingMessageBinding(t *testing.T) {
	s := NewInteractionStore(time.Minute)
	token := s.Issue("qq-1", ChatDM, "user-1", "msg-1", "ask-1", "/answer ask-1 0", []string{"owner"})
	if _, err := s.Consume(token, Interaction{ConnectionID: "qq-1", ChatType: ChatDM, ChatID: "user-1", UserID: "owner"}); err == nil {
		t.Fatal("interaction without source message binding was accepted")
	}
}

func TestInteractionStoreBindsAllOutboundMessageChunks(t *testing.T) {
	s := NewInteractionStore(time.Minute)
	token := s.Issue("qq-1", ChatDM, "user-1", "", "ask-1", "/answer ask-1 0", []string{"owner"})
	if err := s.BindMessage(token, "msg-1", "msg-2"); err != nil {
		t.Fatalf("bind message IDs: %v", err)
	}
	for _, messageID := range []string{"msg-1", "msg-2"} {
		copyToken := s.Issue("qq-1", ChatDM, "user-1", messageID, "x", "noop", []string{"owner"})
		if _, err := s.Consume(copyToken, Interaction{ConnectionID: "qq-1", ChatID: "user-1", UserID: "owner", MessageID: messageID}); err != nil {
			t.Fatalf("control consume %s: %v", messageID, err)
		}
	}
	if _, err := s.Consume(token, Interaction{ConnectionID: "qq-1", ChatID: "user-1", UserID: "owner", MessageID: "msg-2"}); err != nil {
		t.Fatalf("consume bound chunk: %v", err)
	}
}
