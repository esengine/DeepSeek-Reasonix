package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"os"

	"reasonix/internal/base/fileutil"
	fileenc "reasonix/internal/base/fileutil/encoding"
)

func existingWriteSource(path string) (bool, error) {
	_, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

func (w writeFile) ReadPaths(_ context.Context, args json.RawMessage) ([]string, error) {
	paths, identityErr := w.WritePaths(args)
	if identityErr != nil && (!errors.Is(identityErr, fileutil.ErrAmbiguousPath) || len(paths) == 0) {
		return nil, identityErr
	}
	var reads []string
	for _, path := range paths {
		exists, err := existingWriteSource(path)
		if err != nil {
			return nil, err
		}
		if exists {
			reads = append(reads, path)
		}
	}
	return reads, identityErr
}

func (w writeFile) sourceForWrite(ctx context.Context, path string) (editSource, bool, error) {
	exists, err := existingWriteSource(path)
	if err != nil {
		return editSource{}, false, err
	}
	if !exists {
		return editSource{enc: fileenc.UTF8}, false, nil
	}
	if err := checkOverwriteReadAccess(w.readRoots, w.forbidRoots, path); err != nil {
		return editSource{}, true, err
	}
	src, err := readEditSource(ctx, w.overlay, path)
	return src, true, err
}
