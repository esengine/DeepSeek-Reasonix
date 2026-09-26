package encoding

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
)

// CP936 writes the euro as the single byte 0x80, which GB18030 cannot restore.
// Such a file is GBK: it keeps its bytes and an edit writes CP936 back.
func TestDetectGBKWithEuroByte(t *testing.T) {
	data := []byte{0xd6, 0xd0, 0xce, 0xc4, 0x80}
	enc, _ := Detect(data)
	if enc != GBK {
		t.Fatalf("got %v, want GBK", enc)
	}
	if got := string(Decode(data, enc)); got != "中文€" {
		t.Fatalf("Decode = %q, want %q", got, "中文€")
	}
	if out := MustEncode("中文€", enc); !bytes.Equal(out, data) {
		t.Fatalf("Encode = % x, want % x", out, data)
	}
}

// A fragment cut inside a character, with no newline to cut at instead, is
// still the charset it was written in; the split character is left out.
func TestDetectFragmentDropsTheSplitCharacter(t *testing.T) {
	gb, err := simplifiedchinese.GB18030.NewEncoder().String("x" + strings.Repeat("啊", 100))
	if err != nil {
		t.Fatal(err)
	}
	cut := []byte(gb)[:len(gb)-1]
	enc, whole := DetectFragment(cut)
	if enc != GB18030 {
		t.Fatalf("got %v, want GB18030", enc)
	}
	if len(whole) != len(cut)-1 {
		t.Fatalf("whole characters = %d bytes, want %d", len(whole), len(cut)-1)
	}
	utf := []byte("x" + strings.Repeat("啊", 100))
	if enc, whole := DetectFragment(utf[:len(utf)-1]); enc != UTF8 || len(whole) != len(utf)-3 {
		t.Fatalf("UTF-8 fragment = %v, %d bytes; want UTF8, %d", enc, len(whole), len(utf)-3)
	}
}

// Output that no charset restores, because a buffer cut split a code-page
// character, is still read in the code page.
func TestDecodeOutputReadsCutCodePageText(t *testing.T) {
	gb, _ := simplifiedchinese.GB18030.NewEncoder().String("'sh' 不是内部或外部命令")
	if got := string(DecodeOutput([]byte(gb)[:len(gb)-1], Cut{Tail: true})); !strings.HasPrefix(got, "'sh' 不是内部或外部命") {
		t.Fatalf("DecodeOutput = %q", got)
	}
}

// GBK cannot hold every character. Encode names the first one it cannot hold
// and where it sits, rather than writing the text as UTF-8.
func TestEncodeRefusesCharacterTheCharsetCannotHold(t *testing.T) {
	for _, r := range []string{"✅", "🚀", "𠀀", "™", "ᠠ"} {
		_, err := Encode("中文"+r, GBK)
		var ue *UnencodableError
		if !errors.Is(err, ErrUnencodable) || !errors.As(err, &ue) {
			t.Fatalf("Encode(%q, GBK) err = %v, want ErrUnencodable", r, err)
		}
		if string(ue.Rune) != r || ue.Offset != len("中文") || ue.Charset != "GBK" {
			t.Fatalf("UnencodableError = %+v, want %q at byte %d in GBK", ue, r, len("中文"))
		}
		if _, err := Encode("中文"+r, GB18030); err != nil {
			t.Fatalf("GB18030 holds every character, got %v for %q", err, r)
		}
	}
}

// Process output cut inside a UTF-8 character at either end is read as UTF-8,
// less the cut character, not reinterpreted as a code page.
func TestDecodeOutputTrimsCutUTF8Edges(t *testing.T) {
	full := []byte("参数格式不正确")
	if got := string(DecodeOutput(full[1:], Cut{Head: true})); got != "数格式不正确" {
		t.Fatalf("front cut = %q", got)
	}
	if got := string(DecodeOutput(full[:len(full)-1], Cut{Tail: true})); got != "参数格式不正" {
		t.Fatalf("back cut = %q", got)
	}
	if got := string(DecodeOutput(full[2:len(full)-2], Cut{Head: true, Tail: true})); got != "数格式不正" {
		t.Fatalf("both cut = %q", got)
	}
}

// An end no bound cut is where the output began or ended, so a code-page pair
// there is kept rather than trimmed as half of a UTF-8 character.
func TestDecodeOutputKeepsUncutCodePageEdges(t *testing.T) {
	cases := map[string]string{
		"\xbc\xdb":     "价",
		"\xb0\xa1\r\n": "啊\r\n",
		"ok \xe4\xa1":  "ok 洹",
		"\xb0\xa1 ok":  "啊 ok",
	}
	for raw, want := range cases {
		if got := string(DecodeOutput([]byte(raw), Cut{})); got != want {
			t.Fatalf("DecodeOutput(% x) = %q, want %q", raw, got, want)
		}
	}
}

// A process stopped mid-write leaves one incomplete UTF-8 character at an end
// nothing cut. Once a whole multi-byte UTF-8 character has appeared, that is
// the only invalid part, and the output is still UTF-8.
func TestDecodeOutputReadsUncutUTF8EndingInPartialRune(t *testing.T) {
	const text = "编译成功…正在运行"
	half := []byte("中")[:2]
	if got := string(DecodeOutput(append([]byte(text), half...), Cut{})); got != text {
		t.Fatalf("UTF-8 plus half a rune = %q, want %q", got, text)
	}
	// Nothing multi-byte came before the half rune, so no structure says UTF-8;
	// the bytes still form a GBK pair and are read as one.
	if got := string(DecodeOutput(append([]byte("ok "), half...), Cut{})); got != "ok 涓" {
		t.Fatalf("ASCII plus half a rune = %q", got)
	}
	gb, _ := simplifiedchinese.GB18030.NewEncoder().String("参数格式不正确")
	for _, tc := range []struct {
		name string
		raw  string
		cut  Cut
		want string
	}{
		{"uncut GBK", gb, Cut{}, "参数格式不正确"},
		{"GBK cut at the end", gb[:len(gb)-1], Cut{Tail: true}, "参数格式不正"},
		{"GBK cut at the start", gb[2:], Cut{Head: true}, "数格式不正确"},
	} {
		got := string(DecodeOutput([]byte(tc.raw), tc.cut))
		if got != tc.want && !(tc.cut.Tail && strings.HasPrefix(got, tc.want)) {
			t.Fatalf("%s = %q, want %q", tc.name, got, tc.want)
		}
	}
}
