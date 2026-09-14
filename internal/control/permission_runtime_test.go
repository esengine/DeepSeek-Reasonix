package control

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/permission"
	"reasonix/internal/permissionpreset"
	"reasonix/internal/sandbox"
)

func TestPermissionPresetChangeWaitsForApprovalCommit(t *testing.T) {
	previousProcs := runtime.GOMAXPROCS(1)
	t.Cleanup(func() { runtime.GOMAXPROCS(previousProcs) })

	workspace := t.TempDir()
	extra, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	roots := sandbox.NewWritableRootSet([]string{workspace})
	c := newOwnedTestController(t, Options{WriteRoots: roots, WorkspaceRoot: workspace})
	c.SetToolApprovalMode(ToolApprovalWorkspaceWrite)

	entered := make(chan struct{})
	release := make(chan struct{})
	c.sink = event.FuncSink(func(e event.Event) {
		if e.Kind == event.PromptAnswered {
			close(entered)
			<-release
		}
	})
	id, reply := c.approval.registerWriteAccess("bash", "outside", "test", json.RawMessage(`{}`), &event.WriteAccessApproval{
		Directories: []string{extra},
	})
	before := c.PermissionSnapshot()

	resolved := make(chan error, 1)
	go func() {
		resolved <- c.ResolveApprovalAt(id, true, sandbox.ApprovalScopeSession, before.Generation, before.Revision)
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("approval did not reach its commit barrier")
	}

	switched := make(chan struct{})
	go func() {
		c.SetToolApprovalMode(ToolApprovalReadOnly)
		close(switched)
	}()
	// With one scheduler P, Gosched lets the preset goroutine run until it is
	// blocked behind the approval transaction. Publishing a revision before
	// that lock is acquired exposes a mixed permission snapshot.
	runtime.Gosched()
	if got := c.permissionRevision.Load(); got != before.Revision {
		close(release)
		t.Fatalf("permission revision became visible during approval commit: got %d, want %d", got, before.Revision)
	}
	during := c.PermissionSnapshot()
	if during.Revision != before.Revision || during.Preset != before.Preset {
		close(release)
		t.Fatalf("permission snapshot changed during approval commit: before=%+v during=%+v", before, during)
	}

	close(release)
	if err := <-resolved; err != nil {
		t.Fatalf("approval that committed first was rejected: %v", err)
	}
	if got := <-reply; !got.allow || !got.session {
		t.Fatalf("approval reply = %+v, want session allow", got)
	}
	<-switched
	after := c.PermissionSnapshot()
	if after.Preset != ToolApprovalReadOnly || after.Revision <= before.Revision {
		t.Fatalf("permission snapshot after switch = %+v", after)
	}
	if !roots.Covers(extra) {
		t.Fatal("session grant committed before the preset switch was lost")
	}
	found := false
	for _, grant := range after.Grants {
		if grant.Scope == "directory" && grant.Target == extra {
			found = true
		}
	}
	if !found {
		t.Fatalf("committed session grant missing from permission snapshot: %+v", after.Grants)
	}
}

func TestResolveApprovalAtRejectsStalePermissionRevision(t *testing.T) {
	c := newOwnedTestController(t, Options{Policy: permission.New("ask", nil, nil, nil)})
	id, reply := c.approval.registerWriteAccess("bash", "outside", "test", json.RawMessage(`{}`), &event.WriteAccessApproval{})
	revision := c.permissionRevision.Load()
	if err := c.ResolveApprovalAt(id, true, sandbox.ApprovalScopeOnce, c.runtimeGeneration, revision+1); !errors.Is(err, ErrPromptStaleRuntime) {
		t.Fatalf("ResolveApprovalAt error = %v, want ErrPromptStaleRuntime", err)
	}
	select {
	case got := <-reply:
		t.Fatalf("stale response resolved approval: %+v", got)
	default:
	}
	if err := c.ResolveApprovalAt(id, false, sandbox.ApprovalScopeOnce, c.runtimeGeneration, revision); err != nil {
		t.Fatalf("current response failed: %v", err)
	}
}

func TestPermissionPresetChangePublishesRevisionBeforeOldApprovalCanResolve(t *testing.T) {
	c := newOwnedTestController(t, Options{Policy: permission.New("ask", nil, nil, nil)})
	id, reply := c.approval.registerWriteAccess("bash", "outside", "test", json.RawMessage(`{}`), &event.WriteAccessApproval{})
	before := c.PermissionSnapshot()
	after, _, err := c.SetPermissionPreset(ToolApprovalDangerFullAccess, before.Revision)
	if err != nil {
		t.Fatalf("SetPermissionPreset: %v", err)
	}
	if after.Revision <= before.Revision {
		t.Fatalf("revision did not advance: before=%d after=%d", before.Revision, after.Revision)
	}
	if err := c.ResolveApprovalAt(id, true, sandbox.ApprovalScopeOnce, before.Generation, before.Revision); !errors.Is(err, ErrPromptStaleRuntime) {
		t.Fatalf("old approval reply error = %v, want ErrPromptStaleRuntime", err)
	}
	select {
	case got := <-reply:
		t.Fatalf("stale response resolved approval: %+v", got)
	default:
	}
}

