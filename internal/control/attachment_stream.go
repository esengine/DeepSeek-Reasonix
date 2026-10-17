package control

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"reasonix/internal/config"
)

// AttachmentStreamLimitError reports the effective disk safety ceiling.
type AttachmentStreamLimitError struct {
	LimitBytes  int64
	LimitSource string
}

func (e *AttachmentStreamLimitError) Error() string {
	return fmt.Sprintf("attachment exceeds %d byte stream limit (%s)", e.LimitBytes, e.LimitSource)
}

// SaveAttachmentStreamInRoot streams bytes with bounded memory and a byte-counted
// disk ceiling. Omit the policy for 1 GiB; an explicit zero permits unlimited storage.
func SaveAttachmentStreamInRoot(ctx context.Context, root, name string, source io.Reader, limits ...config.AttachmentLimit) (string, error) {
	if len(limits) > 1 {
		return "", fmt.Errorf("only one attachment stream limit may be supplied")
	}
	limit := (*config.Config)(nil).AttachmentStreamLimit()
	if len(limits) == 1 {
		limit = limits[0]
	}
	if limit.Bytes < 0 {
		limit = (*config.Config)(nil).AttachmentStreamLimit()
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	ext := strings.ToLower(filepath.Ext(name))
	if !safeAttachmentExt.MatchString(ext) {
		ext = ".bin"
	}
	owned, err := createOwnedAttachmentFile(root, ext)
	if err != nil {
		return "", err
	}
	defer owned.cleanup()

	buf := make([]byte, 64<<10)
	var total int64
	wrote := false
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		readBuf := buf
		if limit.Bytes > 0 && limit.Bytes-total < int64(len(buf)) {
			// Add the probe only after bounding remaining below len(buf), avoiding
			// limit+1 overflow even when the configured limit is MaxInt64.
			readBuf = buf[:int(limit.Bytes-total)+1]
		}
		n, readErr := source.Read(readBuf)
		if n > 0 {
			if err := ctx.Err(); err != nil {
				return "", err
			}
			if limit.Bytes > 0 && int64(n) > limit.Bytes-total {
				return "", &AttachmentStreamLimitError{LimitBytes: limit.Bytes, LimitSource: limit.Source}
			}
			if written, err := owned.Write(buf[:n]); err != nil {
				return "", err
			} else if written != n {
				return "", io.ErrShortWrite
			}
			wrote = true
			if limit.Bytes > 0 {
				total += int64(n)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return "", readErr
		}
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if !wrote {
		return "", fmt.Errorf("attachment is empty")
	}
	if err := owned.finish(); err != nil {
		return "", err
	}
	return filepath.ToSlash(owned.rel), nil
}
