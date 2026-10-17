package control

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"

	"reasonix/internal/contract/config"
)

func SaveAttachmentStreamInRoot(ctx context.Context, root, origName string, src io.Reader, limits ...config.AttachmentLimit) (string, error) {
	if len(limits) > 1 {
		return "", attachmentWriteError(errors.New("at most one attachment upload limit is supported"))
	}
	limit := config.AttachmentLimit{Bytes: config.DefaultAttachmentStreamBytes, Source: "default"}
	if len(limits) == 1 {
		limit = limits[0]
	}
	ext := strings.ToLower(filepath.Ext(origName))
	if !safeAttachmentExt.MatchString(ext) {
		ext = ".bin"
	}
	owned, err := createOwnedAttachmentFile(root, ext)
	if err != nil {
		return "", attachmentWriteError(err)
	}
	defer owned.cleanup()

	n, err := copyAttachmentStream(owned, attachmentContextReader{ctx, src}, limit)
	if err != nil {
		return "", attachmentWriteError(err)
	}
	if err = ctx.Err(); err != nil {
		return "", attachmentWriteError(err)
	}
	if n == 0 {
		return "", attachmentSizeError(0, maxFileAttachmentBytes)
	}
	if err = owned.finish(); err != nil {
		return "", attachmentWriteError(err)
	}
	return filepath.ToSlash(owned.rel), nil
}

type attachmentContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r attachmentContextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}
