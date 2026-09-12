// Package permissionpreset owns the three user-visible execution permission
// presets. Legacy approval-mode strings are accepted only at compatibility
// boundaries and are normalized before they reach the runtime.
package permissionpreset

import "strings"

type Preset string

const (
	ReadOnly         Preset = "read-only"
	WorkspaceWrite   Preset = "workspace-write"
	DangerFullAccess Preset = "danger-full-access"
)

// Normalize maps persisted and cross-version values to a runtime preset.
// Legacy YOLO is deliberately migrated to workspace-write: older YOLO skipped
// prompts but did not mean that the filesystem sandbox was disabled.
func Normalize(value string) Preset {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case string(ReadOnly), "readonly", "read_only", "ask":
		return ReadOnly
	case string(WorkspaceWrite), "workspace", "workspace_write", "auto", "yolo":
		return WorkspaceWrite
	case string(DangerFullAccess), "danger_full_access", "full", "full-access", "bypass":
		return DangerFullAccess
	default:
		return ReadOnly
	}
}

// NormalizeDefault applies the zero-configuration default for newly-created
// sessions while keeping Normalize's conservative behavior for restored data.
func NormalizeDefault(value string) Preset {
	if strings.TrimSpace(value) == "" {
		return WorkspaceWrite
	}
	return Normalize(value)
}

func Valid(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case string(ReadOnly), string(WorkspaceWrite), string(DangerFullAccess):
		return true
	default:
		return false
	}
}
