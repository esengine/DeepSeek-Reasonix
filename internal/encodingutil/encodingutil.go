// Package encodingutil provides file encoding detection, decoding, and re-encoding
// for text files that may use non-UTF-8 encodings (GBK, Big5, Shift_JIS, etc.).
package encodingutil

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
	"golang.org/x/text/transform"
)

// BOM kinds we recognize.
type bomKind int

const (
	bomNone bomKind = iota
	bomUTF8
	bomUTF16LE
	bomUTF16BE
)

// FileEncoding holds the result of encoding detection for a file.
type FileEncoding struct {
	Kind    bomKind          // BOM type (bomNone for no BOM)
	Charset encoding.Encoding // nil for UTF-8, non-nil for other encodings
	Name    string           // human-readable name
}

// ReadFile reads a text file, detects its encoding, decodes to UTF-8, and returns
// the content along with the detected encoding info (needed for WriteFile).
func ReadFile(path string) (content string, enc FileEncoding, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", enc, err
	}
	enc, decoded, err := detectAndDecode(data)
	if err != nil {
		return "", enc, err
	}
	return string(decoded), enc, nil
}

// WriteFile writes UTF-8 content to a file, re-encoding to the original encoding
// (with BOM if the original had one).
func WriteFile(path string, content string, enc FileEncoding) error {
	encoded, err := encode([]byte(content), enc)
	if err != nil {
		return err
	}
	return os.WriteFile(path, encoded, 0o644)
}

// detectAndDecode detects the encoding of raw bytes and decodes to UTF-8.
func detectAndDecode(data []byte) (FileEncoding, []byte, error) {
	// Step 1: Check for BOM.
	bom := detectBOM(data)
	switch bom {
	case bomUTF8:
		return FileEncoding{Kind: bomUTF8, Name: "UTF-8-BOM"}, data[3:], nil
	case bomUTF16LE:
		decoded := decodeUTF16(data[2:], binary.LittleEndian)
		return FileEncoding{Kind: bomUTF16LE, Name: "UTF-16LE-BOM"}, decoded, nil
	case bomUTF16BE:
		decoded := decodeUTF16(data[2:], binary.BigEndian)
		return FileEncoding{Kind: bomUTF16BE, Name: "UTF-16BE-BOM"}, decoded, nil
	}

	// Step 2: No BOM — check if valid UTF-8.
	if utf8.Valid(data) {
		return FileEncoding{Kind: bomNone, Name: "UTF-8"}, data, nil
	}

	// Step 3: Not valid UTF-8 — try common encodings.
	charset, name := detectEncoding(data)
	if charset != nil {
		decoded, err := decodeCharset(data, charset)
		if err == nil {
			return FileEncoding{Kind: bomNone, Charset: charset, Name: name}, decoded, nil
		}
	}

	// Fallback: return raw bytes as-is (may contain replacement chars).
	return FileEncoding{Kind: bomNone, Name: "unknown"}, data, nil
}

// encode encodes UTF-8 content back to the original encoding.
func encode(data []byte, enc FileEncoding) ([]byte, error) {
	switch enc.Kind {
	case bomUTF8:
		// Prepend UTF-8 BOM.
		return append([]byte{0xEF, 0xBB, 0xBF}, data...), nil
	case bomUTF16LE:
		return encodeUTF16(data, binary.LittleEndian, true), nil
	case bomUTF16BE:
		return encodeUTF16(data, binary.BigEndian, true), nil
	}

	// No BOM — re-encode to the detected charset if known.
	if enc.Charset != nil {
		writer := &bytes.Buffer{}
		w := transform.NewWriter(writer, enc.Charset.NewEncoder())
		if _, err := w.Write(data); err != nil {
			return nil, fmt.Errorf("encode to %s: %w", enc.Name, err)
		}
		return writer.Bytes(), nil
	}

	// UTF-8 or unknown — write as-is.
	return data, nil
}

