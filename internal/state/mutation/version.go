package mutation

import (
	"context"
	"errors"

	"reasonix/internal/base/fileutil"
)

type ContentVersion = fileutil.ContentVersion

func ReadVersionContext(ctx context.Context, path string) (ContentVersion, error) {
	version, err := fileutil.ReadContentVersion(ctx, path)
	if errors.Is(err, fileutil.ErrContentChanged) {
		return ContentVersion{}, &VersionConflict{Path: path}
	}
	if errors.Is(err, errors.ErrUnsupported) {
		return ContentVersion{}, ErrInvalidIntent
	}
	return version, err
}
