// Package builtin provides Reasonix's compile-time built-in tools. Each tool
// self-registers via init(); main blank-imports this package to wire them in.
package builtin

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	fileenc "reasonix/internal/fileutil/encoding"
	"reasonix/internal/tool"
)

const readFileBinaryPeek = 8 * 1024 // bytes scanned for NUL before full read

func init() { tool.RegisterBuiltin(readFile{}) }

// readFile reads a text file. workDir, when non-empty, is the directory a
// relative path is resolved against (see resolveIn); the zero value registered
// at init resolves against the process working directory.
type readFile struct{ workDir string }

const (
	readFileDefaultLimit = 2000 // lines returned when limit is unset
)

func (readFile) Name() string { return "read_file" }

func (readFile) Description() string {
	return "Read a text file with optional line offset/limit. Output prefixes each line with its 1-based number (e.g. `   42→...`) so subsequent edit_file calls can target exact lines. Use `offset` and `limit` to page through large files; the tool reports total length and pagination hints in a trailer."
}

func (readFile) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "path":{"type":"string","description":"File path"},
  "offset":{"type":"integer","description":"0-based line offset to start reading from (default 0)","minimum":0},
  "limit":{"type":"integer","description":"Maximum lines to return (default 2000)","minimum":1}
},
"required":["path"]
}`)
}

func (readFile) ReadOnly() bool { return true }

func (r readFile) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Path   string `json:"path"`
		Offset int    `json:"offset,omitempty"`
		Limit  int    `json:"limit,omitempty"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.Path == "" {
		return "", fmt.Errorf("path is required")
	}
	p.Path = resolveIn(r.workDir, p.Path)
	if p.Offset < 0 {
		p.Offset = 0
	}
	if p.Limit <= 0 {
		p.Limit = readFileDefaultLimit
	}

	// A directory can be os.Open'd but not read as text — catch it up front with
	// an actionable message (and avoid the doubled "read X: read X:" the scanner's
	// error would otherwise produce) so the model switches to the ls tool.
	if info, err := os.Stat(p.Path); err == nil && info.IsDir() {
		return "", fmt.Errorf("%s is a directory, not a file — use the ls tool to list it, or read a specific file inside it", p.Path)
	}

	f, err := os.Open(p.Path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", p.Path, err)
	}
	defer f.Close()

	// Peek the first 8 KiB to reject binary files cheaply — the original
	// implementation did this too, and it prevents a multi-GB archive or
	// executable from being slurped into memory just to be discarded.
	peek := make([]byte, readFileBinaryPeek)
	n, _ := io.ReadFull(f, peek)
	peek = peek[:n]

	// Check for a BOM first: UTF-16 files contain 0x00 for every ASCII
	// character, so a naive NUL check would misidentify them as binary.
	bomKind := fileenc.DetectQuick(peek)
	if bomKind == fileenc.UTF16LE || bomKind == fileenc.UTF16BE || bomKind == fileenc.UTF8BOM {
		rest, err := io.ReadAll(f)
		if err != nil {
			return "", fmt.Errorf("read %s: %w", p.Path, err)
		}
		all := append(peek, rest...)
		src := io.Reader(bytes.NewReader(fileenc.Decode(all, bomKind)))
		return r.scan(src, p.Offset, p.Limit)
	}

	// No BOM — a NUL anywhere in the peek means binary.
	if bytes.IndexByte(peek, 0) >= 0 {
		return "", fmt.Errorf("binary file %s (NUL byte detected); use `bash hexdump` or another tool", p.Path)
	}

	// Non-BOM text: read the rest for full encoding detection (UTF-8 vs
	// GB18030 vs lossy fallback). The peek already passed the NUL check
	// so the remainder is unlikely to contain one, but check anyway.
	rest, err := io.ReadAll(f)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", p.Path, err)
	}
	all := append(peek, rest...)
	enc, _ := fileenc.Detect(all)
	if enc == fileenc.LossyUTF8 && bytes.IndexByte(all, 0) >= 0 {
		return "", fmt.Errorf("binary file %s (NUL byte detected); use `bash hexdump` or another tool", p.Path)
	}
	src := io.Reader(bytes.NewReader(fileenc.Decode(all, enc)))
	return r.scan(src, p.Offset, p.Limit)
}

// scan reads lines from src and returns the formatted output with line numbers.
func (r readFile) scan(src io.Reader, offset, limit int) (string, error) {
	scanner := bufio.NewScanner(src)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	upTo := offset + limit + 1

	var collected []string
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		if lineNo > offset && len(collected) < limit {
			collected = append(collected, scanner.Text())
		}
		if lineNo >= upTo {
			break
		}
	}
	remaining := 0
	for scanner.Scan() {
		remaining++
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("scan: %w", err)
	}
	totalSeen := lineNo + remaining

	if totalSeen == 0 {
		return "(empty file)", nil
	}
	if len(collected) == 0 {
		return fmt.Sprintf("(offset %d is past EOF — file has %d lines)", offset, totalSeen), nil
	}

	maxShown := offset + len(collected)
	w := len(fmt.Sprint(maxShown))

	var b strings.Builder
	for i, line := range collected {
		fmt.Fprintf(&b, "%*d→%s\n", w, offset+i+1, line)
	}
	more := totalSeen - (offset + len(collected))
	if more > 0 {
		fmt.Fprintf(&b, "\n[%d more line(s); pass offset=%d to continue]\n",
			more, offset+len(collected))
	}
	return b.String(), nil
}
