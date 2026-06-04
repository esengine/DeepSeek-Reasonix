package builtin

import (
	"os"
	"strings"

	fileenc "reasonix/internal/fileutil/encoding"
)

// readFileEncoded reads a file and decodes its encoding to UTF-8.
// Returns the decoded content and the detected encoding kind so callers
// can re-encode on write to preserve the original charset.
func readFileEncoded(path string) (content string, enc fileenc.Kind, err error) {
	return readFileEncodedWith(path, "")
}

// readFileEncodedWith reads a file and decodes it to UTF-8. When encName is
// non-empty the encoding is forced (skip auto-detection); otherwise behaves
// like readFileEncoded.
func readFileEncodedWith(path string, encName string) (content string, enc fileenc.Kind, err error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", 0, err
	}
	if forced, ok := fileenc.ParseName(encName); ok {
		return string(fileenc.Decode(b, forced)), forced, nil
	}
	enc, _ = fileenc.Detect(b)
	return string(fileenc.Decode(b, enc)), enc, nil
}

// writeFileEncoded encodes content back to the given encoding and writes it.
func writeFileEncoded(path string, content string, enc fileenc.Kind) error {
	return os.WriteFile(path, fileenc.Encode(content, enc), 0o644)
}

// writeFileEncodedWith writes content to path. When encName is non-empty the
// encoding is forced; otherwise enc (typically from readFileEncoded) is used.
func writeFileEncodedWith(path string, content string, enc fileenc.Kind, encName string) error {
	if forced, ok := fileenc.ParseName(encName); ok {
		return os.WriteFile(path, fileenc.Encode(content, forced), 0o644)
	}
	return writeFileEncoded(path, content, enc)
}

// matchLineEndings adapts an edit's old/new text to a CRLF file when the literal
// old_string isn't present but its CRLF form is. read_file strips '\r' (bufio
// ScanLines), so a model's multi-line old_string arrives LF-only while a
// Windows/CJK source stores '\r\n'; rewriting search and replacement to the
// file's ending fixes the match without rewriting the file's other line endings.
func matchLineEndings(content, old, new string) (string, string) {
	if strings.Contains(content, old) || !strings.Contains(content, "\r\n") {
		return old, new
	}
	toCRLF := func(s string) string {
		return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\n", "\r\n")
	}
	if strings.Contains(content, toCRLF(old)) {
		return toCRLF(old), toCRLF(new)
	}
	return old, new
}
