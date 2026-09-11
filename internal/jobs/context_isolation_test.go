package jobs

import (
	"context"
	"testing"
)

func TestWithoutManagerShadowsAncestorManager(t *testing.T) {
	parent := WithManager(context.Background(), &Manager{})
	child := WithoutManager(parent)
	if _, ok := FromContext(child); ok {
		t.Fatal("child context inherited the parent Jobs manager")
	}
	if _, ok := FromContext(parent); !ok {
		t.Fatal("parent Jobs manager was lost")
	}
}
