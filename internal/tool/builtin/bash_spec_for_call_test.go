package builtin

import (
	"context"
	"testing"

	"reasonix/internal/permissionpreset"
	"reasonix/internal/sandbox"
)

// TestSpecForCallRespectsBashOff proves an explicit [sandbox].bash="off" keeps
// bash unconfined for every permission preset: the preset still governs
// approvals, but it must not force an OS sandbox the user disabled.
func TestSpecForCallRespectsBashOff(t *testing.T) {
	b := bash{sb: sandbox.Spec{Mode: "off"}, workDir: "/work"}
	for _, preset := range []permissionpreset.Preset{
		permissionpreset.ReadOnly,
		permissionpreset.WorkspaceWrite,
		permissionpreset.DangerFullAccess,
	} {
		ctx := sandbox.WithPermissionPreset(context.Background(), string(preset))
		if got := b.specForCall(ctx).Mode; got != "off" {
			t.Errorf("bash=off with preset %s: Mode = %q, want off", preset, got)
		}
	}
}

// TestSpecForCallEnforcesWhenConfigured is the counterpart: when the session
// asks for enforcement, the workspace-write preset still enforces and
// danger-full-access still opts out.
func TestSpecForCallEnforcesWhenConfigured(t *testing.T) {
	b := bash{sb: sandbox.Spec{Mode: "enforce"}, workDir: "/work"}
	workspace := sandbox.WithPermissionPreset(context.Background(), string(permissionpreset.WorkspaceWrite))
	if got := b.specForCall(workspace).Mode; got != "enforce" {
		t.Errorf("workspace-write Mode = %q, want enforce", got)
	}
	full := sandbox.WithPermissionPreset(context.Background(), string(permissionpreset.DangerFullAccess))
	if got := b.specForCall(full).Mode; got != "off" {
		t.Errorf("danger-full-access Mode = %q, want off", got)
	}
}
