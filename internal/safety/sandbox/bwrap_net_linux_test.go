//go:build linux

package sandbox

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

// netlessBwrap is a bwrap that starts a namespace unless asked to unshare the
// network, as a container without CAP_NET_ADMIN behaves.
func netlessBwrap(t *testing.T) {
	t.Helper()
	dir := testenv.TempDir(t)
	script := "#!/bin/sh\nfor a in \"$@\"; do [ \"$a\" = --unshare-net ] && exit 1; done\nexit 0\n"
	if err := os.WriteFile(filepath.Join(dir, "bwrap"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
}

func TestABwrapThatCannotUnshareTheNetworkIsUnavailableToASpecThatNeedsIt(t *testing.T) {
	netlessBwrap(t)
	shut := Spec{Mode: "enforce", Network: false}
	if AvailableFor(shut) {
		t.Fatal("a spec that denies network was reported available though --unshare-net cannot start")
	}
	if argv, wrapped := Command(shut, Shell{Kind: ShellBash, Path: "sh"}, "true"); wrapped || argv[0] != "sh" {
		t.Fatalf("Command = %v wrapped=%v; want the unwrapped shell so the caller fails closed", argv, wrapped)
	}
	if _, wrapped := CommandArgs(shut, []string{"/bin/true"}); wrapped {
		t.Fatal("CommandArgs wrapped in a sandbox that cannot start")
	}
	if err := bwrapFaultFor(shut); !errors.Is(err, ErrBwrapNetworkDenied) || errors.Is(err, ErrBwrapNamespaceDenied) {
		t.Fatalf("reason = %v, want the network-denied identity alone", err)
	}
	if got := UnavailableCodeFor(shut); got != "sandbox.bwrap_network_denied" {
		t.Fatalf("code = %q", got)
	}
	for _, want := range []string{"CAP_NET_ADMIN", "network = true", "only boundary"} {
		if msg := UnavailableMessageFor(shut); !strings.Contains(msg, want) {
			t.Fatalf("message = %q, want it to say %q", msg, want)
		}
	}
}

func TestABwrapThatCannotUnshareTheNetworkStillServesASpecThatLeavesItOpen(t *testing.T) {
	netlessBwrap(t)
	open := Spec{Mode: "enforce", Network: true}
	if !AvailableFor(open) || bwrapFaultFor(open) != nil {
		t.Fatalf("a spec with network on was refused: %v", bwrapFaultFor(open))
	}
	if _, wrapped := Command(open, Shell{Kind: ShellBash, Path: "sh"}, "true"); !wrapped {
		t.Fatal("a spec with network on was not wrapped")
	}
	if !Available() {
		t.Fatal("the spec-less base check lost a namespace that starts")
	}
}
