package control

import (
	"errors"
	"fmt"
	"io"

	"reasonix/internal/contract/config"
)

type AttachmentStreamLimitError struct {
	LimitBytes  int64
	LimitSource string
}

func (e *AttachmentStreamLimitError) Error() string {
	return fmt.Sprintf("attachment exceeds the %d-byte upload limit (%s)", e.LimitBytes, e.LimitSource)
}

func (e *AttachmentStreamLimitError) Unwrap() error { return ErrAttachmentTooLarge }

func copyAttachmentStream(dst io.Writer, src io.Reader, limit config.AttachmentLimit) (int64, error) {
	if limit.Bytes < 0 {
		limit = config.AttachmentLimit{Bytes: config.DefaultAttachmentStreamBytes, Source: "default"}
	}
	reader := src
	if limit.Bytes > 0 {
		reader = io.LimitReader(src, limit.Bytes)
	}
	n, err := io.CopyBuffer(dst, reader, make([]byte, 64<<10))
	if err != nil || limit.Bytes == 0 || n < limit.Bytes {
		return n, err
	}
	// Probe without writing an extra byte or adding one to a possibly MaxInt64
	// limit. A refusal leaves cleanup with the same rooted file owner.
	var extra [1]byte
	read, err := io.ReadFull(src, extra[:])
	if read != 0 {
		return n, &AttachmentStreamLimitError{LimitBytes: limit.Bytes, LimitSource: limit.Source}
	}
	if errors.Is(err, io.EOF) {
		err = nil
	}
	return n, err
}