func TestSettingSamePermissionPresetKeepsRevisionStable(t *testing.T) {
	c := newOwnedTestController(t, Options{Policy: permission.New("allow", nil, nil, nil)})
	c.SetToolApprovalMode(ToolApprovalDangerFullAccess)
	before := c.PermissionSnapshot()
	after, _, err := c.SetPermissionPreset(before.Preset, before.Revision)
	if err != nil {
		t.Fatalf("SetPermissionPreset: %v", err)
	}
	if after.Revision != before.Revision {
		t.Fatalf("same preset changed revision: before=%d after=%d", before.Revision, after.Revision)
	}
}

func TestPermissionSnapshotAndExactGrantRevocation(t *testing.T) {
	workspace := t.TempDir()
	extra := t.TempDir()
	roots := sandbox.NewWritableRootSet([]string{workspace})
	roots.GrantSession([]string{extra})
	c := newOwnedTestController(t, Options{Policy: permission.New("allow", nil, nil, nil), WriteRoots: roots, WorkspaceRoot: workspace})
	snapshot := c.PermissionSnapshot()
	if snapshot.Preset == "" || snapshot.WorkspaceRoot != workspace {
		t.Fatalf("permission snapshot = %+v", snapshot)
	}
	found := false
	for _, grant := range snapshot.Grants {
		if grant.Scope == "directory" && grant.Target != "" {
			found = true
			var err error
			snapshot, err = c.RevokeSessionGrant(grant.Scope, grant.Target, snapshot.Revision)
			if err != nil {
				t.Fatalf("RevokeSessionGrant: %v", err)
			}
			break
		}
	}
	if !found {
		t.Fatalf("directory grant missing from snapshot: %+v", snapshot.Grants)
	}
	if roots.Covers(extra) {
		t.Fatal("revoked directory remains writable")
	}
	if _, err := c.RevokeSessionGrant("directory", extra, snapshot.Revision-1); err == nil {
		t.Fatal("stale grant revocation should fail")
	}
}

func TestWindowsPermissionCapabilitiesDescribePartialBoundaries(t *testing.T) {
	got := permissionCapabilitiesForPlatform("windows", true, "")
	if got.Backend != "windows-write-restricted+appcontainer" || got.Enforcement != "partial" {
		t.Fatalf("Windows capability summary = %+v", got)
	}
	if got.WriteIsolation != "write-restricted-capability-sid" {
		t.Fatalf("write isolation = %q", got.WriteIsolation)
	}
	if got.ReadIsolation == "" || got.NetworkIsolation == "" {
		t.Fatalf("Windows partial boundaries are not reported: %+v", got)
	}
}

func TestUnavailablePermissionBackendOnlyOffersFullAccess(t *testing.T) {
	got := permissionCapabilitiesForPlatform("windows", false, "native API unavailable")
	if got.Enforcement != "unavailable" || got.UnavailableReason != "native API unavailable" {
		t.Fatalf("unavailable capability summary = %+v", got)
	}
	if len(got.SupportedPresets) != 1 || got.SupportedPresets[0] != string(permissionpreset.DangerFullAccess) {
		t.Fatalf("supported presets = %v", got.SupportedPresets)
	}
	if got.WriteIsolation != "" || got.ReadIsolation != "" || got.NetworkIsolation != "" {
		t.Fatalf("unavailable backend advertised active isolation: %+v", got)
	}
}

// TestPermissionCapabilitiesSkipBackendProbeWhenBashOff proves a bash=off
// session never queries (or executes) the sandbox backend when it builds its
// permission snapshot — the probe must be gated on enforcement, not run for
// every snapshot read.
func TestPermissionCapabilitiesSkipBackendProbeWhenBashOff(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows keeps probing the AppContainer backend regardless of bash mode")
	}
	restore := sandboxAvailable
	t.Cleanup(func() { sandboxAvailable = restore })
	probed := false
	sandboxAvailable = func() bool { probed = true; return false }

	c := newOwnedTestController(t, Options{Policy: permission.New("allow", nil, nil, nil)})
	c.PermissionSnapshot()
	if probed {
		t.Fatal("bash=off session probed the sandbox backend")
	}
}

// TestPermissionCapabilitiesProbeBackendWhenBashEnforced is the counterpart:
// when the session requests enforcement the probe still runs.
func TestPermissionCapabilitiesProbeBackendWhenBashEnforced(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows probe path is unconditional")
	}
	restore := sandboxAvailable
	t.Cleanup(func() { sandboxAvailable = restore })
	probed := false
	sandboxAvailable = func() bool { probed = true; return true }

	c := newOwnedTestController(t, Options{Policy: permission.New("allow", nil, nil, nil), BashSandboxRequested: true})
	got := c.PermissionSnapshot()
	if !probed {
		t.Fatal("bash=enforce session did not probe the sandbox backend")
	}
	if got.Capabilities.Enforcement != "full" {
		t.Fatalf("enforcement = %q, want full", got.Capabilities.Enforcement)
	}
}
