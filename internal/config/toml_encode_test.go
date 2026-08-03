package config

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestEncodeTOMLStringBasics(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", `""`},
		{"plain", `"plain"`},
		{`back\slash`, `"back\\slash"`},
		{`say "hi"`, `"say \"hi\""`},
		{"tab\there", `"tab\there"`},
		{"line\nbreak", `"line\nbreak"`},
		{"carriage\rreturn", `"carriage\rreturn"`},
		{"form\ffeed", `"form\ffeed"`},
		{"back\bspace", `"back\bspace"`},
		{"bell\x07", `"bell\u0007"`},
		{"vertical\x0b", `"vertical\u000B"`},
		{"del\x7f", `"del\u007F"`},
		// Unicode preserved verbatim (never \uXXXX-escaped like QuoteToASCII).
		{`D:\开发\项目`, `"D:\\开发\\项目"`},
		{"emoji 🔥 done", `"emoji 🔥 done"`},
	}
	for _, tc := range cases {
		got, err := encodeTOMLString(tc.in)
		if err != nil {
			t.Errorf("encode(%q): %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("encode(%q) = %s, want %s", tc.in, got, tc.want)
		}
	}
}

func TestEncodeTOMLStringRoundTrips(t *testing.T) {
	values := []string{
		`D:\开发\项目`,
		`\\server\share\dir\file.txt`,
		`C:\new\tool`,
		"multi\nline\twith\ttabs",
		`quotes " and \ backslashes`,
		"控制字符\x00\x1f\u007f混合",
		"中文、emoji 🚀、spaces",
		"trailing backslash\\",
		"double quote at end\"",
	}
	for _, v := range values {
		encoded, err := encodeTOMLString(v)
		if err != nil {
			t.Errorf("encode(%q): %v", v, err)
			continue
		}
		var got struct {
			V string `toml:"v"`
		}
		if _, err := decodeTOMLBytes([]byte("v = "+encoded), &got); err != nil {
			t.Errorf("encoded %q does not parse: %v", encoded, err)
			continue
		}
		if got.V != v {
			t.Errorf("round trip %q -> %q -> %q", v, encoded, got.V)
		}
	}
}

func TestEncodeTOMLStringRejectsInvalidUTF8(t *testing.T) {
	invalid := string([]byte{'D', ':', '\\', 0xff, 0xfe, '\\', 'x'})
	if utf8.ValidString(invalid) {
		t.Fatal("test fixture must be invalid UTF-8")
	}
	if _, err := encodeTOMLString(invalid); err == nil {
		t.Fatal("encodeTOMLString accepted invalid UTF-8")
	}
}

func TestEncodeTOMLKeyPart(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"plain", "plain"},
		{"with-dash", "with-dash"},
		{"with_underscore", "with_underscore"},
		{"with space", `"with space"`},
		{"c++", `"c++"`},
		{"github:gh-fix-ci", `"github:gh-fix-ci"`},
		{"1starting-digit", "1starting-digit"},
		{"中文键", `"中文键"`},
		{"", `""`},
	}
	for _, tc := range cases {
		got, err := encodeTOMLKeyPart(tc.in)
		if err != nil {
			t.Errorf("key(%q): %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("key(%q) = %s, want %s", tc.in, got, tc.want)
		}
	}
}

func TestEncodeTOMLStringNeverEmitsGoOnlyEscapes(t *testing.T) {
	// strconv.Quote emits \a, \v and \xNN for control characters; TOML 1.0
	// rejects those escapes. The encoder must never produce them.
	for b := byte(0); b < 0x80; b++ {
		encoded, err := encodeTOMLString(string([]byte{b}))
		if err != nil {
			continue
		}
		if strings.Contains(encoded, `\a`) || strings.Contains(encoded, `\v`) || strings.Contains(encoded, `\x`) {
			t.Errorf("byte 0x%02x encoded with Go-only escape: %s", b, encoded)
		}
	}
}

// FuzzEncodeTOMLString ensures every encodable string round-trips through the
// production TOML parser without errors, and invalid UTF-8 is rejected.
func FuzzEncodeTOMLString(f *testing.F) {
	for _, tc := range [][]byte{
		[]byte(`D:\开发\项目`),
		[]byte("plain"),
		[]byte("tab\tnewline\nend"),
		[]byte("\x00\x1f\x7f"),
		[]byte("emoji 🎉"),
		[]byte("hello"),
		[]byte{0xff, 0xfe},
		[]byte(`C:\Users\name\file`),
	} {
		f.Add(tc)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		encoded, err := encodeTOMLString(string(data))
		if err != nil {
			if utf8.Valid(data) {
				t.Fatalf("valid UTF-8 rejected: %q", data)
			}
			return
		}
		if !utf8.Valid(data) {
			t.Fatalf("invalid UTF-8 accepted: %q -> %q", data, encoded)
		}
		var got struct {
			V string `toml:"v"`
		}
		if _, err := decodeTOMLBytes([]byte("v = "+encoded), &got); err != nil {
			t.Fatalf("encoded %q does not parse: %v", encoded, err)
		}
		if got.V != string(data) {
			t.Fatalf("round trip mismatch: %q -> %q -> %q", data, encoded, got.V)
		}
	})
}
