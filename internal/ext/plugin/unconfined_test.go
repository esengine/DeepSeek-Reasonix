package plugin

import (
	"context"
	"os"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/safety/sandbox"
)

func unconfinedProbeSpec(t *testing.T, mode MCPProcessMode) Spec {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return Spec{
		Name:          "unconfined-probe",
		Command:       exe,
		Args:          []string{"-test.run=TestHelperProcess", "--"},
		WorkspaceRoot: testenv.TempDir(t),
		Env:           map[string]string{"GO_WANT_HELPER_PROCESS": "1"},
		ProcessMode:   mode,
		Sandbox:       sandbox.Spec{Mode: "enforce", WriteRoots: []string{testenv.TempDir(t)}},
	}
}

func launchProbe(t *testing.T, spec Spec) bool {
	t.Helper()
	was := unconfinedLaunch.Load()
	unconfinedLaunch.Store(false)
	t.Cleanup(func() { unconfinedLaunch.Store(was) })
	tr, err := newStdioTransport(context.Background(), spec)
	if err != nil {
		t.Fatalf("newStdioTransport: %v", err)
	}
	tr.close()
	return LaunchedUnconfinedProcess()
}

func TestHostModeLaunchIsRecordedAsUnconfined(t *testing.T) {
	if !launchProbe(t, unconfinedProbeSpec(t, MCPProcessHost)) {
		t.Fatal("a host-mode server ran outside the sandbox without being recorded")
	}
}

func TestConfinedLaunchIsUnconfinedOnlyWithoutABackend(t *testing.T) {
	if got := launchProbe(t, unconfinedProbeSpec(t, MCPProcessConfined)); got == sandbox.Available() {
		t.Fatalf("LaunchedUnconfinedProcess = %v with sandbox available = %v", got, sandbox.Available())
	}
}
