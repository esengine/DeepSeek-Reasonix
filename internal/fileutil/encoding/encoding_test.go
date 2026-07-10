package encoding

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"

	"golang.org/x/text/encoding/simplifiedchinese"
)

// --- Detect ---

func TestDetectUTF8Plain(t *testing.T) {
	enc, _ := Detect([]byte("hello world\n"))
	if enc != UTF8 {
		t.Errorf("got %v, want UTF8", enc)
	}
}

func TestDetectUTF8BOM(t *testing.T) {
	in := append([]byte{0xEF, 0xBB, 0xBF}, []byte("hello")...)
	enc, _ := Detect(in)
	if enc != UTF8BOM {
		t.Errorf("got %v, want UTF8BOM", enc)
	}
}

func TestDetectUTF16LE(t *testing.T) {
	var b bytes.Buffer
	b.Write([]byte{0xFF, 0xFE})
	for _, r := range utf16.Encode([]rune("hello")) {
		_ = binary.Write(&b, binary.LittleEndian, r)
	}
	enc, _ := Detect(b.Bytes())
	if enc != UTF16LE {
		t.Errorf("got %v, want UTF16LE", enc)
	}
}

func TestDetectUTF16BE(t *testing.T) {
	var b bytes.Buffer
	b.Write([]byte{0xFE, 0xFF})
	for _, r := range utf16.Encode([]rune("hello")) {
		_ = binary.Write(&b, binary.BigEndian, r)
	}
	enc, _ := Detect(b.Bytes())
	if enc != UTF16BE {
		t.Errorf("got %v, want UTF16BE", enc)
	}
}

func TestDetectGB18030(t *testing.T) {
	gb, err := simplifiedchinese.GB18030.NewEncoder().String("你好世界")
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	enc, _ := Detect([]byte(gb))
	if enc != GB18030 {
		t.Errorf("got %v, want GB18030", enc)
	}
}

func TestDetectEmpty(t *testing.T) {
	enc, _ := Detect(nil)
	if enc != UTF8 {
		t.Errorf("empty input: got %v, want UTF8", enc)
	}
}

// --- Decode ---

func TestDecodeUTF8(t *testing.T) {
	in := []byte("hello\n世界")
	out := Decode(in, UTF8)
	if string(out) != "hello\n世界" {
		t.Errorf("got %q", out)
	}
}

