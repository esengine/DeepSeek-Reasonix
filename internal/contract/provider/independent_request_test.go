package provider

import (
	"context"
	"testing"
)

func TestIndependentRequestCounterDoesNotChargeParent(t *testing.T) {
	parent := WithRequestAttemptCounter(context.Background())
	recordRequestAttempt(parent)
	child := WithIndependentRequestAttemptCounter(parent)
	recordRequestAttempt(child)
	recordRequestAttempt(child)
	if RequestAttemptCount(parent) != 1 || RequestAttemptCount(child) != 2 {
		t.Fatal("auxiliary and parent attempts were combined")
	}
}
