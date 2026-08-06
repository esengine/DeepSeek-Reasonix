package plugin

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// maxStdioFrameBytes bounds one Content-Length-framed message body. MCP tool
// results can carry large attachments, so the ceiling is deliberately high; it
// exists to stop a misbehaving server from declaring an absurd body size and
// exhausting memory while the caller drains it.
const maxStdioFrameBytes = 64 << 20 // 64 MiB

// readStdioFrame reads one JSON-RPC message from the plugin's stdout. The
// standard MCP/LSP transport frames messages as
//
//	Content-Length: <bytes>\r\n\r\n<json body>
//
// while older Reasonix plugins wrote one JSON object per line (NDJSON). Both
// forms are accepted so official SDK servers (the Python and Node SDKs emit
// Content-Length) and legacy plugins keep working side by side.
//
// The returned payload has surrounding whitespace trimmed. Stray blank lines
// between frames are skipped; an unrecognised header line (e.g. Content-Type)
// is skipped while scanning a header block. EOF or any framing error is
// returned to the caller, which treats it as a terminal read failure.
func readStdioFrame(r *bufio.Reader) ([]byte, error) {
	for {
		line, err := readTrimmedLine(r)
		if err != nil {
			return nil, err
		}
		if len(line) == 0 {
			continue // blank line between frames (or after a previous body)
		}
		if line[0] == '{' {
			return line, nil // legacy NDJSON payload
		}
		n, ok, err := parseContentLength(line)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue // other header line: keep scanning
		}
		return readContentLengthBody(r, n)
	}
}

// readContentLengthBody drains the rest of the header block (until the first
// blank line), then reads exactly n payload bytes.
func readContentLengthBody(r *bufio.Reader, n int) ([]byte, error) {
	for {
		line, err := readTrimmedLine(r)
		if err != nil {
			return nil, err
		}
		if len(line) == 0 {
			break
		}
	}
	if n > maxStdioFrameBytes {
		return nil, fmt.Errorf("stdio frame body is %d bytes; limit is %d", n, maxStdioFrameBytes)
	}
	payload := make([]byte, n)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, err
	}
	return bytes.TrimSpace(payload), nil
}

// parseContentLength recognises a Content-Length header line (case-insensitive,
// per the LSP framing rules). A line that is not a header returns ok=false; a
// header whose value is not a non-negative integer is a framing error.
func parseContentLength(line []byte) (n int, ok bool, err error) {
	const prefix = "content-length:"
	if len(line) < len(prefix) || !strings.EqualFold(string(line[:len(prefix)]), prefix) {
		return 0, false, nil
	}
	value := bytes.TrimSpace(line[len(prefix):])
	n, err = strconv.Atoi(string(value))
	if err != nil || n < 0 {
		return 0, true, fmt.Errorf("stdio frame malformed Content-Length %q", value)
	}
	return n, true, nil
}

// readTrimmedLine reads one line, dropping the trailing line ending and any
// surrounding whitespace (matching the previous NDJSON handling, which also
// trimmed before dispatch).
func readTrimmedLine(r *bufio.Reader) ([]byte, error) {
	line, err := r.ReadBytes('\n')
	return bytes.TrimSpace(line), err
}
