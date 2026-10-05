package encoding

import (
	"testing"
	"unicode/utf8"
)

func TestTrimPartialRune(t *testing.T) {
	full := []byte("包") // 3 bytes
	cases := []struct {
		name string
		in   []byte
		want int // wanted length
	}{
		{"complete text is untouched", []byte("ab包"), 5},
		{"one byte of a three-byte rune", append([]byte("ab"), full[0]), 2},
		{"two bytes of a three-byte rune", append([]byte("ab"), full[:2]...), 2},
		{"ascii only", []byte("abc"), 3},
		{"empty", nil, 0},
		// A byte that is invalid rather than truncated stays, so the file it
		// came from still fails the validity check it should fail.
		{"an invalid leading byte is not truncation", []byte{'a', 0xFF}, 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := TrimPartialRune(c.in)
			if len(got) != c.want {
				t.Fatalf("len = %d, want %d (% x)", len(got), c.want, got)
			}
			if c.want > 0 && c.name != "an invalid leading byte is not truncation" && !utf8.Valid(got) {
				t.Fatalf("trimmed peek is still not valid UTF-8: % x", got)
			}
		})
	}
}
