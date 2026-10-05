package plugin

import (
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
)

func gbk(t *testing.T, s string) []byte {
	t.Helper()
	b, err := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// A GBK lead byte is not a UTF-8 rune start, so a tail that begins with a CJK
// character must be decoded whole; only a tail the limit really cut may lose
// part of one.
func TestStderrTailDecodesTheServersCodePage(t *testing.T) {
	const line = "参数格式不正确，服务器无法启动"
	whole := gbk(t, line)
	for _, tc := range []struct {
		name  string
		limit int
		write [][]byte
		want  string
	}{
		{"uncut GBK starting with CJK", 16 * 1024, [][]byte{whole}, line},
		{"cut GBK starting with CJK", len(whole), [][]byte{gbk(t, "旧输出"), whole}, line},
		{"UTF-8 cut mid-character", len(line) - 1, [][]byte{[]byte(line)}, line[len("参"):]},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := &tailBuffer{limit: tc.limit}
			for _, p := range tc.write {
				_, _ = b.Write(p)
			}
			if got := b.String(); got != tc.want {
				t.Fatalf("String() = %q, want %q", got, tc.want)
			}
		})
	}
}
