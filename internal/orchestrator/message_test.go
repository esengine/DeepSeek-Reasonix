package orchestrator

import (
	"testing"
)

func TestNewInbox(t *testing.T) {
	ib := NewInbox()
	if ib == nil {
		t.Fatal("NewInbox returned nil")
	}
	if msgs := ib.Peek(); len(msgs) != 0 {
		t.Fatalf("expected empty inbox, got %d messages", len(msgs))
	}
}

func TestInboxPushPop(t *testing.T) {
	ib := NewInbox()

	msg := AgentMessage{From: "alice", To: "bob", Content: "hello", Topic: "greeting"}
	ib.Push(msg)

	got, ok := ib.Pop()
	if !ok {
		t.Fatal("Pop returned false on non-empty inbox")
	}
	if got.From != "alice" || got.To != "bob" || got.Content != "hello" || got.Topic != "greeting" {
		t.Fatalf("unexpected message: %+v", got)
	}
}

func TestInboxPopEmpty(t *testing.T) {
	ib := NewInbox()
	_, ok := ib.Pop()
	if ok {
		t.Fatal("Pop returned true on empty inbox")
	}
}

func TestInboxPopOrder(t *testing.T) {
	ib := NewInbox()
	ib.Push(AgentMessage{Content: "first"})
	ib.Push(AgentMessage{Content: "second"})
	ib.Push(AgentMessage{Content: "third"})

	for _, want := range []string{"first", "second", "third"} {
		got, ok := ib.Pop()
		if !ok {
			t.Fatal("unexpected empty")
		}
		if got.Content != want {
			t.Fatalf("expected %q, got %q", want, got.Content)
		}
	}
}

func TestInboxPeek(t *testing.T) {
	ib := NewInbox()
	ib.Push(AgentMessage{Content: "a"})
	ib.Push(AgentMessage{Content: "b"})

	msgs := ib.Peek()
	if len(msgs) != 2 {
		t.Fatalf("Peek returned %d messages, want 2", len(msgs))
	}

	got, ok := ib.Pop()
	if !ok || got.Content != "a" {
		t.Fatal("first pop after Peek should still return 'a'")
	}

	got, ok = ib.Pop()
	if !ok || got.Content != "b" {
		t.Fatal("second pop after Peek should return 'b'")
	}
}

func TestInboxClear(t *testing.T) {
	ib := NewInbox()
	ib.Push(AgentMessage{Content: "x"})
	ib.Push(AgentMessage{Content: "y"})

	ib.Clear()

	if msgs := ib.Peek(); len(msgs) != 0 {
		t.Fatalf("expected empty after Clear, got %d messages", len(msgs))
	}
}
