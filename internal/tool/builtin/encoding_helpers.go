package builtin

import (
	"os"

	fileenc "reasonix/internal/fileutil/encoding"
)

// readFileEncoded reads a file and decodes its encoding to UTF-8.
// Returns the decoded content and the detected encoding kind so callers
// can re-encode on write to preserve the original charset.
func readFileEncoded(path string) (content string, enc fileenc.Kind, err error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", 0, err
	}
	enc, _ = fileenc.Detect(b)
	return string(fileenc.Decode(b, enc)), enc, nil
}

// writeFileEncoded encodes content back to the given encoding and writes it.
func writeFileEncoded(path string, content string, enc fileenc.Kind) error {
	return os.WriteFile(path, fileenc.Encode(content, enc), 0o644)
}
