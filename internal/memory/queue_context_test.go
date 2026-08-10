package memory

import (
	"context"
	"testing"
)

type queueContextProbe struct{}

func (queueContextProbe) QueueMemory(string) {}

func TestWithoutQueueShadowsAncestorQueue(t *testing.T) {
	parent := WithQueue(context.Background(), queueContextProbe{})
	child := WithoutQueue(parent)
	if _, ok := QueueFromContext(child); ok {
		t.Fatal("child context inherited the parent memory queue")
	}
	if _, ok := QueueFromContext(parent); !ok {
		t.Fatal("parent memory queue was lost")
	}
}
