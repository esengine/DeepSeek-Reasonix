// Package encoding detects and converts file encodings for the built-in
// file tools. The detection cascade (BOM → strict UTF-8 → GB18030 → lossy
// UTF-8) mirrors v1's file-encoding.ts and keeps CJK Windows files editable
// without silently mangling their bytes.
package encoding

import (
	"bytes"
	"encoding/binary"
	"os"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

// Kind identifies a detected file encoding.
type Kind int

const (
	// UTF8 is plain UTF-8 without a BOM — the common case.
	UTF8 Kind = iota
	// UTF8BOM is UTF-8 with a leading BOM (EF BB BF).
	UTF8BOM
	// UTF16LE is UTF-16 Little-Endian with a BOM (FF FE).
	UTF16LE
	// UTF16BE is UTF-16 Big-Endian with a BOM (FE FF).
	UTF16BE
	// GB18030 is the Chinese national standard charset (superset of GBK).
	GB18030
	// LossyUTF8 is not valid UTF-8 and not valid GB18030 — decoded lossily
	// as UTF-8 with replacement characters so the model sees something.
	LossyUTF8
	// UTF16LENoBOM is UTF-16 Little-Endian without a BOM — common for source
	// files saved by Windows tools. Detected heuristically from the NUL-byte
	// pattern; written back without a BOM to preserve the original bytes.
	UTF16LENoBOM
	// UTF16BENoBOM is UTF-16 Big-Endian without a BOM.
	UTF16BENoBOM
)

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// Detect returns the encoding kind for the given raw file bytes. The same
// bytes should then be passed to Decode for conversion to UTF-8.
func Detect(data []byte) (Kind, []byte) {
	switch {
	case len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF:
		return UTF8BOM, data
	case len(data) >= 2 && data[0] == 0xFF && data[1] == 0xFE:
		return UTF16LE, data
	case len(data) >= 2 && data[0] == 0xFE && data[1] == 0xFF:
		return UTF16BE, data
	}
	// BOM-less UTF-16 must be tried before utf8.Valid: its low bytes plus 0x00
	// high bytes are all valid UTF-8 code units, so a naive check would tag a
	// UTF-16 source file as UTF-8 and surface the embedded NULs as garbage.
	if k, ok := DetectUTF16NoBOM(data); ok {
		return k, data
	}
	if utf8.Valid(data) {
		return UTF8, data
	}
	// Try GB18030 — it is a strict superset of GBK and rejects truly
	// invalid byte sequences, so a successful decode is a reliable signal.
	dec := simplifiedchinese.GB18030.NewDecoder()
	if _, _, err := transform.Bytes(dec, data); err == nil {
		return GB18030, data
	}
	return LossyUTF8, data
}

// DetectQuick checks only for BOM prefixes in the first few bytes. This is
// the fast path for peek-based binary rejection: BOM-prefixed files (UTF-16,
// UTF-8 BOM) skip the NUL-byte check since 0x00 is normal in UTF-16. Returns
// UTF8 for non-BOM content (the caller should fall through to full Detect
// after verifying no NUL bytes).
func DetectQuick(peek []byte) Kind {
	switch {
	case len(peek) >= 3 && peek[0] == 0xEF && peek[1] == 0xBB && peek[2] == 0xBF:
		return UTF8BOM
	case len(peek) >= 2 && peek[0] == 0xFF && peek[1] == 0xFE:
		return UTF16LE
	case len(peek) >= 2 && peek[0] == 0xFE && peek[1] == 0xFF:
		return UTF16BE
	}
	return UTF8
}

// DetectUTF16NoBOM heuristically recognises BOM-less UTF-16 from the NUL-byte
// distribution: ASCII-range text encodes one byte of payload and one 0x00 per
// code unit, so the NULs cluster on odd offsets (LE) or even offsets (BE). It
// requires a strong skew — one parity heavily NUL, the other almost none — so
// genuine binary (NULs on both parities) and plain UTF-8 (no NULs) fall through.
func DetectUTF16NoBOM(b []byte) (Kind, bool) {
	n := len(b)
	if n < 16 {
		return UTF8, false
	}
	n &^= 1 // examine an even-length window so parity counts are comparable
	var evenNUL, oddNUL int
	for i := 0; i < n; i++ {
		if b[i] != 0 {
			continue
		}
		if i%2 == 0 {
			evenNUL++
		} else {
			oddNUL++
		}
	}
	half := n / 2
	switch {
	case oddNUL*10 >= half*3 && evenNUL*20 <= half:
		return UTF16LENoBOM, true
	case evenNUL*10 >= half*3 && oddNUL*20 <= half:
		return UTF16BENoBOM, true
	}
	return UTF8, false
}

// Decode converts data from the given encoding to UTF-8 bytes.
func Decode(data []byte, enc Kind) []byte {
	switch enc {
	case UTF8BOM:
		return data[3:]
	case UTF16LE:
		return decodeUTF16(data[2:], binary.LittleEndian)
	case UTF16BE:
		return decodeUTF16(data[2:], binary.BigEndian)
	case UTF16LENoBOM:
		return decodeUTF16(data, binary.LittleEndian)
	case UTF16BENoBOM:
		return decodeUTF16(data, binary.BigEndian)
	case GB18030:
		out, _, err := transform.Bytes(simplifiedchinese.GB18030.NewDecoder(), data)
		if err != nil {
			return data // should not happen after Detect, but be safe
		}
		return out
	}
	// UTF8 and LossyUTF8 both pass through — LossyUTF8 is already
	// "best effort" and Go strings can hold arbitrary bytes.
	return data
}

// DecodeToUTF8 converts raw text-like file bytes to UTF-8 using Reasonix's
// shared detection cascade. It is intended for user-editable structured files
// (TOML, JSON, dotenv, Markdown) before handing the content to strict parsers.
func DecodeToUTF8(data []byte) []byte {
	enc, raw := Detect(data)
	return Decode(raw, enc)
}

// ReadFileUTF8 reads path and decodes text-like content to UTF-8.
func ReadFileUTF8(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return DecodeToUTF8(data), nil
}

// Decoder returns a streaming transform.Transformer for the given encoding,
// suitable for wrapping an io.Reader via dec.Reader(r). Returns nil for UTF-8
// and LossyUTF8 (no transformation needed — the caller should read directly).
func Decoder(enc Kind) transform.Transformer {
	switch enc {
	case UTF8BOM:
		// UTF-8 BOM just needs the 3-byte prefix stripped; the content is
		// already valid UTF-8. Callers handle BOM stripping via Decode.
		return nil
	case GB18030:
		return simplifiedchinese.GB18030.NewDecoder()
	}
	// UTF16LE/BE are not self-synchronising and cannot be streamed
	// line-by-line without full-file buffering. Callers must handle
	// them separately. UTF8 and LossyUTF8 need no transformation.
	return nil
}

// Encode converts a UTF-8 string back to the given file encoding.
// UTF8 and LossyUTF8 produce plain UTF-8 bytes.
func Encode(text string, enc Kind) []byte {
	switch enc {
	case UTF8BOM:
		return append(utf8BOM, []byte(text)...)
	case UTF16LE:
		return encodeUTF16(text, binary.LittleEndian, true)
	case UTF16BE:
		return encodeUTF16(text, binary.BigEndian, true)
	case UTF16LENoBOM:
		return encodeUTF16(text, binary.LittleEndian, false)
	case UTF16BENoBOM:
		return encodeUTF16(text, binary.BigEndian, false)
	case GB18030:
		out, _, err := transform.Bytes(simplifiedchinese.GB18030.NewEncoder(), []byte(text))
		if err != nil {
			return []byte(text)
		}
		return out
	}
	return []byte(text)
}

// decodeUTF16 converts UTF-16 bytes (BOM already stripped) to UTF-8.
func decodeUTF16(b []byte, order binary.ByteOrder) []byte {
	u := make([]uint16, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		u = append(u, order.Uint16(b[i:i+2]))
	}
	return []byte(string(utf16Decode(u)))
}

// encodeUTF16 converts a UTF-8 string to UTF-16 bytes, with a BOM when withBOM.
func encodeUTF16(text string, order binary.ByteOrder, withBOM bool) []byte {
	runes := []rune(text)
	encoded := utf16Encode(runes)

	var buf bytes.Buffer
	if withBOM {
		var bom [2]byte
		if order == binary.LittleEndian {
			bom[0], bom[1] = 0xFF, 0xFE
		} else {
			bom[0], bom[1] = 0xFE, 0xFF
		}
		buf.Write(bom[:])
	}
	for _, u := range encoded {
		var b [2]byte
		order.PutUint16(b[:], u)
		buf.Write(b[:])
	}
	return buf.Bytes()
}

// utf16Decode converts UTF-16 code units to runes, handling surrogate pairs.
func utf16Decode(u []uint16) []rune {
	var out []rune
	for i := 0; i < len(u); i++ {
		c := u[i]
		if c >= 0xD800 && c <= 0xDBFF && i+1 < len(u) {
			c2 := u[i+1]
			if c2 >= 0xDC00 && c2 <= 0xDFFF {
				out = append(out, rune(c-0xD800)<<10|rune(c2-0xDC00)+0x10000)
				i++
				continue
			}
		}
		out = append(out, rune(c))
	}
	return out
}

// utf16Encode converts runes to UTF-16 code units, producing surrogates for
// supplementary plane characters.
func utf16Encode(runes []rune) []uint16 {
	var out []uint16
	for _, r := range runes {
		if r >= 0x10000 && r <= 0x10FFFF {
			r -= 0x10000
			out = append(out, uint16(0xD800+(r>>10)), uint16(0xDC00+(r&0x3FF)))
		} else {
			out = append(out, uint16(r))
		}
	}
	return out
}
