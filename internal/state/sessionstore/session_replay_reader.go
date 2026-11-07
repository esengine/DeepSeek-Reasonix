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
	scan := recordScan{depth: 1}
	for scan.depth != 0 {
		if err := r.advance(&scan); err != nil {
			if errors.Is(err, io.EOF) {
				err = io.ErrUnexpectedEOF
			}
			return nil, start, err
		}
	}
	return r.buffer, start, nil
}

// recordScan is the framing state of one JSON object: how deep it is nested and
// whether the scan stands inside a string or just after a backslash.
type recordScan struct {
	depth           int
	quoted, escaped bool
}

// feed consumes bytes of p up to and including the one that closes the object,
// and reports how many it took.
func (s *recordScan) feed(p []byte) int {
	for i, b := range p {
		if s.quoted {
			switch {
			case s.escaped:
				s.escaped = false
			case b == '\\':
				s.escaped = true
			case b == '"':
				s.quoted = false
			}
			continue
		}
		switch b {
		case '"':
			s.quoted = true
		case '{', '[':
			s.depth++
		case '}', ']':
			s.depth--
			if s.depth == 0 {
				return i + 1
			}
		}
	}
	return len(p)
}

// advance moves the scan forward by whatever the reader already holds, within
// both byte budgets. When a budget leaves no room it takes one byte the slow
// way, which is where the refusal for that budget is raised.
func (r *sessionRecordReader) advance(scan *recordScan) error {
	if r.reader.Buffered() == 0 {
		if _, err := r.reader.Peek(1); err != nil {
			return err
		}
	}
	chunk, _ := r.reader.Peek(r.reader.Buffered())
	room := min(int64(len(chunk)), r.maxBytes-r.offset, sessionReplayMaxPayloadBytes-int64(len(r.buffer)))
	if room <= 0 {
		b, err := r.readByte()
		if err != nil {
			return err
		}
		if int64(len(r.buffer)) >= sessionReplayMaxPayloadBytes {
			return sessionReplayLimitError(r.path, "event_record_bytes", int64(len(r.buffer))+1, sessionReplayMaxPayloadBytes)
		}
		r.buffer = append(r.buffer, b)
		scan.feed(r.buffer[len(r.buffer)-1:])
		return nil
	}
	n := scan.feed(chunk[:room])
	r.buffer = append(r.buffer, chunk[:n]...)
	_, _ = r.reader.Discard(n)
	r.offset += int64(n)
	return nil
}