func detectBOM(b []byte) bomKind {
	switch {
	case len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF:
		return bomUTF8
	case len(b) >= 2 && b[0] == 0xFF && b[1] == 0xFE:
		return bomUTF16LE
	case len(b) >= 2 && b[0] == 0xFE && b[1] == 0xFF:
		return bomUTF16BE
	}
	return bomNone
}

func decodeUTF16(b []byte, order binary.ByteOrder) []byte {
	u := make([]uint16, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		u = append(u, order.Uint16(b[i:i+2]))
	}
	return []byte(string(utf16.Decode(u)))
}

func encodeUTF16(data []byte, order binary.ByteOrder, withBOM bool) []byte {
	runes := []rune(string(data))
	buf := make([]byte, 0, len(runes)*2+2)
	if withBOM {
		if order == binary.LittleEndian {
			buf = append(buf, 0xFF, 0xFE)
		} else {
			buf = append(buf, 0xFE, 0xFF)
		}
	}
	for _, r := range runes {
		if r > 0xFFFF {
			// Surrogate pair for characters outside BMP
			r1, r2 := utf16.EncodeRune(r)
			buf = appendUint16(buf, uint16(r1), order)
			buf = appendUint16(buf, uint16(r2), order)
		} else {
			buf = appendUint16(buf, uint16(r), order)
		}
	}
	return buf
}

func appendUint16(buf []byte, v uint16, order binary.ByteOrder) []byte {
	var b [2]byte
	order.PutUint16(b[:], v)
	return append(buf, b[:]...)
}

// commonEncodings lists non-UTF-8 encodings to try, ordered by likelihood.
var commonEncodings = []struct {
	name string
	enc  encoding.Encoding
}{
	{"GBK", simplifiedchinese.GBK},
	{"GB18030", simplifiedchinese.GB18030},
	{"Big5", traditionalchinese.Big5},
	{"Shift_JIS", japanese.ShiftJIS},
	{"EUC-JP", japanese.EUCJP},
	{"EUC-KR", korean.EUCKR},
}

func isCJK(r rune) bool {
	return (r >= 0x4E00 && r <= 0x9FFF) ||
		(r >= 0x3400 && r <= 0x4DBF) ||
		(r >= 0x3000 && r <= 0x303F) ||
		(r >= 0xAC00 && r <= 0xD7AF) ||
		(r >= 0x3040 && r <= 0x309F) ||
		(r >= 0x30A0 && r <= 0x30FF)
}

func detectEncoding(data []byte) (encoding.Encoding, string) {
	var bestEnc encoding.Encoding
	var bestName string
	var bestScore float64

	for _, ce := range commonEncodings {
		reader := transform.NewReader(bytes.NewReader(data), ce.enc.NewDecoder())
		decoded, err := io.ReadAll(reader)
		if err != nil {
			continue
		}
		runes := []rune(string(decoded))
		if len(runes) == 0 {
			continue
		}
		good, cjk := 0, 0
		for _, r := range runes {
			if unicode.IsPrint(r) || unicode.IsSpace(r) {
				good++
			}
			if isCJK(r) {
				cjk++
			}
		}
		printRatio := float64(good) / float64(len(runes))
		cjkRatio := float64(cjk) / float64(len(runes))
		// Bonus for CJK content: when input is not valid UTF-8 but decodes to
		// lots of CJK, it's very likely a CJK encoding.
		score := printRatio * (1.0 + cjkRatio*2.0)
		if score > bestScore {
			bestScore = score
			bestEnc = ce.enc
			bestName = ce.name
		}
	}
	// Only return a match if it's reasonably good (≥80% printable)
	if bestScore >= 0.8 {
		return bestEnc, bestName
	}
	return nil, ""
}

func decodeCharset(data []byte, enc encoding.Encoding) ([]byte, error) {
	reader := transform.NewReader(bytes.NewReader(data), enc.NewDecoder())
	return io.ReadAll(reader)
}
