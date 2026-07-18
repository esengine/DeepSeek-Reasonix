package rpcwire

import (
	"bufio"
	"errors"
)

func readLine(br *bufio.Reader, maxBytes int) ([]byte, error) {
	var buf []byte
	for {
		chunk, err := br.ReadSlice('\n')
		buf = append(buf, chunk...)
		if maxBytes > 0 && len(buf) > maxBytes {
			return nil, &FrameTooLargeError{Direction: "inbound", Size: len(buf), Limit: maxBytes}
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		n := len(buf)
		for n > 0 && (buf[n-1] == '\n' || buf[n-1] == '\r') {
			n--
		}
		return trimSpace(buf[:n]), err
	}
}

func trimSpace(b []byte) []byte {
	i, j := 0, len(b)
	for i < j && isSpace(b[i]) {
		i++
	}
	for j > i && isSpace(b[j-1]) {
		j--
	}
	return b[i:j]
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }
