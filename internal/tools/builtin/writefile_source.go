package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"reasonix/internal/base/fileutil"
	fileenc "reasonix/internal/base/fileutil/encoding"
	"reasonix/internal/base/secrets"
	"reasonix/internal/contract/tool"
	"slices"
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
	if len(reads) == 0 {
		return nil, nil
	}
	return reads, identityErr
}

func (w writeFile) sourceForWrite(ctx context.Context, path string) (editSource, bool, error) {
	exists, err := existingWriteSource(path)
	if err != nil {
		return editSource{}, false, err
	}
	if !exists {
		if w.overlay != nil && filepath.IsAbs(path) && requiresVersionedWrite(ctx) {
			if err := checkUnsavedReadAccess(w.readRoots, w.forbidRoots, path); err != nil {
				return editSource{}, false, err
			}
			if overlay, ok := w.overlay.(VersionedFileOverlay); ok && requiresVersionedWrite(ctx) {
				content, version, handled, err := overlay.ReadTextFileVersion(ctx, path)
				if err != nil {
					return editSource{}, false, err
				}
				if handled {
					return editSource{content: content, enc: fileenc.UTF8, overlay: true, bufferVersion: version}, true, nil
				}
			}
			if content, handled := w.overlay.ReadTextFile(ctx, path); handled {
				return editSource{content: content, enc: fileenc.UTF8, overlay: true}, true, nil
			}
		}
		return editSource{enc: fileenc.UTF8}, false, nil
	}
	if err := checkOverwriteReadAccess(w.readRoots, w.forbidRoots, path); err != nil {
		return editSource{}, true, err
	}
	src, err := readEditSource(ctx, w.overlay, path)
	return src, true, err
}

func checkUnsavedReadAccess(readRoots, forbidden []string, path string) error {
	canonical, err := fileutil.CanonicalWritePath(path)
	if err != nil {
		return err
	}
	readRoots, err = canonicalOverlayRoots(readRoots)
	if err != nil {
		return err
	}
	forbidden, err = canonicalOverlayRoots(forbidden)
	if err != nil {
		return err
	}
	if len(readRoots) > 0 && !slices.ContainsFunc(readRoots, func(root string) bool { return within(root, canonical) }) {
		return tool.Refusal{Code: CodeOverwriteReadOutsideScope, Message: overwriteReadAccessDeniedMessage}
	}
	if confineResolved(forbidden, canonical, secrets.ProtectSensitiveFiles()) {
		return tool.Refusal{Code: CodeOverwriteReadForbidden, Message: overwriteReadAccessDeniedMessage}
	}
	return nil
}

func canonicalOverlayRoots(roots []string) ([]string, error) {
	out := make([]string, 0, len(roots))
	for _, root := range roots {
		path, err := fileutil.CanonicalWritePath(root)
		if err != nil {
			return nil, err
		}
		out = append(out, path)
	}
	return out, nil
}

func (writeFile) FormatReadRefusal(err error) error {
	return overwriteReadRefusal(err)
}
