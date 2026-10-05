package hook

import (
	"strings"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
)

// cmd.exe stderr cut short inside a character is still code-page text.
func TestDecodeHookOutputReadsTruncatedCodePageText(t *testing.T) {
	gb, _ := simplifiedchinese.GB18030.NewEncoder().String("'sh' 不是内部或外部命令，也不是可运行的程序")
	raw := []byte(gb)[:len(gb)-1]
	if got := decodeHookOutput(raw, true); !strings.HasPrefix(got, "'sh' 不是内部或外部命令") {
		t.Fatalf("decoded hook stderr = %q", got)
	}
}

// CP936 output carrying its single-byte euro is read as CP936, not passed
// through as raw bytes.
func TestDecodeHookOutputReadsCP936Euro(t *testing.T) {
	want := "'sh' 不是内部或外部命令 €"
	raw, err := simplifiedchinese.GBK.NewEncoder().String(want)
	if err != nil {
		t.Fatal(err)
	}
	if got := decodeHookOutput([]byte(raw), false); got != want {
		t.Fatalf("decoded hook stderr = %q, want %q", got, want)
	}
}

// Output that ends where the hook stopped writing, not where a bound cut it,
// keeps a code-page character at either edge.
func TestDecodeHookOutputKeepsUncutCodePageEdges(t *testing.T) {
	cases := map[string]string{
		"\xbc\xdb":     "价",
		"\xb0\xa1\r\n": "啊",
		"ok \xe4\xa1":  "ok 洹",
		"\xb0\xa1 ok":  "啊 ok",
	}
	for raw, want := range cases {
		if got := decodeHookOutput([]byte(raw), false); got != want {
			t.Fatalf("decodeHookOutput(% x) = %q, want %q", raw, got, want)
		}
	}
}
