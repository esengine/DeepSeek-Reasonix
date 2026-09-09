package provider

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// StreamScanner retains the distinction ScanLines normally erases: a complete
// SSE line versus the last unterminated fragment returned when the body closes.
// Only a JSON prefix cut at that boundary is a recoverable stream interruption.
// Malformed complete events remain protocol errors.
type StreamScanner struct {
	*bufio.Scanner
	unterminated bool
}

func NewStreamScanner(r io.Reader, maxTokenSize int) *StreamScanner {
	s := &StreamScanner{Scanner: bufio.NewScanner(r)}
	s.Buffer(make([]byte, 0, 64*1024), maxTokenSize)
	s.Split(func(data []byte, atEOF bool) (int, []byte, error) {
		advance, token, err := bufio.ScanLines(data, atEOF)
		if token != nil {
			s.unterminated = atEOF && bytes.IndexByte(data, '\n') < 0
		}
		return advance, token, err
	})
	return s
}

func (s *StreamScanner) DecodeError(name, payload string, err error) error {
	decoded := StreamDecodeError(name, payload, err)
	var syntax *json.SyntaxError
	if s.unterminated && errors.As(err, &syntax) && syntax.Offset >= int64(len(payload)) {
		return StreamInterrupt(fmt.Errorf("%w: %w", io.ErrUnexpectedEOF, decoded), StreamInterruptPrematureEOF)
	}
	return decoded
}
