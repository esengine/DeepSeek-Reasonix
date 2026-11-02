package sessionstore

import (
	"bufio"
	"errors"
	"fmt"
	"io"
)

const sessionReplayMaxPayloadBytes = int64(64 << 20)

// Records are framed before JSON decoding so decoder buffers cannot consume
// the whole file budget for one oversized value.
type sessionRecordReader struct {
	reader   *bufio.Reader
	path     string
	maxBytes int64
	offset   int64
	buffer   []byte
}

func newSessionRecordReader(r io.Reader, path string, maxBytes int64) *sessionRecordReader {
	return &sessionRecordReader{reader: bufio.NewReader(r), path: path, maxBytes: maxBytes}
}

func (r *sessionRecordReader) readByte() (byte, error) {
	b, err := r.reader.ReadByte()
	if err != nil {
		return 0, err
	}
	r.offset++
	if r.offset > r.maxBytes {
		return 0, sessionReplayLimitError(r.path, "encoded_bytes", r.offset, r.maxBytes)
	}
	return b, nil
}

func (r *sessionRecordReader) next() ([]byte, int64, error) {
	r.buffer = r.buffer[:0]
	for {
		b, err := r.readByte()
		if err != nil {
			return nil, r.offset, err
		}
		if b == ' ' || b == '\t' || b == '\r' || b == '\n' {
			continue
		}
		// null is a zero-valued record whose schema must be refused, not a
		// damaged tail a later save may truncate.
		if b == 'n' {
			start := r.offset - 1
			r.buffer = append(r.buffer, b)
			for range 3 {
				b, err := r.readByte()
				if err != nil {
					if errors.Is(err, io.EOF) {
						err = io.ErrUnexpectedEOF
					}
					return nil, start, err
				}
				r.buffer = append(r.buffer, b)
			}
			return r.buffer, start, nil
		}
		if b != '{' {
			return nil, r.offset - 1, fmt.Errorf("session event record must be an object")
		}
		r.buffer = append(r.buffer, b)
		break
	}
	start := r.offset - 1
	depth := 1
	quoted, escaped := false, false
	for depth != 0 {
		b, err := r.readByte()
		if err != nil {
			if errors.Is(err, io.EOF) {
				err = io.ErrUnexpectedEOF
			}
			return nil, start, err
		}
		if int64(len(r.buffer)) >= sessionReplayMaxPayloadBytes {
			return nil, start, sessionReplayLimitError(r.path, "event_record_bytes", int64(len(r.buffer))+1, sessionReplayMaxPayloadBytes)
		}
		r.buffer = append(r.buffer, b)
		if quoted {
			if escaped {
				escaped = false
			} else if b == '\\' {
				escaped = true
			} else if b == '"' {
				quoted = false
			}
			continue
		}
		switch b {
		case '"':
			quoted = true
		case '{', '[':
			depth++
		case '}', ']':
			depth--
		}
	}
	return r.buffer, start, nil
}