func TestReadFileUTF8DecodesGB18030(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, Encode(`{"label":"中文"}`, GB18030), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ReadFileUTF8(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"label":"中文"}` {
		t.Fatalf("ReadFileUTF8 = %q", got)
	}
}

func TestDecodeUTF8BOM(t *testing.T) {
	in := append([]byte{0xEF, 0xBB, 0xBF}, []byte("hello")...)
	out := Decode(in, UTF8BOM)
	if string(out) != "hello" {
		t.Errorf("got %q, want 'hello'", out)
	}
	if bytes.Contains(out, []byte{0xEF, 0xBB, 0xBF}) {
		t.Error("BOM leaked into decoded output")
	}
}

func TestDecodeUTF16LE(t *testing.T) {
	var b bytes.Buffer
	b.Write([]byte{0xFF, 0xFE})
	for _, r := range utf16.Encode([]rune("hello\nworld")) {
		_ = binary.Write(&b, binary.LittleEndian, r)
	}
	out := Decode(b.Bytes(), UTF16LE)
	if string(out) != "hello\nworld" {
		t.Errorf("got %q", out)
	}
}

func TestDecodeUTF16BE(t *testing.T) {
	var b bytes.Buffer
	b.Write([]byte{0xFE, 0xFF})
	for _, r := range utf16.Encode([]rune("hello\nworld")) {
		_ = binary.Write(&b, binary.BigEndian, r)
	}
	out := Decode(b.Bytes(), UTF16BE)
	if string(out) != "hello\nworld" {
		t.Errorf("got %q", out)
	}
}

func TestDecodeGB18030(t *testing.T) {
	gb, _ := simplifiedchinese.GB18030.NewEncoder().String("你好世界\n第二行")
	out := Decode([]byte(gb), GB18030)
	if string(out) != "你好世界\n第二行" {
		t.Errorf("got %q", out)
	}
}

func TestDecodeLossyUTF8(t *testing.T) {
	in := []byte{0xFF, 0xFE, 'a'}
	out := Decode(in, LossyUTF8)
	if !bytes.Equal(out, in) {
		t.Errorf("LossyUTF8 should pass through, got %q", out)
	}
}

// --- Encode ---

func TestEncodeUTF8(t *testing.T) {
	out := Encode("hello", UTF8)
	if string(out) != "hello" {
		t.Errorf("got %q", out)
	}
}

func TestEncodeUTF8BOM(t *testing.T) {
	out := Encode("hello", UTF8BOM)
	if !bytes.HasPrefix(out, []byte{0xEF, 0xBB, 0xBF}) {
		t.Error("missing UTF-8 BOM prefix")
	}
	if string(out[3:]) != "hello" {
		t.Errorf("body = %q", out[3:])
	}
}

func TestEncodeUTF16LE(t *testing.T) {
	out := Encode("hi", UTF16LE)
	if len(out) < 2 || out[0] != 0xFF || out[1] != 0xFE {
		t.Error("missing UTF-16LE BOM")
	}
	decoded := Decode(out, UTF16LE)
	if string(decoded) != "hi" {
		t.Errorf("round-trip failed: got %q", decoded)
	}
}

func TestEncodeUTF16BE(t *testing.T) {
	out := Encode("hi", UTF16BE)
	if len(out) < 2 || out[0] != 0xFE || out[1] != 0xFF {
		t.Error("missing UTF-16BE BOM")
	}
	decoded := Decode(out, UTF16BE)
	if string(decoded) != "hi" {
		t.Errorf("round-trip failed: got %q", decoded)
	}
}

func TestEncodeGB18030(t *testing.T) {
	out := Encode("你好", GB18030)
	dec, _ := simplifiedchinese.GB18030.NewDecoder().Bytes(out)
	if string(dec) != "你好" {
		t.Errorf("round-trip failed: got %q", dec)
	}
}

// --- Round-trip ---

func TestRoundTripGB18030(t *testing.T) {
	original := "你好世界\n第二行\n"
	gb, _ := simplifiedchinese.GB18030.NewEncoder().String(original)

	enc, _ := Detect([]byte(gb))
	decoded := string(Decode([]byte(gb), enc))
	if decoded != original {
		t.Fatalf("decode mismatch: %q", decoded)
	}

	edited := strings.Replace(decoded, "第二行", "新的行", 1)
	reencoded := Encode(edited, enc)
	redecoded := string(Decode(reencoded, enc))
	if redecoded != edited {
		t.Errorf("round-trip failed: got %q, want %q", redecoded, edited)
	}
}

func TestRoundTripUTF16LE(t *testing.T) {
	original := "hello\nworld\n"
	encoded := Encode(original, UTF16LE)
	enc, _ := Detect(encoded)
	decoded := string(Decode(encoded, enc))
	if decoded != original {
		t.Errorf("round-trip failed: got %q, want %q", decoded, original)
	}
}

func TestRoundTripUTF8BOM(t *testing.T) {
	original := "hello world\n"
	encoded := Encode(original, UTF8BOM)
	enc, _ := Detect(encoded)
	decoded := string(Decode(encoded, enc))
	if decoded != original {
		t.Errorf("round-trip failed: got %q, want %q", decoded, original)
	}
}

// --- BOM-less UTF-16 ---

func utf16NoBOM(t *testing.T, s string, order binary.ByteOrder) []byte {
	t.Helper()
	var b bytes.Buffer
	for _, r := range utf16.Encode([]rune(s)) {
		_ = binary.Write(&b, order, r)
	}
	return b.Bytes()
}

func TestDetectUTF16LENoBOM(t *testing.T) {
	in := utf16NoBOM(t, "// Created by 69431 on 2024/12/31\n#include \"x.h\"\n", binary.LittleEndian)
	enc, _ := Detect(in)
	if enc != UTF16LENoBOM {
		t.Errorf("got %v, want UTF16LENoBOM", enc)
	}
}

func TestDetectUTF16BENoBOM(t *testing.T) {
	in := utf16NoBOM(t, "package main\nfunc main() {}\n", binary.BigEndian)
	enc, _ := Detect(in)
	if enc != UTF16BENoBOM {
		t.Errorf("got %v, want UTF16BENoBOM", enc)
	}
}

func TestDetectPlainUTF8NotUTF16(t *testing.T) {
	enc, _ := Detect([]byte("the quick brown fox jumps over the lazy dog\n"))
	if enc != UTF8 {
		t.Errorf("ASCII text misdetected as %v", enc)
	}
}

func TestDetectBinaryNotUTF16NoBOM(t *testing.T) {
	// NULs on both parities — genuine binary must not look like BOM-less UTF-16.
	bin := []byte{0x00, 0x00, 0x01, 0x00, 0x00, 0x02, 0xFF, 0x00, 0x00, 0x03, 0x00, 0x00, 0x04, 0x00, 0x00, 0x05, 0x00, 0x00}
	if k, ok := DetectUTF16NoBOM(bin); ok {
		t.Errorf("binary misdetected as %v", k)
	}
}

func TestDecodeUTF16LENoBOM(t *testing.T) {
	in := utf16NoBOM(t, "hello\nworld", binary.LittleEndian)
	if out := string(Decode(in, UTF16LENoBOM)); out != "hello\nworld" {
		t.Errorf("got %q", out)
	}
}

func TestRoundTripUTF16LENoBOM(t *testing.T) {
	original := "// c++ source\nint main() { return 0; }\n"
	encoded := utf16NoBOM(t, original, binary.LittleEndian)

	enc, _ := Detect(encoded)
	if enc != UTF16LENoBOM {
		t.Fatalf("detect: got %v", enc)
	}
	decoded := string(Decode(encoded, enc))
	if decoded != original {
		t.Fatalf("decode mismatch: %q", decoded)
	}
	edited := strings.Replace(decoded, "return 0", "return 1", 1)
	reencoded := Encode(edited, enc)
	if bytes.HasPrefix(reencoded, []byte{0xFF, 0xFE}) || bytes.HasPrefix(reencoded, []byte{0xFE, 0xFF}) {
		t.Error("no-BOM re-encode leaked a BOM")
	}
	if redecoded := string(Decode(reencoded, enc)); redecoded != edited {
		t.Errorf("round-trip failed: got %q, want %q", redecoded, edited)
	}
}

// --- UTF-16 supplementary plane (surrogate pairs) ---

func TestSurrogatePairRoundTrip(t *testing.T) {
	// U+1F600 (😀) is in the supplementary plane and requires a surrogate pair.
	original := "hello 😀 world"
	encoded := Encode(original, UTF16LE)
	decoded := string(Decode(encoded, UTF16LE))
	if decoded != original {
		t.Errorf("surrogate pair round-trip failed: got %q, want %q", decoded, original)
	}
}
