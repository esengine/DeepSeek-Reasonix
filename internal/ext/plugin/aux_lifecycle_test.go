package plugin

import (
	"context"
	"os"
	"testing"
	"time"
)

// A server slower to come up than the handshake budget is still a server. When
// the child's life was bounded by that budget it was killed mid-startup — the
// spawn never reached the server's own entry point, and the next call read EOF
// from a pipe nobody was left to write. The budget bounds the handshake; the
// caller owns the child.
func TestAuxiliaryClientOutlivesTheHandshakeBudget(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Slower than the 5s the child's life used to be pinned to, the way a server
	// importing a data stack is.
	spec := Spec{
		Name:           "slow-import",
		Command:        os.Args[0],
		Args:           []string{"-test.run=TestHelperProcess", "--"},
		StartupTimeout: 20 * time.Second,
		Env: map[string]string{
			"GO_WANT_HELPER_PROCESS": "1",
			"GO_WANT_HELPER_PROMPTS": "1",
			"GO_WANT_HELPER_INIT_MS": "5500",
		},
	}

	c := &Client{name: spec.Name, spec: spec}
	aux, err := c.auxiliaryClient(ctx)
	if err != nil {
		t.Fatalf("auxiliaryClient on a slow server: %v", err)
	}
	defer aux.close()

	// The child must still be there to answer once the handshake budget for a
	// faster server would long since have expired.
	if _, err := aux.listPrompts(ctx); err != nil {
		t.Fatalf("listPrompts after startup: %v", err)
	}
}
