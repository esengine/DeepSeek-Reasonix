//go:build linux

package cli

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"reasonix/internal/remote/protocol"
)

func isolateRemoteServeEffectiveUID(t *testing.T, uid int) {
	t.Helper()
	previous := remoteServeEffectiveUID
	remoteServeEffectiveUID = func() int { return uid }
	t.Cleanup(func() { remoteServeEffectiveUID = previous })
}

func TestRemoteServeRejectsRootBeforeStartingHost(t *testing.T) {
	isolateRemoteGlobals(t)
	isolateRemoteServeEffectiveUID(t, 0)
	checkRemoteServePrivileges = productionRemoteServePrivilegeGuard
	runRemoteServe = func(context.Context, protocol.BuildID, io.Writer) error {
		t.Fatal("root privilege rejection reached the Host runner")
		return nil
	}

	var stdout, stderr bytes.Buffer
	rc := remoteServeCommand(nil, "dev", remoteCommandIO{stdout: &stdout, stderr: &stderr})
	if rc != 1 {
		t.Fatalf("rc=%d, want 1", rc)
	}
	if stdout.Len() != 0 {
		t.Fatalf("root rejection wrote stdout %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "refuses to run as root") {
		t.Fatalf("root rejection stderr=%q", stderr.String())
	}
}

func TestRemoteServePrivilegeGuardAllowsOrdinaryUser(t *testing.T) {
	isolateRemoteGlobals(t)
	isolateRemoteServeEffectiveUID(t, 1000)
	checkRemoteServePrivileges = productionRemoteServePrivilegeGuard
	called := false
	runRemoteServe = func(_ context.Context, _ protocol.BuildID, _ io.Writer) error {
		called = true
		return nil
	}

	var stdout, stderr bytes.Buffer
	rc := remoteServeCommand(nil, "dev", remoteCommandIO{stdout: &stdout, stderr: &stderr})
	if rc != 0 || !called {
		t.Fatalf("rc=%d called=%v stderr=%q", rc, called, stderr.String())
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("ordinary user stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}
