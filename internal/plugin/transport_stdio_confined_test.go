package plugin

import (
	"strings"
	"testing"

	"reasonix/internal/sandbox"
)

// TestWrapConfinedCommandFailsClosedWhenBackendUnavailable pins the fail-closed
// contract for confined MCP processes: when the mode is enforce and the OS
// sandbox backend cannot wrap the command, launching must error instead of
// silently degrading to an unsandboxed process.
func TestWrapConfinedCommandFailsClosedWhenBackendUnavailable(t *testing.T) {
	spec := sandbox.Spec{Mode: "enforce"}
	_, err := wrapConfinedCommand("my-server", spec, []string{"node", "server.js"},
		func(sandbox.Spec, []string) ([]string, bool) {
			return []string{"node", "server.js"}, false
		})
	if err == nil {
		t.Fatal("confined + enforce + wrapped=false must fail closed, got nil error")
	}
	if !strings.Contains(err.Error(), `"my-server"`) {
		t.Fatalf("error must name the plugin, got %q", err)
	}
	if !strings.Contains(err.Error(), "sandbox backend") {
		t.Fatalf("error must explain the sandbox backend is unavailable, got %q", err)
	}
}

// TestWrapConfinedCommandUsesWrappedArgv verifies a confined process with an
// available backend runs the sandbox-wrapped argv.
func TestWrapConfinedCommandUsesWrappedArgv(t *testing.T) {
	spec := sandbox.Spec{Mode: "enforce"}
	got, err := wrapConfinedCommand("my-server", spec, []string{"node", "server.js"},
		func(sandbox.Spec, []string) ([]string, bool) {
			return []string{"bwrap", "--ro-bind", "/", "/", "node", "server.js"}, true
		})
	if err != nil {
		t.Fatalf("wrapped=true must succeed, got %v", err)
	}
	if len(got) != 6 || got[0] != "bwrap" {
		t.Fatalf("argv = %v, want the sandbox-wrapped argv", got)
	}
}

// TestWrapConfinedCommandAllowsUnwrappedWhenNotEnforced verifies a sandbox
// spec that does not enforce (e.g. host mode) keeps the raw launch argv and
// never fails closed — the product behavior for authorized user installs.
func TestWrapConfinedCommandAllowsUnwrappedWhenNotEnforced(t *testing.T) {
	spec := sandbox.Spec{Mode: "host"}
	launchArgs := []string{"node", "server.js"}
	got, err := wrapConfinedCommand("my-server", spec, launchArgs,
		func(sandbox.Spec, []string) ([]string, bool) {
			return []string{"node", "server.js"}, false
		})
	if err != nil {
		t.Fatalf("non-enforce mode must not fail closed, got %v", err)
	}
	if got[0] != "node" {
		t.Fatalf("argv = %v, want the unchanged launch argv", got)
	}
}
