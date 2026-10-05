package plugin

import (
	"context"
	"runtime"
	"strings"
	"testing"
)

// A command that exists, on the platform the test is running on. Resolving the
// executable happens before the context is consulted, so a missing one fails
// earlier and for another reason — which is what /bin/echo did on Windows,
// where this guard reported the missing file as its answer.
func probeCommand() (string, []string) {
	if runtime.GOOS == "windows" {
		return "cmd", []string{"/c", "exit"}
	}
	return "/bin/echo", nil
}

// A lazily-started server whose context ends says which of the two things
// happened. Before this, both arrived as "context canceled" in 0-4ms with no
// stderr, which reads like the server itself failed to launch.
func TestLaunchFailureNamesWhoCancelled(t *testing.T) {
	for _, tc := range []struct {
		name  string
		cause error
		want  string
	}{
		{"host closed", ErrHostClosed, "MCP host shut down"},
		{"server removed", ErrServerRemoved, "removed, disabled, or reconnected"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancelCause(context.Background())
			cancel(tc.cause)
			command, args := probeCommand()
			_, err := start(ctx, context.Background(), Spec{Name: "probe", Command: command, Args: args})
			if err == nil {
				t.Fatal("a cancelled context started a server")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %q, want it to name %q", err.Error(), tc.want)
			}
		})
	}
}
