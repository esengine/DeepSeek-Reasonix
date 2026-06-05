package lsp

import (
	"fmt"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf16"
)

// Position is a zero-based LSP position. Character is counted in the encoding the
// server negotiated at initialize (utf-16 by default, utf-8 when both sides
// agree).
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// Range is a half-open span between two positions.
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// Location is a file URI plus a range, the shape definition/references return.
type Location struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}

func pathToURI(p string) string {
	p = filepath.ToSlash(p)
	if runtime.GOOS == "windows" && len(p) > 1 && p[1] == ':' {
		p = "/" + p // C:/x → /C:/x so the URI becomes file:///C:/x
	}
	u := url.URL{Scheme: "file", Path: p}
	return u.String()
}

func uriToPath(uri string) string {
	u, err := url.Parse(uri)
	if err != nil {
		return uri
	}
	p := u.Path
	if runtime.GOOS == "windows" && len(p) > 2 && p[0] == '/' && p[2] == ':' {
		p = p[1:]
	}
	return filepath.FromSlash(p)
}

// locate finds symbol on the 1-based line of content and returns the LSP position
// of its first byte, converting the byte column into the server's encoding.
func locate(content string, line1 int, symbol, enc string) (Position, error) {
	lines := strings.Split(content, "\n")
	if line1 < 1 || line1 > len(lines) {
		return Position{}, fmt.Errorf("line %d out of range (file has %d lines)", line1, len(lines))
	}
	text := strings.TrimSuffix(lines[line1-1], "\r")
	col := strings.Index(text, symbol)
	if col < 0 {
		return Position{}, fmt.Errorf("symbol %q not found on line %d", symbol, line1)
	}
	return Position{Line: line1 - 1, Character: encodeChar(text[:col], enc)}, nil
}

func encodeChar(prefix, enc string) int {
	if enc == encodingUTF8 {
		return len(prefix)
	}
	return len(utf16.Encode([]rune(prefix)))
}

const (
	encodingUTF8  = "utf-8"
	encodingUTF16 = "utf-16"
)
