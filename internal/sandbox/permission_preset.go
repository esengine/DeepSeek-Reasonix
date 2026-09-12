package sandbox

import (
	"context"

	"reasonix/internal/permissionpreset"
)

type permissionPresetContextKey struct{}

// WithPermissionPreset binds the host-authoritative execution preset to one
// tool call. It is runtime-only and never changes provider-visible schemas.
func WithPermissionPreset(ctx context.Context, value string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, permissionPresetContextKey{}, permissionpreset.Normalize(value))
}

func PermissionPresetFrom(ctx context.Context) permissionpreset.Preset {
	if ctx == nil {
		return permissionpreset.WorkspaceWrite
	}
	if preset, ok := ctx.Value(permissionPresetContextKey{}).(permissionpreset.Preset); ok {
		return preset
	}
	return permissionpreset.WorkspaceWrite
}
