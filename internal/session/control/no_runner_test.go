package control

import (
	"context"
	"errors"
	"testing"
)

// A controller assembled without a runner reports that by identity. Calling
// through the nil runner instead was a nil dereference the turn guard
// recovered, which on Windows is a hardware exception either way.
func TestATurnWithoutARunnerReportsErrNoRunner(t *testing.T) {
	c := New(Options{})
	defer c.Close()
	if err := c.runSettled(context.Background(), "hello"); !errors.Is(err, ErrNoRunner) {
		t.Fatalf("runSettled = %v, want ErrNoRunner", err)
	}
}
