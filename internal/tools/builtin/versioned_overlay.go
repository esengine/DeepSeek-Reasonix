package builtin

import (
	"context"
	"errors"
)

var ErrOverlayVersionUnavailable = errors.New("editor does not support version-checked writes")

// VersionedFileOverlay atomically checks the observed buffer version on write.
type VersionedFileOverlay interface {
	ReadTextFileVersion(context.Context, string) (content, version string, ok bool, err error)
	CompareWriteTextFile(ctx context.Context, path, version, content string) (ok bool, err error)
}

type versionedWriteKey struct{}

func WithVersionedWrites(ctx context.Context) context.Context {
	return context.WithValue(ctx, versionedWriteKey{}, true)
}

func requiresVersionedWrite(ctx context.Context) bool {
	strict, _ := ctx.Value(versionedWriteKey{}).(bool)
	return strict
}

func compareOverlayWrite(ctx context.Context, overlay FileOverlay, path, version, content string) error {
	versioned, ok := overlay.(VersionedFileOverlay)
	if !ok || version == "" {
		return ErrOverlayVersionUnavailable
	}
	handled, err := versioned.CompareWriteTextFile(ctx, path, version, content)
	if !handled {
		return ErrOverlayVersionUnavailable
	}
	return err
}
