package encoding

import (
	"bytes"
	"errors"
	"fmt"
	"unicode/utf8"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

// charsets are the legacy encodings a file may be stored in, in preference
// order. GBK comes second because it differs from GB18030 only where GB18030
// cannot restore the bytes, such as CP936's single-byte euro, 0x80.
var charsets = []struct {
	kind Kind
	name string
	enc  encoding.Encoding
}{
	{GB18030, "GB18030", simplifiedchinese.GB18030},
	{GBK, "GBK", simplifiedchinese.GBK},
}

func charsetOf(k Kind) encoding.Encoding {
	for _, c := range charsets {
		if c.kind == k {
			return c.enc
		}
	}
	return nil
}

// ErrUnencodable is the identity of a write holding a character the file's
// encoding cannot represent.
var ErrUnencodable = errors.New("character not representable in the file's encoding")

// UnencodableError names the first character a legacy charset cannot represent.
type UnencodableError struct {
	Charset string
	Rune    rune
	Offset  int // byte offset of Rune in the text being written
}

func (e *UnencodableError) Error() string {
	return fmt.Sprintf("%q (U+%04X) at byte %d cannot be written in %s, the file's encoding", e.Rune, e.Rune, e.Offset, e.Charset)
}

func (e *UnencodableError) Unwrap() error { return ErrUnencodable }

func encodeCharset(text string, k Kind) ([]byte, error) {
	for _, c := range charsets {
		if c.kind != k {
			continue
		}
		out, n, err := transform.Bytes(c.enc.NewEncoder(), []byte(text))
		if err != nil {
			r, _ := utf8.DecodeRuneInString(text[n:])
			return nil, &UnencodableError{Charset: c.name, Rune: r, Offset: n}
		}
		return out, nil
	}
	return []byte(text), nil
}

// DetectFragment is Detect for data that is only the start of a longer stream:
// a character the cut split is dropped rather than read as proof against the
// encoding. It returns the prefix that holds whole characters.
func DetectFragment(data []byte) (Kind, []byte) {
	k, n, _ := sniff(data, false)
	return k, data[:n]
}

// DetectAndDecode is Detect followed by Decode, decoding a legacy charset once.
func DetectAndDecode(data []byte) (Kind, []byte) {
	k, _, text := sniff(data, true)
	if text != nil {
		return k, text
	}
	return k, Decode(data, k)
}

// Cut says which ends of a bounded buffer lost bytes to its bound. Only a cut
// end can hold part of a character; an uncut end is where the output began or
// ended, so a byte there belongs to it, such as half of a GBK pair.
type Cut struct {
	Head bool // bytes before the buffer were dropped
	Tail bool // bytes after the buffer were dropped
}

// DecodeOutput reads bytes that are only displayed, never written back, such as
// a process's output. UTF-8 is read without a character split at an end cut
// says was cut; what no charset restores is still read as GB18030, since a cut
// can split a code-page character anywhere.
func DecodeOutput(data []byte, cut Cut) []byte {
	edges := cut.trim(data)
	if utf8.Valid(edges) {
		return edges
	}
	// A process stopped mid-write ends inside a character no bound cut. That is
	// still UTF-8 when it is the only invalid part and a multi-byte UTF-8
	// character already appeared; ASCII alone proves nothing.
	if whole := TrimPartialRune(edges); len(whole) < len(edges) && utf8.Valid(whole) && utf8.RuneCount(whole) < len(whole) {
		return whole
	}
	k, _, text := sniff(data, true)
	if text != nil {
		return text
	}
	if k == LossyUTF8 {
		k = GB18030
	}
	return Decode(data, k)
}

// sniff detects data's encoding. When final is false data is a fragment, and n
// excludes a trailing sequence it cut short. text is the decoded data[:n] when
// a legacy charset won, so the caller need not decode it again.
func sniff(data []byte, final bool) (k Kind, n int, text []byte) {
	switch {
	case len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF:
		return UTF8BOM, len(data), nil
	case len(data) >= 2 && data[0] == 0xFF && data[1] == 0xFE:
		return UTF16LE, len(data), nil
	case len(data) >= 2 && data[0] == 0xFE && data[1] == 0xFF:
		return UTF16BE, len(data), nil
	}
	// BOM-less UTF-16 must be tried before utf8.Valid: its low bytes plus 0x00
	// high bytes are all valid UTF-8 code units.
	if k, ok := DetectUTF16NoBOM(data); ok {
		return k, len(data), nil
	}
	whole := data
	if !final {
		whole = TrimPartialRune(data)
	}
	if utf8.Valid(whole) {
		return UTF8, len(whole), nil
	}
	for _, c := range charsets {
		if text, n, ok := roundTrip(c.enc, data, final); ok {
			return c.kind, n, text
		}
	}
	return LossyUTF8, len(data), nil
}

// roundTrip decodes data and reports whether encoding the text restores it byte
// for byte. The decoders never fail: they turn an invalid sequence into U+FFFD,
// which encodes back as different bytes, so decoding alone is no signal.
func roundTrip(e encoding.Encoding, data []byte, final bool) ([]byte, int, bool) {
	var text []byte
	n := len(data)
	if final {
		var err error
		if text, _, err = transform.Bytes(e.NewDecoder(), data); err != nil {
			return nil, 0, false
		}
	} else {
		// Not at EOF, the decoder stops before an incomplete trailing sequence
		// with ErrShortSrc; nSrc is then the prefix of whole characters. One
		// source byte decodes to at most three.
		dst := make([]byte, 3*len(data)+utf8.UTFMax)
		nDst, nSrc, err := e.NewDecoder().Transform(dst, data, false)
		if err != nil && !errors.Is(err, transform.ErrShortSrc) {
			return nil, 0, false
		}
		text, n = dst[:nDst], nSrc
	}
	back, _, err := transform.Bytes(e.NewEncoder(), text)
	if err != nil || !bytes.Equal(back, data[:n]) {
		return nil, 0, false
	}
	return text, n, true
}

// trim drops the continuation bytes a head cut left at the front and the
// sequence a tail cut left incomplete at the back.
func (c Cut) trim(data []byte) []byte {
	for i := 0; c.Head && i < utf8.UTFMax-1 && len(data) > 0 && !utf8.RuneStart(data[0]); i++ {
		data = data[1:]
	}
	if c.Tail {
		data = TrimPartialRune(data)
	}
	return data
}
