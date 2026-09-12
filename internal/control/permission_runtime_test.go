package control

import (
	"encoding/json"
	"errors"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/permission"
	"reasonix/internal/permissionpreset"
	"reasonix/internal/sandbox"
)

func TestResolveApprovalAtRejectsStalePermissionRevision(t *testing.T) {
	c := New(Options{Policy: permission.New("ask", nil, nil, nil)})
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
	c := New(Options{Policy: permission.New("ask", nil, nil, nil)})
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
	c := New(Options{Policy: permission.New("allow", nil, nil, nil)})
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
	c := New(Options{Policy: permission.New("allow", nil, nil, nil), WriteRoots: roots, WorkspaceRoot: workspace})
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
